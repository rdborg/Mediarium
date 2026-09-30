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

// When the database cannot use its faster mode (a settings folder on a
// network drive), the dashboard says so. On a normal disk it says nothing.
func TestHealthWarnsWhenDatabaseFallsBackToTheOlderMode(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
	}
	if err := os.MkdirAll(cfg.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"normal disk", filepath.Join(cfg.ConfigDir, "app.db"), false},
		{"database that cannot use the faster mode", ":memory:", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := store.Open(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			server, err := api.New(db, cfg, box, "", "test")
			if err != nil {
				t.Fatal(err)
			}
			httpSrv := httptest.NewServer(server.Routes())
			t.Cleanup(httpSrv.Close)
			jar, _ := cookiejar.New(nil)
			client := &http.Client{Jar: jar}
			postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
				"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
			}, http.StatusCreated)

			health := getJSON[map[string]any](t, client, httpSrv.URL+"/api/health")
			found := false
			items, _ := health["items"].([]any)
			for _, raw := range items {
				if raw.(map[string]any)["id"] == "database-mode" {
					found = true
				}
			}
			if found != tc.want {
				t.Fatalf("database-mode item present = %v, want %v (items: %+v)", found, tc.want, items)
			}
		})
	}
}
