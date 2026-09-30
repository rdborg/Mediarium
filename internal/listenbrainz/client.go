// Package listenbrainz is a small read-only client for the public
// ListenBrainz web service (https://listenbrainz.readthedocs.io/): what
// people are listening to across the whole service (top release groups and
// artists), the release groups that came out recently or are about to, and
// the type, date and genre tags of release groups by MusicBrainz id. It
// needs no account or key.
//
// Requests identify Mediarium with a User-Agent, go through the netguard
// dialer, and every answer is kept in memory for an hour: these lists change
// slowly and many people can open the same page.
//
// The package depends on nothing else in Mediarium except netguard.
package listenbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// DefaultBaseURL is the public ListenBrainz server.
const DefaultBaseURL = "https://api.listenbrainz.org"

const (
	defaultCacheTTL = time.Hour
	maxCacheEntries = 200
	maxBodyBytes    = 16 << 20
	requestTimeout  = 25 * time.Second
	// metadataBatch is how many release groups one metadata request asks for.
	metadataBatch = 50
)

// Ranges the statistics can be asked for.
const (
	RangeWeek    = "week"
	RangeMonth   = "month"
	RangeYear    = "year"
	RangeAllTime = "all_time"
)

// ValidRange reports whether r is a range the statistics support.
func ValidRange(r string) bool {
	switch r {
	case RangeWeek, RangeMonth, RangeYear, RangeAllTime:
		return true
	}
	return false
}

// ErrRange is returned for a range that is not one of the Range constants.
var ErrRange = errors.New("unsupported range")

// Client talks to one ListenBrainz server. It is safe for concurrent use.
type Client struct {
	baseURL   string
	userAgent string
	http      *http.Client
	cache     *cache

	locksMu sync.Mutex
	locks   map[string]*sync.Mutex // one cold fetch per URL at a time
}

// Option changes a Client made by New.
type Option func(*Client)

// WithBaseURL points the client at another server (a mirror, or a test fake).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient replaces the HTTP client (timeouts, transport).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithCacheTTL sets how long answers are kept; 0 turns the cache off.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) { c.cache = newCache(ttl, maxCacheEntries) }
}

// UserAgent is the identifying User-Agent sent with every request.
func UserAgent(version string) string {
	if version == "" {
		version = "dev"
	}
	return fmt.Sprintf("Mediarium/%s ( https://github.com/rdborg/Mediarium )", version)
}

// New returns a client for the public server that identifies itself as this
// version of Mediarium.
func New(version string, opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		userAgent: UserAgent(version),
		http:      netguard.Client(requestTimeout),
		cache:     newCache(defaultCacheTTL, maxCacheEntries),
		locks:     map[string]*sync.Mutex{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// get fetches path?query into out through the cache. A 204 (statistics not
// calculated yet) leaves out empty and is not an error.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	full := c.baseURL + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	if body, ok := c.cache.get(full); ok {
		return decode(body, out, path)
	}

	unlock := c.lock(full)
	defer unlock()
	if body, ok := c.cache.get(full); ok { // fetched while we waited
		return decode(body, out, path)
	}
	body, err := c.fetch(ctx, full)
	if err != nil {
		return err
	}
	if err := decode(body, out, path); err != nil {
		return err
	}
	c.cache.put(full, body)
	return nil
}

func (c *Client) lock(key string) (unlock func()) {
	c.locksMu.Lock()
	l := c.locks[key]
	if l == nil {
		l = &sync.Mutex{}
		if len(c.locks) > 1000 { // never grows without bound
			c.locks = map[string]*sync.Mutex{}
		}
		c.locks[key] = l
	}
	c.locksMu.Unlock()
	l.Lock()
	return l.Unlock
}

func decode(body []byte, out any, path string) error {
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("listenbrainz %s: decode answer: %w", path, err)
	}
	return nil
}

func (c *Client) fetch(ctx context.Context, full string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, fmt.Errorf("listenbrainz: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listenbrainz: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("listenbrainz: read answer: %w", err)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("listenbrainz: answer is larger than %d bytes", maxBodyBytes)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNoContent:
		return nil, nil
	default:
		return nil, fmt.Errorf("listenbrainz: unexpected status %d", resp.StatusCode)
	}
}

// ---- Statistics ----

// ReleaseGroupStat is one release group in the sitewide top list.
type ReleaseGroupStat struct {
	MBID        string // release group id
	Title       string
	ArtistName  string
	ArtistMBIDs []string
	ListenCount int
}

// TopReleaseGroups returns the most listened-to release groups over a range,
// most listened first. The service lists a release group once per release
// its listens were counted on; those entries are merged here.
func (c *Client) TopReleaseGroups(ctx context.Context, rng string, count, offset int) ([]ReleaseGroupStat, error) {
	if !ValidRange(rng) {
		return nil, ErrRange
	}
	var resp struct {
		Payload struct {
			ReleaseGroups []struct {
				MBID        string   `json:"release_group_mbid"`
				Name        string   `json:"release_group_name"`
				ArtistName  string   `json:"artist_name"`
				ArtistMBIDs []string `json:"artist_mbids"`
				ListenCount int      `json:"listen_count"`
			} `json:"release_groups"`
		} `json:"payload"`
	}
	q := url.Values{"range": {rng}, "count": {fmt.Sprint(count)}, "offset": {fmt.Sprint(offset)}}
	if err := c.get(ctx, "/1/stats/sitewide/release-groups", q, &resp); err != nil {
		return nil, err
	}
	out := make([]ReleaseGroupStat, 0, len(resp.Payload.ReleaseGroups))
	index := map[string]int{}
	for _, rg := range resp.Payload.ReleaseGroups {
		if rg.MBID == "" {
			continue
		}
		if i, ok := index[rg.MBID]; ok {
			out[i].ListenCount += rg.ListenCount
			continue
		}
		index[rg.MBID] = len(out)
		out = append(out, ReleaseGroupStat{MBID: rg.MBID, Title: rg.Name, ArtistName: rg.ArtistName, ArtistMBIDs: rg.ArtistMBIDs, ListenCount: rg.ListenCount})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ListenCount > out[j].ListenCount })
	return out, nil
}

// ArtistStat is one artist in the sitewide top list.
type ArtistStat struct {
	MBID        string
	Name        string
	ListenCount int
}

// TopArtists returns the most listened-to artists over a range.
func (c *Client) TopArtists(ctx context.Context, rng string, count, offset int) ([]ArtistStat, error) {
	if !ValidRange(rng) {
		return nil, ErrRange
	}
	var resp struct {
		Payload struct {
			Artists []struct {
				MBID        string `json:"artist_mbid"`
				Name        string `json:"artist_name"`
				ListenCount int    `json:"listen_count"`
			} `json:"artists"`
		} `json:"payload"`
	}
	q := url.Values{"range": {rng}, "count": {fmt.Sprint(count)}, "offset": {fmt.Sprint(offset)}}
	if err := c.get(ctx, "/1/stats/sitewide/artists", q, &resp); err != nil {
		return nil, err
	}
	out := make([]ArtistStat, 0, len(resp.Payload.Artists))
	for _, a := range resp.Payload.Artists {
		if a.MBID == "" || a.Name == "" { // nothing to add or open without an id
			continue
		}
		out = append(out, ArtistStat{MBID: a.MBID, Name: a.Name, ListenCount: a.ListenCount})
	}
	return out, nil
}

// ---- Fresh releases ----

// FreshRelease is one release group that came out recently or is coming out.
type FreshRelease struct {
	MBID          string // release group id
	Title         string
	ArtistName    string
	ArtistMBIDs   []string
	Date          string // "YYYY-MM-DD"
	PrimaryType   string // Album, EP, Single, ...; "" if unknown
	SecondaryType string // Live, Compilation, ...; "" for a plain release
	Tags          []string
	ListenCount   int
}

// FreshReleases returns the release groups of the last `days` days (up to
// today), or, with upcoming, the ones dated from today up to `days` days
// ahead. The service allows at most 90 days.
func (c *Client) FreshReleases(ctx context.Context, days int, upcoming bool) ([]FreshRelease, error) {
	q := url.Values{"days": {fmt.Sprint(days)}, "sort": {"release_date"}}
	if upcoming {
		q.Set("past", "false")
		q.Set("future", "true")
	} else {
		q.Set("future", "false")
	}
	// The answer is {"payload":{"releases":[...]}}; older servers answered
	// with the bare array.
	var raw json.RawMessage
	if err := c.get(ctx, "/1/explore/fresh-releases/", q, &raw); err != nil {
		return nil, err
	}
	type release struct {
		MBID          string   `json:"release_group_mbid"`
		Title         string   `json:"release_name"`
		ArtistName    string   `json:"artist_credit_name"`
		ArtistMBIDs   []string `json:"artist_mbids"`
		Date          string   `json:"release_date"`
		PrimaryType   string   `json:"release_group_primary_type"`
		SecondaryType string   `json:"release_group_secondary_type"`
		Tags          []string `json:"release_tags"`
		ListenCount   int      `json:"listen_count"`
	}
	var list []release
	switch trimmed := strings.TrimSpace(string(raw)); {
	case trimmed == "":
		// nothing (a 204)
	case strings.HasPrefix(trimmed, "["):
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("listenbrainz fresh releases: decode answer: %w", err)
		}
	default:
		var wrapped struct {
			Payload struct {
				Releases []release `json:"releases"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return nil, fmt.Errorf("listenbrainz fresh releases: decode answer: %w", err)
		}
		list = wrapped.Payload.Releases
	}
	out := make([]FreshRelease, 0, len(list))
	seen := map[string]bool{}
	for _, r := range list {
		if r.MBID == "" || seen[r.MBID] {
			continue
		}
		seen[r.MBID] = true
		out = append(out, FreshRelease{
			MBID: r.MBID, Title: r.Title, ArtistName: r.ArtistName, ArtistMBIDs: r.ArtistMBIDs, Date: r.Date,
			PrimaryType: r.PrimaryType, SecondaryType: r.SecondaryType, Tags: r.Tags, ListenCount: r.ListenCount,
		})
	}
	return out, nil
}

// ---- Release group details ----

// ReleaseGroupInfo is what the metadata service knows about a release group.
type ReleaseGroupInfo struct {
	Type   string   // Album, EP, Single, ...
	Date   string   // release date, "YYYY[-MM[-DD]]"
	Genres []string // most used first, at most maxGenres
}

// maxGenres is how many genres are kept per release group.
const maxGenres = 4

// ReleaseGroupInfos looks up the type, date and genres of release groups.
// Release groups the service does not know are missing from the result.
// Genres are the release group's own genre tags, else its artists'.
func (c *Client) ReleaseGroupInfos(ctx context.Context, mbids []string) (map[string]ReleaseGroupInfo, error) {
	type tag struct {
		Count int    `json:"count"`
		Tag   string `json:"tag"`
		Genre string `json:"genre_mbid"`
	}
	type entry struct {
		ReleaseGroup struct {
			Date string `json:"date"`
			Type string `json:"type"`
		} `json:"release_group"`
		Tag struct {
			Artist       []tag `json:"artist"`
			ReleaseGroup []tag `json:"release_group"`
		} `json:"tag"`
	}
	genres := func(tags []tag) []string {
		var gs []tag
		for _, t := range tags {
			if t.Genre != "" && strings.TrimSpace(t.Tag) != "" { // only tags that are genres
				gs = append(gs, t)
			}
		}
		sort.SliceStable(gs, func(i, j int) bool { return gs[i].Count > gs[j].Count })
		var out []string
		seen := map[string]bool{}
		for _, t := range gs {
			name := strings.TrimSpace(t.Tag)
			if seen[strings.ToLower(name)] {
				continue
			}
			seen[strings.ToLower(name)] = true
			out = append(out, name)
			if len(out) == maxGenres {
				break
			}
		}
		return out
	}

	out := map[string]ReleaseGroupInfo{}
	for start := 0; start < len(mbids); start += metadataBatch {
		end := min(start+metadataBatch, len(mbids))
		batch := append([]string(nil), mbids[start:end]...)
		sort.Strings(batch) // the same set is the same cache key
		q := url.Values{"release_group_mbids": {strings.Join(batch, ",")}, "inc": {"tag"}}
		var resp map[string]entry
		if err := c.get(ctx, "/1/metadata/release_group/", q, &resp); err != nil {
			return out, err
		}
		for id, e := range resp {
			g := genres(e.Tag.ReleaseGroup)
			if len(g) == 0 {
				g = genres(e.Tag.Artist)
			}
			out[id] = ReleaseGroupInfo{Type: e.ReleaseGroup.Type, Date: e.ReleaseGroup.Date, Genres: g}
		}
	}
	return out, nil
}

// ---- cache ----

type cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	entries map[string]cacheEntry
}

type cacheEntry struct {
	body    []byte
	expires time.Time
}

func newCache(ttl time.Duration, max int) *cache {
	return &cache{ttl: ttl, max: max, entries: map[string]cacheEntry{}}
}

func (c *cache) get(key string) ([]byte, bool) {
	if c == nil || c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return e.body, true
}

func (c *cache) put(key string, body []byte) {
	if c == nil || c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.entries) >= c.max {
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
		for k := range c.entries { // still full: drop an arbitrary entry
			if len(c.entries) < c.max {
				break
			}
			delete(c.entries, k)
		}
	}
	c.entries[key] = cacheEntry{body: body, expires: now.Add(c.ttl)}
}
