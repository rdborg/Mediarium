package api_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanborg/mediarium/internal/api"
	"github.com/ryanborg/mediarium/internal/config"
	"github.com/ryanborg/mediarium/internal/crypto"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/store"
)

// newForYouTMDBServer serves per-movie "similar" lists designed to exercise
// both behaviors handleForYou has to get right:
//   - movie 300 is recommended by BOTH seeds (100 and 200), so it must rank
//     above the movies only one seed recommended (400, 500)
//   - movie 200 appears in seed 100's similar list but is itself already in
//     the library, so it must not be recommended back to the user
func newForYouTMDBServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	writeSimilar := func(w http.ResponseWriter, ids ...int) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"page": 1, "results": [`)
		for i, id := range ids {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"id": %d, "title": "Movie %d", "release_date": "2000-01-01", "poster_path": "/p%d.jpg"}`, id, id, id)
		}
		fmt.Fprint(w, `]}`)
	}
	mux.HandleFunc("/movie/100/similar", func(w http.ResponseWriter, r *http.Request) {
		writeSimilar(w, 300, 400, 200)
	})
	mux.HandleFunc("/movie/200/similar", func(w http.ResponseWriter, r *http.Request) {
		writeSimilar(w, 300, 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestForYouRanksByAgreementAndExcludesLibrary proves the aggregate
// recommendation rail does what makes it different from
// the per-title similar-movies view: ranks by how many library seeds agree
// on a recommendation, and never recommends something already owned.
func TestForYouRanksByAgreementAndExcludesLibrary(t *testing.T) {
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
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newForYouTMDBServer(t).URL)

	for _, m := range []library.Movie{
		{TMDBID: 100, Title: "Seed One", Year: 2000, Monitored: true},
		{TMDBID: 200, Title: "Seed Two", Year: 2000, Monitored: true},
	} {
		if _, err := server.MovieRepo.Add(m); err != nil {
			t.Fatalf("seed library movie %d: %v", m.TMDBID, err)
		}
	}

	httpSrv := httptest.NewServer(server.Routes())
	defer httpSrv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	results := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/discover/for-you")

	ids := make([]int, len(results))
	for i, r := range results {
		ids[i] = int(r["tmdbId"].(float64))
	}
	if len(ids) != 3 {
		t.Fatalf("expected exactly 3 recommendations (300, 400, 500 — not the two library movies), got %v", ids)
	}
	if ids[0] != 300 {
		t.Fatalf("expected 300 first (recommended by both seeds), got order %v", ids)
	}
	got := map[int]bool{}
	for _, id := range ids {
		got[id] = true
	}
	for _, want := range []int{300, 400, 500} {
		if !got[want] {
			t.Errorf("expected %d in the recommendations, got %v", want, ids)
		}
	}
	for _, unwanted := range []int{100, 200} {
		if got[unwanted] {
			t.Errorf("did not expect library movie %d to be recommended back, got %v", unwanted, ids)
		}
	}
}

// TestForYouEmptyLibraryReturnsEmpty proves there's nothing to seed from
// yet is a clean empty result, not an error or a crash.
func TestForYouEmptyLibraryReturnsEmpty(t *testing.T) {
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
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newForYouTMDBServer(t).URL)

	httpSrv := httptest.NewServer(server.Routes())
	defer httpSrv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	results := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/discover/for-you")
	if len(results) != 0 {
		t.Fatalf("expected an empty result for an empty library, got %+v", results)
	}
}
