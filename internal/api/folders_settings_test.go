package api_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

// newFolderTestServer is a signed-in server whose environment names every
// media folder, the way a container with all the *_DIR variables set does.
func newFolderTestServer(t *testing.T) (string, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        "/data/downloads",
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           "/data/Movies",
		TVDir:               "/data/tv",
		MusicDir:            "/data/Music",
		EbooksDir:           "/ebooks",
		AudiobooksDir:       "/audiobooks",
	}
	if err := os.MkdirAll(cfg.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
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
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitBackground(t, server) })
	srv := httptest.NewServer(server.Routes())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postJSON[map[string]any](t, client, srv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple",
	}, http.StatusCreated)
	return srv.URL, client
}

// The settings answer names the folders the container really maps, and the
// folder boxes start from them: nothing saved means the environment value.
func TestSettingsReportTheFoldersFromTheEnvironment(t *testing.T) {
	base, client := newFolderTestServer(t)
	got := getJSON[map[string]any](t, client, base+"/api/settings")

	env, _ := got["containerFolders"].(map[string]any)
	want := map[string]string{
		"movies": "/data/Movies", "tv": "/data/tv", "downloads": "/data/downloads",
		"music": "/data/Music", "ebooks": "/ebooks", "audiobooks": "/audiobooks",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("containerFolders.%s = %v, want %s", k, env[k], v)
		}
	}
	// Nothing is saved yet, so every box shows what the container maps
	// (downloads used to come back empty here).
	for field, v := range map[string]string{
		"moviesPath": "/data/Movies", "tvPath": "/data/tv", "downloadsPath": "/data/downloads",
		"musicPath": "/data/Music", "ebooksPath": "/ebooks", "audiobooksPath": "/audiobooks",
	} {
		if got[field] != v {
			t.Errorf("%s = %v, want %s", field, got[field], v)
		}
	}
}

// A saved folder wins over the environment, but the environment value stays
// visible next to it so the wizard can offer to fix a stale one.
func TestSavedFoldersKeepTheEnvironmentValueVisible(t *testing.T) {
	base, client := newFolderTestServer(t)
	url := base + "/api/settings"
	got := postJSONMethod[map[string]any](t, client, http.MethodPut, url, map[string]any{
		"moviesPath": "/movies", "ebooksPath": " /media/books ", "audiobooksPath": "/media/audio",
	}, http.StatusOK)
	if got["moviesPath"] != "/movies" || got["ebooksPath"] != "/media/books" || got["audiobooksPath"] != "/media/audio" {
		t.Fatalf("saved folders not returned: %v %v %v", got["moviesPath"], got["ebooksPath"], got["audiobooksPath"])
	}
	env, _ := got["containerFolders"].(map[string]any)
	if env["movies"] != "/data/Movies" || env["ebooks"] != "/ebooks" {
		t.Fatalf("the environment values must not change with saved ones: %v", env)
	}
}

func TestEbooksAndAudiobooksFoldersAreChecked(t *testing.T) {
	base, client := newFolderTestServer(t)
	url := base + "/api/settings"
	wantBad(t, client, http.MethodPut, url, map[string]any{"ebooksPath": "books"}, "folder path isn't complete")
	wantBad(t, client, http.MethodPut, url, map[string]any{"audiobooksPath": "~/audio"}, "folder path isn't complete")
	wantOK(t, client, http.MethodPut, url, map[string]any{"ebooksPath": "/data/Ebooks", "audiobooksPath": "/data/Audiobooks"})
}

// A folder that does not exist yet is compared by the folder it will be made
// in, so a new Music folder inside the same mount is not reported as a
// different filesystem.
func TestFilesystemCheckUsesTheNearestExistingFolder(t *testing.T) {
	base, client := newFolderTestServer(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	url := base + "/api/settings/filesystem-check?a=" + filepath.Join(dir, "Music", "New") + "&b=" + filepath.Join(dir, "downloads")
	got := getJSON[map[string]any](t, client, url)
	if got["supported"] != true || got["error"] != nil || got["sameFilesystem"] != true {
		t.Fatalf("a folder not made yet should be judged by its parent: %v", got)
	}
}

// The folder check says whether Mediarium runs in a container, which decides
// whether the wizard offers a compose line.
func TestFolderCheckReportsInDocker(t *testing.T) {
	base, client := newFolderTestServer(t)
	got := getJSON[map[string]any](t, client, base+"/api/settings/folder-check?path="+filepath.Join(t.TempDir(), "nope"))
	if _, ok := got["inDocker"].(bool); !ok {
		t.Fatalf("folder-check should include inDocker: %v", got)
	}
	if got["exists"] != false {
		t.Fatalf("the folder should not exist: %v", got)
	}
}
