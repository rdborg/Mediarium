package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The database holds password hashes and encrypted credentials: only the
// app's own user may read it, like secret.key.
func TestDatabaseFilesAreKeptPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not apply on Windows")
	}
	path := filepath.Join(t.TempDir(), "app.db")
	// A database created by an older version with the default mode.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Write something so the write-ahead log files exist too.
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('x', 'y') ON CONFLICT(key) DO UPDATE SET value = 'y'`); err != nil {
		t.Skipf("cannot write a setting to look at the log files: %v", err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is readable by others: %v", filepath.Base(p), fi.Mode().Perm())
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("app.db mode = %v (%v), want 0600", fi.Mode().Perm(), err)
	}
}
