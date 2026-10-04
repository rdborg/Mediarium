package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASecondMovieFolderWorksEndToEnd(t *testing.T) {
	server, base, client := loginNewServer(t)
	extra := filepath.Join(t.TempDir(), "Movies2")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	// A folder that does not exist is refused.
	if code, _ := doJSONStatus(t, client, http.MethodPut, base+"/api/settings", map[string]any{"moviesExtraPaths": []string{"/no/such/folder"}}); code != http.StatusBadRequest {
		t.Fatalf("a missing folder: status %d", code)
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesExtraPaths": []string{extra}}, http.StatusOK)
	got := getJSON[map[string]any](t, client, base+"/api/settings")
	if list, _ := got["moviesExtraPaths"].([]any); len(list) != 1 || list[0] != extra {
		t.Fatalf("extra folders: %v", got["moviesExtraPaths"])
	}

	// A movie kept in the extra folder gets its file there.
	id := seedMovie(t, server.MovieRepo, 949, "Heat", 1995, "")
	if err := server.MovieRepo.SetRootPath(id, extra); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(filepath.Dir(server.TestWorkDir()), "complete", "Heat.1995.1080p.BluRay.x264-GRP.mkv")
	putFile(t, src, strings.Repeat("x", 21<<20))
	res := postJSON[map[string]any](t, client, base+"/api/manual-import", map[string]any{"path": "complete/Heat.1995.1080p.BluRay.x264-GRP.mkv", "movieId": id}, http.StatusOK)
	dest, _ := res["path"].(string)
	if !strings.HasPrefix(dest, extra+string(filepath.Separator)) {
		t.Fatalf("the file went to %s, not the extra folder", dest)
	}

	// Removing it with its files works there too, through that folder's recycle bin.
	if code := deleteReq(t, client, base+"/api/movies/"+itoa(id)+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("remove: status %d", code)
	}
	if exists(dest) {
		t.Fatal("the file should have left the library")
	}
	bin := getJSON[struct {
		Items []struct{ Kind string } `json:"items"`
	}](t, client, base+"/api/trash")
	if len(bin.Items) != 1 || bin.Items[0].Kind != "movies-2" {
		t.Fatalf("recycle bin: %+v", bin)
	}
}
