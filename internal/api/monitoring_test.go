package api_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
)

// TestStopMonitoringAMovieOnceDownloaded: with the switch on, a film that
// has been imported is no longer monitored, and the release it came from is
// kept with it.
func TestStopMonitoringAMovieOnceDownloaded(t *testing.T) {
	content := []byte("The fixture film. " + strings.Repeat("film-", 300))
	nntpSrv := newFakeNNTPServer(t, content)
	indexerSrv := newFakeIndexerServer(t)

	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir: filepath.Join(dir, "config"), DownloadsDir: filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"), DownloadsComplete: filepath.Join(dir, "downloads", "complete"),
		MoviesDir: filepath.Join(dir, "movies"),
	}
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := api.OpenTestDB(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := httptest.NewServer(server.Routes())
	defer httpSrv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	base := httpSrv.URL
	postJSON[map[string]any](t, client, base+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	// Off by default; the setting round-trips.
	if s := getJSON[map[string]any](t, client, base+"/api/settings"); s["unmonitorDownloaded"] != false || s["unmonitorFinished"] != false {
		t.Fatalf("both switches should start off: %v %v", s["unmonitorDownloaded"], s["unmonitorFinished"])
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesPath": cfg.MoviesDir, "namingPreset": "minimal", "unmonitorDownloaded": true}, http.StatusOK)
	if s := getJSON[map[string]any](t, client, base+"/api/settings"); s["unmonitorDownloaded"] != true {
		t.Fatalf("the switch didn't save: %v", s["unmonitorDownloaded"])
	}
	postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{
		"name": "Fixture Usenet", "host": nntpSrv.addr, "port": nntpSrv.port, "useSsl": false, "connections": 2,
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	res := getJSON[[]map[string]any](t, client, base+"/api/search?q=fixture")
	release := res[0]["title"].(string)
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", base, movie.ID), map[string]any{
		"releaseTitle": release, "downloadUrl": res[0]["downloadUrl"], "sizeBytes": int64(len(content)),
	}, http.StatusAccepted)
	deadline := time.Now().Add(20 * time.Second)
	var m library.Movie
	for time.Now().Before(deadline) {
		m, _ = server.MovieRepo.Get(movie.ID)
		// the film is marked downloaded a moment before it is un-monitored, so wait for both
		if m.Status == library.StatusDownloaded && !m.Monitored {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if m.Status != library.StatusDownloaded {
		t.Fatalf("the film didn't download: %+v", m)
	}
	if m.Monitored {
		t.Fatal("the film should no longer be monitored")
	}
	if m.ReleaseTitle != release {
		t.Fatalf("release title = %q, want %q", m.ReleaseTitle, release)
	}
}

// TestStopMonitoringAFinishedShow: with the switch on, a show that has ended
// stops being monitored once its last monitored episode is in, and not
// before.
func TestStopMonitoringAFinishedShow(t *testing.T) {
	e1 := []byte("episode one " + strings.Repeat("a", 400))
	e2 := []byte("episode two " + strings.Repeat("b", 400))
	env := newTVEnv(t, map[string]nntpArticle{
		"e1@example": {fileName: "Fixture.Show.S01E01.1080p.BluRay.x264-GRP.mkv", content: e1},
		"e2@example": {fileName: "Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv", content: e2},
	})
	base := env.httpSrv.URL
	putJSONStatus(t, env.client, base+"/api/settings", map[string]any{"unmonitorFinished": true}, http.StatusOK)

	// Season 2 isn't wanted, so season 1 is everything.
	putJSONStatus(t, env.client, fmt.Sprintf("%s/api/series/%d/seasons/2/monitored", base, env.seriesID), map[string]any{"monitored": false}, http.StatusOK)

	// The fixture show has ended, but episodes are still missing: a refresh
	// changes nothing.
	if code, _ := doJSONStatus(t, env.client, http.MethodPost, fmt.Sprintf("%s/api/series/%d/refresh", base, env.seriesID), nil); code/100 != 2 {
		t.Fatalf("refresh: %d", code)
	}
	if sr, _ := env.server.MovieRepo.GetSeries(env.seriesID); !sr.Monitored {
		t.Fatal("a show with missing episodes should stay monitored")
	}

	env.grab(t, "Fixture.Show.S01.1080p.BluRay.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the season pack to complete, got status=%q error=%q", status, errMsg)
	}
	ep, _ := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 1)
	if ep.ReleaseTitle == "" {
		t.Fatal("the episode should keep the release it came from")
	}
	// The fixture show says it has ended, so it stops being monitored
	// shortly after the import (a refresh runs in the background).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if sr, _ := env.server.MovieRepo.GetSeries(env.seriesID); !sr.Monitored {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("a finished show with every episode in should no longer be monitored")
}
