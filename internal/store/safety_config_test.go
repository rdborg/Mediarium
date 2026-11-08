package store_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/store"
)

// The copy made before a settings file is applied has its own name, holds the
// data, and only the newest few are kept. It is not one of the copies that
// "going back after an update" lists.
func TestCopyBeforeSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `INSERT INTO settings (key, value, encrypted) VALUES ('marker', 'before', 0)`)
	dir := filepath.Dir(path)
	// Older copies already there.
	for _, n := range []string{"pre-config-20260101-000001.db", "pre-config-20260101-000002.db", "pre-config-20260101-000003.db"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.SnapshotBeforeConfig(db, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(got), "pre-config-") || filepath.Dir(got) != dir {
		t.Fatalf("the copy is next to the database: %s", got)
	}
	if v := store.CopySetting(got, "marker"); v != "before" {
		t.Fatalf("the copy holds the data, marker is %q", v)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "pre-config-*.db"))
	if len(matches) != 3 {
		t.Fatalf("the newest three are kept: %v", matches)
	}
	if _, err := os.Stat(filepath.Join(dir, "pre-config-20260101-000001.db")); !os.IsNotExist(err) {
		t.Fatal("the oldest copy is removed")
	}
	if len(store.SafetyCopies(path)) != 0 {
		t.Fatal("these copies are not offered for going back after an update")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(got); info.Mode().Perm() != 0o600 {
			t.Fatalf("the copy is private to the owner: %v", info.Mode())
		}
	}
}
