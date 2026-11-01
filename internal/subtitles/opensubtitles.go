// Package subtitles searches for and downloads subtitles
// ("Original, using existing subtitle-provider APIs (OpenSubtitles,
// etc.)"). Implements the OpenSubtitles REST API v1
// (https://opensubtitles.stoplight.io/docs/opensubtitles-api).
package subtitles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

const defaultBaseURL = "https://api.opensubtitles.com/api/v1"

// ErrNoAPIKey mirrors internal/metadata's ErrNoAPIKey — same "surface a
// clear setup prompt" reasoning.
var ErrNoAPIKey = fmt.Errorf("no OpenSubtitles API key has been added yet")

// ErrQuota is returned when OpenSubtitles refuses a download because the
// account's daily quota (or rate limit) is used up; callers should stop
// asking for the rest of the day rather than retry.
var ErrQuota = fmt.Errorf("the OpenSubtitles download limit has been reached")

// ErrInvalidKey is returned when OpenSubtitles rejects the API key.
var ErrInvalidKey = fmt.Errorf("OpenSubtitles rejected the API key")

// ErrLoginFailed is returned when the OpenSubtitles.com username or password
// is wrong.
var ErrLoginFailed = fmt.Errorf("OpenSubtitles login failed: check the username and password")

// userAgent identifies Mediarium to OpenSubtitles, which requires every API
// client to send one.
const userAgent = "Mediarium v1"

// tokenLifetime is how long a login token is reused (OpenSubtitles issues
// tokens valid for 24 hours).
const tokenLifetime = 23 * time.Hour

// Client talks to OpenSubtitles. Searching needs only the app API key.
// Downloading works anonymously within a small daily limit per IP address;
// giving the client an OpenSubtitles.com account (SetCredentials) logs in
// and uses that account's higher quota.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client

	mu          sync.Mutex
	username    string
	password    string
	token       string
	tokenExpiry time.Time
}

func New(apiKey string) *Client {
	// Connections go through the address check, and a redirect to another
	// host does not carry the API key header along.
	hc := netguard.Client(15 * time.Second)
	hc.Transport = newPacedTransport(netguard.Transport(), apiRequestInterval)
	return &Client{apiKey: apiKey, baseURL: defaultBaseURL, httpClient: hc}
}

// NewWithBaseURL is used by tests to point at a local fixture server
// instead of the real OpenSubtitles API (tests use local fixtures, not
// live network calls) — same pattern as internal/metadata.
func NewWithBaseURL(apiKey, baseURL string) *Client {
	c := New(apiKey)
	c.baseURL = baseURL
	return c
}

func (c *Client) HasAPIKey() bool { return c.apiKey != "" }

// WrapTransport wraps the HTTP transport this client uses, e.g. to count
// requests. Call it before the client is shared between goroutines.
func (c *Client) WrapTransport(wrap func(http.RoundTripper) http.RoundTripper) {
	rt := c.httpClient.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	c.httpClient.Transport = wrap(rt)
}

// SetCredentials sets (or, with empty strings, clears) the optional
// OpenSubtitles.com account used for downloads.
func (c *Client) SetCredentials(username, password string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.username, c.password = username, password
	c.token, c.tokenExpiry = "", time.Time{}
}

// HasCredentials reports whether an account is configured.
func (c *Client) HasCredentials() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.username != "" && c.password != ""
}

// WithKey returns a separate client with a different API key and no
// account, pointed at the same server — used to test a key before saving it.
func (c *Client) WithKey(apiKey string) *Client {
	n := New(apiKey)
	n.baseURL = c.baseURL
	return n
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// Login exchanges the configured account for a token (POST /login).
func (c *Client) Login(ctx context.Context) error {
	c.mu.Lock()
	user, pass := c.username, c.password
	c.mu.Unlock()
	if user == "" || pass == "" {
		return fmt.Errorf("no OpenSubtitles account has been added yet")
	}
	payload, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, err := c.newRequest(ctx, http.MethodPost, "/login", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("build login request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("log in to OpenSubtitles: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusBadRequest:
		return ErrLoginFailed
	case http.StatusForbidden:
		return ErrInvalidKey
	default:
		return fmt.Errorf("log in to OpenSubtitles: unexpected status %d", resp.StatusCode)
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponse)).Decode(&parsed); err != nil || parsed.Token == "" {
		return fmt.Errorf("log in to OpenSubtitles: no token in the response")
	}
	c.mu.Lock()
	c.token, c.tokenExpiry = parsed.Token, time.Now().Add(tokenLifetime)
	c.mu.Unlock()
	return nil
}

// bearer returns a valid login token, logging in if needed. It returns ""
// when no account is configured (anonymous use).
func (c *Client) bearer(ctx context.Context) (string, error) {
	c.mu.Lock()
	tok, exp, has := c.token, c.tokenExpiry, c.username != "" && c.password != ""
	c.mu.Unlock()
	if !has {
		return "", nil
	}
	if tok != "" && time.Now().Before(exp) {
		return tok, nil
	}
	if err := c.Login(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token, nil
}

// TestConnection checks the API key with a tiny search and, when an account
// is configured, that the login works. It returns a short description of
// what is set up.
func (c *Client) TestConnection(ctx context.Context) (string, error) {
	if _, err := c.search(ctx, url.Values{"query": {"the matrix"}, "languages": {"en"}}); err != nil {
		return "", err
	}
	if c.HasCredentials() {
		if err := c.Login(ctx); err != nil {
			return "", err
		}
		return "The API key works and you're signed in.", nil
	}
	return "The API key works. Without an OpenSubtitles.com account you get only a few subtitles a day.", nil
}

// Result is one subtitle search hit, flattened from OpenSubtitles'
// nested attributes/files JSON shape into what callers actually need.
type Result struct {
	FileID       int
	Language     string
	Release      string // release name the subtitle was synced to, e.g. "Movie.2024.1080p.BluRay-GROUP"
	DownloadsAll int
	Rating       float64
	HashMatch    bool // timed against the exact video file searched with (Query.MovieHash)

	// HearingImpaired: the subtitle also describes sounds (SDH). ForeignPartsOnly:
	// it holds only the lines in a foreign language (a "forced" track). Where a
	// source doesn't say, these are read from the release's name.
	HearingImpaired  bool
	ForeignPartsOnly bool

	Source     string   // where it comes from: SourceOpenSubtitles (empty) or SourceSubDL
	SourceName string   // an add-on's own name, for results from one
	Link       string   // SubDL's download path; OpenSubtitles uses FileID
	Releases   []string // other releases the subtitle is said to fit (SubDL)
}

type searchResponse struct {
	TotalPages int `json:"total_pages"`
	Data       []struct {
		Attributes struct {
			Language      string  `json:"language"`
			Release       string  `json:"release"`
			DownloadCount int     `json:"download_count"`
			Ratings       float64 `json:"ratings"`
			HashMatch     bool    `json:"moviehash_match"`
			// OpenSubtitles marks both on every result.
			HearingImpaired  bool `json:"hearing_impaired"`
			ForeignPartsOnly bool `json:"foreign_parts_only"`
			Files            []struct {
				FileID int `json:"file_id"`
			} `json:"files"`
		} `json:"attributes"`
	} `json:"data"`
}

// Query describes what to find subtitles for. Prefer the TMDB ids when known:
// they identify the title exactly, where a text query can match a remake.
type Query struct {
	Text         string
	TMDBID       int    // a movie's TMDB id
	ParentTMDBID int    // a series' TMDB id, for episodes
	Season       int    // episodes only
	Episode      int    // episodes only
	Type         string // "movie" or "episode"; empty = any
	Language     string // e.g. "en", "pt-BR"
	MovieHash    string // the video's FileHash: results made for that exact file come back marked
	Filename     string // the video's file name, for sources that match by it
	FileSize     int64  // the video's size in bytes

	// What a source can be asked with when it doesn't know the TMDB id: the
	// IMDb id (of the series, for an episode), the title and the year.
	IMDBID string
	Title  string
	Year   int

	// Which kind of subtitle is wanted (see Profile.Apply). The zero values ask
	// for what searches always asked for: anything.
	Forced bool     // only a forced track (foreign parts only)
	Full   FullMode // for a full subtitle: FullNormal or FullSDH narrow the search
}

// Find searches for subtitles matching q. OpenSubtitles doesn't know the TMDB
// id of every title, so when that finds nothing the IMDb id and then the
// title are tried, and the first lookup that finds something wins.
func (c *Client) Find(ctx context.Context, q Query) ([]Result, error) {
	type lookup struct {
		kind, value string
		v           url.Values
	}
	var lookups []lookup
	ids := url.Values{}
	if q.TMDBID != 0 {
		ids.Set("tmdb_id", strconv.Itoa(q.TMDBID))
	}
	if q.ParentTMDBID != 0 {
		ids.Set("parent_tmdb_id", strconv.Itoa(q.ParentTMDBID))
	}
	if len(ids) > 0 {
		id := q.TMDBID
		if id == 0 {
			id = q.ParentTMDBID
		}
		lookups = append(lookups, lookup{"tmdb", strconv.Itoa(id), ids})
	}
	if q.IMDBID != "" {
		v := url.Values{}
		if q.ParentTMDBID != 0 {
			v.Set("parent_imdb_id", q.IMDBID)
		} else {
			v.Set("imdb_id", q.IMDBID)
		}
		lookups = append(lookups, lookup{"imdb", q.IMDBID, v})
	}
	switch {
	case q.Text != "":
		lookups = append(lookups, lookup{"title", q.Text, url.Values{"query": {q.Text}}})
	case q.Title != "":
		v := url.Values{"query": {q.Title}}
		value := q.Title
		if q.Year > 0 && q.ParentTMDBID == 0 {
			v.Set("year", strconv.Itoa(q.Year))
			value += " (" + strconv.Itoa(q.Year) + ")"
		}
		lookups = append(lookups, lookup{"title", value, v})
	}
	if len(lookups) == 0 {
		return nil, nil
	}
	var out []Result
	for _, l := range lookups {
		c.applyFilters(l.v, q)
		res, err := c.searchPages(ctx, l.v)
		line := Diagnostic{Source: "OpenSubtitles", Lookup: l.kind, Value: l.value, Count: len(res), Found: len(res)}
		if err != nil {
			line.Note, line.Detail = "error", err.Error()
		}
		Report(ctx, line)
		if err != nil {
			return nil, err
		}
		if len(res) > 0 {
			return res, nil
		}
		out = res
	}
	return out, nil
}

// applyFilters adds what every lookup of q shares: season, episode, type,
// language, hash and the hearing-impaired and forced flags.
func (c *Client) applyFilters(v url.Values, q Query) {
	if q.Season != 0 {
		v.Set("season_number", strconv.Itoa(q.Season))
	}
	if q.Episode != 0 {
		v.Set("episode_number", strconv.Itoa(q.Episode))
	}
	if q.Type != "" {
		v.Set("type", q.Type)
	}
	if q.Language != "" {
		v.Set("languages", q.Language)
	}
	if q.MovieHash != "" {
		v.Set("moviehash", q.MovieHash)
	}
	// OpenSubtitles filters on the two flags itself: include (the default),
	// exclude or only. The answer is filtered again by the caller, so a source
	// that ignores these still gives the right subtitle.
	switch {
	case q.Forced:
		v.Set("foreign_parts_only", "only")
	case q.Full == FullNormal:
		v.Set("hearing_impaired", "exclude")
		v.Set("foreign_parts_only", "exclude")
	case q.Full == FullSDH:
		v.Set("hearing_impaired", "only")
	}
}

// maxSearchPages is how many pages of one search are read.
const maxSearchPages = 3

// searchPages reads up to maxSearchPages pages of one search.
func (c *Client) searchPages(ctx context.Context, v url.Values) ([]Result, error) {
	var out []Result
	for page := 1; page <= maxSearchPages; page++ {
		pv := url.Values{}
		for k, vals := range v {
			pv[k] = vals
		}
		if page > 1 {
			pv.Set("page", strconv.Itoa(page))
		}
		res, pages, err := c.searchPage(ctx, pv)
		if err != nil {
			if page > 1 {
				break // the first page worked: use it
			}
			return nil, err
		}
		out = append(out, res...)
		if page >= pages {
			break
		}
	}
	return out, nil
}

// Search finds subtitles for a title, optionally scoped by IMDb id and
// language (e.g. "en"). imdbID and language may be empty.
func (c *Client) Search(ctx context.Context, query, imdbID, language string) ([]Result, error) {
	q := url.Values{}
	if query != "" {
		q.Set("query", query)
	}
	if imdbID != "" {
		q.Set("imdb_id", imdbID)
	}
	if language != "" {
		q.Set("languages", language)
	}
	return c.search(ctx, q)
}

func (c *Client) search(ctx context.Context, q url.Values) ([]Result, error) {
	out, _, err := c.searchPage(ctx, q)
	return out, err
}

// searchPage reads one page and says how many pages the search has.
func (c *Client) searchPage(ctx context.Context, q url.Values) ([]Result, int, error) {
	if c.apiKey == "" {
		return nil, 0, ErrNoAPIKey
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/subtitles?"+q.Encode(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build search request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("search subtitles: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, 0, ErrInvalidKey
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("search subtitles: unexpected status %d", resp.StatusCode)
	}

	var parsed searchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponse)).Decode(&parsed); err != nil {
		return nil, 0, fmt.Errorf("decode search response: %w", err)
	}

	var out []Result
	for _, d := range parsed.Data {
		if len(d.Attributes.Files) == 0 {
			continue
		}
		out = append(out, Result{
			FileID:           d.Attributes.Files[0].FileID,
			Language:         d.Attributes.Language,
			Release:          d.Attributes.Release,
			DownloadsAll:     d.Attributes.DownloadCount,
			Rating:           d.Attributes.Ratings,
			HashMatch:        d.Attributes.HashMatch,
			HearingImpaired:  d.Attributes.HearingImpaired,
			ForeignPartsOnly: d.Attributes.ForeignPartsOnly,
		})
	}
	return out, parsed.TotalPages, nil
}

// DownloadInfo is what OpenSubtitles said about a download request: the link
// and, when it reports them, the quota figures.
type DownloadInfo struct {
	Link         string
	Requests     int // downloads used in the current window
	HasRequests  bool
	Remaining    int // downloads left in the current window
	HasRemaining bool
	ResetAt      time.Time // zero when not reported
	Message      string
}

type downloadRequestResponse struct {
	Link         string `json:"link"`
	Requests     *int   `json:"requests"`
	Remaining    *int   `json:"remaining"`
	Message      string `json:"message"`
	ResetTime    string `json:"reset_time"`     // "23 hours and 59 minutes"
	ResetTimeUTC string `json:"reset_time_utc"` // RFC 3339
}

// QuotaError is the error returned when OpenSubtitles refuses a download
// because the daily limit is used up. It matches ErrQuota with errors.Is and
// carries whatever the response said about the limit.
type QuotaError struct {
	Info DownloadInfo
}

func (e *QuotaError) Error() string        { return ErrQuota.Error() }
func (e *QuotaError) Is(target error) bool { return target == ErrQuota }

var (
	hoursRe   = regexp.MustCompile(`(?i)(\d+)\s*(?:hours?|hrs?|h)(?:[^a-z]|$)`)
	minutesRe = regexp.MustCompile(`(?i)(\d+)\s*(?:minutes?|mins?|m)(?:[^a-z]|$)`)
)

// maxResetHours is the longest reset time believed (a week). A huge number
// would overflow the duration and give a nonsense time.
const maxResetHours = 24 * 7

// parseResetDuration reads OpenSubtitles' human "reset_time" text
// ("23 hours and 59 minutes", "12 minutes").
func parseResetDuration(s string) (time.Duration, bool) {
	var d time.Duration
	found := false
	if m := hoursRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		d += time.Duration(min(n, maxResetHours)) * time.Hour
		found = true
	}
	if m := minutesRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		d += time.Duration(min(n, maxResetHours*60)) * time.Minute
		found = true
	}
	return d, found
}

// parseDownloadBody extracts the link and quota figures from a download
// response body; observed is when the response arrived, the base for a
// relative reset time.
func parseDownloadBody(body []byte, observed time.Time) (DownloadInfo, error) {
	var parsed downloadRequestResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return DownloadInfo{}, err
	}
	info := DownloadInfo{Link: parsed.Link, Message: parsed.Message}
	if parsed.Requests != nil {
		info.Requests, info.HasRequests = *parsed.Requests, true
	}
	if parsed.Remaining != nil {
		info.Remaining, info.HasRemaining = *parsed.Remaining, true
	}
	if t, err := time.Parse(time.RFC3339, parsed.ResetTimeUTC); err == nil {
		info.ResetAt = t.UTC()
	} else if t, err := time.Parse(time.RFC3339, parsed.ResetTime); err == nil {
		info.ResetAt = t.UTC()
	} else if d, ok := parseResetDuration(parsed.ResetTime); ok {
		info.ResetAt = observed.Add(d).UTC()
	}
	return info, nil
}

// RequestDownload exchanges a file ID for a time-limited download link. See
// RequestDownloadInfo for the version that also reports the quota.
func (c *Client) RequestDownload(ctx context.Context, fileID int) (string, error) {
	info, err := c.RequestDownloadInfo(ctx, fileID)
	if err != nil {
		var qe *QuotaError
		if errors.As(err, &qe) {
			return "", ErrQuota
		}
		return "", err
	}
	return info.Link, nil
}

// maxThrottleRetries is how often a download link is asked for again after
// "too many requests" before it counts as the limit.
const maxThrottleRetries = 2

// throttleWait is how long to wait after a 429: its Retry-After in seconds,
// one second when it gives none, and never more than ten.
func throttleWait(retryAfter string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && n > 0 {
		return time.Duration(min(n, 10)) * time.Second
	}
	return time.Second
}

// RequestDownloadInfo exchanges a file ID for a time-limited download link —
// OpenSubtitles' download endpoint is a two-step process (this, then a
// plain GET of the returned link) so it can enforce daily download quotas
// per API key. The result carries the quota figures OpenSubtitles reports;
// when the limit is used up the error is a *QuotaError (matching ErrQuota).
func (c *Client) RequestDownloadInfo(ctx context.Context, fileID int) (DownloadInfo, error) {
	if c.apiKey == "" {
		return DownloadInfo{}, ErrNoAPIKey
	}
	body := fmt.Sprintf(`{"file_id":%s}`, strconv.Itoa(fileID))

	var resp *http.Response
	for attempt := 0; ; attempt++ {
		token, err := c.bearer(ctx)
		if err != nil {
			return DownloadInfo{}, err
		}
		req, err := c.newRequest(ctx, http.MethodPost, "/download", strings.NewReader(body))
		if err != nil {
			return DownloadInfo{}, fmt.Errorf("build download request: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err = c.httpClient.Do(req)
		if err != nil {
			return DownloadInfo{}, fmt.Errorf("request download link: %w", err)
		}
		// A stale token: log in again once and retry.
		if resp.StatusCode == http.StatusUnauthorized && token != "" && attempt == 0 {
			resp.Body.Close()
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		// 429 is mostly OpenSubtitles' per-second throttle, not the daily
		// limit: wait as long as it asks (a few seconds at most) and try again.
		// Only an answer that says no downloads are left is the daily limit.
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxThrottleRetries {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if info, _ := parseDownloadBody(raw, time.Now()); !info.HasRemaining || info.Remaining > 0 {
				select {
				case <-ctx.Done():
					return DownloadInfo{}, fmt.Errorf("request download link: %w", ctx.Err())
				case <-time.After(throttleWait(resp.Header.Get("Retry-After"))):
				}
				continue
			}
			resp.Body = io.NopCloser(bytes.NewReader(raw))
		}
		break
	}
	defer resp.Body.Close()
	observed := time.Now()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotAcceptable || resp.StatusCode == http.StatusTooManyRequests {
		info, _ := parseDownloadBody(raw, observed) // best effort: an empty body is fine
		if resp.StatusCode == http.StatusNotAcceptable && !info.HasRemaining {
			// 406 is OpenSubtitles' "daily limit reached".
			info.Remaining, info.HasRemaining = 0, true
		}
		return DownloadInfo{}, &QuotaError{Info: info}
	}
	if resp.StatusCode != http.StatusOK {
		return DownloadInfo{}, fmt.Errorf("request download link: unexpected status %d", resp.StatusCode)
	}

	info, err := parseDownloadBody(raw, observed)
	if err != nil {
		return DownloadInfo{}, fmt.Errorf("decode download response: %w", err)
	}
	if info.Link == "" {
		return DownloadInfo{}, fmt.Errorf("download response had no link")
	}
	return info, nil
}

// DownloadFile fetches the actual .srt bytes from a link returned by
// RequestDownload.
func (c *Client) DownloadFile(ctx context.Context, link string) ([]byte, error) {
	// The link comes out of an API answer: only a web address is followed.
	if u, err := url.Parse(link); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("download subtitle file: the link is not a web address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, fmt.Errorf("build file download request: %w", netguard.CleanError(err))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// the link can carry a one-time key: keep it out of the message
		return nil, fmt.Errorf("download subtitle file: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download subtitle file: unexpected status %d", resp.StatusCode)
	}
	// A subtitle is a few hundred kilobytes at most; a link that keeps sending
	// is not one, and must not be held in memory or written to the library.
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxSubtitleBytes+1))
	if err != nil {
		return nil, fmt.Errorf("download subtitle file: %w", err)
	}
	if len(data) > MaxSubtitleBytes {
		return nil, fmt.Errorf("download subtitle file: the file is larger than %d MB", MaxSubtitleBytes>>20)
	}
	return data, nil
}

// maxAPIResponse is the biggest answer read from the OpenSubtitles API; real ones are tens of KB.
const maxAPIResponse = 16 << 20

// MaxSubtitleBytes is the biggest subtitle file that is accepted.
const MaxSubtitleBytes = 5 << 20
