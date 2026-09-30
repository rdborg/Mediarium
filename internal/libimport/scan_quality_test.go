package libimport_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/libimport"
)

// An episode file name that says nothing about quality takes it from the
// folders it sits in.
func TestScanTVReadsQualityFromFolders(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Show A (2008)/Season 01/Show A - S01E01 - Pilot WEBDL-1080p.mkv", 10)
	touch(t, root, "Show A (2008)/Season 01/Show A - S01E02 - Two.mkv", 10)
	touch(t, root, "Show B [2160p BluRay]/Season 01/Show B - S01E01 - Pilot.mkv", 10)
	touch(t, root, "Show C (2010)/Season 01 720p HDTV/Show C - S01E01 - Pilot.mkv", 10)
	touch(t, root, "Show D (2012)/Season 01/Show D - S01E01 - Pilot.mkv", 10)

	res, err := libimport.ScanTV(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, g := range res.Groups {
		for _, f := range g.Files {
			got[filepath.Base(f.Path)] = f.Quality
		}
	}
	want := map[string]string{
		"Show A - S01E01 - Pilot WEBDL-1080p.mkv": "WEBDL-1080p",
		"Show A - S01E02 - Two.mkv":               "Unknown", // nothing in the file name or the folders
		"Show B - S01E01 - Pilot.mkv":             "Bluray-2160p",
		"Show C - S01E01 - Pilot.mkv":             "HDTV-720p",
		"Show D - S01E01 - Pilot.mkv":             "Unknown",
	}
	for name, q := range want {
		if got[name] != q {
			t.Errorf("%s: quality %q, want %q", name, got[name], q)
		}
	}
}

// Quality is read from the file name, then the folders above it, then the
// show's own folder.
func TestScanSeriesFolderReadsQualityFromFolders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Show (2008) [1080p BluRay]")
	touch(t, root, "Season 01/Show - S01E01 - Pilot.mkv", 10)
	touch(t, root, "Season 02 720p HDTV/Show - S02E01 - Again.mkv", 10)
	touch(t, root, "Season 03/Show - S03E01 - Third WEBDL-2160p.mkv", 10)

	files, _, err := libimport.ScanSeriesFolder(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]string{}
	for _, f := range files {
		got[f.Season] = f.Quality
	}
	want := map[int]string{1: "Bluray-1080p", 2: "HDTV-720p", 3: "WEBDL-2160p"}
	for season, q := range want {
		if got[season] != q {
			t.Errorf("season %d: quality %q, want %q", season, got[season], q)
		}
	}
}
