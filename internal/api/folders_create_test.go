package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAMissingLibraryFolder(t *testing.T) {
	_, base, client := loginNewServer(t)
	// A folder inside the source tree: in Docker that is the mapped /src, so
	// it stands in for a folder mapped from the device (t.TempDir would be
	// inside the container, where creating folders is rightly refused).
	root, err := os.MkdirTemp(".", "folder-create-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if root, err = filepath.Abs(root); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "Ebooks")

	check := getJSON[map[string]any](t, client, base+"/api/settings/folder-check?path="+want)
	if check["exists"] != false || check["canCreate"] != true {
		t.Fatalf("a missing folder inside a writable one can be created: %v", check)
	}
	got := postJSON[map[string]any](t, client, base+"/api/settings/folder-create", map[string]any{"path": want}, http.StatusOK)
	if got["exists"] != true || got["writable"] != true {
		t.Fatalf("created: %v", got)
	}
	if st, err := os.Stat(want); err != nil || !st.IsDir() {
		t.Fatalf("the folder is there: %v", err)
	}
	// Again: nothing to do, still fine.
	postJSON[map[string]any](t, client, base+"/api/settings/folder-create", map[string]any{"path": want}, http.StatusOK)

	// Two levels down, with the middle one missing: refused.
	deep := filepath.Join(root, "missing", "Books")
	if c := getJSON[map[string]any](t, client, base+"/api/settings/folder-check?path="+deep); c["canCreate"] == true {
		t.Fatalf("only one level is created: %v", c)
	}
	postJSON[map[string]any](t, client, base+"/api/settings/folder-create", map[string]any{"path": deep}, http.StatusConflict)
	postJSON[map[string]any](t, client, base+"/api/settings/folder-create", map[string]any{"path": "relative/path"}, http.StatusBadRequest)
}
