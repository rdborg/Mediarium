package api_test

import (
	"net/http"
	"path"
	"strings"
	"testing"
)

// Clean-up and "delete the files too" work on the media folders, and the
// settings folder holds the database and the encryption key: a media folder
// may not be the whole disk, the settings folder, or anything around or inside it.
func TestMediaFolderCannotCoverTheSettingsFolder(t *testing.T) {
	server, base, admin := loginNewServer(t)
	cfg := strings.ReplaceAll(server.TestConfigDir(), "\\", "/")
	for name, folder := range map[string]string{
		"the whole disk":                "/",
		"the settings folder itself":    cfg,
		"a folder inside it":            cfg + "/movies",
		"the folder that holds it":      path.Dir(cfg),
		"the settings folder with dots": cfg + "/../" + path.Base(cfg),
	} {
		t.Run(name, func(t *testing.T) {
			status, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/settings", map[string]any{"moviesPath": folder})
			if status != http.StatusBadRequest {
				t.Fatalf("moviesPath %q: %d %v, want 400", folder, status, body)
			}
			if msg, _ := body["error"].(string); !strings.Contains(msg, "Pick a folder") && !strings.Contains(msg, "settings folder") {
				t.Errorf("the refusal should say why: %q", msg)
			}
		})
	}
	// An ordinary folder next to it is fine.
	other := path.Join(path.Dir(cfg), "media-movies")
	if status, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/settings", map[string]any{"moviesPath": other}); status != http.StatusOK {
		t.Fatalf("moviesPath %q: %d %v, want 200", other, status, body)
	}
}
