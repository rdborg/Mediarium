package store_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	migrations "github.com/rdborg/mediarium/db"
	"github.com/rdborg/mediarium/internal/store"
)

// stage is a set of rows put into a database once the first after migrations
// have been applied, as an install of that age would have had.
type stage struct {
	after int // how many migrations are applied when the rows go in
	seed  func(t *testing.T, db *sql.DB)
}

// installThrough builds the file of an install that is at the migration
// before stopBefore (a file name prefix such as "0275_") and fills it in
// stages as it grows, from the very first schema on.
func installThrough(t *testing.T, stopBefore string, stages ...stage) string {
	t.Helper()
	names := migrationNames(t)
	end := len(names)
	for i, n := range names {
		if strings.HasPrefix(n, stopBefore) {
			end = i
			break
		}
	}
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	mustExec(t, db, `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`)
	for i, n := range names[:end] {
		body, err := migrations.MigrationsFS.ReadFile("migrations/" + n)
		if err != nil {
			t.Fatal(err)
		}
		// A table rebuild runs with foreign keys off, as store.Open does it.
		rebuild := strings.HasPrefix(string(body), "-- mediarium:rebuild")
		if rebuild {
			mustExec(t, db, `PRAGMA foreign_keys = OFF`)
		}
		mustExec(t, db, string(body))
		if rebuild {
			mustExec(t, db, `PRAGMA foreign_keys = ON`)
		}
		mustExec(t, db, `INSERT INTO schema_migrations (version) VALUES (?)`, strings.TrimSuffix(n, ".sql"))
		for _, s := range stages {
			if s.after == i+1 {
				s.seed(t, db)
			}
		}
	}
	for _, s := range stages {
		if s.after > end {
			t.Fatalf("a stage after %d migrations, but the install stops at %d", s.after, end)
		}
	}
	return path
}

func indexOfMigration(t *testing.T, prefix string) int {
	t.Helper()
	for i, n := range migrationNames(t) {
		if strings.HasPrefix(n, prefix) {
			return i
		}
	}
	t.Fatalf("no migration %s", prefix)
	return -1
}

// A realistic first install (users, keys, settings with encrypted values,
// indexers, a news server, movies, downloads, history) as 0001 left it.
func seedFirstInstall(t *testing.T, db *sql.DB) {
	mustExec(t, db, `INSERT INTO users (id, username, password_hash, is_admin) VALUES (1, 'ryan', '$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ0123', 1), (2, 'maría 🎬', '$2a$10$zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz', 0)`)
	mustExec(t, db, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ('tok1', 1, '2099-01-01T00:00:00Z'), ('tok2', 2, '2099-01-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO api_keys (user_id, name, key_hash) VALUES (1, 'Overseerr', 'hash-a'), (1, 'Homepage', 'hash-b'), (2, 'phone', 'hash-c')`)
	mustExec(t, db, `INSERT INTO api_keys (user_id, name, key_hash, revoked_at) VALUES (1, 'old', 'hash-d', '2024-01-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO settings (key, value, encrypted) VALUES
		('library.movies_path', '/media/Movies', 0),
		('tmdb.api_key', 'enc:AAAA+/bbbb==', 1),
		('notify.webhook', 'enc:CCCC+/dddd==', 1),
		('naming.preset', 'plex', 0)`)
	mustExec(t, db, `INSERT INTO indexers (name, definition_id, base_url, api_key_encrypted) VALUES ('NZBgeek', 'newznab', 'https://api.nzbgeek.info', 'enc:key1'), ('Disabled', 'newznab', 'https://x.example', NULL)`)
	mustExec(t, db, `INSERT INTO download_clients (type, name, host, port, username, password_encrypted) VALUES ('nntp', 'Newshosting', 'news.example.com', 563, 'u', 'enc:pw1')`)
	for i := 1; i <= 200; i++ {
		mustExec(t, db, `INSERT INTO movies (tmdb_id, title, year, status, file_path) VALUES (?, ?, ?, ?, ?)`,
			i, fmt.Sprintf("Película número %d: ¿Qué? 日本語", i), 1950+i%70, []string{"missing", "downloaded", "downloading"}[i%3], fmt.Sprintf("/media/Movies/%d.mkv", i))
	}
	mustExec(t, db, `INSERT INTO download_queue (movie_id, indexer_id, download_client_id, release_title, nzb_url, size_bytes, status) VALUES
		(1, 1, 1, 'Movie.1.2020.1080p.WEB-DL', 'https://api.nzbgeek.info/get?apikey=SECRET', 4000000000, 'downloading'),
		(2, 1, 1, 'Movie.2.2020.1080p.WEB-DL', NULL, NULL, 'queued'),
		(3, 2, NULL, 'Movie.3.2020', NULL, 1, 'failed')`)
	mustExec(t, db, `INSERT INTO activity (movie_id, event_type, message) VALUES (1, 'grabbed', 'Grabbed Movie.1'), (1, 'failed', 'oops'), (2, 'blocklisted', 'Blocklisted "x": bad')`)
}

// What a library had gathered by the time of 4.0: shows with episodes, a
// blocklist, import lists, music, books, file details. Added just before the
// "origin" migrations (0270) run.
func seedLibraryOf40(t *testing.T, db *sql.DB) {
	mustExec(t, db, `INSERT INTO series (id, tmdb_id, title, year, added_by) VALUES (1, 100, 'Dark', 2017, 1), (2, 101, 'Dark', 2017, 2), (3, 102, 'Série «Étrange»', 2020, NULL)`)
	for s := 1; s <= 3; s++ {
		for e := 1; e <= 10; e++ {
			mustExec(t, db, `INSERT INTO episodes (series_id, season, episode, title, status) VALUES (1, ?, ?, ?, ?)`, s, e, fmt.Sprintf("Episode %d", e), []string{"missing", "downloaded"}[e%2])
		}
	}
	mustExec(t, db, `INSERT INTO episodes (series_id, season, episode) VALUES (2, 1, 1), (3, 1, 1)`)
	mustExec(t, db, `INSERT INTO download_queue (series_id, season, episode, release_title, status, protocol) VALUES (1, 1, 1, 'Dark.S01E01.1080p', 'completed', 'torrent')`)

	// Lists. Two lists share a name, one is unique; movies came from each.
	mustExec(t, db, `INSERT INTO import_lists (id, name, source, media) VALUES (1, 'Trakt watchlist', 'trakt', 'both'), (2, 'Same', 'tmdb', 'movie'), (3, 'Same', 'tmdb', 'movie')`)
	mustExec(t, db, `INSERT INTO import_list_items (list_id, ref, kind, tmdb_id, title, year, outcome) VALUES (1, 'm1', 'movie', 1, 'Película número 1', 1951, 'added'), (1, 'm2', 'movie', 2, 'x', 1952, 'skipped'), (2, 'm3', 'movie', 3, 'y', 1953, 'added')`)
	mustExec(t, db, `UPDATE movies SET added_by = 1 WHERE id = 1`)

	// A blocklist with entries for movies, shows, loose ones and ones for
	// albums and books that have the download or event on record.
	mustExec(t, db, `INSERT INTO artists (id, mbid, name) VALUES (1, 'mb-1', 'AC/DC'), (2, 'mb-2', 'Sigur Rós')`)
	mustExec(t, db, `INSERT INTO albums (id, artist_id, mbid, title) VALUES (1, 1, 'rg-1', 'Back in Black'), (2, 2, 'rg-2', 'Ágætis byrjun')`)
	mustExec(t, db, `INSERT INTO tracks (album_id, disc, position, title) VALUES (1, 1, 1, 'Hells Bells'), (1, 1, 2, 'Shoot to Thrill'), (2, 1, 1, 'Intro')`)
	mustExec(t, db, `INSERT INTO books (id, ol_key, title, author) VALUES (1, 'OL1W', 'Good Omens', 'Terry Pratchett, Neil Gaiman')`)
	mustExec(t, db, `INSERT INTO download_queue (album_id, release_title, status) VALUES (1, 'AC-DC - Back in Black [FLAC]', 'failed')`)
	mustExec(t, db, `INSERT INTO download_queue (book_id, release_title, status) VALUES (1, 'Good.Omens.EPUB', 'failed')`)
	for _, r := range [][]any{
		{"Movie.1.bad", "movie.1.bad", 1, nil},
		{"Dark.S01.bad", "dark.s01.bad", nil, 1},
		{"AC-DC - Back in Black [FLAC]", "ac-dc - back in black [flac]", nil, nil},
		{"Good.Omens.EPUB", "good.omens.epub", nil, nil},
		{"Loose.Release", "loose.release", nil, nil},
	} {
		mustExec(t, db, `INSERT INTO blocklist (release_title, title_key, movie_id, series_id) VALUES (?, ?, ?, ?)`, r...)
	}

	// File details: one that holds nothing but its container is read again.
	mustExec(t, db, `INSERT INTO media_info (path, size, mtime, scanned_at, failed, info, container) VALUES
		('/media/a.avi', 1, 1, 'x', 0, '{"container":"avi"}', 'avi'),
		('/media/b.mkv', 1, 1, 'x', 0, '{"container":"matroska","duration":60}', 'matroska'),
		('/media/c.mkv', 1, 1, 'x', 1, '', 'mkv')`)

	// Roles and permissions of the day.
	mustExec(t, db, `UPDATE users SET group_role = 'manager', permissions = 'request,approve', request_quota = 5, max_rating = 'PG-13' WHERE id = 2`)
	mustExec(t, db, `INSERT INTO settings (key, value, encrypted) VALUES ('oidc.client_secret', 'enc:EEEE/ffff==', 1)`)
}

var (
	realisticOnce sync.Once
	realisticDB   []byte
)

// realisticInstall is a copy of the file of an install that was made on the
// first schema, kept up with every version and filled with rows on the way,
// stopped just before the update steps of the 4.0 line. Building it applies
// every update step once, so it is built once and copied for each test.
func realisticInstall(t *testing.T) string {
	t.Helper()
	realisticOnce.Do(func() {
		path := installThrough(t, "0275_",
			stage{after: 1, seed: seedFirstInstall},
			stage{after: indexOfMigration(t, "0270_"), seed: seedLibraryOf40},
		)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		realisticDB = b
	})
	if realisticDB == nil {
		t.Fatal("the realistic install could not be built")
	}
	path := filepath.Join(t.TempDir(), "app.db")
	if err := os.WriteFile(path, realisticDB, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// An install that was made on the first schema ever shipped, kept up with
// every version since, upgrades to the current schema without losing a row,
// and a second start (and a third) changes nothing.
func TestFullUpgradeFromTheFirstSchemaKeepsEverything(t *testing.T) {
	names := migrationNames(t)
	path := realisticInstall(t)

	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	info := store.Mode(db)
	if info.SafetyCopy == "" {
		t.Error("an upgrade must make a safety copy first")
	} else if st, err := os.Stat(info.SafetyCopy); err != nil || st.Size() == 0 {
		t.Errorf("safety copy %q: %v", info.SafetyCopy, err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM schema_migrations`); got != len(names) {
		t.Errorf("%d migrations recorded, want %d", got, len(names))
	}

	checks := []struct {
		name, query string
		want        int
	}{
		{"movies", `SELECT COUNT(*) FROM movies`, 200},
		{"movie with a title in other scripts", `SELECT COUNT(*) FROM movies WHERE title = 'Película número 7: ¿Qué? 日本語'`, 1},
		{"shows", `SELECT COUNT(*) FROM series`, 3},
		{"two shows with one name kept apart", `SELECT COUNT(DISTINCT id) FROM series WHERE title = 'Dark'`, 2},
		{"episodes", `SELECT COUNT(*) FROM episodes`, 32},
		{"queue", `SELECT COUNT(*) FROM download_queue`, 3 + 1 + 2},
		{"queue row url kept", `SELECT COUNT(*) FROM download_queue WHERE nzb_url = 'https://api.nzbgeek.info/get?apikey=SECRET'`, 1},
		{"activity", `SELECT COUNT(*) FROM activity`, 3},
		{"users", `SELECT COUNT(*) FROM users`, 2},
		{"admin stays", `SELECT COUNT(*) FROM users WHERE username = 'ryan' AND is_admin = 1`, 1},
		{"role kept", `SELECT COUNT(*) FROM users WHERE id = 2 AND group_role = 'manager' AND request_quota = 5 AND max_rating = 'PG-13'`, 1},
		{"api keys", `SELECT COUNT(*) FROM api_keys`, 4},
		{"revoked key still revoked", `SELECT COUNT(*) FROM api_keys WHERE key_hash = 'hash-d' AND revoked_at IS NOT NULL`, 1},
		{"sessions", `SELECT COUNT(*) FROM sessions`, 2},
		{"settings", `SELECT COUNT(*) FROM settings WHERE key IN ('library.movies_path','tmdb.api_key','notify.webhook','naming.preset','oidc.client_secret')`, 5},
		{"encrypted secrets untouched", `SELECT COUNT(*) FROM settings WHERE encrypted = 1 AND value IN ('enc:AAAA+/bbbb==','enc:CCCC+/dddd==','enc:EEEE/ffff==')`, 3},
		{"indexers", `SELECT COUNT(*) FROM indexers`, 2},
		{"indexer key kept", `SELECT COUNT(*) FROM indexers WHERE api_key_encrypted = 'enc:key1'`, 1},
		{"news server password kept", `SELECT COUNT(*) FROM download_clients WHERE password_encrypted = 'enc:pw1'`, 1},
		{"artists", `SELECT COUNT(*) FROM artists WHERE name IN ('AC/DC', 'Sigur Rós')`, 2},
		{"tracks", `SELECT COUNT(*) FROM tracks`, 3},
		{"books", `SELECT COUNT(*) FROM books WHERE author = 'Terry Pratchett, Neil Gaiman'`, 1},
		{"blocklist", `SELECT COUNT(*) FROM blocklist`, 5},
		{"blocklist entries have no fallback id", `SELECT COUNT(*) FROM blocklist WHERE fallback_id = ''`, 5},
		{"blocklist entry tied to its album", `SELECT COUNT(*) FROM blocklist WHERE title_key = 'ac-dc - back in black [flac]' AND album_id = 1`, 1},
		{"blocklist entry tied to its book", `SELECT COUNT(*) FROM blocklist WHERE title_key = 'good.omens.epub' AND book_id = 1`, 1},
		{"blocklist entry of no title stays loose", `SELECT COUNT(*) FROM blocklist WHERE title_key = 'loose.release' AND album_id IS NULL AND book_id IS NULL AND movie_id IS NULL AND series_id IS NULL`, 1},
		{"blocklist scope", `SELECT COUNT(*) FROM blocklist WHERE scope = 'all'`, 5},
		{"queue rows have no guid", `SELECT COUNT(*) FROM download_queue WHERE release_guid = ''`, 6},
		{"lists", `SELECT COUNT(*) FROM import_lists`, 3},
		{"list log from what was added", `SELECT COUNT(*) FROM import_list_log`, 2},
		{"movie from the one list with that name is credited to it", `SELECT COUNT(*) FROM movies WHERE id = 1 AND origin_list_id IS NULL`, 1},
		{"the file details of a bare container are dropped", `SELECT COUNT(*) FROM media_info`, 2},
		{"the failed read stays", `SELECT COUNT(*) FROM media_info WHERE failed = 1`, 1},
		{"the full read stays", `SELECT COUNT(*) FROM media_info WHERE path = '/media/b.mkv'`, 1},
	}
	for _, c := range checks {
		if got := count(t, db, c.query); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	assertSound(t, db)
	db.Close()

	// Another start, and one more: nothing is applied, no more copies are made
	// and the rows are the same.
	before := dirCount(t, filepath.Dir(path))
	for i := 2; i <= 3; i++ {
		again, err := store.Open(path)
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		if got := count(t, again, `SELECT COUNT(*) FROM schema_migrations`); got != len(names) {
			t.Errorf("start %d: %d migrations recorded, want %d", i, got, len(names))
		}
		if got := store.Mode(again).SafetyCopy; got != "" {
			t.Errorf("start %d made a safety copy though nothing changed: %s", i, got)
		}
		for _, c := range checks {
			if got := count(t, again, c.query); got != c.want {
				t.Errorf("start %d, %s: got %d, want %d", i, c.name, got, c.want)
			}
		}
		again.Close()
	}
	if after := dirCount(t, filepath.Dir(path)); after != before {
		t.Errorf("starting again changed the files next to the database: %d -> %d", before, after)
	}
}

func dirCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// A migration that fails halfway on a database with data in it leaves the file
// exactly as it was (no half-applied change, the earlier ones stay recorded),
// Open names the migration, and the app can go on once the cause is gone.
func TestAFailedMigrationLeavesTheDatabaseAsItWasAndNamesIt(t *testing.T) {
	names := migrationNames(t)
	path := realisticInstall(t)
	// Make the next migration fail partway: the column it adds already exists.
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, raw, `ALTER TABLE download_queue ADD COLUMN release_guid TEXT NOT NULL DEFAULT ''`)
	raw.Close()

	_, err = store.Open(path)
	if err == nil {
		t.Fatal("Open should refuse a migration that cannot be applied")
	}
	if !strings.Contains(err.Error(), "0277_queue_release_guid") {
		t.Errorf("the error does not say which change failed: %v", err)
	}
	if !strings.Contains(err.Error(), "left as it was") {
		t.Errorf("the error does not say the data was left alone: %v", err)
	}

	raw, err = sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var top string
	if err := raw.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&top); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(top, "0276_") {
		t.Errorf("newest recorded change is %s, want 0276_...", top)
	}
	if got := count(t, raw, `SELECT COUNT(*) FROM pragma_table_info('blocklist') WHERE name = 'album_id'`); got != 1 {
		t.Errorf("an earlier, finished change was lost")
	}
	if got := count(t, raw, `SELECT COUNT(*) FROM movies`); got != 200 {
		t.Errorf("movies = %d", got)
	}
	var integrity string
	if err := raw.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Errorf("integrity_check = %q, %v", integrity, err)
	}

	// The cause is removed (as a fixed release would): the rest goes through.
	// The test has no way to drop a column the migration will add itself, so
	// it drops the one it put there.
	mustExec(t, raw, `ALTER TABLE download_queue DROP COLUMN release_guid`)
	raw.Close()
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("second try: %v", err)
	}
	defer db.Close()
	if got := count(t, db, `SELECT COUNT(*) FROM schema_migrations`); got != len(names) {
		t.Errorf("%d migrations recorded, want %d", got, len(names))
	}
	assertSound(t, db)
}

// The migrations that look at other tables for every row (0275 tying blocklist
// entries to albums and books) must stay quick on a library that has been
// running for years: a long blocklist, a long queue history and a long
// activity feed.
func TestUpgradeIsQuickOnBigTables(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a big database")
	}
	blocked, queued, events := 20000/slowdown, 20000/slowdown, 200000/slowdown
	upTo := indexOfMigration(t, "0275_")
	path := installThrough(t, "0275_", stage{after: upTo, seed: func(t *testing.T, db *sql.DB) {
		mustExec(t, db, `INSERT INTO artists (id, mbid, name) VALUES (1, 'mb-1', 'Artist')`)
		mustExec(t, db, `INSERT INTO albums (id, artist_id, mbid, title) VALUES (1, 1, 'rg-1', 'One')`)
		mustExec(t, db, `INSERT INTO books (id, ol_key, title) VALUES (1, 'OL1W', 'Book')`)
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		must := func(q string, a ...any) {
			if _, err := tx.Exec(q, a...); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < blocked; i++ {
			rel := fmt.Sprintf("Some.Release.Name.%d.2020.1080p.WEB-DL.x264-GRP", i)
			must(`INSERT INTO blocklist (release_title, title_key, reason) VALUES (?, ?, 'failed')`, rel, strings.ToLower(rel))
		}
		for i := 0; i < queued; i++ {
			rel := fmt.Sprintf("Some.Release.Name.%d.2020.1080p.WEB-DL.x264-GRP", i+blocked/2)
			if i%2 == 0 {
				must(`INSERT INTO download_queue (album_id, release_title, status) VALUES (1, ?, 'completed')`, rel)
			} else {
				must(`INSERT INTO download_queue (book_id, release_title, status) VALUES (1, ?, 'completed')`, rel)
			}
		}
		for i := 0; i < events; i++ {
			kind := []string{"grabbed", "imported", "blocklisted"}[i%3]
			var album any
			if i%2 == 0 {
				album = 1
			}
			must(`INSERT INTO activity (album_id, event_type, message) VALUES (?, ?, ?)`, album, kind, fmt.Sprintf(`Blocklisted "Another.Release.%d": bad archive`, i))
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}})
	start := time.Now()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	took := time.Since(start)
	t.Logf("upgrade of %d blocklist, %d queue and %d activity rows took %s", blocked, queued, events, took)
	if took > time.Duration(20*slowdown)*time.Second {
		t.Errorf("the upgrade took %s on a big database", took)
	}
}
