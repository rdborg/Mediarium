package api

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A destination folder Mediarium may not write to is caught before the
// download, not after it. A folder that doesn't exist yet is judged by the
// closest folder above it.
func TestDestWritable(t *testing.T) {
	root := t.TempDir()
	if err := destWritable(filepath.Join(root, "Show (2026)", "Season 01")); err != nil {
		t.Fatalf("a writable library must pass: %v", err)
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions can't be tested as root or on Windows")
	}
	locked := filepath.Join(root, "Locked (2026)", "Season 01")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	err := destWritable(locked)
	if err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("want a permission error, got %v", err)
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if filepath.Ext(e.Name()) != "" && e.Name()[0] == '.' {
			t.Fatalf("the check left a file behind: %s", e.Name())
		}
	}
}
