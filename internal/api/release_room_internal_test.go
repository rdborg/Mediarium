package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/problems"
)

func TestCheckRoomForRelease(t *testing.T) {
	dir := t.TempDir()
	if err := checkRoomForRelease(dir, 0); err != nil {
		t.Fatalf("an NZB with no sizes is never refused: %v", err)
	}
	if err := checkRoomForRelease(dir, 1<<20); err != nil {
		t.Fatalf("a small release fits: %v", err)
	}
	err := checkRoomForRelease(dir, 1<<60)
	if err == nil || !strings.Contains(err.Error(), "free disk space") {
		t.Fatalf("a release far bigger than the disk must be refused, got %v", err)
	}
	if problems.CodeFor(err, "") != problems.CodeDiskFull {
		t.Fatalf("the refusal should be a full-disk problem (so it is tried again later), got %q", problems.CodeFor(err, ""))
	}
	// What an earlier try already saved counts as room that is taken, not as
	// something still to fetch.
	if err := os.WriteFile(filepath.Join(dir, "part.bin"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkRoomForRelease(dir, 4096); err != nil {
		t.Fatalf("a release that is fully on disk already needs nothing more: %v", err)
	}
	if err := checkRoomForRelease(filepath.Join(dir, "missing"), 1<<60); err != nil {
		t.Fatalf("a folder that cannot be read is never a reason to refuse: %v", err)
	}
}
