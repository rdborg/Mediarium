package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/library"
)

func wantedTitles(items []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		label := it["title"].(string)
		if sub, ok := it["subtitle"].(string); ok && sub != "" {
			label += " " + sub[:6]
		}
		out[label] = true
	}
	return out
}

func TestWantedMissingAndCutoffUnmet(t *testing.T) {
	past := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
	future := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	env := newTVAutoEnv(t, nil, []episodeSpec{
		{1, 1, past, library.StatusMissing, ""},
		{1, 2, future, library.StatusMissing, ""},              // unaired: not wanted yet
		{1, 3, past, library.StatusDownloaded, "WEBDL-1080p"},  // below the 1080p cutoff
		{1, 4, past, library.StatusDownloaded, "Bluray-1080p"}, // at cutoff
	})
	repo := env.server.MovieRepo
	add := func(tmdb int, title string, monitored bool, status library.Status, q string) {
		m, err := repo.Add(library.Movie{TMDBID: tmdb, Title: title, Monitored: monitored})
		if err != nil {
			t.Fatal(err)
		}
		if status != library.StatusMissing {
			_ = repo.SetStatus(m.ID, status, q, "/m/x.mkv")
		}
	}
	add(1, "Missing One", true, library.StatusMissing, "")
	add(2, "Missing Unmonitored", false, library.StatusMissing, "")
	add(3, "Upgradable", true, library.StatusDownloaded, "WEBDL-1080p")
	add(4, "Done", true, library.StatusDownloaded, "Bluray-1080p")
	add(5, "Untagged", true, library.StatusDownloaded, "Unknown")

	missing := wantedTitles(getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/wanted?kind=missing"))
	if len(missing) != 2 || !missing["Missing One"] || !missing["Fixture Show S01E01"] {
		t.Fatalf("missing = %v", missing)
	}
	cutoff := getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/wanted?kind=cutoff")
	got := wantedTitles(cutoff)
	if len(got) != 2 || !got["Upgradable"] || !got["Fixture Show S01E03"] {
		t.Fatalf("cutoff unmet = %v", got)
	}
	for _, it := range cutoff {
		if it["quality"] != "WEBDL-1080p" || it["cutoff"] != "Bluray-1080p" {
			t.Fatalf("cutoff rows should carry current quality and the cutoff: %+v", it)
		}
	}
	resp, err := env.client.Get(env.baseURL + "/api/wanted?kind=bogus")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("an unknown kind should be a 400, got %d", resp.StatusCode)
	}
}

func TestMonitorTogglesAffectAutomation(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusMissing, ""},
		{1, 2, "2020-01-08", library.StatusMissing, ""},
	})
	seriesURL := fmt.Sprintf("%s/api/series/%d", env.baseURL, env.seriesID)

	// Unmonitor the whole series: nothing is hunted.
	putJSONStatus(t, env.client, seriesURL+"/monitored", map[string]any{"monitored": false}, http.StatusOK)
	if got := getJSON[map[string]any](t, env.client, seriesURL); got["monitored"] != false {
		t.Fatalf("series should be unmonitored: %+v", got)
	}
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t, 0))

	// Back on, but unmonitor season 1 as a whole: still nothing.
	putJSONStatus(t, env.client, seriesURL+"/monitored", map[string]any{"monitored": true}, http.StatusOK)
	putJSONStatus(t, env.client, seriesURL+"/seasons/1/monitored", map[string]any{"monitored": false}, http.StatusOK)
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t, 0))

	// Monitor just episode 2: only that one is grabbed.
	detail := getJSON[map[string]any](t, env.client, seriesURL)
	var e2 float64
	for _, raw := range detail["episodes"].([]any) {
		ep := raw.(map[string]any)
		if ep["monitored"] != false {
			t.Fatalf("season toggle should have unmonitored every episode: %+v", ep)
		}
		if ep["episode"] == float64(2) {
			e2 = ep["id"].(float64)
		}
	}
	putJSONStatus(t, env.client, fmt.Sprintf("%s/api/episodes/%d/monitored", env.baseURL, int64(e2)), map[string]any{"monitored": true}, http.StatusOK)
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t, 1), s1e2WEB)
}

func TestMovieInteractiveSearchExplainsRejections(t *testing.T) {
	env := newTVAutoEnv(t, []string{
		"Some.Movie.2001.1080p.BluRay.x264-A",
		"Some.Movie.2001.720p.WEB-DL.x264-B",
		"Some.Movie.2001.2160p.WEB-DL.x264-C",
		"Other.Movie.2001.1080p.BluRay.x264-D",
	}, nil)
	m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = env.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", "/m/x.mkv")

	results := getJSON[[]map[string]any](t, env.client, fmt.Sprintf("%s/api/movies/%d/search", env.baseURL, m.ID))
	byTitle := map[string][]any{}
	for _, r := range results {
		rej, _ := r["rejections"].([]any)
		byTitle[r["title"].(string)] = rej
	}
	if len(byTitle) != 3 {
		t.Fatalf("expected the 3 releases of this movie (not the other movie), got %v", byTitle)
	}
	if rej := byTitle["Some.Movie.2001.1080p.BluRay.x264-A"]; len(rej) != 0 {
		t.Fatalf("the Bluray release is a valid upgrade, got rejections %v", rej)
	}
	if rej := byTitle["Some.Movie.2001.720p.WEB-DL.x264-B"]; len(rej) != 1 {
		t.Fatalf("720p is allowed but not an upgrade, got %v", rej)
	}
	if rej := byTitle["Some.Movie.2001.2160p.WEB-DL.x264-C"]; len(rej) == 0 {
		t.Fatalf("2160p should be rejected by the 1080p profile")
	}
}

func TestSearchNowIgnoresMonitoredAndScopesToEpisode(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB, "Some.Movie.2001.1080p.WEB-DL.x264-GRP"}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusMissing, ""},
		{1, 2, "2020-01-08", library.StatusMissing, ""},
	})
	seriesURL := fmt.Sprintf("%s/api/series/%d", env.baseURL, env.seriesID)
	putJSONStatus(t, env.client, seriesURL+"/monitored", map[string]any{"monitored": false}, http.StatusOK)

	got := postJSON[map[string]any](t, env.client, seriesURL+"/search-now", map[string]any{"season": 1, "episode": 2}, http.StatusOK)
	if got["grabbed"] != float64(1) {
		t.Fatalf("expected one grab, got %+v", got)
	}
	requireTitles(t, env.grabbedTitles(t, 1), s1e2WEB)

	// Movie search-now works on an unmonitored movie too.
	m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Year: 2001, Monitored: false})
	if err != nil {
		t.Fatal(err)
	}
	res := postJSON[map[string]any](t, env.client, fmt.Sprintf("%s/api/movies/%d/search-now", env.baseURL, m.ID), nil, http.StatusOK)
	if res["grabbed"] != float64(1) {
		t.Fatalf("expected the movie to be grabbed, got %+v", res)
	}
	waitForQueue(t, env.client, env.baseURL, 2)
}
