package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualImportMovesAFileIntoTheLibrary(t *testing.T) {
	server, base, client := loginNewServer(t)
	// A leftover in the downloads folder (the parent of the working folder).
	downloads := filepath.Dir(server.TestWorkDir())
	src := filepath.Join(downloads, "complete", "Heat.1995.1080p.BluRay.x264-GRP", "Heat.1995.1080p.BluRay.x264-GRP.mkv")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, make([]byte, 21<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	id := seedMovie(t, server.MovieRepo, 949, "Heat", 1995, "")

	type list struct {
		Files []struct {
			Path    string `json:"path"`
			Title   string `json:"title"`
			Year    int    `json:"year"`
			Quality string `json:"quality"`
		} `json:"files"`
	}
	got := getJSON[list](t, client, base+"/api/manual-import")
	if len(got.Files) != 1 || got.Files[0].Title != "Heat" || got.Files[0].Year != 1995 || got.Files[0].Quality != "Bluray-1080p" {
		t.Fatalf("list: %+v", got)
	}

	// Paths outside the downloads folder are refused.
	for _, bad := range []string{"../../etc/passwd", "/etc/passwd", ""} {
		if code, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/manual-import", map[string]any{"path": bad, "movieId": id}); code != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400", bad, code)
		}
	}

	res := postJSON[map[string]any](t, client, base+"/api/manual-import", map[string]any{"path": got.Files[0].Path, "movieId": id}, http.StatusOK)
	dest, _ := res["path"].(string)
	if !strings.HasPrefix(dest, server.TestMoviesRoot()) || !exists(dest) {
		t.Fatalf("imported to %q", dest)
	}
	m, err := server.MovieRepo.Get(id)
	if err != nil || m.Status != "downloaded" || m.FilePath != dest {
		t.Fatalf("movie after import: %+v (err %v)", m, err)
	}
}
