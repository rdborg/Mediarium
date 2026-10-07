package fsinfo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnwritableInside(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions can't be tested as root or on Windows")
	}
	root := t.TempDir()
	for _, d := range []string{"Fine (2020)/Season 01", "Locked (2026)/Season 01", "Half (2024)/Season 01", "Half (2024)/Season 02", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lock := func(p string) {
		if err := os.Chmod(filepath.Join(root, p), 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, p), 0o755) })
	}
	lock("Locked (2026)")
	lock("Half (2024)/Season 02")
	bad, checked := UnwritableInside(root, 100)
	want := []string{filepath.Join(root, "Half (2024)/Season 02"), filepath.Join(root, "Locked (2026)")}
	if len(bad) != 2 || bad[0] != want[0] || bad[1] != want[1] {
		t.Fatalf("bad = %v, want %v", bad, want)
	}
	if checked != 6 { // 3 titles + Fine's and Half's seasons; the locked title's inside isn't needed
		t.Fatalf("checked %d folders", checked)
	}
	if _, n := UnwritableInside(root, 2); n != 2 {
		t.Fatalf("the limit must hold, checked %d", n)
	}
}
