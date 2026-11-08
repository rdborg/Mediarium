package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

func shelfTitles(t *testing.T, base string, client *http.Client) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, it := range getJSON[map[string]any](t, client, base+"/api/lifecycle/shelf")["items"].([]any) {
		m := it.(map[string]any)
		out[m["title"].(string)] = m["daysLeft"].(float64)
	}
	return out
}

func TestLifecycleDryRunFirstThenShelfThenRemoval(t *testing.T) {
	server, base, client := loginNewServer(t)
	if _, err := server.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: fakeCleanupPlex(t).URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	longAgo := time.Now().AddDate(0, 0, -90)
	movie := func(tmdb int, title string) (library.Movie, string) {
		t.Helper()
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: title, Year: 2000, Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(server.TestMoviesRoot(), title+" (2000)", title+" (2000).mkv")
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, longAgo, longAgo); err != nil {
			t.Fatal(err)
		}
		if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", file); err != nil {
			t.Fatal(err)
		}
		server.TestSetMovieAddedAt(m.ID, longAgo)
		return m, file
	}
	matrix, matrixFile := movie(603, "The Matrix") // watched 40 days ago
	alien, alienFile := movie(348, "Alien")        // never watched, here for 90 days
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/watched/settings", map[string]any{"sync": true}, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusOK)

	rules := map[string]any{"moviesWatchedDays": 30, "moviesUnwatchedDays": 60, "episodesWatchedDays": 0, "graceDays": 14, "keepTags": []string{}}
	withAct := func(on bool) map[string]any {
		out := map[string]any{"actForReal": on}
		for k, v := range rules {
			out[k] = v
		}
		return out
	}

	// Off until switched on in Media types.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", withAct(false), http.StatusConflict)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"lifecycle": true}, http.StatusOK)

	// Bad rules are refused, and Act for real needs a dry run with these rules.
	bad := withAct(false)
	bad["graceDays"] = 0
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", bad, http.StatusBadRequest)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", withAct(true), http.StatusBadRequest)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", withAct(false), http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", withAct(true), http.StatusBadRequest)

	dry := postJSON[map[string]any](t, client, base+"/api/lifecycle/dry-run", nil, http.StatusOK)
	if n := len(dry["items"].([]any)); n != 2 {
		t.Fatalf("dry run listed %d titles: %v", n, dry)
	}
	if len(shelfTitles(t, base, client)) != 0 {
		t.Fatal("the shelf is empty before Act for real")
	}
	for _, f := range []string{matrixFile, alienFile} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("a dry run must not remove anything: %v", err)
		}
	}

	// Changing a rule asks for a new dry run.
	rules["moviesWatchedDays"] = 31
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", withAct(true), http.StatusBadRequest)
	rules["moviesWatchedDays"] = 30
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", withAct(false), http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/lifecycle/dry-run", nil, http.StatusOK)

	// Act for real: the titles go on the shelf with the grace period ahead.
	got := postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", withAct(true), http.StatusOK)
	if got["onShelf"].(float64) != 2 {
		t.Fatalf("on the shelf: %v", got)
	}
	shelf := shelfTitles(t, base, client)
	if shelf["The Matrix (2000)"] != 14 || shelf["Alien (2000)"] != 14 {
		t.Fatalf("shelf: %v", shelf)
	}
	if err := server.TestRunLifecycle(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{matrixFile, alienFile} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("nothing is removed during the grace period: %v", err)
		}
	}

	// Keep exempts a title; a title that isn't on the shelf can't be kept.
	postJSON[map[string]any](t, client, base+"/api/lifecycle/keep", map[string]any{"kind": "movie", "id": matrix.ID}, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/lifecycle/keep", map[string]any{"kind": "movie", "id": matrix.ID}, http.StatusNotFound)
	postJSON[map[string]any](t, client, base+"/api/lifecycle/keep", map[string]any{"kind": "tv", "id": 1}, http.StatusBadRequest)
	if shelf := shelfTitles(t, base, client); len(shelf) != 1 || shelf["Alien (2000)"] == 0 {
		t.Fatalf("shelf after keep: %v", shelf)
	}

	// After the grace period the title that was not kept goes, and the kept one stays.
	server.TestAgeLifecycleFlags()
	if err := server.TestRunLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(alienFile); !os.IsNotExist(err) {
		t.Errorf("Alien should be gone: %v", err)
	}
	if _, err := os.Stat(matrixFile); err != nil {
		t.Errorf("The Matrix was kept: %v", err)
	}
	if m, _ := server.MovieRepo.Get(alien.ID); m.FilePath != "" || m.Monitored {
		t.Errorf("Alien is still tracked: %+v", m)
	}
	if len(shelfTitles(t, base, client)) != 0 {
		t.Error("the shelf should be empty")
	}
	if acts := activityLines(t, client, base); !strings.Contains(acts, "Library lifecycle removed Alien (2000)") {
		t.Errorf("the removal isn't in Activity: %v", acts)
	}

	// Stopping to keep a title lets the rules see it again; switching Act for
	// real off empties the shelf.
	postJSON[map[string]any](t, client, base+"/api/lifecycle/unkeep", map[string]any{"kind": "movie", "id": matrix.ID}, http.StatusOK)
	if err := server.TestRunLifecycle(); err != nil {
		t.Fatal(err)
	}
	if len(shelfTitles(t, base, client)) != 1 {
		t.Error("The Matrix should be back on the shelf")
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", withAct(false), http.StatusOK)
	if len(shelfTitles(t, base, client)) != 0 {
		t.Error("the shelf should be empty with Act for real off")
	}
}

func TestLifecycleRefusesStaleWatchData(t *testing.T) {
	server, base, client := loginNewServer(t)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"lifecycle": true}, http.StatusOK)
	rules := map[string]any{"moviesWatchedDays": 30, "graceDays": 14, "keepTags": []string{}, "actForReal": false}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", rules, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/lifecycle/dry-run", nil, http.StatusOK)
	rules["actForReal"] = true
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/lifecycle", rules, http.StatusOK)
	// Nothing has been read from a media server, so a run does nothing.
	if err := server.TestRunLifecycle(); err == nil {
		t.Fatal("a run without fresh watch data should be refused")
	}
}
