package metadata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryanborg/mediarium/internal/metadata"
)

func TestNoAPIKey(t *testing.T) {
	c := metadata.New("")
	if c.HasAPIKey() {
		t.Fatal("expected HasAPIKey false for empty key")
	}
	if _, err := c.SearchMovies(context.Background(), "matrix"); err != metadata.ErrNoAPIKey {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestSearchMovies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/movie" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "the matrix" {
			t.Fatalf("unexpected query param: %s", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("api_key") != "test-key" {
			t.Fatalf("missing/incorrect api_key param")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"page": 1,
			"results": []map[string]any{
				{"id": 603, "title": "The Matrix", "release_date": "1999-03-30", "poster_path": "/poster.jpg", "vote_average": 8.7},
			},
		})
	}))
	defer srv.Close()

	c := metadata.NewWithBaseURL("test-key", srv.URL)
	results, err := c.SearchMovies(context.Background(), "the matrix")
	if err != nil {
		t.Fatalf("search movies: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "The Matrix" || results[0].TMDBID != 603 {
		t.Fatalf("unexpected result: %+v", results[0])
	}
	if results[0].Year() != 1999 {
		t.Fatalf("expected year 1999, got %d", results[0].Year())
	}
}

func TestPosterURL(t *testing.T) {
	if got := metadata.PosterURL(""); got != "" {
		t.Fatalf("expected empty poster URL for empty path, got %q", got)
	}
	if got := metadata.PosterURL("/abc.jpg"); got != "https://image.tmdb.org/t/p/w500/abc.jpg" {
		t.Fatalf("unexpected poster URL: %q", got)
	}
}

func TestTrendingAndPopular(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending/movie/week", "/movie/popular", "/movie/603/similar":
			json.NewEncoder(w).Encode(map[string]any{
				"page":    1,
				"results": []map[string]any{{"id": 1, "title": "Fixture Movie"}},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := metadata.NewWithBaseURL("test-key", srv.URL)
	trending, err := c.TrendingMovies(context.Background())
	if err != nil || len(trending) != 1 {
		t.Fatalf("trending: results=%v err=%v", trending, err)
	}
	popular, err := c.PopularMovies(context.Background())
	if err != nil || len(popular) != 1 {
		t.Fatalf("popular: results=%v err=%v", popular, err)
	}
	similar, err := c.SimilarMovies(context.Background(), 603)
	if err != nil || len(similar) != 1 {
		t.Fatalf("similar: results=%v err=%v", similar, err)
	}
}
