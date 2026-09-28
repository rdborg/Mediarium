package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// newTVTMDBServer serves a TMDB-shaped fixture for one show (tmdb id 1399,
// 2 seasons). extraEpisode, when set, makes season 2 report a second
// episode — used to simulate a show gaining an episode between adding it
// and a later refresh.
func newTVTMDBServer(t *testing.T, extraEpisode *atomic.Bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search/tv", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"id": 1399, "name": "Fixture Show", "first_air_date": "2011-04-17", "poster_path": "/p.jpg", "overview": "A show."},
		}})
	})
	mux.HandleFunc("/tv/1399", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id": 1399, "name": "Fixture Show", "first_air_date": "2011-04-17", "poster_path": "/p.jpg", "overview": "A show.",
			"seasons": []map[string]any{{"season_number": 0}, {"season_number": 1}, {"season_number": 2}},
		})
	})
	mux.HandleFunc("/tv/1399/season/1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
			{"season_number": 1, "episode_number": 1, "name": "Pilot", "air_date": "2011-04-17"},
			{"season_number": 1, "episode_number": 2, "name": "Second", "air_date": "2011-04-24"},
		}})
	})
	mux.HandleFunc("/tv/1399/season/2", func(w http.ResponseWriter, r *http.Request) {
		eps := []map[string]any{{"season_number": 2, "episode_number": 1, "name": "Return", "air_date": "2012-04-01"}}
		if extraEpisode != nil && extraEpisode.Load() {
			eps = append(eps, map[string]any{"season_number": 2, "episode_number": 2, "name": "Newly Announced", "air_date": "2012-04-08"})
		}
		json.NewEncoder(w).Encode(map[string]any{"episodes": eps})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSeriesAddListDetailRefreshDelete(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	var extra atomic.Bool
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newTVTMDBServer(t, &extra).URL)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	// Search
	results := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/tv/search?q=fixture+show")
	if len(results) != 1 || results[0]["tmdbId"] != float64(1399) || results[0]["title"] != "Fixture Show" {
		t.Fatalf("unexpected TV search results: %+v", results)
	}

	// Add
	created := postJSON[map[string]any](t, client, httpSrv.URL+"/api/series", map[string]any{"tmdbId": 1399}, http.StatusCreated)
	id := int64(created["id"].(float64))
	if created["episodeCount"] != float64(3) || created["downloadedCount"] != float64(0) {
		t.Fatalf("expected 3 episodes (specials excluded) / 0 downloaded, got %+v", created)
	}

	// Adding the same show again is a conflict, not a silent duplicate.
	dup := postJSON[map[string]any](t, client, httpSrv.URL+"/api/series", map[string]any{"tmdbId": 1399}, http.StatusConflict)
	if dup["error"] == nil {
		t.Fatalf("expected an error message on the duplicate add, got %+v", dup)
	}

	// List + detail
	list := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/series")
	if len(list) != 1 {
		t.Fatalf("expected 1 series in the list, got %d", len(list))
	}
	detail := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/series/%d", httpSrv.URL, id))
	eps := detail["episodes"].([]any)
	if len(eps) != 3 {
		t.Fatalf("expected 3 episodes in the detail, got %d", len(eps))
	}
	first := eps[0].(map[string]any)
	if first["season"] != float64(1) || first["episode"] != float64(1) || first["status"] != "missing" || first["monitored"] != true {
		t.Fatalf("unexpected first episode: %+v", first)
	}

	// Refresh picks up a newly announced episode without disturbing
	// existing ones (mark one downloaded first to prove state survives).
	ep, err := server.MovieRepo.GetEpisode(id, 1, 1)
	if err != nil {
		t.Fatalf("get episode: %v", err)
	}
	if err := server.MovieRepo.SetEpisodeStatus(ep.ID, "downloaded", "WEBDL-1080p", "/tv/x.mkv"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	extra.Store(true)
	refreshed := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/series/%d/refresh", httpSrv.URL, id), nil, http.StatusOK)
	if refreshed["episodeCount"] != float64(4) || refreshed["downloadedCount"] != float64(1) {
		t.Fatalf("expected refresh to add the new episode (4 total) and keep the downloaded one (1), got %+v", refreshed)
	}

	// Delete
	postJSONMethod[map[string]any](t, client, http.MethodDelete, fmt.Sprintf("%s/api/series/%d", httpSrv.URL, id), nil, http.StatusOK)
	list = getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/series")
	if len(list) != 0 {
		t.Fatalf("expected the series to be gone after delete, got %+v", list)
	}
}

func TestSeriesUnknownID404(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	resp, err := client.Get(httpSrv.URL + "/api/series/9999")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown series, got %d", resp.StatusCode)
	}
}
