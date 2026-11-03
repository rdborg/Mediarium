package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
)

// A release a person picks by hand (Choose a release) for a title that has a
// file replaces that file when they agreed to it (replace: true), whatever the
// title's "Better versions" switch and "If a file already exists" say. Without
// it, the grab behaves as it always did.

const (
	hpOldContent = "the old version"
	hpMovieFile  = "The Fixture Movie (1999).mkv"
)

// binFiles lists the files in a library folder's recycle bin (path relative to
// the bin entry's files folder) with their contents.
func hpBinFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	bin := filepath.Join(root, ".mediarium-trash")
	_ = filepath.Walk(bin, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() == "entry.json" {
			return nil
		}
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(bin, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	return out
}

// binHolds reports whether any file in the bin has exactly this content.
func hpBinHolds(t *testing.T, root, content string) bool {
	t.Helper()
	for _, c := range hpBinFiles(t, root) {
		if c == content {
			return true
		}
	}
	return false
}

// grabByHand picks the fixture release for the movie the way the page does.
func (e *controlEnv) grabByHand(t *testing.T, replace bool) int64 {
	t.Helper()
	body := map[string]any{"releaseTitle": releaseName, "downloadUrl": e.nzbURL, "sizeBytes": 1000}
	if replace {
		body["replace"] = true
	}
	out := postJSON[map[string]int64](t, e.client, fmt.Sprintf("%s/api/movies/%d/grab", e.base, e.movie.ID), body, http.StatusAccepted)
	return out["queueId"]
}

func (e *controlEnv) finish(t *testing.T, id int64) map[string]any {
	t.Helper()
	waitFor(t, "the download to end", func() bool { s := e.status(t, id); return s == "completed" || s == "failed" })
	return e.item(t, id)
}

func TestHandPickedReleaseReplacesTheMoviesFile(t *testing.T) {
	sameName := hpMovieFile
	otherName := "The Fixture Movie (1999) [2160p].mkv"
	tests := []struct {
		name       string
		oldName    string
		oldQuality string
		noUpgrade  bool
		policy     string
		replace    bool
		binOff     bool
		replaced   bool
	}{
		{"same name, title left alone, picked by hand", sameName, "HDTV-720p", true, "", true, false, true},
		{"other name, title left alone, picked by hand", otherName, "HDTV-720p", true, "", true, false, true},
		{"a better file than the release is replaced when asked", sameName, "Remux-2160p", false, "", true, false, true},
		{"policy skip, picked by hand", sameName, "HDTV-720p", true, "skip", true, false, true},
		{"overwrite if better, not better, picked by hand", otherName, "Remux-2160p", false, "overwrite_if_better", true, false, true},
		{"recycle bin off: replaced in one rename, nothing binned", sameName, "HDTV-720p", true, "", true, true, true},
		// Without replace nothing changes.
		{"left alone, not asked", sameName, "HDTV-720p", true, "", false, false, false},
		{"not an upgrade, not asked", otherName, "Remux-2160p", false, "", false, false, false},
		{"other name, left alone, not asked", otherName, "HDTV-720p", true, "", false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newControlEnv(t, -1)
			if tc.policy != "" {
				putSettings(t, e, map[string]any{"importConflictPolicy": tc.policy})
			}
			if tc.binOff {
				putSettings(t, e, map[string]any{"trashDays": 0})
			}
			oldPath := filepath.Join(e.movies, "The Fixture Movie (1999)", tc.oldName)
			putFile(t, oldPath, hpOldContent)
			if err := e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, tc.oldQuality, oldPath); err != nil {
				t.Fatal(err)
			}
			if tc.noUpgrade {
				if err := e.server.MovieRepo.SetNoUpgrade(e.movie.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			newPath := filepath.Join(e.movies, "The Fixture Movie (1999)", hpMovieFile)

			id := e.grabByHand(t, tc.replace)
			item := e.finish(t, id)
			if item["status"] != "completed" {
				t.Fatalf("the download ended %v: %v", item["status"], item["error"])
			}
			m, _ := e.server.MovieRepo.Get(e.movie.ID)
			if tc.replaced {
				if item["error"] != nil && item["error"] != "" {
					t.Errorf("a replaced file is a normal import, but the row says %v", item["error"])
				}
				if got, _ := os.ReadFile(newPath); string(got) != string(e.wantFile) {
					t.Fatalf("the new file is not in place: %q", got)
				}
				if m.FilePath != newPath || m.Status != library.StatusDownloaded {
					t.Errorf("the movie should point at %s, got %s (%s)", newPath, m.FilePath, m.Status)
				}
				if tc.oldName != sameName && exists(oldPath) {
					t.Errorf("the old file under its other name is still in the library: %s", tree(t, e.movies))
				}
				wantBinned := !tc.binOff
				if got := hpBinHolds(t, e.movies, hpOldContent); got != wantBinned {
					t.Errorf("old file in the recycle bin = %v, want %v (%v)", got, wantBinned, hpBinFiles(t, e.movies))
				}
				if item["keptFile"] != nil && item["keptFile"] != "" {
					t.Errorf("nothing was kept, but the row names %v", item["keptFile"])
				}
				if left := hpLeftovers(t, filepath.Dir(newPath)); len(left) > 0 {
					t.Errorf("temporary files left behind: %v", left)
				}
				return
			}
			// Not asked: the old behaviour.
			if item["error"] != "Kept the file you already have." {
				t.Errorf("the row should say the file was kept, got %v", item["error"])
			}
			if got, _ := os.ReadFile(oldPath); string(got) != hpOldContent {
				t.Errorf("the file you have changed: %q", got)
			}
			if tc.oldName != sameName && exists(newPath) {
				t.Errorf("the release was imported next to the file that stays")
			}
			if m.FilePath != oldPath {
				t.Errorf("the movie should keep %s, got %s", oldPath, m.FilePath)
			}
			if len(hpBinFiles(t, e.movies)) != 0 {
				t.Errorf("nothing should have gone to the recycle bin: %v", hpBinFiles(t, e.movies))
			}
		})
	}
}

// leftovers lists temporary swap files in dir.
func hpLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		if strings.HasSuffix(en.Name(), ".mediarium-tmp") {
			out = append(out, en.Name())
		}
	}
	return out
}

// A kept download says which file it kept and what became of the data.
func TestKeptDownloadSaysWhichFileAndWhatBecameOfTheData(t *testing.T) {
	e := newControlEnv(t, -1)
	oldPath := filepath.Join(e.movies, "The Fixture Movie (1999)", hpMovieFile)
	putFile(t, oldPath, hpOldContent)
	if err := e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, "HDTV-720p", oldPath); err != nil {
		t.Fatal(err)
	}
	if err := e.server.MovieRepo.SetNoUpgrade(e.movie.ID, true); err != nil {
		t.Fatal(err)
	}
	id := e.grabByHand(t, false)
	item := e.finish(t, id)
	if item["error"] != "Kept the file you already have." {
		t.Fatalf("expected the file to be kept: %v", item["error"])
	}
	// The kept note is written just after the row is marked completed.
	waitFor(t, "the kept note", func() bool { item = e.item(t, id); return item["keptFile"] != nil })
	if item["keptFile"] != oldPath || item["keptData"] != "deleted" {
		t.Errorf("the row should name the kept file and say the data was deleted: %v / %v", item["keptFile"], item["keptData"])
	}
	if item["releaseTitle"] != releaseName {
		t.Errorf("the row should still name the rejected release: %v", item["releaseTitle"])
	}
	if exists(e.workDirFor(id)) {
		t.Errorf("the download's folder should be gone")
	}
	var feed, own bool
	for _, a := range getJSON[[]map[string]any](t, e.client, e.base+"/api/activity?limit=50") {
		if a["eventType"] == "kept" && strings.Contains(fmt.Sprint(a["message"]), oldPath) && strings.Contains(fmt.Sprint(a["message"]), releaseName) {
			feed = true
		}
	}
	for _, ev := range getJSON[[]map[string]any](t, e.client, fmt.Sprintf("%s/api/movies/%d/events", e.base, e.movie.ID)) {
		if ev["kind"] == "kept" && strings.Contains(fmt.Sprint(ev["message"]), "were deleted") {
			own = true
		}
	}
	if !feed {
		t.Errorf("the feed should have the kept line with the file and the release")
	}
	if !own {
		t.Errorf("the title's own log should say the downloaded files were deleted")
	}
}

func TestDiscardedWorkFolderReportsWhatBecameOfTheData(t *testing.T) {
	e := newControlEnv(t, -1)
	// Files laid out as links to an external download client's files.
	clientID := int64(900)
	dir := e.workDirFor(clientID)
	putFile(t, filepath.Join(dir, ".mediarium-client-content"), "/downloads/client/x")
	putFile(t, filepath.Join(dir, "x.mkv"), "x")
	if got := e.server.TestDiscardWorkDirReport(clientID); got != "client" {
		t.Errorf("a download in an external client: %q", got)
	}
	if exists(dir) {
		t.Errorf("the folder should be removed")
	}
	plain := int64(901)
	putFile(t, filepath.Join(e.workDirFor(plain), "x.mkv"), "x")
	if got := e.server.TestDiscardWorkDirReport(plain); got != "deleted" {
		t.Errorf("a plain download: %q", got)
	}
}

// The new file's own download never touches the old one: it stays while the
// download is only part way, and when the download is stopped.
func TestOldFileStaysWhileTheNewDownloadIsIncomplete(t *testing.T) {
	e := newControlEnv(t, 2)
	oldPath := filepath.Join(e.movies, "The Fixture Movie (1999)", hpMovieFile)
	putFile(t, oldPath, hpOldContent)
	_ = e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, "HDTV-720p", oldPath)
	_ = e.server.MovieRepo.SetNoUpgrade(e.movie.ID, true)
	id := e.grabByHand(t, true)
	waitFor(t, "the download to reach the held article", func() bool { return e.nntp.totalHits() >= 3 })
	if got, _ := os.ReadFile(oldPath); string(got) != hpOldContent {
		t.Fatalf("the old file changed while the new one was downloading: %q", got)
	}
	postJSON[map[string]any](t, e.client, e.url(id, "stop"), nil, http.StatusOK)
	if got, _ := os.ReadFile(oldPath); string(got) != hpOldContent {
		t.Fatalf("the old file changed when the download was stopped: %q", got)
	}
	m, _ := e.server.MovieRepo.Get(e.movie.ID)
	if m.FilePath != oldPath {
		t.Errorf("the movie should still point at its file, got %q", m.FilePath)
	}
	if len(hpBinFiles(t, e.movies)) != 0 || len(hpLeftovers(t, filepath.Dir(oldPath))) != 0 {
		t.Errorf("nothing should be binned or staged yet")
	}
}

// A file that belongs to another movie is never replaced, picked by hand or not.
func TestHandPickedReplaceNeverTouchesAnotherTitlesFile(t *testing.T) {
	e := newControlEnv(t, -1)
	mine := filepath.Join(e.movies, "The Fixture Movie (1999)", "The Fixture Movie (1999) [720p].mp4")
	theirs := filepath.Join(e.movies, "The Fixture Movie (1999)", hpMovieFile) // where this release would land
	putFile(t, mine, hpOldContent)
	putFile(t, theirs, "another movie's file")
	_ = e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, "HDTV-720p", mine)
	other, err := e.server.MovieRepo.Add(library.Movie{TMDBID: 604, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.server.MovieRepo.SetStatus(other.ID, library.StatusDownloaded, "HDTV-720p", theirs); err != nil {
		t.Fatal(err)
	}
	id := e.grabByHand(t, true)
	item := e.finish(t, id)
	if item["status"] != "failed" || !strings.Contains(fmt.Sprint(item["error"]), "already has its file") {
		t.Fatalf("the import should be refused: %v / %v", item["status"], item["error"])
	}
	if got, _ := os.ReadFile(theirs); string(got) != "another movie's file" {
		t.Fatalf("the other movie's file changed: %q", got)
	}
	if got, _ := os.ReadFile(mine); string(got) != hpOldContent {
		t.Fatalf("this movie's file changed: %q", got)
	}
	if len(hpBinFiles(t, e.movies)) != 0 {
		t.Errorf("nothing should be binned: %v", hpBinFiles(t, e.movies))
	}
}

// --- the swap itself ---

type swapEnv struct {
	server *api.Server
	root   string
	dest   string
	src    string
	movie  library.Movie
}

func newSwapEnv(t *testing.T) *swapEnv {
	t.Helper()
	server, _, _ := newControlServer(t, false)
	root := server.TestMoviesRoot()
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "The Fixture Movie (1999)", hpMovieFile)
	putFile(t, dest, hpOldContent)
	putFile(t, filepath.Join(dest, "..", "The Fixture Movie (1999).en.srt"), "old subtitles")
	src := filepath.Join(server.TestWorkDir(), "queue-1", "new.mkv")
	putFile(t, src, "the new version, a little longer")
	if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "HDTV-720p", dest); err != nil {
		t.Fatal(err)
	}
	return &swapEnv{server: server, root: root, dest: dest, src: src, movie: m}
}

func (e *swapEnv) oldIntact(t *testing.T) {
	t.Helper()
	if got, _ := os.ReadFile(e.dest); string(got) != hpOldContent {
		t.Fatalf("the file you have is not intact: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(filepath.Dir(e.dest), "The Fixture Movie (1999).en.srt")); string(got) != "old subtitles" {
		t.Fatalf("the subtitles of the file you have are not intact: %q", got)
	}
	if left := hpLeftovers(t, filepath.Dir(e.dest)); len(left) > 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
	if got := hpBinFiles(t, e.root); len(got) != 0 {
		t.Fatalf("nothing should be in the recycle bin: %v", got)
	}
}

func TestSwapPutsTheOldFileAndItsSidecarsInTheRecycleBin(t *testing.T) {
	e := newSwapEnv(t)
	binned, err := e.server.TestImportSwapping(e.root, e.src, e.dest, "The Fixture Movie (1999)", e.movie.ID)
	if err != nil || !binned {
		t.Fatalf("swap: binned=%v err=%v", binned, err)
	}
	if got, _ := os.ReadFile(e.dest); string(got) != "the new version, a little longer" {
		t.Fatalf("the new file is not in place: %q", got)
	}
	bin := hpBinFiles(t, e.root)
	if !hpBinHolds(t, e.root, hpOldContent) || !hpBinHolds(t, e.root, "old subtitles") || len(bin) != 2 {
		t.Fatalf("the old file and its subtitles should be in the bin: %v", bin)
	}
	if left := hpLeftovers(t, filepath.Dir(e.dest)); len(left) > 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
	// The download itself is not touched (it is cleaned up afterwards by the pipeline).
	if _, err := os.Stat(e.src); err != nil {
		t.Errorf("the source should still be there: %v", err)
	}
}

func TestSwapThatFailsAtTheLastStepPutsTheOldFileBack(t *testing.T) {
	e := newSwapEnv(t)
	restore := api.SwapRenameForTest(func(a, b string) error { return errors.New("injected failure") })
	defer restore()
	binned, err := e.server.TestImportSwapping(e.root, e.src, e.dest, "The Fixture Movie (1999)", e.movie.ID)
	if err == nil || binned {
		t.Fatalf("the swap should fail: binned=%v err=%v", binned, err)
	}
	e.oldIntact(t)
}

func TestSwapWithoutARecycleBinNeverTouchesTheOldFileUntilTheRename(t *testing.T) {
	e := newSwapEnv(t)
	if err := e.server.Settings.Set("library.trash_days", "0", false); err != nil {
		t.Fatal(err)
	}
	restore := api.SwapRenameForTest(func(a, b string) error { return errors.New("injected failure") })
	if _, err := e.server.TestImportSwapping(e.root, e.src, e.dest, "x", e.movie.ID); err == nil {
		t.Fatal("the swap should fail")
	}
	restore()
	e.oldIntact(t)
	binned, err := e.server.TestImportSwapping(e.root, e.src, e.dest, "x", e.movie.ID)
	if err != nil || binned {
		t.Fatalf("with the bin off the file is replaced in one rename: binned=%v err=%v", binned, err)
	}
	if got, _ := os.ReadFile(e.dest); string(got) != "the new version, a little longer" {
		t.Fatalf("the new file is not in place: %q", got)
	}
	if len(hpBinFiles(t, e.root)) != 0 {
		t.Errorf("nothing should be binned")
	}
}

func TestSwapWithAMissingOrBrokenDownloadLeavesTheOldFile(t *testing.T) {
	e := newSwapEnv(t)
	if _, err := e.server.TestImportSwapping(e.root, filepath.Join(e.root, "no-such-file.mkv"), e.dest, "x", e.movie.ID); err == nil {
		t.Fatal("a download that is not there can't replace anything")
	}
	e.oldIntact(t)
	// A link is never imported.
	link := filepath.Join(e.server.TestWorkDir(), "queue-1", "link.mkv")
	if err := os.Symlink(e.src, link); err == nil {
		if _, err := e.server.TestImportSwapping(e.root, link, e.dest, "x", e.movie.ID); err == nil {
			t.Fatal("a link can't replace a file")
		}
		e.oldIntact(t)
	}
}

func TestSwapOutsideTheLibraryFolderIsRefused(t *testing.T) {
	e := newSwapEnv(t)
	elsewhere := t.TempDir() // the old file's folder is not this root
	if _, err := e.server.TestImportSwapping(elsewhere, e.src, e.dest, "x", e.movie.ID); err == nil {
		t.Fatal("a file outside the library folder must not be moved to its bin")
	}
	e.oldIntact(t)
}

func TestSwapNeverReplacesAnotherTitlesFile(t *testing.T) {
	e := newSwapEnv(t)
	other, err := e.server.MovieRepo.Add(library.Movie{TMDBID: 604, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.server.MovieRepo.SetStatus(other.ID, library.StatusDownloaded, "HDTV-720p", e.dest); err != nil {
		t.Fatal(err)
	}
	// The file is down for the other movie too: whoever asks, it is not replaced.
	third, err := e.server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "Third", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.server.TestImportSwapping(e.root, e.src, e.dest, "x", third.ID); err == nil {
		t.Fatal("a file that belongs to another movie must not be replaced")
	}
	e.oldIntact(t)
}

// The release list uses the same rules as the import to say what an automatic
// grab would turn away (importKeeps: 0 nothing, 1 left alone, 2 unknown quality).
func TestImportKeepsFollowsTheImportRules(t *testing.T) {
	tests := []struct {
		policy       string
		noUpgrade    bool
		currentKnown bool
		want         int
	}{
		{"", false, true, 0},
		{"", true, true, 1},
		{"", true, false, 1},
		{"", false, false, 2},
		{"skip", true, true, 1},
		{"overwrite", true, true, 0},
		{"overwrite", true, false, 0},
		{"ask", true, true, 0},
		{"overwrite_if_better", true, true, 0}, // ignores the title's switch
		{"overwrite_if_better", true, false, 2},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q noUpgrade=%v known=%v", tc.policy, tc.noUpgrade, tc.currentKnown), func(t *testing.T) {
			e := newControlEnv(t, -1)
			if tc.policy != "" {
				putSettings(t, e, map[string]any{"importConflictPolicy": tc.policy})
			}
			if got := e.server.TestImportKeepsFor(tc.noUpgrade, tc.currentKnown); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}
