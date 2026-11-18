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
	"bytes"
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
	maxCacheBytes   = 64 << 20  // all the answers kept, in bytes
	maxCachedBody   = 512 << 10 // a bigger answer is used but not kept
	maxBodyBytes    = 8 << 20
	// maxRetryAfterSeconds is the longest pause the server can ask for before
	// a retry; a request cannot wait for ever on what a reply says.
	maxRetryAfterSeconds = 60
	// maxPatientRetryAfter is the longest pause a patient request takes on
	// what a reply says.
	maxPatientRetryAfter = 2 * time.Minute
)

// defaultPatientWaits are the pauses of a patient request: it tries again
// after 2 seconds, 5, 15, 45 and 2 minutes, about three and a half minutes
// in all, which covers the busy spells MusicBrainz has now and then. Every
// try still takes its place in the one-request-per-second limit.
var defaultPatientWaits = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 45 * time.Second, 2 * time.Minute}

// ErrUnavailable is what a request ends with when MusicBrainz could not be
// used because it is busy (503, 429) or could not be reached, as opposed to
// answering that it does not know something. Test for it with errors.Is.
var ErrUnavailable = errors.New("the music database is busy or not reachable")

type unavailableError struct{ msg string }

func (e *unavailableError) Error() string        { return e.msg }
func (e *unavailableError) Is(target error) bool { return target == ErrUnavailable }

type patientKey struct{}

// Patient marks the requests made with the returned context as patient: they
// keep trying through a busy spell (see defaultPatientWaits), and also
// through timeouts and 502/504 answers, instead of giving up after a few
// seconds. For background work such as an import, never for a person waiting
// at a page.
func Patient(ctx context.Context) context.Context {
	return context.WithValue(ctx, patientKey{}, true)
}

func isPatient(ctx context.Context) bool {
	v, _ := ctx.Value(patientKey{}).(bool)
	return v
}

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
	patient   []time.Duration // the waits of a patient request (see Patient)
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

// WithPatientWaits sets the pauses before each retry of a patient request
// (see Patient): one retry per entry. Tests pass tiny ones.
func WithPatientWaits(waits ...time.Duration) Option {
	return func(c *Client) { c.patient = waits }
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
		patient:   append([]time.Duration(nil), defaultPatientWaits...),
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
		if head := bytes.TrimSpace(body[:min(len(body), 64)]); bytes.HasPrefix(head, []byte("<")) {
			return &unavailableError{"the music database sent a web page instead of its data (it may be down for maintenance)"}
		}
		return fmt.Errorf("music database %s: decode answer: %w", path, err)
	}
	return nil
}

func (c *Client) fetch(ctx context.Context, full string) ([]byte, error) {
	patient := isPatient(ctx)
	maxRetries, pauseFor := c.retries, c.backoff
	if patient {
		waits := c.patient
		maxRetries = len(waits)
		pauseFor = func(attempt int) time.Duration { return waits[attempt-1] }
	}
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
			if !patient || ctx.Err() != nil {
				return nil, fmt.Errorf("music database: %w", err)
			}
			// A timeout or a dropped connection: MusicBrainz is struggling.
			if attempt >= maxRetries {
				return nil, &unavailableError{fmt.Sprintf("the music database could not be reached, even after %d retries: %v", maxRetries, err)}
			}
			if err := sleep(ctx, pauseFor(attempt+1)); err != nil {
				return nil, err
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		resp.Body.Close()

		busy := resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests ||
			(patient && (resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout))
		switch {
		case resp.StatusCode == http.StatusOK:
			if readErr != nil {
				return nil, fmt.Errorf("music database: read answer: %w", readErr)
			}
			return body, nil
		case resp.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case busy && attempt < maxRetries:
			pause := pauseFor(attempt + 1)
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
				limit := maxRetryAfterSeconds // a huge number would also overflow the multiplication
				if patient {
					limit = int(maxPatientRetryAfter / time.Second)
				}
				if ask := time.Duration(min(s, limit)) * time.Second; ask > pause {
					pause = ask
				}
			}
			if err := sleep(ctx, pause); err != nil {
				return nil, err
			}
			continue
		case busy:
			return nil, &unavailableError{fmt.Sprintf("the music database is busy (%d), even after %d retries. Try again later.", resp.StatusCode, maxRetries)}
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

// maxQueuedSlots is how many requests may be waiting for a token at once.
// Beyond that a caller is turned away, so a burst of searches cannot book
// minutes of the shared limit ahead of everyone else.
const maxQueuedSlots = 30

// ErrBusy is returned when too many requests are already waiting for the
// music database's rate limit.
var ErrBusy = errors.New("the music database is busy right now. Try again in a moment.")

// wait blocks until a token is available (or ctx ends) and takes it. A caller
// whose deadline falls before its turn, or who arrives behind too long a
// queue, is turned away without booking a slot; one that gives up while
// waiting hands its slot back when it was the last in line.
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
	if at.Sub(now) > time.Duration(maxQueuedSlots)*b.interval {
		b.mu.Unlock()
		return ErrBusy
	}
	if dl, ok := ctx.Deadline(); ok && at.After(dl) {
		b.mu.Unlock()
		return context.DeadlineExceeded
	}
	b.next = at.Add(b.interval)
	b.mu.Unlock()
	err := sleep(ctx, time.Until(at))
	if err != nil {
		b.mu.Lock()
		if b.next.Equal(at.Add(b.interval)) {
			b.next = at // nobody queued behind us: give the slot back
		}
		b.mu.Unlock()
	}
	return err
}

// cache keeps raw answers by URL for ttl, at most max of them and maxBytes
// of answers in all. An answer bigger than maxBody isn't kept.
type cache struct {
	mu       sync.Mutex
	ttl      time.Duration
	max      int
	maxBytes int
	maxBody  int
	size     int // bytes of all the answers kept
	entries  map[string]cacheEntry
}

type cacheEntry struct {
	body    []byte
	expires time.Time
}

func newCache(ttl time.Duration, max int) *cache {
	return &cache{ttl: ttl, max: max, maxBytes: maxCacheBytes, maxBody: maxCachedBody, entries: map[string]cacheEntry{}}
}

// drop removes one entry; c.mu is held.
func (c *cache) drop(key string) {
	if e, ok := c.entries[key]; ok {
		c.size -= len(e.body)
		delete(c.entries, key)
	}
}

func (c *cache) get(key string) ([]byte, bool) {
	if c == nil || c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		c.drop(key)
		return nil, false
	}
	return e.body, true
}

func (c *cache) put(key string, body []byte) {
	if c == nil || c.ttl <= 0 || len(body) > c.maxBody {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drop(key)
	full := func() bool { return len(c.entries) >= c.max || c.size+len(body) > c.maxBytes }
	now := time.Now()
	if full() {
		for k, e := range c.entries {
			if now.After(e.expires) {
				c.drop(k)
			}
		}
		for k := range c.entries { // still full: drop arbitrary entries
			if !full() {
				break
			}
			c.drop(k)
		}
	}
	c.entries[key] = cacheEntry{body: body, expires: now.Add(c.ttl)}
	c.size += len(body)
}
