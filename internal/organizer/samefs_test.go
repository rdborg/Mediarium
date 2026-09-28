package organizer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanborg/mediarium/internal/organizer"
)

func TestSameFilesystem(t *testing.T) {
	base := t.TempDir()
	dirA := filepath.Join(base, "a")
	dirB := filepath.Join(base, "b")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}

	same, supported, err := organizer.SameFilesystem(dirA, dirB)
	if err != nil {
		t.Fatalf("SameFilesystem: %v", err)
	}
	if !supported {
		t.Skip("SameFilesystem not supported on this platform (expected on Windows dev machines — the real target is Linux/Docker)")
	}
	if !same {
		t.Error("expected two subdirectories of the same temp dir to be on the same filesystem")
	}
}

func TestSameFilesystemMissingPath(t *testing.T) {
	_, supported, err := organizer.SameFilesystem(t.TempDir(), filepath.Join(t.TempDir(), "does-not-exist"))
	if !supported {
		t.Skip("SameFilesystem not supported on this platform")
	}
	if err == nil {
		t.Error("expected an error for a nonexistent path")
	}
}
