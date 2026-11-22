package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
)

type otherEnv struct {
	server *api.Server
	base   string
	client *http.Client
	root   string
}

// newOtherEnv boots a server with an administrator and the Other module on,
// its folder in a temp dir.
func newOtherEnv(t *testing.T) *otherEnv {
	t.Helper()
	server, base, client := loginNewServer(t)
	root := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"otherPath": root}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"other": true}, http.StatusOK)
	return &otherEnv{server: server, base: base, client: client, root: root}
}

func TestOtherRoutesAnswer409WhileTheModuleIsOff(t *testing.T) {
	_, base, client := loginNewServer(t)
	for _, path := range []string{"/api/other", "/api/other/1"} {
		status, body := doStatus(t, client, http.MethodGet, base+path)
		if status != http.StatusConflict || !strings.Contains(fmt.Sprint(body["error"]), "Other is switched off") {
			t.Errorf("GET %s with Other off: %d %v", path, status, body)
		}
	}
	if status, _ := doStatus(t, client, http.MethodPost, base+"/api/other"); status != http.StatusConflict {
		t.Errorf("POST /api/other with Other off: %d", status)
	}
	mods := getJSON[map[string]any](t, client, base+"/api/modules")
	if on := mods["other"].(map[string]any)["enabled"]; on != false {
		t.Errorf("Other should start off, got %v", on)
	}
}

func TestOtherItemsAreMadeByNameAndRefuseBadNames(t *testing.T) {
	e := newOtherEnv(t)
	item := postJSON[map[string]any](t, e.client, e.base+"/api/other", map[string]any{"name": "  Home   Videos 1998 "}, http.StatusCreated)
	if item["name"] != "Home Videos 1998" || item["status"] != "missing" {
		t.Fatalf("item = %v", item)
	}
	postJSON[map[string]any](t, e.client, e.base+"/api/other", map[string]any{"name": "home videos 1998"}, http.StatusConflict)
	postJSON[map[string]any](t, e.client, e.base+"/api/other", map[string]any{"name": "   "}, http.StatusBadRequest)
	postJSON[map[string]any](t, e.client, e.base+"/api/other", map[string]any{"name": strings.Repeat("x", 201)}, http.StatusBadRequest)

	url := fmt.Sprintf("%s/api/other/%v", e.base, item["id"])
	renamed := postJSONMethod[map[string]any](t, e.client, http.MethodPut, url, map[string]any{"name": "Holiday tapes"}, http.StatusOK)
	if renamed["name"] != "Holiday tapes" {
		t.Errorf("renamed = %v", renamed)
	}
	list := getJSON[map[string]any](t, e.client, e.base+"/api/other")
	if items := list["items"].([]any); len(items) != 1 || list["root"] != e.root {
		t.Errorf("list = %v", list)
	}
}

func TestOtherDownloadByHandIsFiledUnderItsOwnFolder(t *testing.T) {
	e := newOtherEnv(t)
	articles := map[string]nntpArticle{
		"o1@x": {fileName: "Lecture 01.mkv", content: []byte("lecture one " + strings.Repeat("a", 300))},
		"o2@x": {fileName: "Lecture 02.mkv", content: []byte("lecture two " + strings.Repeat("b", 300))},
		"o3@x": {fileName: "notes.txt", content: []byte("notes")},
	}
	host, port := newArticleNNTPServer(t, articles)
	postJSON[map[string]any](t, e.client, e.base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2}, http.StatusCreated)

	// A new item, named after the release.
	status, body := postManual(t, e.client, e.base, manualForm{
		fields:   map[string]string{"other": "new"},
		fileName: "Some.Lecture.Series.nzb",
		file:     []byte(nzbFor(articles)),
	})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body %v", status, body)
	}
	waitBackground(t, e.server)

	list := getJSON[map[string]any](t, e.client, e.base+"/api/other")["items"].([]any)
	if len(list) != 1 {
		t.Fatalf("items = %v", list)
	}
	item := list[0].(map[string]any)
	if item["name"] != "Some Lecture Series" {
		t.Fatalf("the name should come from the release, got %v", item["name"])
	}
	folder := filepath.Join(e.root, "Some Lecture Series")
	if got, err := os.ReadFile(filepath.Join(folder, "Lecture 02.mkv")); err != nil || !strings.HasPrefix(string(got), "lecture two") {
		t.Fatalf("file in the library: %q (%v)", got, err)
	}
	if item["status"] != "downloaded" || item["path"] != folder || item["fileCount"] != float64(3) {
		t.Errorf("item after the download = %v", item)
	}
	q := getJSON[[]map[string]any](t, e.client, e.base+"/api/queue")
	if len(q) != 1 || q[0]["status"] != "completed" || q[0]["otherId"] != item["id"] || q[0]["title"] != "Some Lecture Series" {
		t.Fatalf("queue: %v", q)
	}

	// A second download into the same item needs the item named.
	if status, _ := postManual(t, e.client, e.base, manualForm{fields: map[string]string{"otherId": "9999"}, fileName: "a.nzb", file: []byte(nzbFor(articles))}); status != http.StatusNotFound {
		t.Errorf("an item that isn't there: %d", status)
	}
	if status, body := postManual(t, e.client, e.base, manualForm{fields: map[string]string{"otherId": "1", "movieId": "1"}, fileName: "a.nzb", file: []byte(nzbFor(articles))}); status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(body["error"]), "not both") {
		t.Errorf("an item and a movie: %d %v", status, body)
	}

	// Removing the item with its files takes the folder away.
	url := fmt.Sprintf("%s/api/other/%v?deleteFiles=true", e.base, item["id"])
	if status, _ := doStatus(t, e.client, http.MethodDelete, url); status != http.StatusOK {
		t.Fatalf("delete: %d", status)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Errorf("the item's folder should be gone: %v", err)
	}
}

func TestOtherImportMovesFilesIntoTheItemsFolder(t *testing.T) {
	e := newOtherEnv(t)
	item := postJSON[map[string]any](t, e.client, e.base+"/api/other", map[string]any{"name": "Game Soundtrack"}, http.StatusCreated)
	incoming := filepath.Join(e.root, "incoming")
	if err := os.MkdirAll(filepath.Join(incoming, "CD1"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"CD1/01 Intro.flac", "CD1/02 Theme.flac", "cover.jpg"} {
		if err := os.WriteFile(filepath.Join(incoming, filepath.FromSlash(f)), []byte("data "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	url := fmt.Sprintf("%s/api/other/%v/import", e.base, item["id"])
	postJSON[map[string]any](t, e.client, url, map[string]any{"path": filepath.Join(e.root, "missing")}, http.StatusBadRequest)
	got := postJSON[map[string]any](t, e.client, url, map[string]any{"path": incoming}, http.StatusOK)
	folder := filepath.Join(e.root, "Game Soundtrack")
	if got["status"] != "downloaded" || got["path"] != folder || got["fileCount"] != float64(3) {
		t.Fatalf("after the import: %v", got)
	}
	for _, f := range []string{"01 Intro.flac", "02 Theme.flac", "cover.jpg"} {
		if _, err := os.Stat(filepath.Join(folder, f)); err != nil {
			t.Errorf("%s should be in the item's folder: %v", f, err)
		}
	}
	if files := got["files"].([]any); len(files) != 3 {
		t.Errorf("files = %v", files)
	}
}

func TestOtherRenameKeepsTheFolderAndImportRefusesAWholeLibraryFolder(t *testing.T) {
	e := newOtherEnv(t)
	item := postJSON[map[string]any](t, e.client, e.base+"/api/other", map[string]any{"name": "Old Name"}, http.StatusCreated)
	id := item["id"]
	importURL := fmt.Sprintf("%s/api/other/%v/import", e.base, id)
	first := filepath.Join(e.root, "in1")
	second := filepath.Join(e.root, "in2")
	for _, d := range []string{first, second} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(first, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The whole Other folder is not something to import.
	postJSON[map[string]any](t, e.client, importURL, map[string]any{"path": e.root}, http.StatusBadRequest)
	postJSON[map[string]any](t, e.client, importURL, map[string]any{"path": first}, http.StatusOK)
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, fmt.Sprintf("%s/api/other/%v", e.base, id), map[string]any{"name": "New Name"}, http.StatusOK)
	got := postJSON[map[string]any](t, e.client, importURL, map[string]any{"path": second}, http.StatusOK)
	folder := filepath.Join(e.root, "Old Name")
	if got["path"] != folder || got["fileCount"] != float64(2) {
		t.Fatalf("after a rename the files should stay in one folder, got %v", got)
	}
	if _, err := os.Stat(filepath.Join(e.root, "New Name")); err == nil {
		t.Error("a second folder was made after the rename")
	}
}
