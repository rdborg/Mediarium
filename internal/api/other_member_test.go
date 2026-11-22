package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A member can look at the Other library but is not told where the server
// keeps it, and cannot change anything.
func TestOtherMemberSeesNoServerFolders(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	root := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/settings", map[string]any{"otherPath": root}, http.StatusOK)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/modules", map[string]any{"other": true}, http.StatusOK)
	item := postJSON[map[string]any](t, admin, base+"/api/other", map[string]any{"name": "Manuals"}, http.StatusCreated)
	incoming := filepath.Join(root, "in")
	if err := os.MkdirAll(incoming, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incoming, "a.pdf"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/api/other/%v", base, item["id"])
	postJSON[map[string]any](t, admin, url+"/import", map[string]any{"path": incoming}, http.StatusOK)

	list := getJSON[map[string]any](t, member, base+"/api/other")
	if list["root"] != "" {
		t.Errorf("a member was told the Other folder: %v", list["root"])
	}
	one := getJSON[map[string]any](t, member, url)
	for _, p := range []any{list["items"].([]any)[0].(map[string]any)["path"], one["path"]} {
		if s, _ := p.(string); strings.ContainsAny(s, `/\`) {
			t.Errorf("a member was told a server path: %q", s)
		}
	}
	for _, c := range []struct{ method, path string }{{http.MethodPost, "/api/other"}, {http.MethodPut, "/api/other/1"}, {http.MethodDelete, "/api/other/1"}, {http.MethodPost, "/api/other/1/import"}} {
		if status, _ := doStatus(t, member, c.method, base+c.path); status != http.StatusForbidden {
			t.Errorf("member %s %s: %d, want 403", c.method, c.path, status)
		}
	}
	if one["fileCount"] != float64(1) {
		t.Errorf("the member should still see what the item holds: %v", one)
	}
}
