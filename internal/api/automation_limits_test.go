package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/store"
)

// newBudgetIndexer offers one release for each of "Budget Movie 001".."00N".
// Its NZB files never arrive until the returned function is called, so the
// downloads that start stay running.
func newBudgetIndexer(t *testing.T, movies int) (srv *httptest.Server, release func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
		for i := 1; i <= movies; i++ {
			fmt.Fprintf(&b, `<item><title>Budget.Movie.%03d.2000.1080p.WEB-DL.x264-GRP</title><guid>b%d</guid><enclosure url="http://%s/nzb/%d.nzb" length="1000" type="application/x-nzb"/><newznab:attr name="size" value="1000"/><newznab:attr name="category" value="2000"/></item>`, i, i, r.Host, i)
		}
		b.WriteString(`</channel></rss>`)
		fmt.Fprint(w, b.String())
	})
	mux.HandleFunc("/nzb/", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gate:
		case <-r.Context().Done():
		}
		http.Error(w, "not available", http.StatusNotFound)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, release
}

func TestScheduledHuntOnlyAddsAsManyDownloadsAsItMay(t *testing.T) {
	const wanted = 6
	tests := []struct {
		name       string
		beforehand int  // downloads the automatic searches added earlier in the hour
		open       bool // they are still in the line, not finished
		want       int  // downloads the hunt adds to the line
	}{
		{"an empty line takes what is wanted, and they wait their turn", 0, false, 6},
		{"a busy hour allows none", 10, false, 0},
		{"nine earlier this hour leaves room for one", 9, false, 1},
		{"a full line allows none", 10, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, httpSrv, client := newHuntTestServer(t)
			indexerSrv, release := newBudgetIndexer(t, wanted)
			signInStress(t, client, httpSrv.URL)
			postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
				"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "k",
			}, http.StatusCreated)

			for i := 1; i <= wanted; i++ {
				if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 100 + i, Title: fmt.Sprintf("Budget Movie %03d", i), Year: 2000, Monitored: true}); err != nil {
					t.Fatal(err)
				}
			}
			idle, err := server.MovieRepo.Add(library.Movie{TMDBID: 999, Title: "Something Else", Year: 1990, Monitored: false})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < tc.beforehand; i++ {
				id, err := server.QueueRepo.Enqueue(queue.Item{MovieID: idle.ID, ReleaseTitle: fmt.Sprintf("Old.Release.%d", i), NZBURL: "http://example.invalid/x.nzb"})
				if err != nil {
					t.Fatal(err)
				}
				status := queue.StatusCompleted
				if tc.open {
					status = queue.StatusDownloading
				}
				if err := server.QueueRepo.SetStatus(id, status, ""); err != nil {
					t.Fatal(err)
				}
			}

			server.TestHunt(context.Background())
			// A second run right after must not start more either.
			server.TestRSSSync(context.Background())
			server.TestHunt(context.Background())

			started := len(getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")) - tc.beforehand
			if started != tc.want {
				t.Fatalf("started %d downloads, want %d", started, tc.want)
			}
			release()
			waitBackground(t, server)
		})
	}
}

func signInStress(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	postJSON[map[string]any](t, client, baseURL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
}

// Safe mode: nothing runs by itself, the pages still work, and the dashboard
// says why.
func TestSafeModeSwitchesAutomationOff(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
		PauseAutomation:     true,
	}
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitBackground(t, server) })
	httpSrv := httptest.NewServer(server.Routes())
	t.Cleanup(httpSrv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	signInStress(t, client, httpSrv.URL)

	indexerSrv, release := newBudgetIndexer(t, 1)
	defer release()
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "k",
	}, http.StatusCreated)
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 101, Title: "Budget Movie 001", Year: 2000, Monitored: true}); err != nil {
		t.Fatal(err)
	}

	sched := server.StartAutomation()
	stopMonitor := server.StartMonitor()
	server.TestHunt(context.Background())
	server.TestRSSSync(context.Background())
	time.Sleep(200 * time.Millisecond)
	sched.Stop()
	stopMonitor()

	if q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue"); len(q) != 0 {
		t.Fatalf("safe mode started a download: %+v", q)
	}
	found := false
	for _, raw := range getJSON[map[string]any](t, client, httpSrv.URL+"/api/health")["items"].([]any) {
		if raw.(map[string]any)["id"] == "safe-mode" {
			found = true
		}
	}
	if !found {
		t.Fatal("the dashboard should say safe mode is on")
	}
	if got := getJSON[map[string]any](t, client, httpSrv.URL+"/api/settings")["automationEnabled"]; got != false {
		t.Fatalf("settings say automation is %v in safe mode, want off", got)
	}
}
