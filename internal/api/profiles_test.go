package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/api"
	"github.com/ryanborg/mediarium/internal/config"
	"github.com/ryanborg/mediarium/internal/crypto"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/settings"
	"github.com/ryanborg/mediarium/internal/store"
)

func putJSONStatus(t *testing.T, client *http.Client, url string, body any, want int) map[string]any {
	t.Helper()
	return postJSONMethod[map[string]any](t, client, http.MethodPut, url, body, want)
}

func profileIDByName(t *testing.T, list map[string]any, name string) int64 {
	t.Helper()
	for _, raw := range list["profiles"].([]any) {
		p := raw.(map[string]any)
		if p["name"] == name {
			return int64(p["id"].(float64))
		}
	}
	t.Fatalf("no profile named %q in %+v", name, list["profiles"])
	return 0
}

func TestProfilesCRUDAndGuards(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	base := httpSrv.URL + "/api/quality-profiles"

	list := getJSON[map[string]any](t, client, base)
	if len(list["profiles"].([]any)) != 3 || len(list["tiers"].([]any)) < 10 {
		t.Fatalf("expected 3 seeded profiles and the tier list, got %+v", list)
	}
	defaultID := int64(list["defaultId"].(float64))
	if defaultID != profileIDByName(t, list, "Up to 1080p") {
		t.Fatalf("default should be the 1080p preset, got %d", defaultID)
	}

	// Validation.
	postJSON[map[string]any](t, client, base, map[string]any{"name": "Bad", "allowed": []string{"WEBDL-720p"}, "cutoff": "Bluray-1080p"}, http.StatusBadRequest)
	postJSON[map[string]any](t, client, base, map[string]any{"name": "", "allowed": []string{"WEBDL-720p"}, "cutoff": "WEBDL-720p"}, http.StatusBadRequest)
	postJSON[map[string]any](t, client, base, map[string]any{"name": "Up to 1080p", "allowed": []string{"WEBDL-720p"}, "cutoff": "WEBDL-720p"}, http.StatusBadRequest)

	created := postJSON[map[string]any](t, client, base, map[string]any{
		"name": "Small files", "allowed": []string{"WEBDL-720p", "HDTV-720p"}, "cutoff": "WEBDL-720p", "upgradeAllowed": false,
	}, http.StatusCreated)
	id := int64(created["id"].(float64))
	if created["upgradeAllowed"] != false {
		t.Fatalf("upgradeAllowed should round-trip false: %+v", created)
	}

	// Assign it to a movie: now it's in use and can't be deleted.
	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "M", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	putJSONStatus(t, client, fmt.Sprintf("%s/api/movies/%d/profile", httpSrv.URL, movie.ID), map[string]any{"profileId": id}, http.StatusOK)
	if got := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", httpSrv.URL, movie.ID)); got["profileId"] != float64(id) {
		t.Fatalf("movie payload should carry its profile, got %+v", got)
	}
	if code := deleteReq(t, client, base+"/"+strconv.FormatInt(id, 10)); code != http.StatusConflict {
		t.Fatalf("deleting an in-use profile should be 409, got %d", code)
	}
	putJSONStatus(t, client, fmt.Sprintf("%s/api/movies/%d/profile", httpSrv.URL, movie.ID), map[string]any{"profileId": 0}, http.StatusOK)
	putJSONStatus(t, client, fmt.Sprintf("%s/api/movies/%d/profile", httpSrv.URL, movie.ID), map[string]any{"profileId": 9999}, http.StatusBadRequest)

	// The default can't be deleted either; anything else unreferenced can.
	if code := deleteReq(t, client, base+"/"+strconv.FormatInt(defaultID, 10)); code != http.StatusConflict {
		t.Fatalf("deleting the default profile should be 409, got %d", code)
	}
	if code := deleteReq(t, client, base+"/"+strconv.FormatInt(id, 10)); code != http.StatusOK {
		t.Fatalf("delete unreferenced profile: %d", code)
	}

	// Changing the default goes through settings.
	ultra := profileIDByName(t, getJSON[map[string]any](t, client, base), "Ultra-HD (up to 2160p)")
	putJSONStatus(t, client, httpSrv.URL+"/api/settings", map[string]any{"defaultProfileId": ultra}, http.StatusOK)
	if got := getJSON[map[string]any](t, client, base); int64(got["defaultId"].(float64)) != ultra {
		t.Fatalf("default not updated: %+v", got["defaultId"])
	}
	putJSONStatus(t, client, httpSrv.URL+"/api/settings", map[string]any{"defaultProfileId": 9999}, http.StatusBadRequest)
}

func TestHuntUsesEachItemsOwnProfile(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Idx", "definitionId": "fixture", "baseUrl": newTVIndexerWith(t, []string{"Some.Movie.2001.1080p.WEB-DL.x264-GRP"}).URL, "apiKey": "k",
	}, http.StatusCreated)

	// A 720p-only profile must not accept a 1080p release, while the same
	// release is fine for a movie on the default (up to 1080p) profile.
	only720 := postJSON[map[string]any](t, client, httpSrv.URL+"/api/quality-profiles", map[string]any{
		"name": "720p only", "allowed": []string{"WEBDL-720p"}, "cutoff": "WEBDL-720p", "upgradeAllowed": true,
	}, http.StatusCreated)

	picky, err := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Some Movie", Year: 2001, Monitored: true, ProfileID: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetProfile(picky.ID, int64(only720["id"].(float64))); err != nil {
		t.Fatal(err)
	}

	server.TestHunt(context.Background())
	time.Sleep(300 * time.Millisecond)
	if q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue"); len(q) != 0 {
		t.Fatalf("a 720p-only profile grabbed a 1080p release: %+v", q)
	}

	// Back on the default profile it is grabbed.
	if err := server.MovieRepo.SetProfile(picky.ID, 0); err != nil {
		t.Fatal(err)
	}
	server.TestHunt(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue"); len(q) == 1 {
			waitForQueueTerminal(t, client, httpSrv.URL)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the default profile should have grabbed the 1080p release")
}

func TestProfileWithUpgradesOffNeverUpgrades(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Idx", "definitionId": "fixture",
		"baseUrl": newTVIndexerWith(t, []string{"Some.Movie.2001.1080p.BluRay.x264-GRP"}).URL, "apiKey": "k",
	}, http.StatusCreated)
	once := postJSON[map[string]any](t, client, httpSrv.URL+"/api/quality-profiles", map[string]any{
		"name": "Grab once", "allowed": []string{"WEBDL-1080p", "Bluray-1080p"}, "cutoff": "Bluray-1080p", "upgradeAllowed": false,
	}, http.StatusCreated)

	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Some Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", "/movies/x.mkv")
	_ = server.MovieRepo.SetProfile(m.ID, int64(once["id"].(float64)))

	server.TestHunt(context.Background())
	server.TestRSSSync(context.Background())
	time.Sleep(300 * time.Millisecond)
	if q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue"); len(q) != 0 {
		t.Fatalf("upgrades are off for this profile but something was grabbed: %+v", q)
	}
}

// The old single global preset choice becomes the initial default profile.
func TestLegacyPresetSettingBecomesDefaultProfile(t *testing.T) {
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
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.New(db, box).Set(settings.KeyQualityProfile, "ultra-hd", false); err != nil {
		t.Fatal(err)
	}

	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatalf("new api server: %v", err)
	}
	httpSrv := httptest.NewServer(server.Routes())
	t.Cleanup(httpSrv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	list := getJSON[map[string]any](t, client, httpSrv.URL+"/api/quality-profiles")
	if int64(list["defaultId"].(float64)) != profileIDByName(t, list, "Ultra-HD (up to 2160p)") {
		t.Fatalf("the legacy ultra-hd choice should have become the default, got %+v", list)
	}
}
