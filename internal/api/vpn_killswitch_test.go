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
	"github.com/rdborg/mediarium/internal/store"
)

// TestVPNKillSwitchBlocksTorrentGrabWhenDisconnected proves the VPN kill
// switch actually blocks a real grab through the HTTP API, end to end: with
// "require VPN for torrents" on and no VPN connected, a torrent grab must
// fail closed (queue item ends up "failed", never silently proceeds over a
// direct connection). internal/torrentclient's own tests already prove the
// lower-level dialer wiring; this proves the setting is actually wired up
// to the real grab path a user would hit.
func TestVPNKillSwitchBlocksTorrentGrabWhenDisconnected(t *testing.T) {
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
	defer db.Close()
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatalf("new api server: %v", err)
	}

	httpSrv := httptest.NewServer(server.Routes())
	defer httpSrv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	// Turn the kill switch on without ever connecting a VPN.
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{
		"requireVpnForTorrents": true,
	}, http.StatusOK)

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 604, Title: "The Kill-Switch Movie", Year: 2000, Monitored: true})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	grabResp := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", httpSrv.URL, movie.ID), map[string]any{
		"releaseTitle": "The Kill-Switch Movie 2000 1080p",
		"downloadUrl":  "magnet:?xt=urn:btih:0000000000000000000000000000000000000000&dn=fixture",
		"sizeBytes":    int64(123456),
		"protocol":     "torrent",
	}, http.StatusAccepted)
	if grabResp["queueId"] == nil {
		t.Fatalf("expected queueId in grab response, got %+v", grabResp)
	}

	deadline := time.Now().Add(10 * time.Second)
	var finalStatus, statusMessage string
	for time.Now().Before(deadline) {
		queueList := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(queueList) == 1 {
			finalStatus, _ = queueList[0]["status"].(string)
			statusMessage, _ = queueList[0]["error"].(string)
			if finalStatus == "completed" || finalStatus == "failed" {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalStatus != "failed" {
		t.Fatalf("expected the grab to fail closed (kill switch, no VPN connected), got status=%q message=%q", finalStatus, statusMessage)
	}
	if !strings.Contains(strings.ToLower(statusMessage), "vpn") {
		t.Fatalf("expected the failure message to mention the VPN requirement, got %q", statusMessage)
	}

	// The movie must go back to "missing", not get stuck on "downloading"
	// forever — same fail-closed contract as any other pipeline failure.
	// The queue row is marked failed a moment before the movie is reset, so
	// give it a little time.
	var finalMovie map[string]any
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		finalMovie = getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", httpSrv.URL, movie.ID))
		if finalMovie["status"] == "missing" {
			break
		}
	}
	if finalMovie["status"] != "missing" {
		t.Fatalf("expected movie status missing after a failed grab, got %+v", finalMovie)
	}
}
