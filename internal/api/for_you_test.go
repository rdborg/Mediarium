package api_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/store"
)

// newForYouTMDBServer serves per-movie "similar" lists designed to exercise
// both behaviors handleForYou has to get right:
//   - movie 300 is recommended by BOTH seeds (100 and 200), so it must rank
//     above the movies only one seed recommended (400, 500)
//   - movie 200 appears in seed 100's similar list but is itself already in
//     the library, so it must not be recommended back to the user
func newForYouTMDBServer(t *testing.T) *httptest.Server {
	t.Helper()
	recent := time.Now().Year() - 1
	// Release year and poster per movie: 600 is old, 700 has no poster.
	year := map[int]int{300: recent, 400: recent - 1, 500: recent - 2, 600: 1985, 700: recent, 200: recent}
	mux := http.NewServeMux()
	writeSimilar := func(w http.ResponseWriter, ids ...int) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"page": 1, "results": [`)
		for i, id := range ids {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			poster := fmt.Sprintf("/p%d.jpg", id)
			if id == 700 {
				poster = ""
			}
			fmt.Fprintf(w, `{"id": %d, "title": "Movie %d", "release_date": "%d-01-01", "poster_path": %q, "popularity": %d}`, id, id, year[id], poster, 100-id/10)
		}
		fmt.Fprint(w, `]}`)
	}
	mux.HandleFunc("/movie/100/similar", func(w http.ResponseWriter, r *http.Request) {
		writeSimilar(w, 300, 400, 200, 600)
	})
	mux.HandleFunc("/movie/200/similar", func(w http.ResponseWriter, r *http.Request) {
		writeSimilar(w, 300, 500, 700)
	})
	// A show in the library points to two shows.
	mux.HandleFunc("/tv/900/recommendations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"page": 1, "results": [
			{"id": 910, "name": "Show 910", "first_air_date": "%d-05-01", "poster_path": "/s910.jpg", "popularity": 50},
			{"id": 920, "name": "Show 920", "first_air_date": "1990-05-01", "poster_path": "/s920.jpg", "popularity": 90}]}`, recent)
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
	if len(ids) != 4 {
		t.Fatalf("expected 4 recommendations (300, 400, 500 and the one without a poster, 700; not the two library movies or the 1985 one), got %v", ids)
	}
	if ids[0] != 300 {
		t.Fatalf("expected 300 first (recommended by both seeds), got order %v", ids)
	}
	got := map[int]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if ids[len(ids)-1] != 700 {
		t.Errorf("the movie without a poster should come last, got order %v", ids)
	}
	for _, want := range []int{300, 400, 500, 700} {
		if !got[want] {
			t.Errorf("expected %d in the recommendations, got %v", want, ids)
		}
	}
	for _, unwanted := range []int{100, 200, 600} {
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

// The paged list behind "Browse more": recent titles by default, older ones
// when asked, never the library's own, one page at a time, for movies and shows.
func TestDiscoverListSimilarPagesAndFilters(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newForYouTMDBServer(t).URL)
	for _, m := range []library.Movie{{TMDBID: 100, Title: "Seed One", Year: 2000}, {TMDBID: 200, Title: "Seed Two", Year: 2001}} {
		if _, err := server.MovieRepo.Add(m); err != nil {
			t.Fatalf("seed movie %d: %v", m.TMDBID, err)
		}
	}
	if _, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 900, Title: "Seed Show", Year: 2010}, nil); err != nil {
		t.Fatalf("seed show: %v", err)
	}

	idsOf := func(page map[string]any) []int {
		var out []int
		for _, r := range page["results"].([]any) {
			out = append(out, int(r.(map[string]any)["tmdbId"].(float64)))
		}
		return out
	}
	tests := []struct {
		name      string
		query     string
		want      []int
		wantTotal float64
	}{
		{"movies, recent only", "kind=movie&list=similar", []int{300, 400, 500, 700}, 4},
		{"movies, older allowed", "kind=movie&list=similar&older=1", []int{300, 400, 500, 600, 700}, 5},
		{"movies, a page past the end is empty", "kind=movie&list=similar&page=2", nil, 4},
		{"shows, recent only", "kind=tv&list=similar", []int{910}, 1},
		{"shows, older allowed", "kind=tv&list=similar&older=true", []int{910, 920}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := getJSON[map[string]any](t, client, base+"/api/discover/list?"+tt.query)
			if got := idsOf(page); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			if page["totalResults"] != tt.wantTotal {
				t.Errorf("totalResults = %v, want %v", page["totalResults"], tt.wantTotal)
			}
			if page["totalPages"].(float64) < 1 {
				t.Errorf("totalPages should be at least 1, got %v", page["totalPages"])
			}
			for _, r := range page["results"].([]any) {
				if r.(map[string]any)["inLibrary"] == true {
					t.Errorf("a title in the library was listed: %v", r)
				}
			}
		})
	}
}
