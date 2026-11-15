package metadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
)

func TestParseServices(t *testing.T) {
	cases := []struct {
		in     string
		want   []string
		wantOK bool
	}{
		{"", nil, true},
		{" , ", nil, true},
		{"netflix", []string{"netflix"}, true},
		{"Netflix, prime ,netflix", []string{"netflix", "prime"}, true},
		{"netflix,nope", nil, false},
		{"all", nil, false},
	}
	for _, tc := range cases {
		got, ok := metadata.ParseServices(tc.in)
		if ok != tc.wantOK || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseServices(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestStreamingServicesAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range metadata.StreamingServices() {
		if s.Key == "" || s.Name == "" || len(s.Providers) == 0 || seen[s.Key] {
			t.Errorf("bad or repeated service %+v", s)
		}
		seen[s.Key] = true
	}
	if len(seen) < 8 {
		t.Errorf("expected the common services, got %d", len(seen))
	}
}

// Hiding goes to TMDB as a watch region and a list of providers to leave out,
// on a browse, and on every list, where trending and popular become a browse.
func TestHidingReachesTMDB(t *testing.T) {
	var last url.Values
	var lastPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last, lastPath = r.URL.Query(), r.URL.Path
		w.Write([]byte(`{"page":1,"total_pages":1,"results":[{"id":1,"title":"A","name":"A"}]}`))
	}))
	defer srv.Close()
	c := metadata.NewWithBaseURL("k", srv.URL)
	ctx := context.Background()
	hide := metadata.Hiding{Services: []string{"netflix", "disney"}, Region: "de"}

	cases := []struct {
		name     string
		run      func() error
		wantPath string
		want     map[string]string // "" = must be absent
	}{
		{"movie browse", func() error { _, err := c.DiscoverMovies(ctx, metadata.DiscoverQuery{Hide: hide}); return err },
			"/discover/movie", map[string]string{"watch_region": "DE", "without_watch_providers": "8|1796|337"}},
		{"tv browse without a region", func() error {
			_, err := c.DiscoverTV(ctx, metadata.DiscoverQuery{Hide: metadata.Hiding{Services: []string{"hulu"}}})
			return err
		}, "/discover/tv", map[string]string{"watch_region": "US", "without_watch_providers": "15"}},
		{"browse with nothing to hide", func() error { _, err := c.DiscoverMovies(ctx, metadata.DiscoverQuery{}); return err },
			"/discover/movie", map[string]string{"watch_region": "", "without_watch_providers": ""}},
		{"popular movies become a browse", func() error { _, err := c.MovieListHiding(ctx, metadata.ListPopular, 2, hide); return err },
			"/discover/movie", map[string]string{"without_watch_providers": "8|1796|337", "sort_by": "popularity.desc", "page": "2"}},
		{"trending shows become a browse", func() error { _, err := c.TVListHiding(ctx, metadata.ListTrending, 1, hide); return err },
			"/discover/tv", map[string]string{"without_watch_providers": "8|1796|337", "sort_by": "popularity.desc"}},
		{"upcoming movies keep their date", func() error { _, err := c.MovieListHiding(ctx, metadata.ListUpcoming, 1, hide); return err },
			"/discover/movie", map[string]string{"without_watch_providers": "8|1796|337", "primary_release_date.gte": "*"}},
		{"popular with nothing to hide is TMDB's own list", func() error { _, err := c.TVListHiding(ctx, metadata.ListPopular, 1, metadata.Hiding{}); return err },
			"/tv/popular", map[string]string{"without_watch_providers": ""}},
	}
	for _, tc := range cases {
		if err := tc.run(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if lastPath != tc.wantPath {
			t.Errorf("%s: path %s, want %s", tc.name, lastPath, tc.wantPath)
		}
		for k, want := range tc.want {
			got := last.Get(k)
			if want == "*" {
				if got == "" {
					t.Errorf("%s: %s missing", tc.name, k)
				}
				continue
			}
			if got != want {
				t.Errorf("%s: %s = %q, want %q", tc.name, k, got, want)
			}
		}
	}
}
