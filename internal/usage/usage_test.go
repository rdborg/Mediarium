package usage

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/store"
)

func TestRollingWindow(t *testing.T) {
	tr, err := NewTracker(nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	tr.now = func() time.Time { return now }

	tr.RecordRequest("trakt")
	tr.RecordLimitHit("trakt")
	now = now.Add(20 * time.Hour)
	tr.RecordRequest("trakt")
	tr.RecordRequest("tmdb")
	if st := tr.Stats("trakt"); st.Requests != 2 || st.LimitHits != 1 {
		t.Fatalf("after 20h: %+v", st)
	}

	// The first request and the limit hit are now more than 24 hours old.
	now = now.Add(5 * time.Hour)
	st := tr.Stats("trakt")
	if st.Requests != 1 || st.LimitHits != 0 || !st.LastLimitHit.IsZero() {
		t.Fatalf("after 25h: %+v", st)
	}
	// Services are separate.
	if st := tr.Stats("tmdb"); st.Requests != 1 {
		t.Fatalf("tmdb: %+v", st)
	}
	if st := tr.Stats("opensubtitles"); st != (Stats{}) {
		t.Fatalf("unused service: %+v", st)
	}
}

func TestLimitHitsSurviveARestart(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	first, err := NewTracker(db)
	if err != nil {
		t.Fatal(err)
	}
	first.RecordRequest("trakt")
	first.RecordLimitHit("trakt")
	first.RecordLimitHit("trakt")
	first.RecordLimitHit("opensubtitles")

	second, err := NewTracker(db) // as after a restart
	if err != nil {
		t.Fatal(err)
	}
	if st := second.Stats("trakt"); st.LimitHits != 2 || st.Requests != 0 || st.LastLimitHit.IsZero() {
		t.Fatalf("trakt after restart: %+v (requests are in memory only)", st)
	}
	if st := second.Stats("opensubtitles"); st.LimitHits != 1 {
		t.Fatalf("opensubtitles after restart: %+v", st)
	}

	// Hits older than the window are not loaded.
	if _, err := db.Exec(`INSERT INTO service_limit_hits (service, at) VALUES ('tmdb', ?)`, time.Now().Add(-30*time.Hour).UTC().Format(timeLayout)); err != nil {
		t.Fatal(err)
	}
	third, _ := NewTracker(db)
	if st := third.Stats("tmdb"); st.LimitHits != 0 {
		t.Fatalf("old hit was loaded: %+v", st)
	}
}

func TestWrapCountsRequestsAndLimitHits(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	defer srv.Close()

	tr, _ := NewTracker(nil)
	client := &http.Client{Transport: tr.Wrap("trakt", OnTooManyRequests)(nil)}

	tests := []struct {
		name         string
		status       int
		wantRequests int
		wantHits     int
	}{
		{"ok", http.StatusOK, 1, 0},
		{"not found is not a limit", http.StatusNotFound, 2, 0},
		{"429 is a limit hit", http.StatusTooManyRequests, 3, 1},
		{"another 429", http.StatusTooManyRequests, 4, 2},
		{"server error is not a limit", http.StatusInternalServerError, 5, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status = tc.status
			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if st := tr.Stats("trakt"); st.Requests != tc.wantRequests || st.LimitHits != tc.wantHits {
				t.Fatalf("stats = %+v, want %d requests and %d hits", st, tc.wantRequests, tc.wantHits)
			}
		})
	}

	// A failed connection still counts as a request, but is not a limit hit.
	srv.Close()
	if _, err := client.Get(srv.URL); err == nil {
		t.Fatal("expected a connection error")
	}
	if st := tr.Stats("trakt"); st.Requests != 6 || st.LimitHits != 2 {
		t.Fatalf("after a connection error: %+v", st)
	}
}

func TestLimitHitsAreAnnounced(t *testing.T) {
	tr, err := NewTracker(nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	tr.OnLimitHit = func(service string) { got = append(got, service) }
	tr.RecordLimitHit("tmdb")
	tr.RecordRequest("tmdb")
	tr.RecordLimitHit("trakt")
	if len(got) != 2 || got[0] != "tmdb" || got[1] != "trakt" {
		t.Fatalf("announced %v", got)
	}
}
