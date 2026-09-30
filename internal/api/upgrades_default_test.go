package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
)

// enableUpgrades switches "keep looking for better versions" on for every
// stored quality profile. A fresh install has it off, so a test about the
// search for upgrades turns it on first, as a person would.
func enableUpgrades(t *testing.T, server *api.Server) {
	t.Helper()
	profiles, err := server.QualityRepo.List()
	if err != nil || len(profiles) == 0 {
		t.Fatalf("list quality profiles: %v (%d)", err, len(profiles))
	}
	for _, p := range profiles {
		p.UpgradeAllowed = true
		if _, err := server.QualityRepo.Update(p); err != nil {
			t.Fatalf("update profile %q: %v", p.Name, err)
		}
	}
}

// On a fresh install nothing is searched again once it is downloaded, even
// when the file is below the profile's cutoff.
func TestFreshInstallLeavesDownloadedTitlesAlone(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	indexerSrv := newTwoQualityIndexerServer(t)
	signIn(t, client, httpSrv.URL)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)

	profiles := getJSON[map[string]any](t, client, httpSrv.URL+"/api/quality-profiles")
	for _, raw := range profiles["profiles"].([]any) {
		p := raw.(map[string]any)
		if p["upgradeAllowed"] != false {
			t.Fatalf("profile %v should start with better versions off", p["name"])
		}
	}

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetStatus(movie.ID, library.StatusDownloaded, "WEBDL-1080p", "/movies/fixture.mkv"); err != nil {
		t.Fatal(err)
	}

	if cutoff := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/wanted?kind=cutoff"); len(cutoff) != 0 {
		t.Fatalf("nothing should be listed as an upgrade: %v", cutoff)
	}
	server.TestHunt(context.Background())
	server.TestRSSSync(context.Background())
	time.Sleep(200 * time.Millisecond)
	if q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue"); len(q) != 0 {
		t.Fatalf("a downloaded title must not be searched again: %v", q)
	}

	// Switching it on for the profile is all it takes.
	enableUpgrades(t, server)
	if cutoff := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/wanted?kind=cutoff"); len(cutoff) != 1 {
		t.Fatalf("with better versions on the title is an upgrade candidate: %v", cutoff)
	}
}
