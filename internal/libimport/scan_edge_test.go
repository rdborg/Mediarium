package libimport_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/libimport"
)

func TestScanTVSpecialsAreSeasonZero(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Show (2015)/Specials/Show.S00E01.Christmas.mkv", 10)
	touch(t, root, "Show (2015)/Season 01/Show.S01E01.mkv", 10)
	res, err := libimport.ScanTV(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || len(res.Groups[0].Files) != 2 || len(res.Skipped) != 0 {
		t.Fatalf("groups %+v skipped %v", res.Groups, res.Skipped)
	}
	seasons := map[int]bool{}
	for _, f := range res.Groups[0].Files {
		seasons[f.Season] = true
	}
	if !seasons[0] || !seasons[1] {
		t.Fatalf("want seasons 0 and 1, got %v", seasons)
	}

	files, skipped, err := libimport.ScanSeriesFolder(filepath.Join(root, "Show (2015)"))
	if err != nil || len(files) != 2 || len(skipped) != 0 {
		t.Fatalf("ScanSeriesFolder = %+v, %v, %v", files, skipped, err)
	}
}

func TestScanIgnoresHiddenStubsAndSampleParentFolders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sample", "Movies")
	touch(t, root, "Heat (1995)/Heat (1995).mkv", 10)
	touch(t, root, "Heat (1995)/._Heat (1995).mkv", 1)
	touch(t, root, "Heat (1995)/Samples/clip.mkv", 1)
	res, err := libimport.ScanMovies(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || len(res.Groups[0].Files) != 1 || res.Groups[0].Title != "Heat" {
		t.Fatalf("groups %+v skipped %v", res.Groups, res.Skipped)
	}
}

func TestScanOddFolderAndFileNames(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "1917 (2019)/1917.2019.1080p.BluRay.x264-GRP.mkv", 10)
	touch(t, root, "Cam (2018)/Cam.2018.1080p.WEBRip.mkv", 10)
	touch(t, root, "Spider-Man (2002)/movie.mkv", 10)
	touch(t, root, "Amélie (2001)/Amélie.2001.mkv", 10)
	touch(t, root, "千と千尋の神隠し (2001)/千と千尋の神隠し.mkv", 10)
	touch(t, root, "...And Justice for All (1979)/x.mkv", 10)
	touch(t, root, "Emoji 🎬 (2020)/x.mkv", 10)
	touch(t, root, strings.Repeat("Long ", 40)+"(2020)/x.mkv", 10)
	touch(t, root, ".mkv", 10)
	touch(t, root, "mkv.mkv", 10)
	touch(t, root, "2019.mkv", 10)
	res, err := libimport.ScanMovies(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"1917": 2019, "Cam": 2018, "Spider-Man": 2002, "Amélie": 2001, "千と千尋の神隠し": 2001}
	got := map[string]int{}
	for _, g := range res.Groups {
		got[g.Title] = g.Year
		if g.Title == "" || strings.TrimSpace(g.Title) != g.Title {
			t.Errorf("bad title %q", g.Title)
		}
	}
	for title, year := range want {
		if y, ok := got[title]; !ok || y != year {
			t.Errorf("want %q (%d), groups %v, skipped %v", title, year, got, res.Skipped)
		}
	}
}

func TestScanNestedSeasonFolders(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Show Name (2010)/Season 01/Show.Name.S01E01.720p.HDTV.mkv", 10)
	touch(t, root, "Show Name (2010)/Season 01/Extras/Behind.S01E01.mkv", 10)
	touch(t, root, "Show Name (2010)/Season.02/Show.Name.S02E01-E02.mkv", 10)
	touch(t, root, "Show Name (2010)/S03/s03e01.mkv", 10)
	touch(t, root, "Show Name (2010)/Show Name Season 4/Show.Name.S04E01.mkv", 10)
	res, err := libimport.ScanTV(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups %+v skipped %v", res.Groups, res.Skipped)
	}
	g := res.Groups[0]
	if g.Title != "Show Name" || g.Year != 2010 || len(g.Files) != 4 {
		t.Fatalf("group %+v", g)
	}
	for _, f := range g.Files {
		if f.Season == 2 && len(f.Episodes) != 2 {
			t.Errorf("multi-episode file read as %v", f.Episodes)
		}
	}
}

func TestScanSymlinkLoopsAndBrokenLinksTerminate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	touch(t, root, "Heat (1995)/Heat (1995).mkv", 10)
	if err := os.Symlink(root, filepath.Join(root, "Heat (1995)", "loop")); err != nil {
		t.Skip("cannot make a symlink here")
	}
	if err := os.Symlink(filepath.Join(root, "missing.mkv"), filepath.Join(root, "Heat (1995)", "broken.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("self", filepath.Join(root, "self")); err != nil {
		t.Fatal(err)
	}
	res, err := libimport.ScanMovies(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range res.Groups {
		for _, f := range g.Files {
			if !strings.HasPrefix(f.Path, root) {
				t.Errorf("file outside the scan folder: %s", f.Path)
			}
		}
	}
}

func TestScanUnreadableFolders(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced here")
	}
	root := t.TempDir()
	touch(t, root, "Heat (1995)/Heat (1995).mkv", 10)
	touch(t, root, "Locked (2001)/Locked.mkv", 10)
	locked := filepath.Join(root, "Locked (2001)")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	res, err := libimport.ScanMovies(root)
	if err != nil {
		t.Fatalf("one unreadable folder must not fail the scan: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Title != "Heat" {
		t.Fatalf("groups %+v", res.Groups)
	}

	// The scan folder itself cannot be read: say so instead of finding nothing.
	if err := os.Chmod(root, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })
	if _, err := libimport.ScanMovies(root); err == nil {
		t.Fatal("expected an error for an unreadable scan folder")
	}
}

func TestScanMissingOrNotAFolder(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "file.mkv", 1)
	for _, path := range []string{filepath.Join(root, "nope"), filepath.Join(root, "file.mkv"), ""} {
		if _, err := libimport.ScanMovies(path); err == nil {
			t.Errorf("ScanMovies(%q): expected an error", path)
		}
		if _, err := libimport.ScanTV(path); err == nil {
			t.Errorf("ScanTV(%q): expected an error", path)
		}
	}
}
