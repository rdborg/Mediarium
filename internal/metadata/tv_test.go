package metadata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryanborg/mediarium/internal/metadata"
)

func newTVFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search/tv", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") != "fixture show" {
			t.Errorf("unexpected query %q", r.URL.Query().Get("query"))
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"id": 1399, "name": "Fixture Show", "first_air_date": "2011-04-17", "poster_path": "/p.jpg", "overview": "o"},
		}})
	})
	mux.HandleFunc("/tv/1399", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id": 1399, "name": "Fixture Show", "first_air_date": "2011-04-17",
			"seasons": []map[string]any{
				{"season_number": 0, "episode_count": 1}, // Specials — must be skipped
				{"season_number": 1, "episode_count": 2},
				{"season_number": 2, "episode_count": 1},
			},
		})
	})
	mux.HandleFunc("/tv/1399/season/0", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("season 0 (specials) must not be fetched")
		http.NotFound(w, r)
	})
	mux.HandleFunc("/tv/1399/season/1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
			{"season_number": 1, "episode_number": 1, "name": "Pilot", "air_date": "2011-04-17"},
			{"season_number": 1, "episode_number": 2, "name": "Second", "air_date": "2011-04-24"},
		}})
	})
	mux.HandleFunc("/tv/1399/season/2", func(w http.ResponseWriter, r *http.Request) {
		// season_number deliberately omitted — GetSeason must fill it in.
		json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
			{"episode_number": 1, "name": "Return", "air_date": "2012-04-01"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSearchTV(t *testing.T) {
	srv := newTVFixtureServer(t)
	c := metadata.NewWithBaseURL("k", srv.URL)
	shows, err := c.SearchTV(context.Background(), "fixture show")
	if err != nil {
		t.Fatalf("SearchTV: %v", err)
	}
	if len(shows) != 1 || shows[0].TMDBID != 1399 || shows[0].Name != "Fixture Show" || shows[0].Year() != 2011 {
		t.Fatalf("unexpected results: %+v", shows)
	}
}

func TestGetShowEpisodesSkipsSpecialsAndFillsSeason(t *testing.T) {
	srv := newTVFixtureServer(t)
	c := metadata.NewWithBaseURL("k", srv.URL)
	detail, eps, err := c.GetShowEpisodes(context.Background(), 1399)
	if err != nil {
		t.Fatalf("GetShowEpisodes: %v", err)
	}
	if detail.Name != "Fixture Show" || detail.Year() != 2011 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	if len(eps) != 3 {
		t.Fatalf("expected 3 episodes (specials skipped), got %d: %+v", len(eps), eps)
	}
	// Order follows season order regardless of fetch concurrency.
	want := [][2]int{{1, 1}, {1, 2}, {2, 1}}
	for i, w := range want {
		if eps[i].Season != w[0] || eps[i].Episode != w[1] {
			t.Errorf("episode %d: want S%dE%d, got S%dE%d", i, w[0], w[1], eps[i].Season, eps[i].Episode)
		}
	}
}

func TestGetShowEpisodesSeasonFailurePropagates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tv/5", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 5, "name": "X", "seasons": []map[string]any{{"season_number": 1}}})
	})
	mux.HandleFunc("/tv/5/season/1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := metadata.NewWithBaseURL("k", srv.URL)
	if _, _, err := c.GetShowEpisodes(context.Background(), 5); err == nil {
		t.Fatal("expected a season fetch failure to fail the whole call rather than silently return a partial episode list")
	}
}
