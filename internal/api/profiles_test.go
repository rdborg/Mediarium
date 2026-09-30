package api_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
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
	if len(list["profiles"].([]any)) != 5 || len(list["tiers"].([]any)) < 10 {
		t.Fatalf("expected 5 seeded profiles and the tier list, got %+v", list)
	}
	defaultID := int64(list["defaultId"].(float64))
	if defaultID != profileIDByName(t, list, "1080p") {
		t.Fatalf("default should be the 1080p preset, got %d", defaultID)
	}

	// Validation.
	postJSON[map[string]any](t, client, base, map[string]any{"name": "Bad", "allowed": []string{"WEBDL-720p"}, "cutoff": "Bluray-1080p"}, http.StatusBadRequest)
	postJSON[map[string]any](t, client, base, map[string]any{"name": "", "allowed": []string{"WEBDL-720p"}, "cutoff": "WEBDL-720p"}, http.StatusBadRequest)
	postJSON[map[string]any](t, client, base, map[string]any{"name": "1080p", "allowed": []string{"WEBDL-720p"}, "cutoff": "WEBDL-720p"}, http.StatusBadRequest)

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
	ultra := profileIDByName(t, getJSON[map[string]any](t, client, base), "4K & over")
	putJSONStatus(t, client, httpSrv.URL+"/api/settings", map[string]any{"defaultProfileId": ultra}, http.StatusOK)
	if got := getJSON[map[string]any](t, client, base); int64(got["defaultId"].(float64)) != ultra {
		t.Fatalf("default not updated: %+v", got["defaultId"])
	}
	putJSONStatus(t, client, httpSrv.URL+"/api/settings", map[string]any{"defaultProfileId": 9999}, http.StatusBadRequest)
}

func TestProfileFallbackAPI(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	base := httpSrv.URL + "/api/quality-profiles"
	list := getJSON[map[string]any](t, client, base)
	for _, raw := range list["profiles"].([]any) {
		if fb, ok := raw.(map[string]any)["fallback"].([]any); !ok || len(fb) != 0 {
			t.Fatalf("every profile starts with an empty fallback list, got %+v", raw)
		}
	}
	cinema := profileIDByName(t, list, "Cinema recordings")
	hd := profileIDByName(t, list, "720p")
	main := profileIDByName(t, list, "1080p")

	fallbackOf := func(p map[string]any) []int64 {
		var out []int64
		for _, v := range p["fallback"].([]any) {
			out = append(out, int64(v.(float64)))
		}
		return out
	}
	// Unknown ids and the profile's own id are ignored; order is kept.
	body := map[string]any{"name": "1080p", "allowed": []string{"HDTV-1080p", "WEBDL-1080p", "Bluray-1080p"}, "cutoff": "Bluray-1080p",
		"upgradeAllowed": true, "fallback": []int64{cinema, 9999, main, hd}}
	updated := putJSONStatus(t, client, fmt.Sprintf("%s/%d", base, main), body, http.StatusOK)
	if got := fallbackOf(updated); len(got) != 2 || got[0] != cinema || got[1] != hd {
		t.Fatalf("fallback = %v, want [%d %d]", got, cinema, hd)
	}
	// An update that leaves fallback out keeps it.
	delete(body, "fallback")
	body["upgradeAllowed"] = false
	putJSONStatus(t, client, fmt.Sprintf("%s/%d", base, main), body, http.StatusOK)
	if got := fallbackOf(findProfile(t, getJSON[map[string]any](t, client, base), main)); len(got) != 2 {
		t.Fatalf("an update without fallback should keep it, got %v", got)
	}
	// Creating with a fallback works too.
	created := postJSON[map[string]any](t, client, base, map[string]any{
		"name": "Mine", "allowed": []string{"WEBDL-2160p"}, "cutoff": "WEBDL-2160p", "fallback": []int64{main},
	}, http.StatusCreated)
	if got := fallbackOf(created); len(got) != 1 || got[0] != main {
		t.Fatalf("created fallback = %v", got)
	}
	// Deleting a profile removes it from every fallback list.
	if code := deleteReq(t, client, fmt.Sprintf("%s/%d", base, cinema)); code != http.StatusOK {
		t.Fatalf("delete cinema: %d", code)
	}
	if got := fallbackOf(findProfile(t, getJSON[map[string]any](t, client, base), main)); len(got) != 1 || got[0] != hd {
		t.Fatalf("after deleting a fallback profile: %v", got)
	}
}

func findProfile(t *testing.T, list map[string]any, id int64) map[string]any {
	t.Helper()
	for _, raw := range list["profiles"].([]any) {
		if p := raw.(map[string]any); int64(p["id"].(float64)) == id {
			return p
		}
	}
	t.Fatalf("no profile %d", id)
	return nil
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

// startOnPreparedDB opens a fresh database, lets prepare put an older
// install's state in it, then starts the server on it (which runs the
// start-up profile seeding) and signs in.
func startOnPreparedDB(t *testing.T, prepare func(db *sql.DB, set *settings.Store)) (*http.Client, string) {
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
	prepare(db, settings.New(db, box))

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
	return client, httpSrv.URL
}

// The old single global preset choice becomes the initial default profile
// (mapped to its current equivalent).
func TestLegacyPresetSettingBecomesDefaultProfile(t *testing.T) {
	client, url := startOnPreparedDB(t, func(_ *sql.DB, set *settings.Store) {
		if err := set.Set(settings.KeyQualityProfile, "ultra-hd", false); err != nil {
			t.Fatal(err)
		}
	})
	list := getJSON[map[string]any](t, client, url+"/api/quality-profiles")
	if int64(list["defaultId"].(float64)) != profileIDByName(t, list, "4K & over") {
		t.Fatalf("the legacy ultra-hd choice should have become 4K & over as the default, got %+v", list)
	}
}

// An install from before the four presets existed: its untouched presets are
// redefined in place (same ids), so the default and assigned items follow.
func TestOldInstallPresetsUpgradedOnStart(t *testing.T) {
	var oldDefault, oldUltra, movieID int64
	client, url := startOnPreparedDB(t, func(db *sql.DB, set *settings.Store) {
		repo := quality.NewRepo(db)
		for _, p := range []quality.Profile{
			{Name: "Up to 1080p", UpgradeAllowed: true, Cutoff: quality.TierBluray1080p, Allowed: []quality.Tier{
				quality.TierHDTV720p, quality.TierWebDL720p, quality.TierBluray720p,
				quality.TierHDTV1080p, quality.TierWebDL1080p, quality.TierBluray1080p, quality.TierRemux1080p}},
			{Name: "Ultra-HD (up to 2160p)", UpgradeAllowed: true, Cutoff: quality.TierBluray2160p, Allowed: []quality.Tier{
				quality.TierWebDL1080p, quality.TierBluray1080p, quality.TierRemux1080p,
				quality.TierWebDL2160p, quality.TierBluray2160p, quality.TierRemux2160p}},
			{Name: "Any", UpgradeAllowed: true, Cutoff: quality.TierRemux2160p, Allowed: quality.AllTiers()},
		} {
			created, err := repo.Create(p)
			if err != nil {
				t.Fatal(err)
			}
			switch p.Name {
			case "Up to 1080p":
				oldDefault = created.ID
			case "Ultra-HD (up to 2160p)":
				oldUltra = created.ID
			}
		}
		if err := set.Set(settings.KeyDefaultProfileID, strconv.FormatInt(oldDefault, 10), false); err != nil {
			t.Fatal(err)
		}
		movies := library.NewRepo(db)
		m, err := movies.Add(library.Movie{TMDBID: 7, Title: "Old", Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		movieID = m.ID
		if err := movies.SetProfile(m.ID, oldUltra); err != nil {
			t.Fatal(err)
		}
	})

	list := getJSON[map[string]any](t, client, url+"/api/quality-profiles")
	if n := len(list["profiles"].([]any)); n != 5 {
		t.Fatalf("want exactly the five presets after the upgrade, got %d: %+v", n, list["profiles"])
	}
	if got := int64(list["defaultId"].(float64)); got != oldDefault || profileIDByName(t, list, "1080p") != oldDefault {
		t.Fatalf("the old default (Up to 1080p, id %d) should now be 1080p and still the default, got default %d: %+v", oldDefault, got, list)
	}
	if profileIDByName(t, list, "4K & over") != oldUltra {
		t.Fatalf("Ultra-HD should have become 4K & over in place")
	}
	if got := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", url, movieID)); got["profileId"] != float64(oldUltra) {
		t.Fatalf("the movie should still point at the upgraded profile, got %+v", got["profileId"])
	}
	// The settings payload exposes the default too.
	if s := getJSON[map[string]any](t, client, url+"/api/settings"); s["defaultProfileId"] != float64(oldDefault) {
		t.Fatalf("settings should expose defaultProfileId %d, got %+v", oldDefault, s["defaultProfileId"])
	}
}
