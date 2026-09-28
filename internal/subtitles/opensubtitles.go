// Package subtitles searches for and downloads subtitles (PRD.md §4.4 —
// "Original, using existing subtitle-provider APIs (OpenSubtitles,
// etc.)"). Implements the OpenSubtitles REST API v1
// (https://opensubtitles.stoplight.io/docs/opensubtitles-api).
package subtitles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.opensubtitles.com/api/v1"

// ErrNoAPIKey mirrors internal/metadata's ErrNoAPIKey — same "surface a
// clear setup prompt" reasoning.
var ErrNoAPIKey = fmt.Errorf("no OpenSubtitles API key configured")

// ErrQuota is returned when OpenSubtitles refuses a download because the
// account's daily quota (or rate limit) is used up; callers should stop
// asking for the rest of the day rather than retry.
var ErrQuota = fmt.Errorf("OpenSubtitles download quota reached")

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
	return &Client{apiKey: apiKey, baseURL: defaultBaseURL, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

// NewWithBaseURL is used by tests to point at a local fixture server
// instead of the real OpenSubtitles API (CLAUDE.md: local fixtures, not
// live network calls, for tests) — same pattern as internal/metadata.
func NewWithBaseURL(apiKey, baseURL string) *Client {
	c := New(apiKey)
	c.baseURL = baseURL
	return c
}

func (c *Client) HasAPIKey() bool { return c.apiKey != "" }

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
		return fmt.Errorf("no OpenSubtitles account configured")
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
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil || parsed.Token == "" {
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
		return "API key accepted and account login works.", nil
	}
	return "API key accepted. Without an OpenSubtitles.com account, downloads are limited to a few per day.", nil
}

// Result is one subtitle search hit, flattened from OpenSubtitles'
// nested attributes/files JSON shape into what callers actually need.
type Result struct {
	FileID       int
	Language     string
	Release      string // release name the subtitle was synced to, e.g. "Movie.2024.1080p.BluRay-GROUP"
	DownloadsAll int
	Rating       float64
}

type searchResponse struct {
	Data []struct {
		Attributes struct {
			Language      string  `json:"language"`
			Release       string  `json:"release"`
			DownloadCount int     `json:"download_count"`
			Ratings       float64 `json:"ratings"`
			Files         []struct {
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
}

// Find searches for subtitles matching q.
func (c *Client) Find(ctx context.Context, q Query) ([]Result, error) {
	v := url.Values{}
	if q.Text != "" {
		v.Set("query", q.Text)
	}
	if q.TMDBID != 0 {
		v.Set("tmdb_id", strconv.Itoa(q.TMDBID))
	}
	if q.ParentTMDBID != 0 {
		v.Set("parent_tmdb_id", strconv.Itoa(q.ParentTMDBID))
	}
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
	return c.search(ctx, v)
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
	if c.apiKey == "" {
		return nil, ErrNoAPIKey
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/subtitles?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search subtitles: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrInvalidKey
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search subtitles: unexpected status %d", resp.StatusCode)
	}

	var parsed searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	var out []Result
	for _, d := range parsed.Data {
		if len(d.Attributes.Files) == 0 {
			continue
		}
		out = append(out, Result{
			FileID:       d.Attributes.Files[0].FileID,
			Language:     d.Attributes.Language,
			Release:      d.Attributes.Release,
			DownloadsAll: d.Attributes.DownloadCount,
			Rating:       d.Attributes.Ratings,
		})
	}
	return out, nil
}

type downloadRequestResponse struct {
	Link string `json:"link"`
}

// RequestDownload exchanges a file ID for a time-limited download link —
// OpenSubtitles' download endpoint is a two-step process (this, then a
// plain GET of the returned link) so it can enforce daily download quotas
// per API key.
func (c *Client) RequestDownload(ctx context.Context, fileID int) (string, error) {
	if c.apiKey == "" {
		return "", ErrNoAPIKey
	}
	body := fmt.Sprintf(`{"file_id":%s}`, strconv.Itoa(fileID))

	var resp *http.Response
	for attempt := 0; ; attempt++ {
		token, err := c.bearer(ctx)
		if err != nil {
			return "", err
		}
		req, err := c.newRequest(ctx, http.MethodPost, "/download", strings.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("build download request: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err = c.httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("request download link: %w", err)
		}
		// A stale token: log in again once and retry.
		if resp.StatusCode == http.StatusUnauthorized && token != "" && attempt == 0 {
			resp.Body.Close()
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		break
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotAcceptable || resp.StatusCode == http.StatusTooManyRequests {
		return "", ErrQuota
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request download link: unexpected status %d", resp.StatusCode)
	}

	var parsed downloadRequestResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode download response: %w", err)
	}
	if parsed.Link == "" {
		return "", fmt.Errorf("download response had no link")
	}
	return parsed.Link, nil
}

// DownloadFile fetches the actual .srt bytes from a link returned by
// RequestDownload.
func (c *Client) DownloadFile(ctx context.Context, link string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, fmt.Errorf("build file download request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download subtitle file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download subtitle file: unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
