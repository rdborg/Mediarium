package cleanup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/cleanup"
)

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// After a show's last video is deleted, the .nfo files and artwork in the
// season and show folders go with the empty folders (#180).
func TestClearLeftoversRemovesMetadataOnlyFolders(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "The Gantry Show (2005)")
	season := filepath.Join(show, "Season 01")
	touch(t, filepath.Join(season, "season.nfo"))
	touch(t, filepath.Join(season, "The Gantry Show - S01E01 - Pilot.nfo"))
	touch(t, filepath.Join(season, "The Gantry Show - S01E01 - Pilot.en.srt"))
	touch(t, filepath.Join(show, "tvshow.nfo"))
	touch(t, filepath.Join(show, "poster.jpg"))
	cleanup.ClearLeftovers(root, season)
	if exists(show) {
		t.Errorf("the show folder should be gone")
	}
	if !exists(root) {
		t.Errorf("the library root must stay")
	}
}

// A folder that still holds a video, a note or another season is left as it is.
func TestClearLeftoversStopsAtAnythingElse(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	s1 := filepath.Join(show, "Season 01")
	s2 := filepath.Join(show, "Season 02")
	touch(t, filepath.Join(s1, "season.nfo"))
	touch(t, filepath.Join(s2, "Show - S02E01.mkv"))
	touch(t, filepath.Join(show, "tvshow.nfo"))
	cleanup.ClearLeftovers(root, s1)
	if exists(s1) {
		t.Errorf("season 1 holds only metadata and should be gone")
	}
	if !exists(filepath.Join(show, "tvshow.nfo")) || !exists(filepath.Join(s2, "Show - S02E01.mkv")) {
		t.Errorf("the show folder still has another season and must stay whole")
	}

	note := filepath.Join(root, "Other", "Season 01")
	touch(t, filepath.Join(note, "season.nfo"))
	touch(t, filepath.Join(note, "readme.txt"))
	cleanup.ClearLeftovers(root, note)
	if !exists(filepath.Join(note, "season.nfo")) || !exists(filepath.Join(note, "readme.txt")) {
		t.Errorf("a folder with a text note is left alone")
	}
}

// The library root itself is never removed or emptied.
func TestClearLeftoversNeverTouchesTheRoot(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "folder.jpg"))
	cleanup.ClearLeftovers(root, root)
	if !exists(filepath.Join(root, "folder.jpg")) {
		t.Errorf("the root's own files must stay")
	}
}
