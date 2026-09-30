package api

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMBIDPatternOnlyTakesUUIDs(t *testing.T) {
	for in, want := range map[string]bool{
		"aaaaaaaa-0000-4000-8000-000000000001":  true,
		"AAAAAAAA-0000-4000-8000-00000000000A":  true,
		"12345678":                              false,
		"../../etc/passwd":                      false,
		"aaaaaaaa-0000-4000-8000-00000000000":   false,
		"aaaaaaaa-0000-4000-8000-0000000000012": false,
		"aaaaaaaa-0000-4000-8000-00000000000g":  false,
		"":                                      false,
	} {
		if got := mbidPattern.MatchString(in); got != want {
			t.Errorf("mbidPattern(%q) = %v, want %v", in, got, want)
		}
	}
}

// Made-up ids cannot fill the disk: old "no cover" marks go first, then the
// oldest files until the limit holds.
func TestPruneCoverCacheKeepsTheLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	write("stale.none", 48*time.Hour)  // an old "no cover" mark
	write("leftover.tmp", 3*time.Hour) // an abandoned download
	write("fresh.tmp", time.Minute)    // one under way
	for i := 0; i < 6; i++ {
		write(fmt.Sprintf("cover-%d.img", i), time.Duration(6-i)*time.Hour) // cover-5 is the newest
	}
	write("notes.txt", 100*time.Hour) // not ours: never touched

	pruneCoverCache(dir, 4, 24*time.Hour)

	left := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left[e.Name()] = true
	}
	for _, gone := range []string{"stale.none", "leftover.tmp", "cover-0.img", "cover-1.img", "cover-2.img"} {
		if left[gone] {
			t.Errorf("%s should have been removed", gone)
		}
	}
	for _, kept := range []string{"fresh.tmp", "cover-3.img", "cover-4.img", "cover-5.img", "notes.txt"} {
		if !left[kept] {
			t.Errorf("%s should have been kept", kept)
		}
	}
}
