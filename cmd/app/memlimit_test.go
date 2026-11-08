package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

func TestApplyMemoryLimit(t *testing.T) {
	write := func(t *testing.T, body string) string {
		root := t.TempDir()
		p := filepath.Join(root, "sys", "fs", "cgroup", "memory.max")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}
	before := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(before) })

	t.Run("follows the container limit", func(t *testing.T) {
		t.Setenv("GOMEMLIMIT", "")
		if got := applyMemoryLimit(write(t, "1073741824\n")); got != 912680550 {
			t.Fatalf("soft limit = %d", got)
		}
	})
	t.Run("an explicit GOMEMLIMIT wins", func(t *testing.T) {
		t.Setenv("GOMEMLIMIT", "500MiB")
		if got := applyMemoryLimit(write(t, "1073741824\n")); got != 0 {
			t.Fatalf("changed the limit: %d", got)
		}
	})
	t.Run("no limit or a tiny one is left alone", func(t *testing.T) {
		t.Setenv("GOMEMLIMIT", "")
		if got := applyMemoryLimit(write(t, "max\n")); got != 0 {
			t.Fatalf("limit = %d", got)
		}
		if got := applyMemoryLimit(write(t, "67108864\n")); got != 0 {
			t.Fatalf("tiny limit = %d", got)
		}
	})
}
