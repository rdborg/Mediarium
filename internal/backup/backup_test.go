package backup_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/backup"
	"github.com/ryanborg/mediarium/internal/crypto"
	"github.com/ryanborg/mediarium/internal/store"
)

var fixedNow = time.Date(2026, 9, 28, 14, 30, 5, 0, time.UTC)

// newInstall builds a config dir like a real one: a migrated app.db holding
// a marker setting, plus a secret.key. It returns the open db (caller closes
// it via t.Cleanup) and the dir.
func newInstall(t *testing.T, marker string) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, backup.DBFile))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('marker', ?)`, marker); err != nil {
		t.Fatalf("insert marker: %v", err)
	}
	if _, err := crypto.LoadOrCreateKey(filepath.Join(dir, backup.KeyFile)); err != nil {
		t.Fatalf("create key: %v", err)
	}
	return db, dir
}

func readMarker(t *testing.T, dir string) string {
	t.Helper()
	db, err := store.Open(filepath.Join(dir, backup.DBFile))
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer db.Close()
	var v string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='marker'`).Scan(&v); err != nil {
		t.Fatalf("read marker: %v", err)
	}
	return v
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func writeBackupZip(t *testing.T, db *sql.DB, dir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backup.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := backup.Create(context.Background(), db, dir, "v1.2.3", fixedNow, f); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	return path
}

type zipEntry struct {
	name string
	data []byte
	dir  bool
}

func buildZip(t *testing.T, entries ...zipEntry) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		name := e.name
		if e.dir {
			name += "/"
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(e.data)
	}
	zw.Close()
	path := filepath.Join(t.TempDir(), "crafted.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// dbBytes returns the bytes of a fresh migrated Mediarium database, optionally
// altered by mutate before it is closed.
func dbBytes(t *testing.T, mutate func(*sql.DB)) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(db)
	}
	db.Close()
	return mustRead(t, path)
}

func validKey(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "k")
	if _, err := crypto.LoadOrCreateKey(path); err != nil {
		t.Fatal(err)
	}
	return mustRead(t, path)
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestCreateWritesConsistentSnapshotManifestAndKey(t *testing.T) {
	db, dir := newInstall(t, "original")
	zipPath := writeBackupZip(t, db, dir)

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open backup zip: %v", err)
	}
	defer zr.Close()
	got := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = b
	}
	if len(got) != 3 {
		t.Fatalf("backup should hold exactly manifest.json, app.db and secret.key, got %d entries", len(got))
	}
	if !bytes.Equal(got[backup.KeyFile], mustRead(t, filepath.Join(dir, backup.KeyFile))) {
		t.Error("secret.key in the backup differs from the live key")
	}
	var m backup.Manifest
	if err := json.Unmarshal(got[backup.ManifestFile], &m); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	latest, _ := backup.LatestMigration()
	if m.App != "mediarium" || m.Version != "v1.2.3" || m.CreatedAt != "2026-09-28T14:30:05Z" || m.Migrations != latest || latest == "" {
		t.Errorf("unexpected manifest: %+v (latest migration %q)", m, latest)
	}
	if !bytes.HasPrefix(got[backup.DBFile], []byte("SQLite format 3\x00")) {
		t.Error("app.db in the backup is not a SQLite file")
	}
	// The temporary snapshot must not linger in the config dir.
	for _, n := range dirNames(t, dir) {
		if strings.HasPrefix(n, ".backup-snapshot-") {
			t.Errorf("snapshot temp file left behind: %s", n)
		}
	}
}

func TestBackupStageApplyRoundTrip(t *testing.T) {
	srcDB, srcDir := newInstall(t, "original")
	zipPath := writeBackupZip(t, srcDB, srcDir)
	wantKey := mustRead(t, filepath.Join(srcDir, backup.KeyFile))

	// The install being restored onto has different data, and stray WAL files.
	dstDB, dstDir := newInstall(t, "replaced-by-restore")
	dstDB.Close()
	oldKey := mustRead(t, filepath.Join(dstDir, backup.KeyFile))

	m, err := backup.Stage(dstDir, zipPath)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	latest, _ := backup.LatestMigration()
	if m.Migrations != latest {
		t.Errorf("staged manifest migrations = %q", m.Migrations)
	}
	// Staging alone must not touch the live data.
	if got := readMarker(t, dstDir); got != "replaced-by-restore" {
		t.Fatalf("live data changed during staging: %q", got)
	}
	for _, f := range []string{backup.DBFile, backup.KeyFile} {
		if _, err := os.Stat(filepath.Join(dstDir, backup.PendingDir, f)); err != nil {
			t.Errorf("%s not staged: %v", f, err)
		}
	}

	// Stray WAL files from the old database (created after the read above,
	// which would have cleaned them up).
	for _, n := range []string{"app.db-wal", "app.db-shm"} {
		if err := os.WriteFile(filepath.Join(dstDir, n), []byte("stale "+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := backup.ApplyPending(dstDir, fixedNow)
	if err != nil || !res.Applied || res.Failed {
		t.Fatalf("apply: result %+v, err %v", res, err)
	}
	if res.Message == "" || !strings.Contains(res.Message, res.BeforeDir) {
		t.Errorf("message should say where the old data went: %q", res.Message)
	}
	if got := readMarker(t, dstDir); got != "original" {
		t.Errorf("restored marker = %q, want original", got)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(dstDir, backup.KeyFile)), wantKey) {
		t.Error("restored secret.key is not the backup's key")
	}
	if _, err := os.Stat(filepath.Join(dstDir, backup.PendingDir)); !os.IsNotExist(err) {
		t.Error("restore-pending should be gone after applying")
	}
	// Nothing was deleted: the old files are all in before-restore-<ts>/.
	if filepath.Base(res.BeforeDir) != "before-restore-20260928-143005" {
		t.Errorf("unexpected before dir %s", res.BeforeDir)
	}
	for _, n := range []string{"app.db", "app.db-wal", "app.db-shm", "secret.key"} {
		if _, err := os.Stat(filepath.Join(res.BeforeDir, n)); err != nil {
			t.Errorf("old %s should be preserved: %v", n, err)
		}
	}
	if !bytes.Equal(mustRead(t, filepath.Join(res.BeforeDir, backup.KeyFile)), oldKey) {
		t.Error("old key was not preserved intact")
	}
	if got := readMarker(t, res.BeforeDir); got != "replaced-by-restore" {
		t.Errorf("old database not preserved: marker %q", got)
	}
	for _, n := range dirNames(t, dstDir) {
		if n == "app.db-wal" || n == "app.db-shm" {
			t.Errorf("stale %s left next to the restored database", n)
		}
	}

	// Applying again is a no-op.
	if res2, err := backup.ApplyPending(dstDir, fixedNow); err != nil || res2 != (backup.Result{}) {
		t.Errorf("second apply should do nothing: %+v, %v", res2, err)
	}
}

func TestApplyPendingWorksOnAFreshConfigDir(t *testing.T) {
	srcDB, srcDir := newInstall(t, "original")
	zipPath := writeBackupZip(t, srcDB, srcDir)
	fresh := t.TempDir()
	if _, err := backup.Stage(fresh, zipPath); err != nil {
		t.Fatalf("stage: %v", err)
	}
	res, err := backup.ApplyPending(fresh, fixedNow)
	if err != nil || !res.Applied {
		t.Fatalf("apply: %+v, %v", res, err)
	}
	if got := readMarker(t, fresh); got != "original" {
		t.Errorf("marker = %q", got)
	}
}

func TestStageRejectsBadBackups(t *testing.T) {
	key := validKey(t)
	goodDB := dbBytes(t, nil)
	tests := []struct {
		name    string
		zipPath func(t *testing.T) string
		wantMsg string
	}{
		{"not a zip", func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "junk.zip")
			os.WriteFile(p, []byte("this is definitely not a zip file"), 0o644)
			return p
		}, "not a valid Mediarium backup"},
		{"truncated zip", func(t *testing.T) string {
			good := buildZip(t, zipEntry{name: "app.db", data: goodDB}, zipEntry{name: "secret.key", data: key})
			b := mustRead(t, good)
			p := filepath.Join(t.TempDir(), "trunc.zip")
			os.WriteFile(p, b[:len(b)/2], 0o644)
			return p
		}, "not a valid Mediarium backup"},
		{"extra entry", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", data: goodDB}, zipEntry{name: "secret.key", data: key}, zipEntry{name: "notes.txt", data: []byte("hi")})
		}, "unexpected entry"},
		{"parent traversal", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "../app.db", data: goodDB}, zipEntry{name: "secret.key", data: key})
		}, "unexpected entry"},
		{"nested path", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "config/app.db", data: goodDB}, zipEntry{name: "secret.key", data: key})
		}, "unexpected entry"},
		{"absolute path", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "/app.db", data: goodDB}, zipEntry{name: "secret.key", data: key})
		}, "unexpected entry"},
		{"backslash path", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: `..\app.db`, data: goodDB}, zipEntry{name: "secret.key", data: key})
		}, "unexpected entry"},
		{"directory entry", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", dir: true}, zipEntry{name: "secret.key", data: key})
		}, "unexpected entry"},
		{"duplicate entry", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", data: goodDB}, zipEntry{name: "app.db", data: goodDB}, zipEntry{name: "secret.key", data: key})
		}, "more than once"},
		{"missing key", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", data: goodDB})
		}, "secret.key is missing"},
		{"missing database", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "secret.key", data: key})
		}, "app.db is missing"},
		{"database is not sqlite", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", data: bytes.Repeat([]byte("not sqlite "), 500)}, zipEntry{name: "secret.key", data: key})
		}, "not a healthy SQLite database"},
		{"sqlite without Mediarium tables", func(t *testing.T) string {
			other := dbBytes(t, func(db *sql.DB) { db.Exec(`DROP TABLE users`) })
			return buildZip(t, zipEntry{name: "app.db", data: other}, zipEntry{name: "secret.key", data: key})
		}, "does not look like a Mediarium database"},
		{"database from a newer version", func(t *testing.T) string {
			newer := dbBytes(t, func(db *sql.DB) {
				db.Exec(`INSERT INTO schema_migrations (version) VALUES ('9999_from_the_future')`)
			})
			return buildZip(t, zipEntry{name: "app.db", data: newer}, zipEntry{name: "secret.key", data: key})
		}, "newer version of Mediarium"},
		{"manifest from a newer version", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", data: goodDB}, zipEntry{name: "secret.key", data: key},
				zipEntry{name: "manifest.json", data: []byte(`{"app":"mediarium","migrations":"9999_x"}`)})
		}, "newer version of Mediarium"},
		{"manifest from another app", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", data: goodDB}, zipEntry{name: "secret.key", data: key},
				zipEntry{name: "manifest.json", data: []byte(`{"app":"sonarr"}`)})
		}, "different application"},
		{"manifest not json", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", data: goodDB}, zipEntry{name: "secret.key", data: key},
				zipEntry{name: "manifest.json", data: []byte(`{nope`)})
		}, "manifest.json"},
		{"key not base64", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", data: goodDB}, zipEntry{name: "secret.key", data: []byte("%%% not a key %%%")})
		}, "not a valid encryption key"},
		{"key wrong length", func(t *testing.T) string {
			return buildZip(t, zipEntry{name: "app.db", data: goodDB}, zipEntry{name: "secret.key", data: []byte("YWJjZA==")})
		}, "not a valid encryption key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, dir := newInstall(t, "live")
			before := dirNames(t, dir)

			zipPath := tt.zipPath(t)
			_, err := backup.Stage(dir, zipPath)
			var inv *backup.InvalidError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *InvalidError", err)
			}
			if !strings.Contains(inv.Reason, tt.wantMsg) {
				t.Errorf("reason %q should contain %q", inv.Reason, tt.wantMsg)
			}
			if _, statErr := os.Stat(filepath.Join(dir, backup.PendingDir)); !os.IsNotExist(statErr) {
				t.Error("a rejected backup must not leave restore-pending behind")
			}
			after := dirNames(t, dir)
			if strings.Join(before, ",") != strings.Join(after, ",") {
				t.Errorf("config dir changed by a rejected backup: %v -> %v", before, after)
			}
			if _, err := backup.Validate(dir, zipPath); err == nil {
				t.Error("Validate should reject it too")
			}
		})
	}
}

func TestStageReplacesAnEarlierPendingRestore(t *testing.T) {
	srcA, dirA := newInstall(t, "first")
	srcB, dirB := newInstall(t, "second")
	zipA, zipB := writeBackupZip(t, srcA, dirA), writeBackupZip(t, srcB, dirB)

	targetDB, target := newInstall(t, "live")
	targetDB.Close() // on Windows an open database file cannot be moved
	if _, err := backup.Stage(target, zipA); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Stage(target, zipB); err != nil {
		t.Fatal(err)
	}
	if res, err := backup.ApplyPending(target, fixedNow); err != nil || !res.Applied {
		t.Fatalf("apply: %+v %v", res, err)
	}
	if got := readMarker(t, target); got != "second" {
		t.Errorf("marker = %q, want the most recently staged backup", got)
	}
}

func TestApplyPendingWithNothingPendingDoesNothing(t *testing.T) {
	_, dir := newInstall(t, "live")
	res, err := backup.ApplyPending(dir, fixedNow)
	if err != nil || res != (backup.Result{}) {
		t.Fatalf("result %+v, err %v", res, err)
	}
	if got := readMarker(t, dir); got != "live" {
		t.Errorf("live data changed: %q", got)
	}
}

func TestApplyPendingLeavesLiveDataAloneWhenStagingIsBad(t *testing.T) {
	key := validKey(t)
	tests := []struct {
		name    string
		staged  map[string][]byte
		wantMsg string
	}{
		{"unfinished: only the database", map[string][]byte{"app.db": dbBytes(t, nil)}, "secret.key is missing"},
		{"unfinished: only the key", map[string][]byte{"secret.key": key}, "app.db is missing"},
		{"empty folder", map[string][]byte{}, "missing"},
		{"corrupt database", map[string][]byte{"app.db": []byte("garbage"), "secret.key": key}, "not a healthy SQLite database"},
		{"bad key", map[string][]byte{"app.db": dbBytes(t, nil), "secret.key": []byte("short")}, "encryption key"},
		{"newer schema", map[string][]byte{
			"app.db": dbBytes(t, func(db *sql.DB) {
				db.Exec(`INSERT INTO schema_migrations (version) VALUES ('9999_future')`)
			}),
			"secret.key": key,
		}, "newer version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			liveDB, dir := newInstall(t, "live")
			liveDB.Close()
			liveKey := mustRead(t, filepath.Join(dir, backup.KeyFile))

			pending := filepath.Join(dir, backup.PendingDir)
			if err := os.MkdirAll(pending, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, data := range tt.staged {
				if err := os.WriteFile(filepath.Join(pending, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			res, err := backup.ApplyPending(dir, fixedNow)
			if err != nil {
				t.Fatalf("a bad staging is not fatal, got %v", err)
			}
			if res.Applied || !res.Failed {
				t.Fatalf("result %+v: bad staging must be reported as failed", res)
			}
			if !strings.Contains(res.Message, tt.wantMsg) || !strings.Contains(res.Message, "untouched") {
				t.Errorf("message %q should explain why (%q) and that data is untouched", res.Message, tt.wantMsg)
			}
			if got := readMarker(t, dir); got != "live" {
				t.Errorf("live database was modified: marker %q", got)
			}
			if !bytes.Equal(mustRead(t, filepath.Join(dir, backup.KeyFile)), liveKey) {
				t.Error("live key was modified")
			}
			if _, err := os.Stat(pending); !os.IsNotExist(err) {
				t.Error("bad staging should have been moved out of restore-pending")
			}
			if filepath.Base(res.FailedDir) != "restore-failed-20260928-143005" {
				t.Errorf("unexpected failed dir %s", res.FailedDir)
			}
			for name := range tt.staged {
				if _, err := os.Stat(filepath.Join(res.FailedDir, name)); err != nil {
					t.Errorf("staged %s should be kept in the failed folder: %v", name, err)
				}
			}
			for _, n := range dirNames(t, dir) {
				if strings.HasPrefix(n, backup.BeforePrefix) {
					t.Errorf("no before-restore folder should exist when nothing was applied: %s", n)
				}
			}
		})
	}
}

func TestApplyPendingNeverOverwritesEarlierFailedFolders(t *testing.T) {
	_, dir := newInstall(t, "live")
	for i := 0; i < 2; i++ {
		if err := os.MkdirAll(filepath.Join(dir, backup.PendingDir), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, backup.PendingDir, "marker.txt"), []byte{byte('a' + i)}, 0o600)
		res, err := backup.ApplyPending(dir, fixedNow)
		if err != nil || !res.Failed {
			t.Fatalf("round %d: %+v, %v", i, res, err)
		}
	}
	first := mustRead(t, filepath.Join(dir, "restore-failed-20260928-143005", "marker.txt"))
	second := mustRead(t, filepath.Join(dir, "restore-failed-20260928-143005-2", "marker.txt"))
	if string(first) != "a" || string(second) != "b" {
		t.Errorf("earlier failed folder was overwritten: %q %q", first, second)
	}
}

func TestApplyPendingCleansUpCrashedUploads(t *testing.T) {
	_, dir := newInstall(t, "live")
	stale := []string{backup.PendingDir + ".tmp-123", ".restore-validate-456"}
	for _, d := range stale {
		os.MkdirAll(filepath.Join(dir, d), 0o755)
		os.WriteFile(filepath.Join(dir, d, "app.db"), []byte("partial"), 0o600)
	}
	os.WriteFile(filepath.Join(dir, ".restore-upload-789.zip"), []byte("partial"), 0o600)
	os.WriteFile(filepath.Join(dir, ".backup-snapshot-1.db"), []byte("partial"), 0o600)

	if res, err := backup.ApplyPending(dir, fixedNow); err != nil || res != (backup.Result{}) {
		t.Fatalf("result %+v, err %v", res, err)
	}
	for _, n := range dirNames(t, dir) {
		if strings.HasPrefix(n, ".") || strings.Contains(n, ".tmp-") {
			t.Errorf("crashed-upload leftover not cleaned: %s", n)
		}
	}
	if got := readMarker(t, dir); got != "live" {
		t.Errorf("live data changed: %q", got)
	}
}
