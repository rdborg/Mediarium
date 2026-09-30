package metadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

func TestDiscoverLists(t *testing.T) {
	var (
		mu       sync.Mutex
		last     url.Values
		lastPath string
		calls    int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		last, lastPath = r.URL.Query(), r.URL.Path
		calls++
		mu.Unlock()
		w.Write([]byte(`{"page":3,"total_pages":1000,"total_results":19990,"results":[{"id":1,"title":"A","name":"A","release_date":"2026-10-12","first_air_date":"2026-11-01"}]}`))
	}))
	defer srv.Close()
	c := metadata.NewWithBaseURL("k", srv.URL)
	c.SetClock(func() time.Time { return time.Date(2026, 9, 28, 23, 30, 0, 0, time.UTC) })
	ctx := context.Background()

	cases := []struct {
		name  string
		tv    bool
		list  string
		page  int
		path  string
		want  map[string]string // "" = must be absent
		first string            // first result's date
	}{
		{"trending movies", false, metadata.ListTrending, 3, "/trending/movie/week", map[string]string{"page": "3", "sort_by": ""}, "2026-10-12"},
		{"popular movies, page below 1", false, metadata.ListPopular, 0, "/movie/popular", map[string]string{"page": "1"}, "2026-10-12"},
		{"upcoming movies", false, metadata.ListUpcoming, 2, "/discover/movie", map[string]string{
			"primary_release_date.gte": "2026-09-28", "sort_by": "popularity.desc", "page": "2", "region": "", "include_adult": "false",
		}, "2026-10-12"},
		{"trending shows, page capped", true, metadata.ListTrending, 9999, "/trending/tv/week", map[string]string{"page": "500"}, "2026-11-01"},
		{"popular shows", true, metadata.ListPopular, 4, "/tv/popular", map[string]string{"page": "4"}, "2026-11-01"},
		{"upcoming shows", true, metadata.ListUpcoming, 1, "/discover/tv", map[string]string{
			"first_air_date.gte": "2026-09-28", "sort_by": "popularity.desc", "page": "1",
		}, "2026-11-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var total, results int
			var date string
			if tc.tv {
				p, err := c.TVList(ctx, tc.list, tc.page)
				if err != nil {
					t.Fatal(err)
				}
				total, results, date = p.TotalPages, p.TotalResults, p.Results[0].FirstAirDate
			} else {
				p, err := c.MovieList(ctx, tc.list, tc.page)
				if err != nil {
					t.Fatal(err)
				}
				total, results, date = p.TotalPages, p.TotalResults, p.Results[0].ReleaseDate
			}
			if total != 500 || results != 10000 {
				t.Fatalf("paging should be capped at 500 pages / 10000 results, got %d / %d", total, results)
			}
			if date != tc.first {
				t.Fatalf("date %q, want %q", date, tc.first)
			}
			mu.Lock()
			defer mu.Unlock()
			if lastPath != tc.path {
				t.Fatalf("path %s, want %s", lastPath, tc.path)
			}
			for k, v := range tc.want {
				if got := last.Get(k); got != v {
					t.Fatalf("%s = %q, want %q (query %v)", k, got, v, last)
				}
			}
		})
	}

	// A page asked for again comes from the cache.
	mu.Lock()
	before := calls
	mu.Unlock()
	if _, err := c.MovieList(ctx, metadata.ListTrending, 3); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if calls != before {
		t.Fatalf("a repeated page should be served from the cache (%d TMDB calls, want %d)", calls, before)
	}
	mu.Unlock()

	if _, err := c.MovieList(ctx, "soon", 1); err == nil {
		t.Fatal("an unknown list should be an error")
	}
	if metadata.ValidList("soon") || !metadata.ValidList(metadata.ListUpcoming) {
		t.Fatal("ValidList")
	}
}

func TestDiscoverListErrorsAreNotCached(t *testing.T) {
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"page":1,"total_pages":1,"total_results":0,"results":null}`))
	}))
	defer srv.Close()
	c := metadata.NewWithBaseURL("k", srv.URL)
	if _, err := c.TVList(context.Background(), metadata.ListPopular, 1); err == nil {
		t.Fatal("want an error while TMDB is down")
	}
	fail = false
	p, err := c.TVList(context.Background(), metadata.ListPopular, 1)
	if err != nil {
		t.Fatalf("a failure must not be cached: %v", err)
	}
	if p.Results == nil {
		t.Fatal("results should be an empty list, not nil")
	}
}
