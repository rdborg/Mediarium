package migrate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeApp serves a Radarr/Sonarr/Prowlarr/SABnzbd API from the JSON
// fixtures in testdata, checks the API key, and records every request so a
// test can prove only GETs were sent.
type fakeApp struct {
	*httptest.Server
	t   *testing.T
	key string

	mu       sync.Mutex
	requests []string // "METHOD /path"
}

func (f *fakeApp) record(r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
}

// assertOnlyGET fails the test if anything but GET reached the app.
func (f *fakeApp) assertOnlyGET(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatalf("%s: expected requests, got none", f.URL)
	}
	for _, r := range f.requests {
		if !strings.HasPrefix(r, "GET ") {
			t.Fatalf("the importer sent %q; only GET is allowed", r)
		}
	}
}

func serveFixture(t *testing.T, w http.ResponseWriter, file string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Errorf("read fixture %s: %v", file, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// newFakeArr serves routes (path -> fixture file) behind the X-Api-Key
// header, the way Radarr, Sonarr and Prowlarr do.
func newFakeArr(t *testing.T, key string, routes map[string]string) *fakeApp {
	t.Helper()
	f := &fakeApp{t: t, key: key}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.URL.Query().Get("apikey") != "" {
			t.Errorf("API key sent in the query string to %s", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		file, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		serveFixture(t, w, file)
	}))
	t.Cleanup(f.Close)
	return f
}

func newFakeRadarr(t *testing.T, key string) *fakeApp {
	return newFakeArr(t, key, map[string]string{
		"/api/v3/system/status":  "radarr/system_status.json",
		"/api/v3/movie":          "radarr/movie.json",
		"/api/v3/qualityprofile": "radarr/qualityprofile.json",
		"/api/v3/rootfolder":     "radarr/rootfolder.json",
	})
}

func newFakeSonarr(t *testing.T, key string) *fakeApp {
	return newFakeArr(t, key, map[string]string{
		"/api/v3/system/status":  "sonarr/system_status.json",
		"/api/v3/series":         "sonarr/series.json",
		"/api/v3/qualityprofile": "sonarr/qualityprofile.json",
		"/api/v3/rootfolder":     "sonarr/rootfolder.json",
	})
}

func newFakeProwlarr(t *testing.T, key string) *fakeApp {
	return newFakeArr(t, key, map[string]string{
		"/api/v1/system/status": "prowlarr/system_status.json",
		"/api/v1/indexer":       "prowlarr/indexer.json",
	})
}

// newFakeSABnzbd answers api?mode=version without a key and everything
// else only with the right apikey, as SABnzbd does.
func newFakeSABnzbd(t *testing.T, key string) *fakeApp {
	t.Helper()
	f := &fakeApp{t: t, key: key}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.URL.Path != "/api" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("output") != "json" {
			t.Errorf("SABnzbd request without output=json: %s", r.URL.RawQuery)
		}
		switch q.Get("mode") {
		case "version":
			serveFixture(t, w, "sabnzbd/version.json")
		case "get_config":
			if q.Get("apikey") != key {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"status": false, "error": "API Key Incorrect"}`)
				return
			}
			serveFixture(t, w, "sabnzbd/get_config.json")
		default:
			t.Errorf("unexpected SABnzbd mode %q", q.Get("mode"))
			http.Error(w, "not implemented", http.StatusBadRequest)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// newFakeTMDB serves the movies and shows the fixtures refer to.
func newFakeTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	enc := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	movie := func(id int, title, date string) {
		mux.HandleFunc(fmt.Sprintf("/movie/%d", id), func(w http.ResponseWriter, r *http.Request) {
			enc(w, map[string]any{"id": id, "title": title, "release_date": date, "overview": "o", "poster_path": "/p.jpg",
				"genres": []map[string]any{{"id": 28, "name": "Action"}}})
		})
	}
	movie(27205, "Inception", "2010-07-15")
	movie(949, "Heat", "1995-12-15")
	movie(693134, "Dune: Part Two", "2024-02-27")
	movie(603, "The Matrix", "1999-03-30")

	show := func(id int, name, date string, seasons map[int]int) {
		var list []map[string]any
		list = append(list, map[string]any{"season_number": 0, "episode_count": 3})
		for n, count := range seasons {
			list = append(list, map[string]any{"season_number": n, "episode_count": count})
			mux.HandleFunc(fmt.Sprintf("/tv/%d/season/%d", id, n), func(w http.ResponseWriter, r *http.Request) {
				var eps []map[string]any
				for e := 1; e <= count; e++ {
					eps = append(eps, map[string]any{"season_number": n, "episode_number": e, "name": fmt.Sprintf("Episode %d", e), "air_date": date})
				}
				enc(w, map[string]any{"episodes": eps})
			})
		}
		mux.HandleFunc(fmt.Sprintf("/tv/%d", id), func(w http.ResponseWriter, r *http.Request) {
			enc(w, map[string]any{"id": id, "name": name, "first_air_date": date, "seasons": list,
				"genres": []map[string]any{{"id": 18, "name": "Drama"}}})
		})
	}
	show(1396, "Breaking Bad", "2008-01-20", map[int]int{1: 7, 2: 13})
	show(5555, "Fixture Old Show", "2015-03-01", map[int]int{1: 5, 2: 5})

	mux.HandleFunc("/find/12345", func(w http.ResponseWriter, r *http.Request) {
		enc(w, map[string]any{"movie_results": []any{}, "tv_results": []map[string]any{{"id": 5555, "name": "Fixture Old Show", "first_air_date": "2015-03-01"}}})
	})
	mux.HandleFunc("/find/81189", func(w http.ResponseWriter, r *http.Request) {
		enc(w, map[string]any{"movie_results": []any{}, "tv_results": []map[string]any{{"id": 1396, "name": "Breaking Bad", "first_air_date": "2008-01-20"}}})
	})
	mux.HandleFunc("/find/4242", func(w http.ResponseWriter, r *http.Request) {
		enc(w, map[string]any{"movie_results": []any{}, "tv_results": []map[string]any{{"id": 7777, "name": "Gone Show", "first_air_date": "1999-01-01"}}})
	})
	mux.HandleFunc("/find/99999", func(w http.ResponseWriter, r *http.Request) {
		enc(w, map[string]any{"movie_results": []any{}, "tv_results": []any{}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
