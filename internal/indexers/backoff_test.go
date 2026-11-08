package indexers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/store"
)

// fakeClock is a clock the test moves by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)} }

func TestBackoffFor(t *testing.T) {
	min := time.Minute
	tests := []struct {
		name        string
		failures    int
		rateLimited bool
		retryAfter  time.Duration
		want        time.Duration
	}{
		{"first failure is not paused", 1, false, 0, 0},
		{"second failure is not paused", 2, false, 0, 0},
		{"third failure pauses five minutes", 3, false, 0, 5 * min},
		{"fourth doubles", 4, false, 0, 10 * min},
		{"fifth doubles again", 5, false, 0, 20 * min},
		{"eighth is two hours forty", 8, false, 0, 160 * min},
		{"tenth is capped at six hours", 10, false, 0, 6 * time.Hour},
		{"far beyond stays at the cap", 200, false, 0, 6 * time.Hour},
		{"rate limit pauses from the first", 1, true, 0, 5 * min},
		{"rate limit doubles", 2, true, 0, 10 * min},
		{"retry-after longer than the back-off wins", 1, true, 30 * min, 30 * min},
		{"retry-after shorter than the back-off loses", 4, false, 1 * min, 10 * min},
		{"retry-after alone pauses even one failure", 1, false, 15 * min, 15 * min},
		{"retry-after is capped", 1, true, 48 * time.Hour, 6 * time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := indexers.BackoffFor(tc.failures, tc.rateLimited, tc.retryAfter); got != tc.want {
				t.Fatalf("BackoffFor(%d, %v, %v) = %v, want %v", tc.failures, tc.rateLimited, tc.retryAfter, got, tc.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name, header string
		want         time.Duration
	}{
		{"empty", "", 0},
		{"seconds", "120", 2 * time.Minute},
		{"seconds with spaces", " 30 ", 30 * time.Second},
		{"negative", "-5", 0},
		{"garbage", "soon", 0},
		{"a date in the future", now.Add(90 * time.Second).UTC().Format(http.TimeFormat), 90 * time.Second},
		{"a date in the past", now.Add(-time.Hour).UTC().Format(http.TimeFormat), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := indexers.ParseRetryAfter(tc.header, now); got != tc.want {
				t.Fatalf("ParseRetryAfter(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

func trackerWithIndexers(t *testing.T, clock *fakeClock, names ...string) (*indexers.Tracker, []indexers.Instance, func() *indexers.Tracker) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	repo := indexers.NewRepo(db, box)
	var list []indexers.Instance
	for _, n := range names {
		inst, err := repo.Create(indexers.Instance{Name: n, BaseURL: "https://" + strings.ToLower(n) + ".test", APIKey: "k", Enabled: true}, nil)
		if err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
		list = append(list, inst)
	}
	tr := indexers.NewTracker(db, clock.now)
	for i := range list {
		list[i].Tracker = tr
	}
	// again opens a second tracker over the same database, as after a restart.
	again := func() *indexers.Tracker { return indexers.NewTracker(db, clock.now) }
	return tr, list, again
}

func TestTrackerPausesAndResets(t *testing.T) {
	clock := newClock()
	tr, list, _ := trackerWithIndexers(t, clock, "Alpha")
	id := list[0].ID
	down := errors.New("dial tcp 10.0.0.1:443: connect: connection refused")

	for i := 1; i <= 2; i++ {
		tr.Failed(id, down)
		if _, paused := tr.PausedUntil(id); paused {
			t.Fatalf("paused after only %d failures", i)
		}
	}
	h := tr.Failed(id, down)
	if h.Failures != 3 {
		t.Fatalf("failures = %d, want 3", h.Failures)
	}
	until, paused := tr.PausedUntil(id)
	if !paused || !until.Equal(clock.t.Add(5*time.Minute)) {
		t.Fatalf("after 3 failures want paused until %v, got %v (paused %v)", clock.t.Add(5*time.Minute), until, paused)
	}
	if !strings.Contains(h.Reason, "could not be reached") {
		t.Fatalf("reason = %q", h.Reason)
	}

	clock.advance(5*time.Minute + time.Second)
	if _, paused := tr.PausedUntil(id); paused {
		t.Fatal("still paused after the pause ended")
	}
	// the first try after a pause fails again: a longer pause
	tr.Failed(id, down)
	until, _ = tr.PausedUntil(id)
	if want := clock.t.Add(10 * time.Minute); !until.Equal(want) {
		t.Fatalf("second pause ends %v, want %v", until, want)
	}

	// a first success clears it all
	tr.Succeeded(id)
	if _, ok := tr.Health(id); ok {
		t.Fatal("health kept after a success")
	}
	tr.Failed(id, down)
	if _, paused := tr.PausedUntil(id); paused {
		t.Fatal("one failure after a success must not pause")
	}
}

func TestTrackerRetryAfterAndResume(t *testing.T) {
	clock := newClock()
	tr, list, _ := trackerWithIndexers(t, clock, "Alpha")
	id := list[0].ID
	limited := &indexers.StatusError{Status: 429, RetryAfter: 40 * time.Minute}
	tr.Failed(id, fmt.Errorf("search: %w", limited))
	until, paused := tr.PausedUntil(id)
	if !paused || !until.Equal(clock.t.Add(40*time.Minute)) {
		t.Fatalf("429 with Retry-After 40 minutes: paused=%v until=%v", paused, until)
	}
	h, _ := tr.Health(id)
	if !strings.Contains(h.Reason, "too many requests") {
		t.Fatalf("reason = %q", h.Reason)
	}
	tr.Resume(id)
	if _, paused := tr.PausedUntil(id); paused {
		t.Fatal("still paused after Resume")
	}
}

func TestTrackerRemembersPausesAcrossRestart(t *testing.T) {
	clock := newClock()
	tr, list, again := trackerWithIndexers(t, clock, "Alpha")
	id := list[0].ID
	for i := 0; i < 4; i++ {
		tr.Failed(id, errors.New("unexpected status 503"))
	}
	want, _ := tr.PausedUntil(id)

	restarted := again()
	got, paused := restarted.PausedUntil(id)
	if !paused || !got.Equal(want) {
		t.Fatalf("after restart paused=%v until=%v, want until %v", paused, got, want)
	}
	h, _ := restarted.Health(id)
	if h.Failures != 4 {
		t.Fatalf("failures after restart = %d, want 4", h.Failures)
	}
	restarted.Succeeded(id)
	if _, ok := again().Health(id); ok {
		t.Fatal("a cleared pause came back after a restart")
	}
}

func TestPausedMessage(t *testing.T) {
	loc := time.UTC
	h := indexers.Health{Failures: 3, PausedUntil: time.Date(2026, 10, 9, 14, 20, 0, 0, loc), LastFailure: time.Date(2026, 10, 9, 14, 15, 0, 0, loc), Reason: "it could not be reached"}
	got := indexers.PausedMessage("Alpha", h, loc)
	want := "Alpha is paused until 14:20 after 3 failures in a row, because it could not be reached."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSearchAllSkipsPausedIndexerAndRetriesAfterPause(t *testing.T) {
	var hits atomic.Int32
	var failing atomic.Bool
	failing.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if failing.Load() {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(fixtureRSS))
	}))
	defer srv.Close()

	clock := newClock()
	tr, list, _ := trackerWithIndexers(t, clock, "Alpha")
	list[0].BaseURL = srv.URL

	search := func() indexers.Outcome {
		out := indexers.SearchAll(context.Background(), list, "matrix", nil)
		if len(out) != 1 {
			t.Fatalf("outcomes = %d", len(out))
		}
		return out[0]
	}
	for i := 0; i < 3; i++ {
		if o := search(); o.Err == nil || o.Skipped {
			t.Fatalf("search %d should fail for real, got %+v", i+1, o)
		}
	}
	if hits.Load() != 3 {
		t.Fatalf("hits = %d, want 3", hits.Load())
	}
	// paused now: not asked, no error
	o := search()
	if !o.Skipped || o.Err != nil || !o.PausedUntil.Equal(clock.t.Add(5*time.Minute)) {
		t.Fatalf("expected a skipped outcome until +5m, got %+v", o)
	}
	if hits.Load() != 3 {
		t.Fatalf("a paused indexer was asked: hits = %d", hits.Load())
	}
	// after the pause it is asked again, and a success clears the count
	clock.advance(6 * time.Minute)
	failing.Store(false)
	o = search()
	if o.Skipped || o.Err != nil || len(o.Results) != 1 {
		t.Fatalf("expected a working search after the pause, got %+v", o)
	}
	if _, ok := tr.Health(list[0].ID); ok {
		t.Fatal("success did not clear the failures")
	}
	// counters: the skipped search is not counted, so 4 searches, 3 failed
	stats, err := tr.Stats(clock.t.AddDate(0, 0, -1))
	if err != nil || len(stats) != 1 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	if s := stats[0]; s.Queries != 4 || s.Failures != 3 || s.Results != 1 {
		t.Fatalf("stats = %+v, want 4 queries, 3 failures, 1 result", s)
	}
	// A grab of that release counts with its size (#167).
	tr.NoteGrab(o.Results[0].DownloadURL, 5<<30)
	if stats, _ = tr.Stats(clock.t.AddDate(0, 0, -1)); stats[0].Grabs != 1 || stats[0].GrabBytes != 5<<30 {
		t.Fatalf("stats after a grab = %+v, want 1 grab of 5 GiB", stats[0])
	}
}

func TestSearchAllCancelledSearchIsNotAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	clock := newClock()
	tr, list, _ := trackerWithIndexers(t, clock, "Alpha")
	list[0].BaseURL = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	indexers.SearchAll(ctx, list, "matrix", nil)
	if _, ok := tr.Health(list[0].ID); ok {
		t.Fatal("a search cancelled by the caller was counted as a failure")
	}
}
