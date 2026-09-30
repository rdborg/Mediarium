package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"
)

// A stolen API key must not be able to leave itself a way back in: making
// accounts and further keys, changing accounts and restoring a backup (which
// replaces all of them) need a signed-in browser.
func TestAPIKeyCannotMakeAccountsKeysOrRestore(t *testing.T) {
	_, base, admin := loginNewServer(t)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]string{"name": "script"}, http.StatusCreated)["key"].(string)

	call := func(method, path string, body any, contentType string) int {
		t.Helper()
		var rdr *bytes.Reader
		switch b := body.(type) {
		case []byte:
			rdr = bytes.NewReader(b)
		default:
			enc, _ := json.Marshal(b)
			rdr = bytes.NewReader(enc)
		}
		req, err := http.NewRequest(method, base+path, rdr)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("X-API-Key", key)
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	fw, _ := mw.CreateFormFile("file", "backup.zip")
	fw.Write([]byte("not a zip"))
	mw.Close()

	for _, tc := range []struct {
		name, method, path string
		body               any
		contentType        string
	}{
		{"a new API key", "POST", "/api/auth/api-keys", map[string]string{"name": "second"}, "application/json"},
		{"a new account", "POST", "/api/users", map[string]string{"username": "eve", "password": "long-enough-password", "role": "admin"}, "application/json"},
		{"an account change", "PUT", "/api/users/1", map[string]string{"password": "another-long-password"}, "application/json"},
		{"a restore", "POST", "/api/system/restore", form.Bytes(), mw.FormDataContentType()},
		{"a backup download, which holds the encryption key", "GET", "/api/system/backup", struct{}{}, "application/json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := call(tc.method, tc.path, tc.body, tc.contentType); got != http.StatusForbidden {
				t.Fatalf("with an API key: %d, want 403", got)
			}
		})
	}

	// Reading is still fine with a key, and the browser session can do all of it.
	if got := call("GET", "/api/users", struct{}{}, "application/json"); got != http.StatusOK {
		t.Fatalf("listing accounts with a key: %d, want 200", got)
	}
	postJSON[map[string]any](t, admin, base+"/api/users", map[string]any{"username": "sam", "password": "kids-password", "role": "member"}, http.StatusCreated)
	postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]string{"name": "third"}, http.StatusCreated)
	resp, err := admin.Get(base + "/api/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a signed-in administrator downloading a backup: %d, want 200", resp.StatusCode)
	}
}

// What an administrator did to the program itself, and from which address,
// is not shown to basic users on the Activity page.
func TestMemberDoesNotSeeUpdateActivity(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	if err := server.QueueRepo.LogActivity(0, "update", "Mediarium 9.9.9 was installed by ryan (pushed through the API from 203.0.113.9, checksum abcdef123456). It restarts to use it."); err != nil {
		t.Fatal(err)
	}
	if err := server.QueueRepo.LogActivity(0, "added", "Fixture added to library"); err != nil {
		t.Fatal(err)
	}
	list := func(c *http.Client) []map[string]any {
		t.Helper()
		resp, err := c.Get(base + "/api/activity")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	has := func(entries []map[string]any, kind string) bool {
		for _, e := range entries {
			if e["eventType"] == kind {
				return true
			}
		}
		return false
	}
	if a := list(admin); !has(a, "update") || !has(a, "added") {
		t.Fatalf("the administrator should see both entries, got %v", a)
	}
	m := list(member)
	if has(m, "update") {
		t.Fatalf("a basic user sees the update entry: %v", m)
	}
	if !has(m, "added") {
		t.Fatalf("a basic user should still see ordinary entries: %v", m)
	}
}
