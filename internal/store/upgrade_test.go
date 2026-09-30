package store_test

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	migrations "github.com/rdborg/mediarium/db"
	"github.com/rdborg/mediarium/internal/store"
)

// migrationNames lists the embedded migration files in order.
func migrationNames(t *testing.T) []string {
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
	return names
}

// oldInstall builds the database file of an install that stopped at the
// first upTo migrations, exactly as store.Open would have left it, and lets
// seed put rows in.
func oldInstall(t *testing.T, upTo int, seed func(*sql.DB)) string {
	t.Helper()
	names := migrationNames(t)
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	mustExec(t, db, `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`)
	for _, n := range names[:upTo] {
		body, err := migrations.MigrationsFS.ReadFile("migrations/" + n)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, string(body))
		mustExec(t, db, `INSERT INTO schema_migrations (version) VALUES (?)`, strings.TrimSuffix(n, ".sql"))
	}
	if seed != nil {
		seed(db)
	}
	return path
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", firstLine(q), err)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	return s
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// An install from the time of the blocklist (migration 0009) keeps every row
// through the later migrations, ends with a sound database, and a second
// start changes nothing.
func TestUpgradeFromAnOldInstallKeepsItsData(t *testing.T) {
	names := migrationNames(t)
	path := oldInstall(t, 9, func(db *sql.DB) {
		mustExec(t, db, `INSERT INTO users (username, password_hash, is_admin) VALUES ('ryan', 'hash', 0)`)
		mustExec(t, db, `INSERT INTO settings (key, value, encrypted) VALUES ('library.movies_path', '/media/movies', 0)`)
		mustExec(t, db, `INSERT INTO indexers (name, definition_id, base_url, protocol) VALUES ('nzb', 'newznab', 'https://a', 'usenet'), ('tor', 'torznab', 'https://b', 'torrent')`)
		mustExec(t, db, `INSERT INTO movies (tmdb_id, title) VALUES (10, 'Alien')`)
		mustExec(t, db, `INSERT INTO series (tmdb_id, title) VALUES (20, 'Dark')`)
		mustExec(t, db, `INSERT INTO episodes (series_id, season, episode) VALUES (1, 1, 1)`)
		mustExec(t, db, `INSERT INTO download_queue (movie_id, release_title, status) VALUES (1, 'Alien.1979', 'queued')`)
		mustExec(t, db, `INSERT INTO download_queue (series_id, season, release_title, status) VALUES (1, 1, 'Dark.S01', 'queued')`)
		mustExec(t, db, `INSERT INTO activity (movie_id, event_type, message) VALUES (1, 'failed', 'oops'), (1, 'blocklisted', 'bad'), (NULL, 'grabbed', 'ok')`)
		mustExec(t, db, `INSERT INTO blocklist (release_title, title_key, movie_id) VALUES ('Alien.bad', 'alien.bad', 1)`)
		mustExec(t, db, `INSERT INTO blocklist (release_title, title_key, series_id) VALUES ('Dark.bad', 'dark.bad', 1)`)
	})

	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer db.Close()

	if got := count(t, db, `SELECT COUNT(*) FROM schema_migrations`); got != len(names) {
		t.Errorf("%d migrations recorded, want %d", got, len(names))
	}
	if got := count(t, db, `SELECT COUNT(*) FROM movies`); got != 1 {
		t.Errorf("movies = %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM download_queue`); got != 2 {
		t.Errorf("queue rows = %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM activity WHERE level = 'error' AND event_type = 'failed'`); got != 1 {
		t.Errorf("a failed event should be level error after migration 0022, got %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM activity WHERE level = 'warn' AND event_type = 'blocklisted'`); got != 1 {
		t.Errorf("a blocklisted event should be level warn, got %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM users WHERE is_admin = 1`); got != 1 {
		t.Errorf("an account from before roles existed must become an administrator")
	}
	if got := count(t, db, `SELECT COUNT(*) FROM indexers WHERE kind = 'torznab' AND protocol = 'torrent'`); got != 1 {
		t.Errorf("the torrent indexer should be kind torznab, got %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM indexers WHERE kind = 'newznab' AND protocol = 'usenet'`); got != 1 {
		t.Errorf("the usenet indexer should be kind newznab, got %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM download_queue WHERE line_seq = id`); got != 2 {
		t.Errorf("waiting downloads should get their line place from their id, got %d", got)
	}
	assertSound(t, db)

	// Deleting a movie takes its downloads and blocklist entries with it and
	// keeps its history, detached.
	mustExec(t, db, `DELETE FROM movies WHERE id = 1`)
	if got := count(t, db, `SELECT COUNT(*) FROM download_queue WHERE movie_id = 1`); got != 0 {
		t.Errorf("queue rows of the deleted movie remain: %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM blocklist WHERE title_key = 'alien.bad'`); got != 0 {
		t.Errorf("blocklist rows of the deleted movie remain: %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM activity`); got != 3 {
		t.Errorf("activity should be kept, got %d rows", got)
	}
	// Deleting a show takes its episodes and blocklist entries.
	mustExec(t, db, `DELETE FROM series WHERE id = 1`)
	if got := count(t, db, `SELECT COUNT(*) FROM episodes`); got != 0 {
		t.Errorf("episodes of the deleted show remain: %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM blocklist`); got != 0 {
		t.Errorf("blocklist rows of the deleted show remain: %d", got)
	}
	assertSound(t, db)
	db.Close()

	// A second start runs nothing.
	again, err := store.Open(path)
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	defer again.Close()
	if got := count(t, again, `SELECT COUNT(*) FROM schema_migrations`); got != len(names) {
		t.Errorf("after a second start %d migrations recorded, want %d", got, len(names))
	}
	if got := count(t, again, `SELECT COUNT(*) FROM users`); got != 1 {
		t.Errorf("users = %d after second start", got)
	}
}

// assertSound checks the whole file: SQLite's own integrity check and that no
// row points at something that is gone.
func assertSound(t *testing.T, db *sql.DB) {
	t.Helper()
	var res string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		t.Errorf("integrity_check = %q, %v", res, err)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, parent string
		var rowid, fk sql.NullInt64
		_ = rows.Scan(&table, &rowid, &parent, &fk)
		t.Errorf("foreign key problem: %s row %d points at missing %s", table, rowid.Int64, parent)
	}
}

// A migration that fails halfway leaves nothing of itself behind, and the
// versions before it stay recorded.
func TestAFailingMigrationRollsBackCleanly(t *testing.T) {
	names := migrationNames(t)
	path := oldInstall(t, len(names)-1, nil)

	last, err := migrations.MigrationsFS.ReadFile("migrations/" + names[len(names)-1])
	if err != nil {
		t.Fatal(err)
	}
	type column struct{ table, name string }
	var added []column
	for _, line := range strings.Split(string(last), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) >= 6 && strings.EqualFold(f[0], "ALTER") && strings.EqualFold(f[1], "TABLE") && strings.EqualFold(f[3], "ADD") {
			added = append(added, column{f[2], f[5]})
		}
	}
	if len(added) < 2 {
		t.Skip("the newest migration adds fewer than two columns; nothing to fail halfway")
	}
	first, second := added[0], added[len(added)-1]

	// Add the last column by hand so the migration fails after its first
	// statements have already run.
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, raw, `ALTER TABLE `+second.table+` ADD COLUMN `+second.name+` INTEGER`)
	raw.Close()

	if _, err := store.Open(path); err == nil {
		t.Fatal("Open should fail when a migration cannot be applied")
	}

	raw, err = sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if got := count(t, raw, `SELECT COUNT(*) FROM schema_migrations`); got != len(names)-1 {
		t.Errorf("%d migrations recorded, want %d", got, len(names)-1)
	}
	if got := count(t, raw, `SELECT COUNT(*) FROM pragma_table_info('`+first.table+`') WHERE name = ?`, first.name); got != 0 {
		t.Errorf("column %s.%s from the failed migration was left behind", first.table, first.name)
	}
}
