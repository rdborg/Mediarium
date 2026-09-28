package organizer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanborg/mediarium/internal/organizer"
)

func TestFindPrimaryArchives(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"movie.part1.rar", "movie.part2.rar", "movie.part10.rar",
		"other.rar",
		"leftover.r00", "leftover.r01",
		"notes.txt",
	} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}

	got, err := organizer.FindPrimaryArchives(dir)
	if err != nil {
		t.Fatalf("find primary archives: %v", err)
	}
	want := map[string]bool{"movie.part1.rar": true, "other.rar": true}
	if len(got) != len(want) {
		t.Fatalf("expected %d primary archives, got %d: %v", len(want), len(got), got)
	}
	for _, p := range got {
		if !want[filepath.Base(p)] {
			t.Errorf("unexpected primary archive included: %s", p)
		}
	}
}
