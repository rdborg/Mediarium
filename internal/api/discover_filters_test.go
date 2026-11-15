package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// The Discover filters (original language, lowest rating, runtime) reach
// TMDB as discover parameters, and anything outside the allowed values is a
// 400 that never reaches TMDB.
func TestDiscoverBrowseFilters(t *testing.T) {
	server, base, _, member, _ := familyServer(t)

	var (
		mu    sync.Mutex
		calls int
		last  url.Values
		path  string
	)
	answer := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		last, path = r.URL.Query(), r.URL.Path
		mu.Unlock()
		w.Write([]byte(`{"page":1,"total_pages":1,"total_results":0,"results":[]}`))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/discover/movie", answer)
	mux.HandleFunc("/discover/tv", answer)
	tmdb := httptest.NewServer(mux)
	t.Cleanup(tmdb.Close)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	good := []struct {
		name  string
		query string
		path  string
		want  map[string]string // "" = must be absent
	}{
		{"korean tv rated 7+", "kind=tv&language=ko&minRating=7", "/discover/tv", map[string]string{
			"with_original_language": "ko", "vote_average.gte": "7", "vote_count.gte": "50", "with_runtime.lte": "",
		}},
		{"movies over two hours", "kind=movie&runtime=over120", "/discover/movie", map[string]string{
			"with_runtime.gte": "121", "with_runtime.lte": "", "vote_average.gte": "", "vote_count.gte": "",
		}},
		{"three letter language, rated 8+, short", "kind=movie&language=yue&minRating=8&runtime=under90", "/discover/movie", map[string]string{
			"with_original_language": "yue", "vote_average.gte": "8", "vote_count.gte": "100", "with_runtime.lte": "89",
		}},
		{"hide Netflix and Disney+", "kind=tv&hide=netflix,disney", "/discover/tv", map[string]string{
			"without_watch_providers": "8|1796|337", "watch_region": "US",
		}},
		{"no services to hide", "kind=tv&hide=", "/discover/tv", map[string]string{"without_watch_providers": "", "watch_region": ""}},
		{"no filters", "kind=movie", "/discover/movie", map[string]string{
			"with_original_language": "", "vote_average.gte": "", "with_runtime.gte": "",
		}},
	}
	for _, tc := range good {
		getJSON[map[string]any](t, member, base+"/api/discover/browse?"+tc.query)
		mu.Lock()
		if path != tc.path {
			t.Fatalf("%s: TMDB path %s, want %s", tc.name, path, tc.path)
		}
		for k, v := range tc.want {
			if got := last.Get(k); got != v {
				t.Fatalf("%s: %s = %q, want %q (query %v)", tc.name, k, got, v, last)
			}
		}
		mu.Unlock()
	}

	mu.Lock()
	before := calls
	mu.Unlock()
	bad := []struct{ query, mention string }{
		{"kind=movie&language=KO", "language"},
		{"kind=movie&language=k", "language"},
		{"kind=movie&language=korean", "language"},
		{"kind=movie&language=k0", "language"},
		{"kind=movie&language=ko%26with_genres%3D1", "language"},
		{"kind=movie&minRating=5", "minRating"},
		{"kind=movie&minRating=10", "minRating"},
		{"kind=movie&minRating=abc", "minRating"},
		{"kind=movie&hide=netflix,nope", "hide"},
		{"kind=movie&hide=all", "hide"},
		{"kind=movie&runtime=120", "runtime"},
		{"kind=movie&runtime=over120%26x%3D1", "runtime"},
	}
	for _, tc := range bad {
		status, body := doStatus(t, member, http.MethodGet, base+"/api/discover/browse?"+tc.query)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d", tc.query, status)
		}
		if msg, _ := body["error"].(string); !strings.Contains(msg, tc.mention) {
			t.Fatalf("%s: the error should name %s, got %v", tc.query, tc.mention, body)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != before {
		t.Fatalf("a rejected filter reached TMDB (%d calls)", calls-before)
	}
}
