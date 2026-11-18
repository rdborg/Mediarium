package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/monitor"
)

type probeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *probeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *probeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeNewznab answers t=caps and t=search and counts each kind.
type fakeNewznab struct {
	*httptest.Server
	mu       sync.Mutex
	searches int
	caps     int
	capsBody string
}

func newFakeNewznab(t *testing.T) *fakeNewznab {
	f := &fakeNewznab{capsBody: `<?xml version="1.0"?><caps><server title="fake"/></caps>`}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Query().Get("t") {
		case "caps":
			f.caps++
			w.Write([]byte(f.capsBody))
		case "search":
			f.searches++
			w.Write([]byte(`<?xml version="1.0"?><rss><channel></channel></rss>`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeNewznab) counts() (searches, caps int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.searches, f.caps
}

// The monitor's check of an indexer asks the capabilities question, never a
// search, and leaves an indexer alone that has just answered a real search.
func TestMonitorProbeSpendsNoSearch(t *testing.T) {
	restore := indexers.SetSearchGaps(0, 0)
	defer restore()
	fake := newFakeNewznab(t)
	clock := &probeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	tr := indexers.NewTracker(nil, clock.now)
	inst := indexers.Instance{ID: 7, Name: "Slug", BaseURL: fake.URL, APIKey: "key", Enabled: true, Tracker: tr}
	s := &Server{}
	ctx := context.Background()

	// nothing is known yet: one capabilities call, no search
	if err := s.probeIndexer(ctx, inst); err != nil {
		t.Fatalf("first check: %v", err)
	}
	if searches, caps := fake.counts(); searches != 0 || caps != 1 {
		t.Fatalf("searches = %d, caps = %d; want 0 and 1", searches, caps)
	}

	// the schedule comes round every half hour: within six hours nothing is asked
	for i := 0; i < 5; i++ {
		clock.advance(30 * time.Minute)
		if err := s.probeIndexer(ctx, inst); !errors.Is(err, monitor.ErrSkip) {
			t.Fatalf("check %d within six hours should be skipped, got %v", i+2, err)
		}
	}
	if searches, caps := fake.counts(); searches != 0 || caps != 1 {
		t.Fatalf("searches = %d, caps = %d; want 0 and 1", searches, caps)
	}

	// a real search worked since: the indexer is known to be fine, nothing is asked
	clock.advance(time.Hour)
	if out := indexers.SearchAll(ctx, []indexers.Instance{inst}, "matrix", nil); out[0].Err != nil {
		t.Fatal(out[0].Err)
	}
	searches, caps := fake.counts()
	if searches != 1 {
		t.Fatalf("the real search should have been asked once, searches = %d", searches)
	}
	if err := s.probeIndexer(ctx, inst); err != nil {
		t.Fatalf("check after a working search: %v", err)
	}
	if s2, c2 := fake.counts(); s2 != searches || c2 != caps {
		t.Fatalf("a check after a working search asked the indexer: searches %d, caps %d", s2, c2)
	}

	// long after that, with no news, it is asked again with caps, not a search
	clock.advance(8 * time.Hour)
	if err := s.probeIndexer(ctx, inst); err != nil {
		t.Fatalf("late check: %v", err)
	}
	if s2, c2 := fake.counts(); s2 != searches || c2 != caps+1 {
		t.Fatalf("searches = %d, caps = %d; want %d and %d", s2, c2, searches, caps+1)
	}

	// "Check now" asks whatever the schedule says
	if err := s.probeIndexer(context.WithValue(ctx, forceProbeKey{}, true), inst); err != nil {
		t.Fatalf("forced check: %v", err)
	}
	if _, c2 := fake.counts(); c2 != caps+2 {
		t.Fatalf("a forced check should ask, caps = %d", c2)
	}
}

func TestMonitorProbeFailsAndLearnsLimit(t *testing.T) {
	fake := newFakeNewznab(t)
	clock := &probeClock{t: time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC)}
	tr := indexers.NewTracker(nil, clock.now)
	inst := indexers.Instance{ID: 3, Name: "Slug", BaseURL: fake.URL, APIKey: "key", Enabled: true, Tracker: tr}
	s := &Server{}
	ctx := context.Background()

	fake.capsBody = `<?xml version="1.0"?><error code="100" description="Incorrect user credentials"/>`
	err := s.probeIndexer(ctx, inst)
	if err == nil || errors.Is(err, monitor.ErrSkip) || !strings.Contains(err.Error(), "Incorrect user credentials") {
		t.Fatalf("a refused key should fail the check, got %v", err)
	}

	// over the daily limit is not "down": the check is skipped and the limit learned
	clock.advance(7 * time.Hour)
	fake.capsBody = `<?xml version="1.0"?><error code="429" description="Request limit reached (26/25)"/>`
	if err := s.probeIndexer(ctx, inst); !errors.Is(err, monitor.ErrSkip) {
		t.Fatalf("over the limit should be skipped, got %v", err)
	}
	if q := tr.Quota(inst.ID, 0); q.Limit != 25 || !q.Blocked {
		t.Fatalf("limit not learned: %+v", q)
	}
	// and while it is used up it is not asked at all
	clock.advance(7 * time.Hour)
	_, before := fake.counts()
	if err := s.probeIndexer(ctx, inst); !errors.Is(err, monitor.ErrSkip) {
		t.Fatalf("got %v", err)
	}
	if _, after := fake.counts(); after != before {
		t.Fatal("an indexer over its limit was asked")
	}
}

// A check that works lifts the pause an indexer was under after failures, and
// a paused indexer is checked after half an hour, so one that came back (or
// whose key was refused for a while) is not left out of searches for hours.
func TestMonitorProbeLiftsAPause(t *testing.T) {
	fake := newFakeNewznab(t)
	clock := &probeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	tr := indexers.NewTracker(nil, clock.now)
	inst := indexers.Instance{ID: 7, Name: "Slug", BaseURL: fake.URL, APIKey: "key", Enabled: true, Tracker: tr}
	s := &Server{}
	ctx := context.Background()

	for i := 0; i < 6; i++ {
		tr.Failed(inst.ID, errors.New("newznab error from Slug: Incorrect user credentials (code 100)"))
	}
	if _, paused := tr.PausedUntil(inst.ID); !paused {
		t.Fatal("the indexer should be paused")
	}
	// The first check was a while ago (the pause itself shows it is not new).
	tr.NoteProbe(inst.ID)
	clock.advance(10 * time.Minute)
	if err := s.probeIndexer(ctx, inst); !errors.Is(err, monitor.ErrSkip) {
		t.Fatalf("10 minutes later it is too soon, got %v", err)
	}
	if _, paused := tr.PausedUntil(inst.ID); !paused {
		t.Fatal("a skipped check lifted the pause")
	}
	clock.advance(25 * time.Minute)
	if err := s.probeIndexer(ctx, inst); err != nil {
		t.Fatalf("check of a paused indexer: %v", err)
	}
	if _, paused := tr.PausedUntil(inst.ID); paused {
		t.Fatal("a check that worked did not lift the pause")
	}
	if h, ok := tr.Health(inst.ID); ok {
		t.Fatalf("the failures should be forgotten, got %+v", h)
	}

	// A check that fails does not lift it.
	fake.mu.Lock()
	fake.capsBody = `<error code="100" description="Incorrect user credentials"/>`
	fake.mu.Unlock()
	for i := 0; i < 6; i++ {
		tr.Failed(inst.ID, errors.New("newznab error from Slug: Incorrect user credentials (code 100)"))
	}
	clock.advance(35 * time.Minute)
	if err := s.probeIndexer(ctx, inst); err == nil {
		t.Fatal("the check should have failed")
	}
	if _, ok := tr.Health(inst.ID); !ok {
		t.Fatal("a failed check cleared the failures")
	}
}
