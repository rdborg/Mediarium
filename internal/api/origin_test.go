package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/origin"
	"github.com/rdborg/mediarium/internal/queue"
)

func originOf(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	o, _ := m["origin"].(map[string]any)
	return o
}

// A title says how it came in: by a person, or by an import list. Titles added
// before that was recorded say nothing. The list's history keeps what it added
// even when the list is told to start over.
func TestOriginsOfTitlesAndAListHistory(t *testing.T) {
	server, base, admin := listsServer(t)

	// A person adds Alpha by hand; a title from "before" has no origin at all.
	by := postJSON[map[string]any](t, admin, base+"/api/movies", map[string]any{"tmdbId": 100}, http.StatusCreated)
	if o := originOf(t, by); o == nil || o["kind"] != "manual" {
		t.Fatalf("a movie added by hand: %v", by["origin"])
	}
	if u, _ := originOf(t, by)["user"].(map[string]any); u == nil || u["name"] != "Ryan Tester" {
		t.Fatalf("the person behind it: %v", by["origin"])
	}
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 9001, Title: "Old one"}); err != nil {
		t.Fatal(err)
	}
	for _, m := range getJSON[[]map[string]any](t, admin, base+"/api/movies") {
		if m["tmdbId"] == float64(9001) && m["origin"] != nil {
			t.Fatalf("a title from before has no origin: %v", m["origin"])
		}
	}

	created := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"name": "Friends picks", "source": "tmdb-list", "media": "both", "config": map[string]any{"url": "555"},
		"intervalHours": 24, "minRating": 7, "minYear": 2000, "maxPerRun": 25, "searchNow": false,
	}, http.StatusCreated)
	id := created["id"].(float64)
	waitBackground(t, server)

	shows := getJSON[[]map[string]any](t, admin, base+"/api/series")
	if len(shows) != 1 {
		t.Fatalf("expected the list to add one show, got %d", len(shows))
	}
	if o := originOf(t, shows[0]); o == nil || o["kind"] != "list" || o["name"] != "Friends picks" {
		t.Fatalf("a show a list added: %v", shows[0]["origin"])
	}
	if shows[0]["addedBy"] != nil {
		t.Fatalf("a list is not a person: %v", shows[0]["addedBy"])
	}

	history := getJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/import-lists/%v/history", base, id))
	// Alpha was there already, Bravo, Charlie and Echo are filtered out; Delta
	// and the show were added.
	if history["added"] != float64(2) || history["name"] != "Friends picks" || history["lastAddedAt"] == "" {
		t.Fatalf("history: %v", history)
	}
	byTitle := map[string]map[string]any{}
	for _, x := range history["titles"].([]any) {
		byTitle[x.(map[string]any)["title"].(string)] = x.(map[string]any)
	}
	if len(byTitle) != 2 || byTitle["Show One"]["kind"] != "tv" || byTitle["Delta"]["kind"] != "movie" || byTitle["Delta"]["libraryId"] == nil || byTitle["Delta"]["hasFile"] != false {
		t.Fatalf("history titles: %v", history["titles"])
	}
	out := history["outcomes"].(map[string]any)
	if out["in-library"] != float64(1) || out["added"] != float64(2) || out["filtered"] != float64(3) {
		t.Fatalf("history outcomes: %v", out)
	}

	// Starting the list over forgets what it has seen, not what it added.
	postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/import-lists/%v/forget", base, id), nil, http.StatusOK)
	history = getJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/import-lists/%v/history", base, id))
	if history["added"] != float64(2) || len(history["outcomes"].(map[string]any)) != 0 || len(history["titles"].([]any)) != 2 {
		t.Fatalf("history after starting over: %v", history)
	}
}

// Who started a download is shown to administrators only.
func TestDownloadOriginIsForAdministrators(t *testing.T) {
	server, base, admin, member, memberID := familyServer(t)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix"})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range []queue.Item{
		{MovieID: m.ID, ReleaseTitle: "The.Matrix.1999.a", NZBURL: "http://x/1", Origin: origin.Origin{Kind: origin.Manual, UserID: int64(memberID), User: "Sam"}},
		{MovieID: m.ID, ReleaseTitle: "The.Matrix.1999.b", NZBURL: "http://x/2", Origin: origin.Origin{Kind: origin.List, Name: "Trending"}},
		{MovieID: m.ID, ReleaseTitle: "The.Matrix.1999.c", NZBURL: "http://x/3"},
	} {
		if _, err := server.QueueRepo.Enqueue(it); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]map[string]any{}
	for _, it := range getJSON[[]map[string]any](t, admin, base+"/api/queue") {
		seen[it["releaseTitle"].(string)] = it
	}
	if o := originOf(t, seen["The.Matrix.1999.a"]); o == nil || o["kind"] != "manual" || o["user"].(map[string]any)["name"] != "Sam" {
		t.Fatalf("a download Sam picked: %v", seen["The.Matrix.1999.a"]["origin"])
	}
	if o := originOf(t, seen["The.Matrix.1999.b"]); o == nil || o["kind"] != "list" || o["name"] != "Trending" {
		t.Fatalf("a download a list started: %v", seen["The.Matrix.1999.b"]["origin"])
	}
	if seen["The.Matrix.1999.c"]["origin"] != nil {
		t.Fatalf("a download with no origin says nothing: %v", seen["The.Matrix.1999.c"]["origin"])
	}
	for _, it := range getJSON[[]map[string]any](t, member, base+"/api/queue") {
		if it["origin"] != nil {
			t.Fatalf("a member must not see who started a download: %v", it)
		}
	}
}
