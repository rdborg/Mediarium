package api_test

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
)

// removalLine finds the activity line written when a title left the library.
func removalLine(t *testing.T, server *api.Server, title string) string {
	t.Helper()
	entries, err := server.QueueRepo.ListActivity(200)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, e := range entries {
		seen = append(seen, e.Message)
		if strings.HasPrefix(e.Message, title+" removed from library.") {
			return e.Message
		}
	}
	t.Fatalf("no removal line for %q in %v", title, seen)
	return ""
}

func TestRemovalLineSaysWhatHappenedToTheFiles(t *testing.T) {
	server, base, client := loginNewServer(t)
	movies := server.TestMoviesRoot()
	movieURL := func(id int64) string { return base + "/api/movies/" + strconv.FormatInt(id, 10) }

	keepFile := filepath.Join(movies, "Keep (2001)", "Keep (2001).mkv")
	putFile(t, keepFile, "keep")
	keepID := seedMovie(t, server.MovieRepo, 1, "Keep", 2001, keepFile)
	if code := deleteReq(t, client, movieURL(keepID)); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if got, want := removalLine(t, server, "Keep"), "Keep removed from library. Its files were kept (1 file)."; got != want {
		t.Errorf("kept: %q, want %q", got, want)
	}

	folder := filepath.Join(movies, "Gone (2002)")
	for _, f := range []string{"Gone (2002).mkv", "Gone (2002).en.srt", "poster.jpg"} {
		putFile(t, filepath.Join(folder, f), "x")
	}
	goneID := seedMovie(t, server.MovieRepo, 2, "Gone", 2002, filepath.Join(folder, "Gone (2002).mkv"))
	if code := deleteReq(t, client, movieURL(goneID)+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if got, want := removalLine(t, server, "Gone"), "Gone removed from library. Its files were moved to the recycle bin (3 files, 1 folder)."; got != want {
		t.Errorf("deleted: %q, want %q", got, want)
	}

	// A movie with no file has nothing to delete, even when asked to.
	emptyID := seedMovie(t, server.MovieRepo, 3, "Empty", 2003, "")
	if code := deleteReq(t, client, movieURL(emptyID)+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if got, want := removalLine(t, server, "Empty"), "Empty removed from library. It had no files on disk."; got != want {
		t.Errorf("no files: %q, want %q", got, want)
	}

	// The bulk remove writes the same lines.
	bulkFolder := filepath.Join(movies, "Bulk (2004)")
	putFile(t, filepath.Join(bulkFolder, "Bulk (2004).mkv"), "x")
	bulkID := seedMovie(t, server.MovieRepo, 4, "Bulk", 2004, filepath.Join(bulkFolder, "Bulk (2004).mkv"))
	keepBulk := filepath.Join(movies, "Bulk Keep (2005)", "Bulk Keep (2005).mkv")
	putFile(t, keepBulk, "x")
	bulkKeepID := seedMovie(t, server.MovieRepo, 5, "Bulk Keep", 2005, keepBulk)
	items := func(ids ...int64) []map[string]any {
		var out []map[string]any
		for _, id := range ids {
			out = append(out, map[string]any{"kind": string(library.KindMovie), "id": id})
		}
		return out
	}
	postJSON[map[string]any](t, client, base+"/api/library/bulk/remove", map[string]any{"items": items(bulkID), "deleteFiles": true}, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/library/bulk/remove", map[string]any{"items": items(bulkKeepID)}, http.StatusOK)
	if got, want := removalLine(t, server, "Bulk"), "Bulk removed from library. Its files were moved to the recycle bin (1 file, 1 folder)."; got != want {
		t.Errorf("bulk deleted: %q, want %q", got, want)
	}
	if got, want := removalLine(t, server, "Bulk Keep"), "Bulk Keep removed from library. Its files were kept (1 file)."; got != want {
		t.Errorf("bulk kept: %q, want %q", got, want)
	}
}
