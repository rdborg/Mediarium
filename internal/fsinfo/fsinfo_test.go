package fsinfo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanborg/mediarium/internal/fsinfo"
)

func TestInspectHealthyFolder(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "keep.mkv")
	if err := os.WriteFile(existing, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := fsinfo.Inspect(dir)
	if !f.Exists || !f.IsDir || !f.Writable {
		t.Fatalf("a normal temp dir should be existing and writable: %+v", f)
	}
	if f.TotalBytes == 0 || f.FreeBytes == 0 || f.FreeBytes > f.TotalBytes {
		t.Fatalf("disk usage looks wrong: free %d of %d", f.FreeBytes, f.TotalBytes)
	}

	// Inspecting must leave everything exactly as it was: the existing file
	// intact and no probe file left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "keep.mkv" {
		t.Fatalf("the folder was modified: %v", entries)
	}
	if data, _ := os.ReadFile(existing); string(data) != "precious" {
		t.Fatal("an existing file was changed")
	}
}

func TestInspectMissingFolderExplainsWhatToDo(t *testing.T) {
	f := fsinfo.Inspect(filepath.Join(t.TempDir(), "not-mounted"))
	if f.Exists || len(f.Warnings) == 0 || !strings.Contains(f.Warnings[0], "Map a folder") {
		t.Fatalf("expected a helpful warning for a missing folder, got %+v", f)
	}
}

func TestInspectFileIsNotAFolder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := fsinfo.Inspect(file)
	if !f.Exists || f.IsDir || len(f.Warnings) == 0 {
		t.Fatalf("a file should be flagged: %+v", f)
	}
}

func TestCheckWritableLeavesNoTrace(t *testing.T) {
	dir := t.TempDir()
	if err := fsinfo.CheckWritable(dir); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("probe file left behind: %v", entries)
	}
}
