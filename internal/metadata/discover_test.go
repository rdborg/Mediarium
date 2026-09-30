package metadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
)

func TestDiscoverQueryParams(t *testing.T) {
	var last url.Values
	var lastPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last, lastPath = r.URL.Query(), r.URL.Path
		w.Write([]byte(`{"page":2,"total_pages":900,"results":[{"id":1,"title":"A","name":"A"}]}`))
	}))
	defer srv.Close()
	c := metadata.NewWithBaseURL("k", srv.URL)
	ctx := context.Background()

	cases := []struct {
		name  string
		tv    bool
		q     metadata.DiscoverQuery
		want  map[string]string // "" = must be absent
		wantP string
	}{
		{"movie defaults", false, metadata.DiscoverQuery{}, map[string]string{
			"sort_by": "popularity.desc", "page": "1", "with_genres": "", "primary_release_year": "", "vote_count.gte": "",
		}, "/discover/movie"},
		{"movie genre, exact year, rating", false, metadata.DiscoverQuery{Genre: 18, Year: 1999, YearFrom: 1980, Sort: metadata.SortRating, Page: 3}, map[string]string{
			"with_genres": "18", "primary_release_year": "1999", "primary_release_date.gte": "",
			"sort_by": "vote_average.desc", "vote_count.gte": "200", "page": "3",
		}, "/discover/movie"},
		{"movie range, newest", false, metadata.DiscoverQuery{YearFrom: 1990, YearTo: 1999, Sort: metadata.SortNewest}, map[string]string{
			"primary_release_date.gte": "1990-01-01", "primary_release_date.lte": "1999-12-31", "sort_by": "primary_release_date.desc",
		}, "/discover/movie"},
		{"tv exact year, oldest", true, metadata.DiscoverQuery{Year: 2011, Sort: metadata.SortOldest}, map[string]string{
			"first_air_date_year": "2011", "sort_by": "first_air_date.asc",
		}, "/discover/tv"},
		{"tv range from only, page capped", true, metadata.DiscoverQuery{YearFrom: 2020, Page: 9999}, map[string]string{
			"first_air_date.gte": "2020-01-01", "first_air_date.lte": "", "page": "500",
		}, "/discover/tv"},
	}
	for _, tc := range cases {
		var total int
		if tc.tv {
			p, err := c.DiscoverTV(ctx, tc.q)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			total = p.TotalPages
		} else {
			p, err := c.DiscoverMovies(ctx, tc.q)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			total = p.TotalPages
		}
		if total != 500 {
			t.Fatalf("%s: total pages should be capped at 500, got %d", tc.name, total)
		}
		if lastPath != tc.wantP {
			t.Fatalf("%s: path %s, want %s", tc.name, lastPath, tc.wantP)
		}
		for k, v := range tc.want {
			if got := last.Get(k); got != v {
				t.Fatalf("%s: %s = %q, want %q (query %v)", tc.name, k, got, v, last)
			}
		}
	}
}

func TestGenresListSorted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"genres":[{"id":18,"name":"Drama"},{"id":28,"name":"Action"}]}`))
	}))
	defer srv.Close()
	c := metadata.NewWithBaseURL("k", srv.URL)
	got, err := c.Genres(context.Background(), "tv")
	if err != nil || len(got) != 2 || got[0].Name != "Action" || got[1].ID != 18 {
		t.Fatalf("unexpected genres %+v err=%v", got, err)
	}
	if _, err := c.Genres(context.Background(), "books"); err == nil {
		t.Fatal("unknown kind should be an error")
	}
}
