package backup_test

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	migrations "github.com/rdborg/mediarium/db"
	"github.com/rdborg/mediarium/internal/backup"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

// A config folder whose name holds characters that mean something inside a
// file: URI (# ? %) or a space must still restore. The staged database is
// opened through such a URI to check it.
func TestRestoreWorksInAConfigFolderWithAwkwardCharacters(t *testing.T) {
	for _, name := range []string{"has space", "hash#tag", "question?mark", "percent%20sign", "100%"} {
		t.Run(name, func(t *testing.T) {
			src, srcDir := newInstall(t, "from-backup")
			zipPath := writeBackupZip(t, src, srcDir)

			dir := filepath.Join(t.TempDir(), name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			live, err := store.Open(filepath.Join(dir, backup.DBFile))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := live.Exec(`INSERT INTO settings (key, value) VALUES ('marker', 'live')`); err != nil {
				t.Fatal(err)
			}
			live.Close()
			if _, err := crypto.LoadOrCreateKey(filepath.Join(dir, backup.KeyFile)); err != nil {
				t.Fatal(err)
			}

			if _, err := backup.Validate(dir, zipPath); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if _, err := backup.Stage(dir, zipPath); err != nil {
				t.Fatalf("Stage: %v", err)
			}
			res, err := backup.ApplyPending(dir, fixedNow)
			if err != nil {
				t.Fatalf("ApplyPending: %v", err)
			}
			if !res.Applied {
				t.Fatalf("the restore was not applied: %+v", res)
			}
			if got := readMarker(t, dir); got != "from-backup" {
				t.Errorf("marker = %q, want from-backup", got)
			}
		})
	}
}

// oldSchemaDB writes a database as an older version left it: only the first
// upTo migrations applied, with a marker row in settings.
func oldSchemaDB(t *testing.T, upTo int) []byte {
	t.Helper()
	entries, err := fs.ReadDir(migrations.MigrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if upTo > len(names) {
		t.Fatalf("only %d migrations exist", len(names))
	}
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`); err != nil {
		t.Fatal(err)
	}
	for _, n := range names[:upTo] {
		body, err := migrations.MigrationsFS.ReadFile("migrations/" + n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, strings.TrimSuffix(n, ".sql")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('marker', 'old-backup')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, is_admin) VALUES ('ryan', 'x', 0)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	return mustRead(t, path)
}

// A backup made by an older version restores, and the app's own start-up
// migrations bring it up to date with the data intact.
func TestRestoringAnOlderSchemaBackupMigratesOnNextStart(t *testing.T) {
	old := oldSchemaDB(t, 10)
	zipPath := buildZip(t, zipEntry{name: "app.db", data: old}, zipEntry{name: "secret.key", data: validKey(t)})

	_, dir := newInstall(t, "live")
	man, err := backup.Stage(dir, zipPath)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if !strings.HasPrefix(man.Migrations, "0010") {
		t.Errorf("manifest migrations = %q, want the backup's own 0010", man.Migrations)
	}
	res, err := backup.ApplyPending(dir, fixedNow)
	if err != nil || !res.Applied {
		t.Fatalf("ApplyPending = %+v, %v", res, err)
	}

	db, err := store.Open(filepath.Join(dir, backup.DBFile))
	if err != nil {
		t.Fatalf("opening the restored database (this runs the migrations): %v", err)
	}
	defer db.Close()
	latest, err := backup.LatestMigration()
	if err != nil {
		t.Fatal(err)
	}
	var top string
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&top); err != nil {
		t.Fatal(err)
	}
	if top != latest {
		t.Errorf("schema is at %s, want %s", top, latest)
	}
	var marker string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='marker'`).Scan(&marker); err != nil || marker != "old-backup" {
		t.Errorf("marker = %q, %v", marker, err)
	}
	var admin int
	if err := db.QueryRow(`SELECT is_admin FROM users WHERE username='ryan'`).Scan(&admin); err != nil || admin != 1 {
		t.Errorf("the account from before roles existed should become an administrator, got is_admin=%d, %v", admin, err)
	}
	var bad int
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		bad++
	}
	rows.Close()
	if bad != 0 {
		t.Errorf("foreign_key_check found %d problems", bad)
	}
}

// An empty upload is refused without leaving anything behind.
func TestStageRefusesAnEmptyFile(t *testing.T) {
	_, dir := newInstall(t, "live")
	before := dirNames(t, dir)

	empty := filepath.Join(t.TempDir(), "empty.zip")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Stage(dir, empty); err == nil {
		t.Fatal("an empty file must be refused")
	}

	after := dirNames(t, dir)
	if strings.Join(before, ",") != strings.Join(after, ",") {
		t.Errorf("config dir changed: %v -> %v", before, after)
	}
}
