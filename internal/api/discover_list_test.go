package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiscoverList(t *testing.T) {
	server, base, _, member, _ := familyServer(t)

	var (
		mu    sync.Mutex
		paths []string
		query string
	)
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	record := func(r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		query = r.URL.RawQuery
		mu.Unlock()
	}
	movies := func(w http.ResponseWriter, r *http.Request) {
		record(r)
		write(w, map[string]any{"page": 2, "total_pages": 740, "total_results": 14781, "results": []map[string]any{
			{"id": 603, "title": "The Matrix", "release_date": "1999-03-31", "genre_ids": []int{28}},
			{"id": 777, "title": "Coming Soon", "release_date": "2026-10-12", "genre_ids": []int{878}},
			{"id": 778, "title": "No Date", "release_date": ""},
		}})
	}
	shows := func(w http.ResponseWriter, r *http.Request) {
		record(r)
		write(w, map[string]any{"page": 1, "total_pages": 3, "total_results": 55, "results": []map[string]any{
			{"id": 1399, "name": "Fixture Show", "first_air_date": "2026-11-01", "genre_ids": []int{18}},
		}})
	}
	mux.HandleFunc("/trending/movie/week", movies)
	mux.HandleFunc("/movie/popular", movies)
	mux.HandleFunc("/discover/movie", movies)
	mux.HandleFunc("/trending/tv/week", shows)
	mux.HandleFunc("/tv/popular", shows)
	mux.HandleFunc("/discover/tv", shows)
	mux.HandleFunc("/movie/603", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"id": 603, "title": "The Matrix", "release_date": "1999-03-31"})
	})
	mux.HandleFunc("/genre/movie/list", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"genres": []map[string]any{{"id": 28, "name": "Action"}, {"id": 878, "name": "Science Fiction"}}})
	})
	mux.HandleFunc("/genre/tv/list", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"genres": []map[string]any{{"id": 18, "name": "Drama"}}})
	})
	tmdb := httptest.NewServer(mux)
	t.Cleanup(tmdb.Close)
	server.TestSetTMDBBaseURL("k", tmdb.URL)
	postJSON[map[string]any](t, member, base+"/api/movies", map[string]any{"tmdbId": 603}, http.StatusCreated)

	// Every rail maps to its TMDB list.
	cases := []struct {
		query, path string
	}{
		{"list=trending&kind=movie&page=2", "/trending/movie/week"},
		{"list=popular&kind=movie&page=2", "/movie/popular"},
		{"list=upcoming&kind=movie&page=2", "/discover/movie"},
		{"list=trending&kind=tv", "/trending/tv/week"},
		{"list=popular&kind=tv", "/tv/popular"},
		{"list=upcoming&kind=tv&page=1", "/discover/tv"},
	}
	for _, tc := range cases {
		mu.Lock()
		paths = nil
		mu.Unlock()
		page := getJSON[map[string]any](t, member, base+"/api/discover/list?"+tc.query)
		mu.Lock()
		got := strings.Join(paths, ",")
		mu.Unlock()
		if !strings.Contains(got, tc.path) {
			t.Fatalf("%s: TMDB paths %q, want %s", tc.query, got, tc.path)
		}
		if _, ok := page["results"].([]any); !ok {
			t.Fatalf("%s: results missing: %+v", tc.query, page)
		}
	}

	// Paging is capped at TMDB's 500 pages, and items carry library state and dates.
	page := getJSON[map[string]any](t, member, base+"/api/discover/list?list=upcoming&kind=movie&page=2")
	if page["page"] != float64(2) || page["totalPages"] != float64(500) || page["totalResults"] != float64(10000) {
		t.Fatalf("paging: page %v totalPages %v totalResults %v", page["page"], page["totalPages"], page["totalResults"])
	}
	results := page["results"].([]any)
	matrix, soon, noDate := results[0].(map[string]any), results[1].(map[string]any), results[2].(map[string]any)
	if matrix["inLibrary"] != true || matrix["libraryId"] == nil || matrix["status"] == nil || matrix["releaseDate"] != "1999-03-31" {
		t.Fatalf("library title: %+v", matrix)
	}
	if soon["inLibrary"] != false || soon["releaseDate"] != "2026-10-12" || soon["mediaType"] != "movie" ||
		strings.Join(toStrings(soon["genres"]), ",") != "Science Fiction" {
		t.Fatalf("coming soon title: %+v", soon)
	}
	if _, has := noDate["releaseDate"]; has {
		t.Fatalf("an unknown date should be left out: %+v", noDate)
	}

	// Upcoming asks TMDB for titles from today on, most popular first.
	mu.Lock()
	paths = nil
	mu.Unlock()
	getJSON[map[string]any](t, member, base+"/api/discover/list?list=upcoming&kind=tv&page=3")
	mu.Lock()
	q := query
	mu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	for _, want := range []string{"first_air_date.gte=" + today, "sort_by=popularity.desc", "page=3"} {
		if !strings.Contains(q, want) {
			t.Fatalf("upcoming TV query %q should contain %q", q, want)
		}
	}
	tv := getJSON[map[string]any](t, member, base+"/api/discover/list?list=popular&kind=tv")
	show := tv["results"].([]any)[0].(map[string]any)
	if show["mediaType"] != "tv" || show["releaseDate"] != "2026-11-01" || tv["totalResults"] != float64(55) {
		t.Fatalf("tv list: %+v", tv)
	}

	// The browse endpoint reports the same fields.
	browse := getJSON[map[string]any](t, member, base+"/api/discover/browse?kind=movie")
	if browse["totalResults"] != float64(10000) || browse["results"].([]any)[1].(map[string]any)["releaseDate"] != "2026-10-12" {
		t.Fatalf("browse: %+v", browse)
	}

	// A page past the end is capped rather than refused.
	mu.Lock()
	paths = nil
	mu.Unlock()
	getJSON[map[string]any](t, member, base+"/api/discover/list?list=trending&kind=tv&page=9999")
	mu.Lock()
	if !strings.Contains(query, "page=500") {
		t.Fatalf("page should be capped at 500: %s", query)
	}
	mu.Unlock()

	for _, bad := range []string{"list=soon&kind=movie", "kind=movie", "list=popular&kind=books", "list=popular&page=abc", "list=popular&page=-1"} {
		if status, _ := doStatus(t, member, http.MethodGet, base+"/api/discover/list?"+bad); status != http.StatusBadRequest {
			t.Fatalf("list?%s: want 400, got %d", bad, status)
		}
	}
}
