package organizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindLargestVideoFileSaysWhatIsInTheFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a1b2c3d4"), make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release.nfo"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := FindLargestVideoFile(dir)
	if err == nil {
		t.Fatal("a folder without a video file must fail")
	}
	for _, want := range []string{"no video file found in", "2 files", "a1b2c3d4 (4.9 KiB)", "release.nfo (1 B)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	empty := t.TempDir()
	if _, err := FindLargestVideoFile(empty); err == nil || !strings.Contains(err.Error(), "the folder is empty") {
		t.Errorf("an empty folder should say so, got %v", err)
	}
}

func TestHumanBytes(t *testing.T) {
	for in, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 20_400_000_000: "19.0 GiB"} {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
