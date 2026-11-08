// Package store owns the SQLite connection and schema migrations
// (one file covering config, library, indexer state, queue).
//
// The database runs in WAL mode with a small pool of connections: readers
// (every page the UI loads) never wait for the writer, and SQLite itself
// serialises the writers. Every write transaction starts with BEGIN
// IMMEDIATE, so two of them queue up for the write lock instead of failing
// halfway through. When WAL cannot be switched on (some network file
// systems), the database stays in the old rollback-journal mode and the pool
// shrinks to one connection, as before; Mode reports which one it is.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	migrations "github.com/rdborg/mediarium/db"
	_ "modernc.org/sqlite"
)

// poolSize is how many connections the pool may open in WAL mode. Small on
// purpose: a NAS has few cores, and SQLite allows one writer at a time anyway.
const poolSize = 8

// pageCacheKiB is SQLite's page cache for each connection, in KiB (a negative
// cache_size means KiB). The default is about 2 MB, which a library list with
// thousands of titles outgrows on every call. The cache is per connection and
// fills only as pages are used, so the most the pool of poolSize can ever hold
// is 64 MB, and a quiet app holds a fraction of that.
const pageCacheKiB = 8000

// Journal modes reported by Mode.
const (
	ModeWAL      = "wal"
	ModeFallback = "delete" // rollback journal, one connection
)

// Info describes how a database was opened.
type Info struct {
	// JournalMode is ModeWAL, or ModeFallback when WAL could not be enabled.
	JournalMode string
	// WALProblem says why WAL could not be enabled ("" when it is on).
	WALProblem string
	// NewerMigrations lists the schema changes in the database that this
	// program does not know: a newer version has already been here. Empty for
	// a database this program (or an older one) migrated.
	NewerMigrations []string
	// SafetyCopy is the file made before this start applied schema changes
	// ("" when there was nothing to apply or the copy could not be made).
	SafetyCopy string
}

var (
	infoMu sync.Mutex
	infos  = map[*sql.DB]Info{}
)

// Mode reports how db was opened by Open. A database that Open did not
// create reports ModeWAL with no problem.
func Mode(db *sql.DB) Info {
	infoMu.Lock()
	defer infoMu.Unlock()
	if in, ok := infos[db]; ok {
		return in
	}
	return Info{JournalMode: ModeWAL}
}

func setInfo(db *sql.DB, in Info) {
	infoMu.Lock()
	infos[db] = in
	infoMu.Unlock()
}

// Open opens (creating if needed) the SQLite database at path and applies
// any pending migrations embedded from db/migrations (migrations.MigrationsFS).
func Open(path string) (*sql.DB, error) {
	conn, err := sql.Open(driverName, dsn(path, "NORMAL"))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	info := enableWAL(conn)
	if info.JournalMode == ModeWAL {
		conn.SetMaxOpenConns(poolSize)
		conn.SetMaxIdleConns(poolSize)
	} else {
		// NORMAL is only crash-safe with a write-ahead log. Without one (the
		// file systems where that happens are the less reliable ones), every
		// connection must sync fully, so the file is opened again with that.
		conn.Close()
		if conn, err = sql.Open(driverName, dsn(path, "FULL")); err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
		// The rollback journal lets a reader block the writer, so keep the
		// old rule of one connection at a time.
		conn.SetMaxOpenConns(1)
		log.Printf("database: WAL mode is not available here, using the older journal with one connection: %s", info.WALProblem)
	}

	if err := migrate(conn, path, &info); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	setInfo(conn, info)
	restrictFiles(path)
	go watchPool(conn)
	return conn, nil
}

// restrictFiles makes the database, and its write-ahead log files, readable by
// the app's own user only. The database holds password hashes, the encrypted
// credentials and session records; the file next to it, secret.key, is already
// private, and the database should not be the weak one of the pair. Errors are
// ignored: some file systems (network shares, Windows) have no such modes.
func restrictFiles(path string) {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			_ = os.Chmod(p, 0o600)
		}
	}
}

// dsn builds the connection string. The pragmas run on every new connection.
func dsn(path, synchronous string) string {
	// The path is part of a file: URI, so the characters that would start a
	// query or fragment are escaped.
	escaped := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(path)
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous("+synchronous+")")
	q.Add("_pragma", "journal_size_limit(67108864)")
	q.Add("_pragma", fmt.Sprintf("cache_size(-%d)", pageCacheKiB))
	q.Set("_txlock", "immediate")
	return "file:" + escaped + "?" + q.Encode()
}

// enableWAL switches the database file to write-ahead logging. It never
// fails Open: if the file system cannot do it, the caller falls back.
func enableWAL(conn *sql.DB) Info {
	var mode string
	if err := conn.QueryRow(`PRAGMA journal_mode = WAL`).Scan(&mode); err != nil {
		return Info{JournalMode: ModeFallback, WALProblem: err.Error()}
	}
	if !strings.EqualFold(mode, "wal") {
		return Info{JournalMode: ModeFallback, WALProblem: fmt.Sprintf("SQLite stayed in %q mode", mode)}
	}
	return Info{JournalMode: ModeWAL}
}

// renumbered lists migrations that 3.x shipped under another number than 4.0
// uses (4.0 already had files at those numbers). New name -> name 3.x recorded.
var renumbered = map[string]string{
	"0238_display_faces":         "0234_display_faces",
	"0239_display_face_overview": "0235_display_face_overview",
}

func migrate(conn *sql.DB, path string, info *Info) error {
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrations.MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	applied, err := appliedVersions(conn)
	if err != nil {
		return err
	}
	// A change that 3.x recorded under its old number is the same change as
	// the renumbered file here: record the new name and do not run it again
	// (adding a column twice fails).
	for newName, oldName := range renumbered {
		if applied[oldName] && !applied[newName] {
			if _, err := conn.Exec(`INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)`, newName); err != nil {
				return fmt.Errorf("record %s: %w", newName, err)
			}
			applied[newName] = true
		}
	}
	known := map[string]bool{}
	for _, oldName := range renumbered {
		known[oldName] = true
	}
	var pending []string
	for _, name := range names {
		version := strings.TrimSuffix(name, filepath.Ext(name))
		known[version] = true
		if !applied[version] {
			pending = append(pending, name)
		}
	}

	// A program older than the database: a newer one already changed it (an
	// update that was rolled back, or an older image started by hand). Changes
	// that only add columns are harmless to the older program, and nothing here
	// can tell which kind they were, so this warns and does not refuse; the
	// message says how to get out of it.
	for v := range applied {
		if !known[v] {
			info.NewerMigrations = append(info.NewerMigrations, v)
		}
	}
	sort.Strings(info.NewerMigrations)
	if len(info.NewerMigrations) > 0 {
		log.Printf("warning: %s", NewerMessage(info.NewerMigrations))
	}

	// A copy of the database as it is before this start changes its tables, so
	// a change that goes wrong can be undone from something newer than the
	// nightly backup. Not for a database that is just being created.
	if len(pending) > 0 && len(applied) > 0 && !isMemory(path) {
		last := strings.TrimSuffix(pending[len(pending)-1], filepath.Ext(pending[len(pending)-1]))
		copyPath, err := snapshotBeforeMigrating(conn, path, last)
		if err != nil {
			log.Printf("warning: couldn't make a safety copy of the database before updating it: %v", err)
		} else {
			info.SafetyCopy = copyPath
			log.Printf("database: saved a copy of it as %s before updating its tables", filepath.Base(copyPath))
		}
	}

	for _, name := range pending {
		version := strings.TrimSuffix(name, filepath.Ext(name))
		sqlBytes, err := migrations.MigrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := applyMigration(conn, name, version, string(sqlBytes)); err != nil {
			return err
		}
	}
	return nil
}

// brokenLinks counts the rows whose foreign key points at nothing.
func brokenLinks(tx *sql.Tx) (int, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&n)
	return n, err
}

// rebuildMarker starts a migration that rebuilds a table (create the new
// one, copy, drop the old, rename). Those run with foreign keys off, as
// SQLite asks, so dropping the old table doesn't delete or empty the rows
// that point at it; the keys are checked before the change is kept.
const rebuildMarker = "-- mediarium:rebuild"

func applyMigration(db *sql.DB, name, version, body string) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx) // the pragma is per connection: keep this one
	if err != nil {
		return fmt.Errorf("connect for %s: %w", name, err)
	}
	defer conn.Close()
	rebuild := strings.HasPrefix(body, rebuildMarker)
	if rebuild {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return fmt.Errorf("prepare %s: %w", name, err)
		}
		defer func() { _, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", name, err)
	}
	// A rebuild must not leave a row pointing at nothing. An old database can
	// already have such rows somewhere else; those don't stop the upgrade,
	// only new ones do.
	brokenBefore := 0
	if rebuild {
		if brokenBefore, err = brokenLinks(tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("check before %s: %w", name, err)
		}
	}
	if _, err := tx.Exec(body); err != nil {
		tx.Rollback()
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if rebuild {
		after, err := brokenLinks(tx)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("check %s: %w", name, err)
		}
		if after > brokenBefore {
			tx.Rollback()
			return fmt.Errorf("apply %s: it would leave rows pointing at nothing", name)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
		tx.Rollback()
		return fmt.Errorf("record %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}
