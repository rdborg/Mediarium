package api_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

// newFakeTraktServer serves a fixed two-item public-list response (one
// movie with a real tmdb id, one Trakt couldn't match to TMDB) regardless
// of which user/list is requested — enough to prove the import path end to
// end without a real Trakt client ID.
func newFakeTraktServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"rank": 1, "movie": {"title": "Fixture Movie One", "year": 2001, "ids": {"tmdb": 900}}},
			{"rank": 2, "movie": {"title": "No TMDB Match", "year": 2002, "ids": {"tmdb": null}}}
		]`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newFakeTMDBServer serves GetMovie-shaped responses for whatever id is
// requested, so the import handler's per-item enrichment call resolves to
// real-looking data without hitting the live TMDB API.
func newFakeTMDBServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/movie/900", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id": 900, "title": "Fixture Movie One", "overview": "A fixture movie.", "release_date": "2001-05-01", "poster_path": "/fixture.jpg"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestImportListEndToEnd proves the whole Trakt-list-URL -> TMDB-enriched
// Discover payload path works over the real HTTP API: parses the list URL,
// calls the (fake) Trakt API, enriches the one resolvable item via the
// (fake) TMDB API, and skips the unresolvable one rather than failing.
func TestImportListEndToEnd(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
	}
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	db, err := store.Open(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatalf("new api server: %v", err)
	}

	traktSrv := newFakeTraktServer(t)
	tmdbSrv := newFakeTMDBServer(t)
	// Point both live clients at the fixture servers instead of the real
	// APIs (tests use local fixtures, not live network calls).
	server.TestSetTraktBaseURL("fixture-trakt-client-id", traktSrv.URL)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", tmdbSrv.URL)

	httpSrv := httptest.NewServer(server.Routes())
	defer httpSrv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	listURL := "https://trakt.tv/users/someone/lists/a-list"
	results := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/discover/import-list?url="+listURL)

	if len(results) != 1 {
		t.Fatalf("expected 1 resolved item (the one with a tmdb id), got %d: %+v", len(results), results)
	}
	if results[0]["title"] != "Fixture Movie One" {
		t.Fatalf("expected the fixture movie's title, got %+v", results[0])
	}
	if results[0]["tmdbId"] != float64(900) {
		t.Fatalf("expected tmdbId 900, got %+v", results[0])
	}
}
