package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/api"
	"github.com/ryanborg/mediarium/internal/config"
	"github.com/ryanborg/mediarium/internal/crypto"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/store"
)

// newTwoQualityIndexerServer returns a fake Newznab indexer that always
// offers two releases for the same fixture movie: a WEBDL-1080p one and a
// Bluray-1080p one — enough for internal/quality's any-1080p preset to
// treat the Bluray release as a genuine upgrade over the WEBDL one, and
// nothing downstream of "which one got grabbed" (NZB fetch/NNTP download)
// needs to be real for these tests, which only check the hunt loop's
// selection logic, not the full download pipeline already proven in
// TestPhase1ExitCriteriaEndToEnd.
func newTwoQualityIndexerServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
<channel>
<item>
<title>The.Fixture.Movie.1999.1080p.WEB-DL.x264-FIXTURE</title>
<guid>fixture-guid-webdl</guid>
<enclosure url="%[1]s/nzb/webdl.nzb" length="1000" type="application/x-nzb" />
<newznab:attr name="size" value="1000"/>
<newznab:attr name="category" value="2000"/>
</item>
<item>
<title>The.Fixture.Movie.1999.1080p.BluRay.x264-FIXTURE</title>
<guid>fixture-guid-bluray</guid>
<enclosure url="%[1]s/nzb/bluray.nzb" length="2000" type="application/x-nzb" />
<newznab:attr name="size" value="2000"/>
<newznab:attr name="category" value="2000"/>
</item>
</channel>
</rss>`, "http://"+r.Host)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newHuntTestServer(t *testing.T) (*api.Server, *httptest.Server, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
	}
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	db, err := store.Open(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatalf("new api server: %v", err)
	}

	httpSrv := httptest.NewServer(server.Routes())
	t.Cleanup(httpSrv.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	return server, httpSrv, client
}

// TestHuntGrabsUpgradeForDownloadedMovie proves the automation hunt loop
// actually picks the better-quality release for an already-downloaded
// movie below its profile's cutoff, not just that internal/quality's
// IsUpgradeOverTier compiles — real HTTP indexer responses in, a real
// queue item with the Bluray release out.
func TestHuntGrabsUpgradeForDownloadedMovie(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	indexerSrv := newTwoQualityIndexerServer(t)

	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)

	// Seeded as already downloaded at WEBDL-1080p — below the default
	// any-1080p profile's Bluray-1080p cutoff, so it's a genuine upgrade
	// candidate.
	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := server.MovieRepo.SetStatus(movie.ID, library.StatusDownloaded, "WEBDL-1080p", "/movies/fixture.mkv"); err != nil {
		t.Fatalf("seed downloaded status: %v", err)
	}

	server.TestHunt(context.Background())

	deadline := time.Now().Add(5 * time.Second)
	var queueList []map[string]any
	for time.Now().Before(deadline) {
		queueList = getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(queueList) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(queueList) != 1 {
		t.Fatalf("expected exactly 1 queue item (the upgrade grab), got %d: %+v", len(queueList), queueList)
	}
	releaseTitle, _ := queueList[0]["releaseTitle"].(string)
	if releaseTitle != "The.Fixture.Movie.1999.1080p.BluRay.x264-FIXTURE" {
		t.Fatalf("expected the Bluray release to be grabbed as the upgrade, got %q", releaseTitle)
	}
	// The fixture indexer doesn't serve the actual NZB (this test only cares
	// about *which* release got selected, not the download itself — that's
	// covered by TestPhase1ExitCriteriaEndToEnd), so the pipeline goroutine
	// fails fetching it. Wait for that to actually happen before the test
	// ends, so it doesn't outlive the test and touch the DB after Cleanup
	// closes it.
	waitForQueueTerminal(t, client, httpSrv.URL)
}

// TestHuntSkipsMovieAlreadyAtCutoff proves the flip side: a movie already
// at (or above) the profile's cutoff must not be re-grabbed even though a
// nominally-better-titled release exists — this is what keeps upgrade
// hunting from endlessly re-downloading an already-good file.
func TestHuntSkipsMovieAlreadyAtCutoff(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	indexerSrv := newTwoQualityIndexerServer(t)

	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)

	// Already at Bluray-1080p — any-1080p's own cutoff — so even though the
	// indexer offers a Bluray release too, it must not be re-grabbed.
	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 606, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := server.MovieRepo.SetStatus(movie.ID, library.StatusDownloaded, "Bluray-1080p", "/movies/fixture.mkv"); err != nil {
		t.Fatalf("seed downloaded status: %v", err)
	}

	server.TestHunt(context.Background())

	// Give any (incorrect) background grab a moment to show up before
	// asserting its absence.
	time.Sleep(300 * time.Millisecond)
	queueList := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
	if len(queueList) != 0 {
		t.Fatalf("expected no grab for a movie already at cutoff, got %+v", queueList)
	}
}

// TestRSSSyncGrabsUpgrade proves the same upgrade-hunting logic is wired
// into rssSync, not just hunt — the two share pickUpgradeResult, but this
// exercises rssSync's own empty-query search + per-movie title matching
// path end to end.
func TestRSSSyncGrabsUpgrade(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	indexerSrv := newTwoQualityIndexerServer(t)

	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 607, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := server.MovieRepo.SetStatus(movie.ID, library.StatusDownloaded, "WEBDL-1080p", "/movies/fixture.mkv"); err != nil {
		t.Fatalf("seed downloaded status: %v", err)
	}

	server.TestRSSSync(context.Background())

	deadline := time.Now().Add(5 * time.Second)
	var queueList []map[string]any
	for time.Now().Before(deadline) {
		queueList = getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(queueList) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(queueList) != 1 {
		t.Fatalf("expected exactly 1 queue item (the upgrade grab), got %d: %+v", len(queueList), queueList)
	}
	releaseTitle, _ := queueList[0]["releaseTitle"].(string)
	if releaseTitle != "The.Fixture.Movie.1999.1080p.BluRay.x264-FIXTURE" {
		t.Fatalf("expected the Bluray release to be grabbed as the upgrade, got %q", releaseTitle)
	}
	waitForQueueTerminal(t, client, httpSrv.URL)
}

// waitForQueueTerminal waits for the single queue item to reach a terminal
// status (completed or failed) so the background pipeline goroutine that
// produced it is guaranteed to have finished before a test's t.Cleanup
// closes the DB out from under it.
func waitForQueueTerminal(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		queueList := getJSON[[]map[string]any](t, client, baseURL+"/api/queue")
		if len(queueList) == 1 {
			if status, _ := queueList[0]["status"].(string); status == "completed" || status == "failed" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the queue item to reach a terminal status")
}
