// Package usage keeps a rolling 24-hour record of how much each third-party
// service (TMDB, Trakt, OpenSubtitles) is used and how often it refused a
// request because a limit was reached. Mediarium ships shared keys for these
// services, so the record lets it tell the person when the shared key is busy
// and a free personal key would help.
//
// Request counts are kept in memory; limit hits are also stored in the
// database so they survive a restart.
package usage

import (
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Window is how far back the record looks.
const Window = 24 * time.Hour

const timeLayout = "2006-01-02T15:04:05.000Z"

// Stats is the last 24 hours for one service.
type Stats struct {
	Requests     int
	LimitHits    int
	LastLimitHit time.Time // zero when there were none
}

// Tracker records requests and limit hits per service. It is safe for
// concurrent use.
type Tracker struct {
	db  *sql.DB // nil keeps limit hits in memory only
	now func() time.Time

	// OnLimitHit, when set, is called after each limit hit. It must not block.
	OnLimitHit func(service string)

	mu       sync.Mutex
	requests map[string][]time.Time
	hits     map[string][]time.Time
}

// NewTracker creates a tracker and loads the limit hits stored in db within
// the window. db may be nil (memory only).
func NewTracker(db *sql.DB) (*Tracker, error) {
	t := &Tracker{db: db, now: time.Now, requests: map[string][]time.Time{}, hits: map[string][]time.Time{}}
	if db == nil {
		return t, nil
	}
	rows, err := db.Query(`SELECT service, at FROM service_limit_hits WHERE at >= ? ORDER BY at`, t.now().Add(-Window).UTC().Format(timeLayout))
	if err != nil {
		return nil, fmt.Errorf("load service limit hits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var service, at string
		if err := rows.Scan(&service, &at); err != nil {
			return nil, fmt.Errorf("scan service limit hit: %w", err)
		}
		when, err := time.Parse(timeLayout, at)
		if err != nil {
			continue // a malformed row is not worth failing startup over
		}
		t.hits[service] = append(t.hits[service], when)
	}
	return t, rows.Err()
}

// prune drops entries older than the window from list.
func prune(list []time.Time, now time.Time) []time.Time {
	cut := now.Add(-Window)
	i := 0
	for i < len(list) && list[i].Before(cut) {
		i++
	}
	return list[i:]
}

// RecordRequest counts one request to service.
func (t *Tracker) RecordRequest(service string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.requests[service] = append(prune(t.requests[service], now), now)
}

// RecordLimitHit notes that service refused a request because of a limit.
func (t *Tracker) RecordLimitHit(service string) {
	now := t.now()
	t.mu.Lock()
	t.hits[service] = append(prune(t.hits[service], now), now)
	t.mu.Unlock()
	if t.OnLimitHit != nil {
		t.OnLimitHit(service)
	}
	if t.db == nil {
		return
	}
	// Best effort: failing to store a hit must never break the request it
	// belongs to; the in-memory record still counts until restart.
	_, _ = t.db.Exec(`INSERT INTO service_limit_hits (service, at) VALUES (?, ?)`, service, now.UTC().Format(timeLayout))
	_, _ = t.db.Exec(`DELETE FROM service_limit_hits WHERE at < ?`, now.Add(-2*Window).UTC().Format(timeLayout))
}

// Stats returns the last 24 hours for service.
func (t *Tracker) Stats(service string) Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.requests[service] = prune(t.requests[service], now)
	t.hits[service] = prune(t.hits[service], now)
	st := Stats{Requests: len(t.requests[service]), LimitHits: len(t.hits[service])}
	if n := len(t.hits[service]); n > 0 {
		st.LastLimitHit = t.hits[service][n-1]
	}
	return st
}

// LimitFunc says whether a response means the service refused because of a limit.
type LimitFunc func(req *http.Request, resp *http.Response) bool

// OnTooManyRequests treats HTTP 429 as a limit hit.
func OnTooManyRequests(_ *http.Request, resp *http.Response) bool {
	return resp.StatusCode == http.StatusTooManyRequests
}

// Wrap returns a function that wraps an http.RoundTripper so every request is
// counted for service, and every response isLimit accepts is recorded as a
// limit hit.
func (t *Tracker) Wrap(service string, isLimit LimitFunc) func(http.RoundTripper) http.RoundTripper {
	return func(next http.RoundTripper) http.RoundTripper {
		if next == nil {
			next = http.DefaultTransport
		}
		return &countingTransport{tracker: t, service: service, isLimit: isLimit, next: next}
	}
}

type countingTransport struct {
	tracker *Tracker
	service string
	isLimit LimitFunc
	next    http.RoundTripper
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.tracker.RecordRequest(c.service)
	resp, err := c.next.RoundTrip(req)
	if err == nil && c.isLimit != nil && c.isLimit(req, resp) {
		c.tracker.RecordLimitHit(c.service)
	}
	return resp, err
}
