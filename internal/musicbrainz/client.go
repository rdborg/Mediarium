// Package musicbrainz is a small client for the MusicBrainz web service
// (https://musicbrainz.org/doc/MusicBrainz_API): artist search, an artist's
// release groups (albums, EPs, singles), a release group's releases and one
// release's tracklist, plus the Cover Art Archive image address for a
// release group.
//
// MusicBrainz asks every client to identify itself with a meaningful
// User-Agent and to make at most one request per second; both are built in
// here. Every request waits for a token from one shared bucket (one request
// per second), a 503 (MusicBrainz's "slow down") is retried with growing
// pauses, and answers are kept in memory for a while so browsing the same
// artist twice does not ask again.
//
// The package depends on nothing else in Mediarium except netguard.
package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// DefaultBaseURL is the public MusicBrainz server.
const DefaultBaseURL = "https://musicbrainz.org"

// CoverArtBaseURL is the Cover Art Archive.
const CoverArtBaseURL = "https://coverartarchive.org"

// ErrNotFound is returned when MusicBrainz does not know the id asked for.
var ErrNotFound = errors.New("not found in the music database")

const (
	defaultInterval = time.Second // MusicBrainz: at most one request per second
	defaultCacheTTL = 6 * time.Hour
	defaultRetries  = 3
	maxCacheEntries = 2000
	maxBodyBytes    = 8 << 20
	// maxRetryAfterSeconds is the longest pause the server can ask for before
	// a retry; a request cannot wait for ever on what a reply says.
	maxRetryAfterSeconds = 60
)

// Client talks to one MusicBrainz server. It is safe for concurrent use; all
// requests made through one Client share its rate limit and cache.
type Client struct {
	baseURL   string
	coverBase string // the Cover Art Archive
	userAgent string
	http      *http.Client
	bucket    *tokenBucket
	cache     *cache
	retries   int
	backoff   func(attempt int) time.Duration
}

// Option changes a Client made by New.
type Option func(*Client)

// WithBaseURL points the client at another server (a mirror, or a test fake).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient replaces the HTTP client (timeouts, transport).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithRateLimit sets the minimum time between two requests; 0 removes the
// limit (only for tests against a local fake).
func WithRateLimit(interval time.Duration) Option {
	return func(c *Client) { c.bucket = newTokenBucket(interval) }
}

// WithCacheTTL sets how long answers are kept; 0 turns the cache off.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) { c.cache = newCache(ttl, maxCacheEntries) }
}

// WithBackoff sets the pause before retry attempt n (1, 2, ...) after a 503.
func WithBackoff(f func(attempt int) time.Duration) Option {
	return func(c *Client) { c.backoff = f }
}

// UserAgent is the identifying User-Agent MusicBrainz asks for:
// application name, version and a contact address.
func UserAgent(version string) string {
	if version == "" {
		version = "dev"
	}
	return fmt.Sprintf("Mediarium/%s ( https://github.com/rdborg/Mediarium )", version)
}

// New returns a client for the public MusicBrainz server that identifies
// itself as this version of Mediarium.
func New(version string, opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		coverBase: CoverArtBaseURL,
		userAgent: UserAgent(version),
		http:      netguard.Client(20 * time.Second),
		bucket:    newTokenBucket(defaultInterval),
		cache:     newCache(defaultCacheTTL, maxCacheEntries),
		retries:   defaultRetries,
		backoff:   func(attempt int) time.Duration { return time.Duration(attempt) * 2 * time.Second },
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// get fetches path?query from the web service (fmt=json is added) into out,
// through the cache, the rate limit and the 503 retries.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	if query == nil {
		query = url.Values{}
	}
	query.Set("fmt", "json")
	// MusicBrainz separates "inc" values with "+"; url.Values would escape it.
	raw := strings.ReplaceAll(query.Encode(), "%2B", "+")
	full := c.baseURL + path + "?" + raw

	if body, ok := c.cache.get(full); ok {
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

func decode(body []byte, out any, path string) error {
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("music database %s: decode answer: %w", path, err)
	}
	return nil
}

func (c *Client) fetch(ctx context.Context, full string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		if err := c.bucket.wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
		if err != nil {
			return nil, fmt.Errorf("music database: build request: %w", err)
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("music database: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusOK:
			if readErr != nil {
				return nil, fmt.Errorf("music database: read answer: %w", readErr)
			}
			return body, nil
		case resp.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case resp.StatusCode == http.StatusServiceUnavailable && attempt < c.retries:
			pause := c.backoff(attempt + 1)
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
				if s > maxRetryAfterSeconds {
					s = maxRetryAfterSeconds // a huge number would also overflow the multiplication
				}
				if time.Duration(s)*time.Second > pause {
					pause = time.Duration(s) * time.Second
				}
			}
			if err := sleep(ctx, pause); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode == http.StatusServiceUnavailable:
			return nil, fmt.Errorf("the music database is busy (503), even after %d retries. Try again later.", c.retries)
		default:
			return nil, fmt.Errorf("music database: unexpected status %d", resp.StatusCode)
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// tokenBucket hands out one token per interval with a burst of one: the
// MusicBrainz limit of one request per second, shared by every caller.
type tokenBucket struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time // when the next token is available
}

func newTokenBucket(interval time.Duration) *tokenBucket { return &tokenBucket{interval: interval} }

// wait blocks until a token is available (or ctx ends) and takes it.
func (b *tokenBucket) wait(ctx context.Context) error {
	if b == nil || b.interval <= 0 {
		return ctx.Err()
	}
	b.mu.Lock()
	now := time.Now()
	at := b.next
	if at.Before(now) {
		at = now
	}
	b.next = at.Add(b.interval)
	b.mu.Unlock()
	return sleep(ctx, time.Until(at))
}

// cache keeps raw answers by URL for ttl, at most max of them.
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
