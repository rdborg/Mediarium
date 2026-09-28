package api_test

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/api"
	"github.com/ryanborg/mediarium/internal/store"
)

// exitSpy replaces the server's exit function so a staged restore does not
// end the test process, and lets the test wait for the call.
func exitSpy(server *api.Server) <-chan struct{} {
	called := make(chan struct{}, 4)
	server.SetExitFunc(func() { called <- struct{}{} })
	server.TestSetRestartDelay(time.Millisecond)
	return called
}

func neverExited(t *testing.T, called <-chan struct{}) {
	t.Helper()
	select {
	case <-called:
		t.Fatal("the app must not restart when the restore was rejected")
	case <-time.After(150 * time.Millisecond):
	}
}

func downloadBackup(t *testing.T, client *http.Client, base string) []byte {
	t.Helper()
	resp, err := client.Get(base + "/api/system/backup")
	if err != nil {
		t.Fatalf("GET backup: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET backup: status %d: %s", resp.StatusCode, body)
	}
	return body
}

// postRestore uploads data as the multipart field fieldName.
func postRestore(t *testing.T, client *http.Client, base, fieldName string, data []byte) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(fieldName, "backup.zip")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, base+"/api/system/restore", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST restore: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func zipEntries(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("response is not a zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = b
	}
	return out
}

func TestBackupEndpointsRequireLogin(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/system/backup"},
		{http.MethodPost, "/api/system/restore"},
		{http.MethodGet, "/api/system/info"},
	} {
		req, _ := http.NewRequest(tc.method, httpSrv.URL+tc.path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: status %d, want 401", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestBackupEndpointsAreAdminOnly(t *testing.T) {
	server, base, _ := loginNewServer(t)
	called := exitSpy(server)
	if _, err := server.Auth.CreateUser("guest", "another-long-password"); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	guest := &http.Client{Jar: jar}
	postJSON[map[string]any](t, guest, base+"/api/auth/login", map[string]string{"username": "guest", "password": "another-long-password"}, http.StatusOK)

	for _, path := range []string{"/api/system/backup", "/api/system/info"} {
		resp, err := guest.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]string
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(body["error"], "administrator") {
			t.Errorf("GET %s as a non-admin: %d %v, want 403 with an explanation", path, resp.StatusCode, body)
		}
	}
	if code, _ := postRestore(t, guest, base, "file", []byte("x")); code != http.StatusForbidden {
		t.Errorf("restore as a non-admin: status %d, want 403", code)
	}
	neverExited(t, called)
}

func TestSystemInfo(t *testing.T) {
	server, base, client := loginNewServer(t)
	_ = server
	info := getJSON[map[string]any](t, client, base+"/api/system/info")
	if info["version"] != "test" || info["configDir"] == "" || info["lastBackupHint"] != "" {
		t.Errorf("unexpected info: %+v", info)
	}
	if size, _ := info["dbSizeBytes"].(float64); size <= 0 {
		t.Errorf("dbSizeBytes should be the database file size, got %v", info["dbSizeBytes"])
	}
}

func TestBackupDownload(t *testing.T) {
	_, base, client := loginNewServer(t)

	resp, err := client.Get(base + "/api/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	disp := resp.Header.Get("Content-Disposition")
	if !regexp.MustCompile(`^attachment; filename="mediarium-backup-\d{8}-\d{6}\.zip"$`).MatchString(disp) {
		t.Errorf("Content-Disposition = %q", disp)
	}

	files := zipEntries(t, body)
	if len(files) != 3 {
		t.Fatalf("backup should hold 3 files, got %d", len(files))
	}
	var manifest struct {
		App, Version, CreatedAt, Migrations string
	}
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if manifest.App != "mediarium" || manifest.Version != "test" || manifest.Migrations == "" || manifest.CreatedAt == "" {
		t.Errorf("unexpected manifest: %+v", manifest)
	}
	if len(files["secret.key"]) == 0 {
		t.Error("secret.key missing from the backup")
	}

	// The snapshot is a real database containing the admin created at onboarding.
	dbPath := filepath.Join(t.TempDir(), "app.db")
	if err := os.WriteFile(dbPath, files["app.db"], 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer db.Close()
	var name string
	if err := db.QueryRow(`SELECT username FROM users WHERE is_admin = 1`).Scan(&name); err != nil || name != "ryan" {
		t.Errorf("snapshot should contain the admin account: %q, %v", name, err)
	}
}

func TestRestoreStagesABackupAndRestarts(t *testing.T) {
	// Server A supplies a backup; server B (another install) restores it.
	_, baseA, clientA := loginNewServer(t)
	backupZip := downloadBackup(t, clientA, baseA)

	serverB, baseB, clientB := loginNewServer(t)
	called := exitSpy(serverB)
	configDirB := getJSON[map[string]any](t, clientB, baseB+"/api/system/info")["configDir"].(string)
	liveKeyBefore, _ := os.ReadFile(filepath.Join(configDirB, "secret.key"))

	code, out := postRestore(t, clientB, baseB, "file", backupZip)
	if code != http.StatusOK || out["ok"] != true || out["restarting"] != true {
		t.Fatalf("restore: %d %+v", code, out)
	}
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("the app should restart after a successful restore")
	}

	pending := filepath.Join(configDirB, "restore-pending")
	staged := zipEntries(t, backupZip)
	for _, name := range []string{"app.db", "secret.key"} {
		got, err := os.ReadFile(filepath.Join(pending, name))
		if err != nil {
			t.Fatalf("%s not staged: %v", name, err)
		}
		if !bytes.Equal(got, staged[name]) {
			t.Errorf("staged %s differs from the uploaded backup's", name)
		}
	}
	// Nothing live changed: the running install keeps working until restart.
	liveKeyAfter, _ := os.ReadFile(filepath.Join(configDirB, "secret.key"))
	if !bytes.Equal(liveKeyBefore, liveKeyAfter) {
		t.Error("the live key must not change until the restart")
	}
	getJSON[map[string]any](t, clientB, baseB+"/api/auth/me")
	// The uploaded temp file was cleaned up.
	entries, _ := os.ReadDir(configDirB)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".restore-upload-") || strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temporary upload/staging leftover: %s", e.Name())
		}
	}
}

func TestRestoreRejectsBadUploads(t *testing.T) {
	_, baseA, clientA := loginNewServer(t)
	goodZip := downloadBackup(t, clientA, baseA)

	// A zip with an extra entry and one whose files are wrong.
	build := func(entries map[string][]byte) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for name, data := range entries {
			w, _ := zw.Create(name)
			w.Write(data)
		}
		zw.Close()
		return buf.Bytes()
	}
	good := zipEntries(t, goodZip)

	tests := []struct {
		name      string
		field     string
		data      []byte
		wantCode  int
		wantInMsg string
	}{
		{"not a zip", "file", []byte("plain text, not a backup"), http.StatusBadRequest, "not a valid Mediarium backup"},
		{"wrong form field", "upload", goodZip, http.StatusBadRequest, "No backup file"},
		{"extra entry", "file", build(map[string][]byte{"app.db": good["app.db"], "secret.key": good["secret.key"], "evil.sh": []byte("#!/bin/sh")}), http.StatusBadRequest, "unexpected entry"},
		{"path traversal entry", "file", build(map[string][]byte{"../app.db": good["app.db"], "secret.key": good["secret.key"]}), http.StatusBadRequest, "unexpected entry"},
		{"database is not sqlite", "file", build(map[string][]byte{"app.db": []byte("nope"), "secret.key": good["secret.key"]}), http.StatusBadRequest, "SQLite"},
		{"key is not a key", "file", build(map[string][]byte{"app.db": good["app.db"], "secret.key": []byte("short")}), http.StatusBadRequest, "encryption key"},
		{"missing key", "file", build(map[string][]byte{"app.db": good["app.db"]}), http.StatusBadRequest, "secret.key is missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, base, client := loginNewServer(t)
			called := exitSpy(server)
			configDir := getJSON[map[string]any](t, client, base+"/api/system/info")["configDir"].(string)

			code, out := postRestore(t, client, base, tt.field, tt.data)
			msg, _ := out["error"].(string)
			if code != tt.wantCode || !strings.Contains(msg, tt.wantInMsg) {
				t.Fatalf("status %d, error %q; want %d containing %q", code, msg, tt.wantCode, tt.wantInMsg)
			}
			if _, err := os.Stat(filepath.Join(configDir, "restore-pending")); !os.IsNotExist(err) {
				t.Error("a rejected upload must not stage anything")
			}
			neverExited(t, called)
			getJSON[map[string]any](t, client, base+"/api/auth/me") // still healthy
		})
	}

	t.Run("body is not multipart", func(t *testing.T) {
		server, base, client := loginNewServer(t)
		called := exitSpy(server)
		code, out := postRestore2(t, client, base, "application/json", []byte(`{"file":"x"}`))
		if code != http.StatusBadRequest || out["error"] == nil {
			t.Fatalf("status %d %+v, want 400 with an error", code, out)
		}
		neverExited(t, called)
	})
}

func postRestore2(t *testing.T, client *http.Client, base, contentType string, body []byte) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/api/system/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestRestoreRefusesABackupFromANewerVersion covers the schema check end to
// end: a backup whose database has a migration this binary does not know.
func TestRestoreRefusesABackupFromANewerVersion(t *testing.T) {
	_, baseA, clientA := loginNewServer(t)
	files := zipEntries(t, downloadBackup(t, clientA, baseA))

	dbPath := filepath.Join(t.TempDir(), "app.db")
	os.WriteFile(dbPath, files["app.db"], 0o600)
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ('9999_from_the_future')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	newer, _ := os.ReadFile(dbPath)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range map[string][]byte{"app.db": newer, "secret.key": files["secret.key"]} {
		w, _ := zw.Create(name)
		w.Write(data)
	}
	zw.Close()

	server, base, client := loginNewServer(t)
	called := exitSpy(server)
	code, out := postRestore(t, client, base, "file", buf.Bytes())
	if msg, _ := out["error"].(string); code != http.StatusBadRequest || !strings.Contains(msg, "newer version") {
		t.Fatalf("status %d %+v, want 400 mentioning a newer version", code, out)
	}
	neverExited(t, called)
}
