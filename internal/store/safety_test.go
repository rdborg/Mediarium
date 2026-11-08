package store_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/store"
)

// An install that is about to get new tables gets a copy of its database
// first, private to the owner, and only the newest two copies are kept.
func TestACopyIsMadeBeforeTheTablesChange(t *testing.T) {
	names := migrationNames(t)
	path := oldInstall(t, len(names)-1, func(db *sql.DB) {
		mustExec(t, db, `INSERT INTO settings (key, value, encrypted) VALUES ('marker', 'before', 0)`)
	})
	last := strings.TrimSuffix(names[len(names)-1], ".sql")
	dir := filepath.Dir(path)
	// Two older copies already there.
	for i, n := range []string{"pre-migration-0001_a.db", "pre-migration-0002_b.db"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := int64(1000 + i)
		if err := os.Chtimes(filepath.Join(dir, n), time.Unix(old, 0), time.Unix(old, 0)); err != nil {
			t.Fatal(err)
		}
	}

	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	copyPath := store.Mode(db).SafetyCopy
	if filepath.Dir(copyPath) != dir || !regexp.MustCompile(`^pre-migration-`+regexp.QuoteMeta(last)+`-\d{8}-\d{6}\.db$`).MatchString(filepath.Base(copyPath)) {
		t.Fatalf("the copy is %q, want %s/pre-migration-%s-<date>-<time>.db", copyPath, dir, last)
	}
	info, err := os.Stat(copyPath)
	if err != nil {
		t.Fatalf("no copy made before the change: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("the copy is readable by others: %v", info.Mode().Perm())
	}
	// It is the database as it was: it has the marker, and not the new change.
	cp, err := sql.Open("sqlite", "file:"+filepath.ToSlash(copyPath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	var v string
	if err := cp.QueryRow(`SELECT value FROM settings WHERE key = 'marker'`).Scan(&v); err != nil || v != "before" {
		t.Errorf("the copy lacks the data: %q, %v", v, err)
	}
	var n int
	if err := cp.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, last).Scan(&n); err != nil || n != 0 {
		t.Errorf("the copy already has the new change (%d, %v)", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pre-migration-0001_a.db")); err == nil {
		t.Error("the oldest copy was kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "pre-migration-0002_b.db")); err != nil {
		t.Error("the second newest copy was removed")
	}
}

// Updating the same old database twice to the same change (as happens when a
// backup from before the update is restored) keeps both copies: the one from
// the real update is not replaced by the second.
func TestASecondUpdateKeepsTheFirstCopy(t *testing.T) {
	path := oldInstall(t, len(migrationNames(t))-1, nil)
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var copies []string
	for i := 0; i < 2; i++ {
		if i == 1 { // put the old database back, like a restore does
			for _, ext := range []string{"-wal", "-shm"} {
				_ = os.Remove(path + ext)
			}
			if err := os.WriteFile(path, old, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		copies = append(copies, store.Mode(db).SafetyCopy)
		db.Close()
	}
	if copies[0] == "" || copies[0] == copies[1] {
		t.Fatalf("the second update reused the first copy's name: %v", copies)
	}
	for _, c := range copies {
		if _, err := os.Stat(c); err != nil {
			t.Errorf("copy %s is gone: %v", filepath.Base(c), err)
		}
	}
}

// A new database, and one with nothing to apply, get no copy.
func TestNoCopyWhenNothingChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	for i := 0; i < 2; i++ { // first start creates it, the second has nothing to apply
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := store.Mode(db).SafetyCopy; got != "" {
			t.Errorf("start %d made a copy: %s", i+1, got)
		}
		db.Close()
	}
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "pre-migration-*")); len(m) != 0 {
		t.Errorf("copies on disk: %v", m)
	}
}

// A database that a newer version has already changed is still opened (changes
// that only add columns are harmless), but it is reported.
func TestADatabaseFromANewerVersionIsReported(t *testing.T) {
	path := oldInstall(t, len(migrationNames(t)), func(db *sql.DB) {
		mustExec(t, db, `INSERT INTO schema_migrations (version) VALUES ('9999_from_the_future')`)
	})
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("an older program must still start: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	got := store.Mode(db).NewerMigrations
	if len(got) != 1 || got[0] != "9999_from_the_future" {
		t.Fatalf("not reported: %v", got)
	}
	if msg := store.NewerMessage(got); !strings.Contains(msg, "newer version") || !strings.Contains(msg, "restore a backup") {
		t.Errorf("message doesn't say what to do: %s", msg)
	}
}

// Without a write-ahead log, SQLite is only crash-safe with full syncing.
func TestFallbackJournalSyncsFully(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if store.Mode(db).JournalMode == store.ModeWAL {
		t.Skip("this database could use the write-ahead log")
	}
	var sync int
	if err := db.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil {
		t.Fatal(err)
	}
	if sync != 2 { // 2 = FULL
		t.Errorf("synchronous = %d, want 2 (FULL)", sync)
	}
}

// The copies are listed newest first with the time of the update from their
// name, a name that isn't one of them is refused, and a setting is read from a
// copy without changing it.
func TestSafetyCopiesAreListed(t *testing.T) {
	path := oldInstall(t, len(migrationNames(t))-1, func(db *sql.DB) {
		mustExec(t, db, `INSERT INTO settings (key, value, encrypted) VALUES ('system.version', '3.9.2', 0), ('secret', 'x', 1)`)
	})
	dir := filepath.Dir(path)
	older := filepath.Join(dir, "pre-migration-0100_old-20261001-080000.db")
	if err := os.WriteFile(older, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	got := store.SafetyCopies(path)
	if len(got) != 2 || got[1].Name != filepath.Base(older) || got[0].Path != store.Mode(db).SafetyCopy {
		t.Fatalf("copies = %+v", got)
	}
	if want := time.Date(2026, 10, 1, 8, 0, 0, 0, time.Local); !got[1].At.Equal(want) {
		t.Errorf("the older copy is from %v, want %v", got[1].At, want)
	}
	if time.Since(got[0].At) > time.Minute || got[0].Size == 0 {
		t.Errorf("the new copy = %+v", got[0])
	}
	if p, ok := store.SafetyCopyPath(path, got[0].Name); !ok || p != got[0].Path {
		t.Errorf("SafetyCopyPath = %q, %v", p, ok)
	}
	for _, bad := range []string{"app.db", "../app.db", "pre-migration-nope.db", ""} {
		if _, ok := store.SafetyCopyPath(path, bad); ok {
			t.Errorf("%q was accepted", bad)
		}
	}
	if v := store.CopySetting(got[0].Path, "system.version"); v != "3.9.2" {
		t.Errorf("version in the copy = %q", v)
	}
	if v := store.CopySetting(got[0].Path, "secret"); v != "" {
		t.Errorf("an encrypted setting was read: %q", v)
	}
	if v := store.CopySetting(older, "system.version"); v != "" {
		t.Errorf("a broken copy gave %q", v)
	}
	if m, _ := filepath.Glob(got[0].Path + "-*"); len(m) != 0 {
		t.Errorf("reading the copy left files: %v", m)
	}
}
