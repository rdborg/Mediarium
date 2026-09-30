package organizer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

func TestImportHardlinksWithinSameDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.mkv")
	if err := os.WriteFile(src, []byte("movie bytes"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	dest := filepath.Join(dir, "library", "Movie (2024)", "Movie (2024).mkv")

	result, err := organizer.Import(src, dest, organizer.ConflictSkip, nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !result.UsedHardlink {
		t.Error("expected hardlink to be used within the same filesystem")
	}
	if result.Skipped {
		t.Error("did not expect skip on first import")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != "movie bytes" {
		t.Fatalf("unexpected dest content: %q", got)
	}
}

func TestImportConflictSkip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.mkv")
	os.WriteFile(src, []byte("new content"), 0o644)
	dest := filepath.Join(dir, "existing.mkv")
	os.WriteFile(dest, []byte("old content"), 0o644)

	result, err := organizer.Import(src, dest, organizer.ConflictSkip, nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !result.Skipped {
		t.Error("expected skip when destination already exists")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "old content" {
		t.Fatal("expected existing file to be left untouched on skip")
	}
}

func TestImportConflictOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.mkv")
	os.WriteFile(src, []byte("new content"), 0o644)
	dest := filepath.Join(dir, "existing.mkv")
	os.WriteFile(dest, []byte("old content"), 0o644)

	result, err := organizer.Import(src, dest, organizer.ConflictOverwrite, nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Skipped {
		t.Error("expected overwrite, not skip")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "new content" {
		t.Fatalf("expected overwritten content, got %q", got)
	}
}

func TestImportConflictOverwriteIfBetterSkipsWhenNotBetter(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.mkv")
	os.WriteFile(src, []byte("new content"), 0o644)
	dest := filepath.Join(dir, "existing.mkv")
	os.WriteFile(dest, []byte("old content"), 0o644)

	result, err := organizer.Import(src, dest, organizer.ConflictOverwriteIfBetter, func() bool { return false })
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !result.Skipped {
		t.Error("expected skip when isBetter says no")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "old content" {
		t.Fatal("expected existing file to be left untouched")
	}
}

func TestImportConflictOverwriteIfBetterOverwritesWhenBetter(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.mkv")
	os.WriteFile(src, []byte("new content"), 0o644)
	dest := filepath.Join(dir, "existing.mkv")
	os.WriteFile(dest, []byte("old content"), 0o644)

	result, err := organizer.Import(src, dest, organizer.ConflictOverwriteIfBetter, func() bool { return true })
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Skipped {
		t.Error("expected overwrite when isBetter says yes")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "new content" {
		t.Fatalf("expected overwritten content, got %q", got)
	}
}

func TestImportConflictOverwriteIfBetterNilCallbackSkips(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.mkv")
	os.WriteFile(src, []byte("new content"), 0o644)
	dest := filepath.Join(dir, "existing.mkv")
	os.WriteFile(dest, []byte("old content"), 0o644)

	// A nil isBetter must fail closed (skip), never overwrite by default.
	result, err := organizer.Import(src, dest, organizer.ConflictOverwriteIfBetter, nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !result.Skipped {
		t.Error("expected skip when isBetter is nil")
	}
}

func TestFindLargestVideoFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sample.mkv"), make([]byte, 100), 0o644)
	os.WriteFile(filepath.Join(dir, "movie.mkv"), make([]byte, 10_000), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.nfo"), make([]byte, 50_000), 0o644)

	got, err := organizer.FindLargestVideoFile(dir)
	if err != nil {
		t.Fatalf("find largest video file: %v", err)
	}
	if filepath.Base(got) != "movie.mkv" {
		t.Fatalf("expected movie.mkv (largest video file, ignoring larger non-video nfo), got %s", got)
	}
}

func TestFindLargestVideoFileNoneFound(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644)
	if _, err := organizer.FindLargestVideoFile(dir); err == nil {
		t.Fatal("expected error when no video file exists")
	}
}

func TestFindVideoFilesSkipsSamplesAndSorts(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Sample"), 0o755)
	os.WriteFile(filepath.Join(dir, "Show.S01E02.1080p.mkv"), make([]byte, 10), 0o644)
	os.WriteFile(filepath.Join(dir, "Show.S01E01.1080p.mkv"), make([]byte, 10), 0o644)
	os.WriteFile(filepath.Join(dir, "show.s01e01.sample.mkv"), make([]byte, 5), 0o644)
	os.WriteFile(filepath.Join(dir, "Sample", "clip.mkv"), make([]byte, 5), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.nfo"), make([]byte, 5), 0o644)

	got, err := organizer.FindVideoFiles(dir)
	if err != nil {
		t.Fatalf("FindVideoFiles: %v", err)
	}
	want := []string{"Show.S01E01.1080p.mkv", "Show.S01E02.1080p.mkv"}
	if len(got) != len(want) {
		t.Fatalf("expected %d files, got %v", len(want), got)
	}
	for i, w := range want {
		if filepath.Base(got[i]) != w {
			t.Errorf("file %d: want %s, got %s", i, w, filepath.Base(got[i]))
		}
	}
}

func TestImportOverwriteLeavesNoTempFileAndKeepsSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.mkv")
	os.WriteFile(src, []byte("new content"), 0o644)
	dest := filepath.Join(dir, "lib", "movie.mkv")
	os.MkdirAll(filepath.Dir(dest), 0o755)
	os.WriteFile(dest, []byte("old content"), 0o644)

	if _, err := organizer.Import(src, dest, organizer.ConflictOverwrite, nil); err != nil {
		t.Fatalf("import: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(dest))
	if len(entries) != 1 || entries[0].Name() != "movie.mkv" {
		t.Fatalf("staging file should not be left behind: %v", entries)
	}
	if got, _ := os.ReadFile(src); string(got) != "new content" {
		t.Fatalf("source must be untouched, got %q", got)
	}
}

func TestImportFailureKeepsTheExistingFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "movie.mkv")
	os.WriteFile(dest, []byte("precious"), 0o644)

	if _, err := organizer.Import(filepath.Join(dir, "missing.mkv"), dest, organizer.ConflictOverwrite, nil); err == nil {
		t.Fatal("expected an error for a missing source")
	}
	if got, _ := os.ReadFile(dest); string(got) != "precious" {
		t.Fatalf("a failed overwrite must not destroy the existing file, got %q", got)
	}
}
