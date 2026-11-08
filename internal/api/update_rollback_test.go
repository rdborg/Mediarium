package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/backup"
	"github.com/rdborg/mediarium/internal/selfupdate"
)

// Going back after an update: the copy made before the update and the program
// kept from before it are offered, and choosing them stages the copy for the
// next start, puts the program back and restarts.
func TestRollbackPutsTheCopyAndTheProgramBack(t *testing.T) {
	e := newUpdateEnv(t)
	configDir := filepath.Dir(e.dir)
	e.server.TestSetDBPath(filepath.Join(configDir, backup.DBFile))

	// A copy of a database from before the update, made the way the real one is.
	old, err := api.OpenTestDB(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO settings (key, value, encrypted) VALUES ('system.version', '1.0.0', 0)`); err != nil {
		t.Fatal(err)
	}
	name := "pre-migration-0200_x-20261101-190000.db"
	if _, err := old.Exec(`VACUUM INTO '` + strings.ReplaceAll(filepath.Join(configDir, name), "'", "''") + `'`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	got := getJSON[map[string]any](t, e.client, e.base+"/api/system/update/rollback")
	copies, _ := got["copies"].([]any)
	if len(copies) != 1 || copies[0].(map[string]any)["name"] != name || copies[0].(map[string]any)["version"] != "1.0.0" || got["previous"] != nil {
		t.Fatalf("rollback state = %v", got)
	}

	for _, c := range []struct {
		body   map[string]any
		status int
	}{
		{map[string]any{}, http.StatusBadRequest},
		{map[string]any{"copy": "pre-migration-nope.db"}, http.StatusNotFound},
		{map[string]any{"copy": "../app.db"}, http.StatusNotFound},
		{map[string]any{"copy": name, "program": true}, http.StatusConflict}, // no earlier program
	} {
		postJSON[map[string]any](t, e.client, e.base+"/api/system/update/rollback", c.body, c.status)
	}
	if _, err := os.Stat(filepath.Join(configDir, backup.PendingDir)); !os.IsNotExist(err) {
		t.Fatal("a refused request left a restore staged")
	}
	e.mustNotExit(t)

	// Two installed updates: the first one is kept and can start again.
	for i, v := range []string{"1.1.0", "1.2.0"} {
		tmp := filepath.Join(e.dir, "up"+v)
		if err := os.MkdirAll(e.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmp, []byte("program "+v), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := selfupdate.Install(e.dir, tmp, v, strings.Repeat(string(rune('a'+i)), 64)); err != nil {
			t.Fatal(err)
		}
	}
	if got := getJSON[map[string]any](t, e.client, e.base+"/api/system/update/rollback"); got["previous"] != "1.1.0" {
		t.Fatalf("previous = %v", got["previous"])
	}

	res := postJSON[map[string]any](t, e.client, e.base+"/api/system/update/rollback", map[string]any{"copy": name, "program": true}, http.StatusAccepted)
	if res["version"] != "1.1.0" {
		t.Errorf("answer = %v", res)
	}
	e.mustExit(t)
	if _, err := os.Stat(filepath.Join(configDir, backup.PendingDir, backup.DBFile)); err != nil {
		t.Errorf("the copy wasn't staged: %v", err)
	}
	if pushed, _ := selfupdate.State(e.dir); pushed == nil || pushed.Version != "1.1.0" || pushed.PreviousVersion != "1.2.0" {
		t.Errorf("installed = %+v", pushed)
	}
}

// Going back throws away everything since the copy, so a request with an API
// key is refused.
func TestRollbackNeedsASignedInBrowser(t *testing.T) {
	e := newUpdateEnv(t)
	key := postJSON[map[string]any](t, e.client, e.base+"/api/auth/api-keys", map[string]string{"name": "script"}, http.StatusCreated)["key"].(string)
	req, _ := http.NewRequest(http.MethodPost, e.base+"/api/system/update/rollback", strings.NewReader(`{"program":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	e.mustNotExit(t)
}
