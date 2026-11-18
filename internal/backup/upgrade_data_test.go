package backup_test

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	migrations "github.com/rdborg/mediarium/db"
	"github.com/rdborg/mediarium/internal/backup"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

// A backup an older version made (a library with movies, a blocklist, lists,
// a person with an API key and an encrypted setting) is restored on a new
// install and brought up to date at the next start, and the same backup
// restored again does it just the same.
func TestBackupOfAnOlderLibraryRestoresAndMigratesWithItsData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a restore moves the live database aside, which Windows refuses while the test holds it open; this runs on Linux")
	}
	entries, err := fs.ReadDir(migrations.MigrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	upTo := -1
	for i, n := range names {
		if strings.HasPrefix(n, "0275_") {
			upTo = i
		}
	}
	if upTo < 0 {
		t.Fatal("no 0275 migration")
	}

	// The old install: its files, then the backup it made.
	oldDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, backup.DBFile), oldSchemaDB(t, upTo), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.LoadOrCreateKey(filepath.Join(oldDir, backup.KeyFile)); err != nil {
		t.Fatal(err)
	}
	old, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(oldDir, backup.DBFile))+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO api_keys (user_id, name, key_hash) VALUES (1, 'Overseerr', 'hash-a')`,
		`INSERT INTO settings (key, value, encrypted) VALUES ('tmdb.api_key', 'enc:AAAA+/bbbb==', 1)`,
		`INSERT INTO movies (id, tmdb_id, title, year) VALUES (1, 10, 'Alien', 1979), (2, 11, 'Aliens', 1986)`,
		`INSERT INTO artists (id, mbid, name) VALUES (1, 'mb-1', 'AC/DC')`,
		`INSERT INTO albums (id, artist_id, mbid, title) VALUES (1, 1, 'rg-1', 'Back in Black')`,
		`INSERT INTO download_queue (album_id, release_title, status) VALUES (1, 'AC-DC - Back in Black [FLAC]', 'failed')`,
		`INSERT INTO download_queue (movie_id, release_title, status, nzb_url) VALUES (1, 'Alien.1979', 'queued', 'https://x/get?apikey=K')`,
		`INSERT INTO blocklist (release_title, title_key, movie_id) VALUES ('Alien.bad', 'alien.bad', 1)`,
		`INSERT INTO blocklist (release_title, title_key) VALUES ('AC-DC - Back in Black [FLAC]', 'ac-dc - back in black [flac]')`,
		`INSERT INTO import_lists (id, name, source) VALUES (1, 'Watchlist', 'trakt')`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	zipPath := writeBackupZip(t, old, oldDir)
	old.Close()

	for round := 1; round <= 2; round++ {
		_, dir := newInstall(t, "live")
		man, err := backup.Stage(dir, zipPath)
		if err != nil {
			t.Fatalf("round %d: Stage: %v", round, err)
		}
		if !strings.HasPrefix(man.Migrations, "0274") {
			t.Errorf("round %d: manifest migrations = %q", round, man.Migrations)
		}
		if res, err := backup.ApplyPending(dir, fixedNow); err != nil || !res.Applied {
			t.Fatalf("round %d: ApplyPending = %+v, %v", round, res, err)
		}
		db, err := store.Open(filepath.Join(dir, backup.DBFile))
		if err != nil {
			t.Fatalf("round %d: the restored database does not open: %v", round, err)
		}
		latest, _ := backup.LatestMigration()
		for q, want := range map[string]int{
			`SELECT COUNT(*) FROM schema_migrations WHERE version = '` + latest + `'`: 1,
			`SELECT COUNT(*) FROM movies`:                                                      2,
			`SELECT COUNT(*) FROM api_keys WHERE key_hash = 'hash-a'`:                          1,
			`SELECT COUNT(*) FROM settings WHERE encrypted = 1 AND value = 'enc:AAAA+/bbbb=='`: 1,
			`SELECT COUNT(*) FROM blocklist`:                                                   2,
			`SELECT COUNT(*) FROM blocklist WHERE album_id = 1`:                                1,
			`SELECT COUNT(*) FROM blocklist WHERE movie_id = 1 AND fallback_id = ''`:           1,
			`SELECT COUNT(*) FROM download_queue WHERE release_guid = ''`:                      2,
			`SELECT COUNT(*) FROM import_lists`:                                                1,
		} {
			var got int
			if err := db.QueryRow(q).Scan(&got); err != nil || got != want {
				t.Errorf("round %d: %s = %d (%v), want %d", round, q, got, err, want)
			}
		}
		var integrity string
		if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
			t.Errorf("round %d: integrity_check = %q, %v", round, integrity, err)
		}
		db.Close()
	}
	_ = context.Background
}
