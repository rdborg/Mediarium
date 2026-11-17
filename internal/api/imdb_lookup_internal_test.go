package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The IMDb id is looked up once for every indexer of a search. When the
// indexer that asks first has already been cancelled, the lookup still runs,
// so the other indexers get the id too.
func TestMovieIMDbLookupIgnoresTheFirstCallersCancel(t *testing.T) {
	s := newAuditServer(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/movie/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 987654, "title": "Fixture Movie", "imdb_id": "tt0987654"}`))
	}))
	t.Cleanup(srv.Close)
	s.TestSetTMDBBaseURL("fixture-tmdb-key", srv.URL)

	const tmdb = 987654
	movieIMDbIDs.Delete(tmdb)
	t.Cleanup(func() { movieIMDbIDs.Delete(tmdb) })
	lookup := s.movieIMDbLookup(tmdb)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := lookup(cancelled); got != "tt0987654" {
		t.Fatalf("first caller (cancelled) got %q", got)
	}
	if got := lookup(context.Background()); got != "tt0987654" {
		t.Fatalf("second caller got %q", got)
	}
}
