package api_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/ryanborg/mediarium/internal/library"
)

func TestHuntHonoursProfileRestrictionsAndPreferredScores(t *testing.T) {
	env := newTVAutoEnvWith(t, []string{
		"Some.Movie.2001.1080p.BluRay.HDR.x265-A",
		"Some.Movie.2001.1080p.BluRay.x264-B",
		"Some.Movie.2001.1080p.BluRay.x265-C",
	}, nil, http.StatusOK, "this is not an nzb")

	// Excludes HDR, prefers x265: of three equal-quality releases the only
	// allowed x265 one (C) must win — A is excluded, B has no score.
	profile := postJSON[map[string]any](t, env.client, env.baseURL+"/api/quality-profiles", map[string]any{
		"name": "No HDR, likes x265", "allowed": []string{"Bluray-1080p"}, "cutoff": "Bluray-1080p", "upgradeAllowed": true,
		"mustNotContain": []string{"hdr"},
		"preferred":      []map[string]any{{"term": "x265", "score": 50}},
	}, http.StatusCreated)

	m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetProfile(m.ID, int64(profile["id"].(float64))); err != nil {
		t.Fatal(err)
	}

	// The fixture's NZB URLs serve something that is not an NZB, so each grab
	// fails as a bad release and the automatic retry moves on to the
	// next-best allowed one: C first (highest score), then B, and never the
	// excluded A.
	env.server.TestHunt(context.Background())
	env.finish(t)
	requireTitles(t, env.grabbedTitles(t), "Some.Movie.2001.1080p.BluRay.x264-B", "Some.Movie.2001.1080p.BluRay.x265-C")
	first := ""
	var firstID float64 = -1
	for _, it := range getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/queue") {
		if id := it["id"].(float64); firstID < 0 || id < firstID {
			firstID, first = id, it["releaseTitle"].(string)
		}
	}
	if first != "Some.Movie.2001.1080p.BluRay.x265-C" {
		t.Fatalf("the preferred x265 release should have been tried first, got %q", first)
	}

	// The interactive search explains the exclusion.
	results := getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/movies/"+strconv.FormatInt(m.ID, 10)+"/search")
	for _, r := range results {
		rej, _ := r["rejections"].([]any)
		hasRejection := len(rej) > 0
		if excluded := r["title"] == "Some.Movie.2001.1080p.BluRay.HDR.x265-A"; excluded != hasRejection {
			t.Fatalf("%v: rejections = %v", r["title"], rej)
		}
	}
}

func TestProfileRestrictionsRoundTripThroughAPI(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	base := httpSrv.URL + "/api/quality-profiles"

	created := postJSON[map[string]any](t, client, base, map[string]any{
		"name": "P", "allowed": []string{"WEBDL-1080p"}, "cutoff": "WEBDL-1080p",
		"mustContain": []string{"x265"}, "mustNotContain": []string{"cam", "hdts"},
		"preferred": []map[string]any{{"term": "REPACK", "score": 10}},
	}, http.StatusCreated)
	if len(created["mustContain"].([]any)) != 1 || len(created["mustNotContain"].([]any)) != 2 || len(created["preferred"].([]any)) != 1 {
		t.Fatalf("create payload lost the restrictions: %+v", created)
	}
	postJSON[map[string]any](t, client, base, map[string]any{
		"name": "Bad", "allowed": []string{"WEBDL-1080p"}, "cutoff": "WEBDL-1080p", "mustContain": []string{"a|b"},
	}, http.StatusBadRequest)

	list := getJSON[map[string]any](t, client, base)
	for _, raw := range list["profiles"].([]any) {
		p := raw.(map[string]any)
		if p["mustContain"] == nil || p["mustNotContain"] == nil || p["preferred"] == nil {
			t.Fatalf("list payload must always carry arrays (never null): %+v", p)
		}
	}
}
