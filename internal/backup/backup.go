// Package backup creates, validates and restores Mediarium backups.
//
// A backup is a zip holding exactly three files:
//
//	manifest.json  {app, version, createdAt, migrations}
//	app.db         a consistent SQLite snapshot (VACUUM INTO)
//	secret.key     the encryption key that decrypts the credentials in app.db
//
// Restoring is two-phase because the database is open while the app runs:
// Stage validates an uploaded zip and parks its files in
// <configDir>/restore-pending/; ApplyPending, called at the very start of
// the next boot before the database is opened, swaps them into place. The
// files being replaced are moved aside into before-restore-<timestamp>/ and
// never deleted.
package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	migrations "github.com/rdborg/mediarium/db"
	"github.com/rdborg/mediarium/internal/crypto"
	_ "modernc.org/sqlite" // registers the "sqlite" driver used to inspect staged databases
)

// File names inside a backup zip and inside the config folder.
const (
	DBFile       = "app.db"
	KeyFile      = "secret.key"
	ManifestFile = "manifest.json"

	// PendingDir is where a validated restore waits for the next boot.
	PendingDir = "restore-pending"
	// BeforePrefix names the folder the replaced files are moved into.
	BeforePrefix = "before-restore-"
	// FailedPrefix names the folder a bad staging is quarantined in.
	FailedPrefix = "restore-failed-"

	// MaxRestoreBytes is the largest backup zip a restore will accept.
	MaxRestoreBytes int64 = 1 << 30

	appName = "mediarium"

	// Sanity caps on the *uncompressed* size of each entry, so a small zip
	// cannot expand to fill the disk.
	maxDBBytes       int64 = 8 << 30
	maxKeyBytes      int64 = 4 << 10
	maxManifestBytes int64 = 64 << 10

	timeLayout = "20060102-150405"
)

// Manifest describes a backup. Migrations is the highest applied schema
// migration in the snapshot, e.g. "0015_notification_events".
type Manifest struct {
	App        string `json:"app"`
	Version    string `json:"version"`
	CreatedAt  string `json:"createdAt"`
	Migrations string `json:"migrations"`
}

// InvalidError means the uploaded backup itself is unusable. Its message is
// written for the person who uploaded it and is safe to show in the UI;
// anything else returned by this package is an internal failure.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

// lowerFirst makes the first letter lower case, so a full sentence can sit
// inside a parenthesis.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func invalidf(format string, args ...any) error {
	return &InvalidError{Reason: fmt.Sprintf(format, args...)}
}

// FileName returns the download name for a backup created at t.
func FileName(t time.Time) string {
	return "mediarium-backup-" + t.Format(timeLayout) + ".zip"
}

// LatestMigration is the highest migration this binary knows about.
func LatestMigration() (string, error) {
	entries, err := fs.ReadDir(migrations.MigrationsFS, "migrations")
	if err != nil {
		return "", fmt.Errorf("read embedded migrations: %w", err)
	}
	latest := ""
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if v := strings.TrimSuffix(e.Name(), ".sql"); v > latest {
			latest = v
		}
	}
	return latest, nil
}

func highestMigration(ctx context.Context, db *sql.DB) (string, error) {
	var v sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return "", fmt.Errorf("read applied migrations: %w", err)
	}
	return v.String, nil
}

// Create writes a backup zip of the live database db plus the key in
// configDir to w. The database is snapshotted first with VACUUM INTO (a
// transactionally consistent copy, unlike copying a file that is being
// written), into a temporary file in configDir that is always deleted. If it
// fails before anything has been written to w, w is untouched. Contents are
// never logged.
func Create(ctx context.Context, db *sql.DB, configDir, version string, now time.Time, w io.Writer) error {
	keyData, err := os.ReadFile(filepath.Join(configDir, KeyFile))
	if err != nil {
		return fmt.Errorf("read secret key: %w", err)
	}
	migration, err := highestMigration(ctx, db)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(configDir, ".backup-snapshot-*.db")
	if err != nil {
		return fmt.Errorf("create snapshot file: %w", err)
	}
	snapshot := tmp.Name()
	tmp.Close()
	defer os.Remove(snapshot)
	// VACUUM INTO refuses to overwrite an existing file.
	if err := os.Remove(snapshot); err != nil {
		return fmt.Errorf("prepare snapshot file: %w", err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(snapshot, "'", "''")+"'"); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	_ = os.Chmod(snapshot, 0o600) // the copy holds the whole database while it exists

	manifest, err := json.MarshalIndent(Manifest{
		App:        appName,
		Version:    version,
		CreatedAt:  now.UTC().Format(time.RFC3339),
		Migrations: migration,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	zw := zip.NewWriter(w)
	if err := addBytes(zw, ManifestFile, manifest, now); err != nil {
		return err
	}
	f, err := os.Open(snapshot)
	if err != nil {
		return fmt.Errorf("open snapshot: %w", err)
	}
	defer f.Close()
	hdr := &zip.FileHeader{Name: DBFile, Method: zip.Deflate, Modified: now}
	dw, err := zw.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("add %s to backup: %w", DBFile, err)
	}
	if _, err := io.Copy(dw, f); err != nil {
		return fmt.Errorf("add %s to backup: %w", DBFile, err)
	}
	if err := addBytes(zw, KeyFile, keyData, now); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finish backup: %w", err)
	}
	return nil
}

func addBytes(zw *zip.Writer, name string, data []byte, modified time.Time) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
	if err != nil {
		return fmt.Errorf("add %s to backup: %w", name, err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("add %s to backup: %w", name, err)
	}
	return nil
}

// Validate checks a backup zip without staging it. scratchDir must be a
// writable folder (the config dir); anything created there is removed.
func Validate(scratchDir, zipPath string) (Manifest, error) {
	dir, err := os.MkdirTemp(scratchDir, ".restore-validate-*")
	if err != nil {
		return Manifest{}, fmt.Errorf("create scratch folder: %w", err)
	}
	defer os.RemoveAll(dir)
	return extractAndValidate(zipPath, dir)
}

// Stage validates the backup zip at zipPath and, only if it is fully valid,
// places its files in <configDir>/restore-pending/ for ApplyPending to pick
// up on the next start. Files are first written to a temporary folder and
// moved into place with one rename, so restore-pending/ never exists half
// written. A previously staged (never applied) restore is replaced. The
// running database and key are not touched.
func Stage(configDir, zipPath string) (Manifest, error) {
	staging, err := os.MkdirTemp(configDir, PendingDir+".tmp-")
	if err != nil {
		return Manifest{}, fmt.Errorf("create staging folder: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(staging)
		}
	}()

	manifest, err := extractAndValidate(zipPath, staging)
	if err != nil {
		return Manifest{}, err
	}
	pending := filepath.Join(configDir, PendingDir)
	if err := os.RemoveAll(pending); err != nil {
		return Manifest{}, fmt.Errorf("replace earlier staged restore: %w", err)
	}
	if err := os.Rename(staging, pending); err != nil {
		return Manifest{}, fmt.Errorf("stage restore: %w", err)
	}
	cleanup = false
	return manifest, nil
}

// extractAndValidate unpacks the zip into dir (which must exist) and runs
// every check. Only the three known entry names are accepted, so there is no
// path handling to get wrong: an entry whose name is anything else, however
// it is spelled, rejects the whole backup.
func extractAndValidate(zipPath, dir string) (Manifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return Manifest{}, invalidf("This is not a valid Mediarium backup file. It could not be read as a zip.")
	}
	defer zr.Close()

	limits := map[string]int64{DBFile: maxDBBytes, KeyFile: maxKeyBytes, ManifestFile: maxManifestBytes}
	seen := map[string]bool{}
	for _, f := range zr.File {
		limit, known := limits[f.Name]
		switch {
		case !known:
			return Manifest{}, invalidf("This backup contains an unexpected entry (%q). Only %s, %s and %s are allowed.", displayName(f.Name), DBFile, KeyFile, ManifestFile)
		case seen[f.Name]:
			return Manifest{}, invalidf("This backup lists %s more than once.", f.Name)
		case f.Mode()&os.ModeType != 0 || f.FileInfo().IsDir():
			return Manifest{}, invalidf("The %s in this backup is not a regular file.", f.Name)
		case f.Flags&0x1 != 0:
			return Manifest{}, invalidf("The %s in this backup is password protected, but Mediarium backups never are.", f.Name)
		case f.UncompressedSize64 > uint64(limit):
			return Manifest{}, invalidf("The %s in this backup is too large.", f.Name)
		}
		seen[f.Name] = true
		if err := extractEntry(f, filepath.Join(dir, f.Name), limit); err != nil {
			return Manifest{}, err
		}
	}
	for _, required := range []string{DBFile, KeyFile} {
		if !seen[required] {
			return Manifest{}, invalidf("This backup is incomplete: %s is missing.", required)
		}
	}
	return validateFiles(dir)
}

func displayName(name string) string {
	if len(name) > 60 {
		return name[:60] + "..."
	}
	return name
}

func extractEntry(f *zip.File, dest string, limit int64) error {
	rc, err := f.Open()
	if err != nil {
		return invalidf("The %s in this backup could not be read.", f.Name)
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", f.Name, err)
	}
	// Read one byte past the cap so a header that lies about its size is caught.
	n, copyErr := io.Copy(out, io.LimitReader(rc, limit+1))
	closeErr := out.Close()
	switch {
	case n > limit:
		return invalidf("The %s in this backup is too large.", f.Name)
	case copyErr != nil:
		// A bad CRC or truncated stream: the zip itself is damaged.
		return invalidf("The %s in this backup is damaged (%v).", f.Name, copyErr)
	case closeErr != nil:
		return fmt.Errorf("write %s: %w", f.Name, closeErr)
	}
	return nil
}

// requiredTables must exist in any Mediarium database.
var requiredTables = []string{"users", "settings", "movies", "download_queue", "schema_migrations"}

// validateFiles checks a folder holding app.db, secret.key and optionally
// manifest.json: the key parses, the database opens read-only, passes an
// integrity check, has Mediarium's tables, and was not written by a newer
// version than this binary.
func validateFiles(dir string) (Manifest, error) {
	var manifest Manifest
	if data, err := os.ReadFile(filepath.Join(dir, ManifestFile)); err == nil {
		if err := json.Unmarshal(data, &manifest); err != nil {
			return Manifest{}, invalidf("The manifest.json in this backup is not valid.")
		}
		if manifest.App != "" && manifest.App != appName {
			return Manifest{}, invalidf("This backup was made by a different application (%q).", displayName(manifest.App))
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}

	keyData, err := os.ReadFile(filepath.Join(dir, KeyFile))
	if err != nil {
		return Manifest{}, invalidf("This backup is incomplete: %s is missing.", KeyFile)
	}
	if err := crypto.ValidateKeyFile(keyData); err != nil {
		return Manifest{}, invalidf("The %s in this backup is not a valid encryption key.", KeyFile)
	}

	dbPath := filepath.Join(dir, DBFile)
	if _, err := os.Stat(dbPath); err != nil {
		return Manifest{}, invalidf("This backup is incomplete: %s is missing.", DBFile)
	}
	latest, err := LatestMigration()
	if err != nil {
		return Manifest{}, err
	}
	// immutable=1 opens the file read-only without locks and without ever
	// creating -wal/-shm files next to it.
	// The path is part of a file: URI, so the characters that would start a
	// query or fragment (or an escape) are escaped, like store.Open does.
	escaped := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(dbPath)
	db, err := sql.Open("sqlite", "file:"+escaped+"?mode=ro&immutable=1")
	if err != nil {
		return Manifest{}, fmt.Errorf("open staged database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()

	var check string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		return Manifest{}, invalidf("The %s in this backup is damaged or is not a SQLite database.", DBFile)
	}
	for _, table := range requiredTables {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n == 0 {
			return Manifest{}, invalidf("The %s in this backup does not look like a Mediarium database (table %q is missing).", DBFile, table)
		}
	}
	highest, err := highestMigration(ctx, db)
	if err != nil {
		return Manifest{}, invalidf("The %s in this backup does not look like a Mediarium database.", DBFile)
	}
	if highest == "" {
		return Manifest{}, invalidf("The %s in this backup has no schema history.", DBFile)
	}
	if highest > latest {
		return Manifest{}, invalidf("This backup was made by a newer version of Mediarium (schema %s, and this version only knows up to %s). Update Mediarium first.", highest, latest)
	}
	if manifest.Migrations > latest {
		return Manifest{}, invalidf("This backup was made by a newer version of Mediarium. Update Mediarium first.")
	}
	if manifest.Migrations == "" {
		manifest.Migrations = highest
	}
	return manifest, nil
}

// Result reports what ApplyPending did.
type Result struct {
	// Applied is true when a staged restore replaced the live data.
	Applied bool
	// BeforeDir is where the replaced files now live (when Applied).
	BeforeDir string
	// Failed is true when a staged restore was found but could not be used.
	// The live data was left as it was.
	Failed bool
	// FailedDir is where the unusable staging was moved (when Failed).
	FailedDir string
	// Message is a plain-language line for the log; empty if nothing pending.
	Message string
}

// liveFiles are moved aside on restore. The -wal/-shm files belong to
// app.db and must never be left behind next to a different database.
var liveFiles = []string{DBFile, DBFile + "-wal", DBFile + "-shm", KeyFile}

// ApplyPending applies a staged restore, if there is one. It must run before
// the database is opened. now names the before-restore / restore-failed
// folders.
//
// Nothing is ever deleted except the staging folder itself, once its files
// are safely in place. If the staging is incomplete, invalid, or cannot be
// swapped in, the live data is left as it was, the staging is moved to
// restore-failed-<timestamp>/ and the reason is in Result.Message. The only
// error returned is the last-resort case where the swap failed and could not
// be rolled back; the message then names the folder holding the old files.
func ApplyPending(configDir string, now time.Time) (Result, error) {
	removeStaleStaging(configDir)

	pending := filepath.Join(configDir, PendingDir)
	if _, err := os.Stat(pending); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{}, nil
		}
		return Result{}, fmt.Errorf("check for a pending restore: %w", err)
	}

	fail := func(reason string) (Result, error) {
		dest := uniqueDir(filepath.Join(configDir, FailedPrefix+now.Format(timeLayout)))
		if err := os.Rename(pending, dest); err != nil {
			return Result{}, fmt.Errorf("a pending restore is unusable (%s) and could not be moved aside: %w", reason, err)
		}
		return Result{
			Failed:    true,
			FailedDir: dest,
			Message:   fmt.Sprintf("A pending restore wasn't applied (%s). Your data is untouched and the unused files are in %s.", reason, dest),
		}, nil
	}

	if _, err := validateFiles(pending); err != nil {
		var inv *InvalidError
		if errors.As(err, &inv) {
			return fail(lowerFirst(strings.TrimSuffix(inv.Reason, ".")))
		}
		return fail(err.Error())
	}

	before := uniqueDir(filepath.Join(configDir, BeforePrefix+now.Format(timeLayout)))
	if err := os.MkdirAll(before, 0o755); err != nil {
		return fail("could not create " + before + ": " + err.Error())
	}

	moved, err := moveAside(configDir, before)
	if err != nil {
		if rbErr := rollback(configDir, before, moved); rbErr != nil {
			return Result{}, fmt.Errorf("restore failed (%v) and rolling back also failed (%v); your previous files are in %s", err, rbErr, before)
		}
		os.Remove(before)
		return fail("could not move the current files aside: " + err.Error())
	}
	var placed []string
	for _, name := range []string{DBFile, KeyFile} {
		if err := os.Rename(filepath.Join(pending, name), filepath.Join(configDir, name)); err != nil {
			for _, p := range placed {
				os.Remove(filepath.Join(configDir, p))
			}
			if rbErr := rollback(configDir, before, moved); rbErr != nil {
				return Result{}, fmt.Errorf("restore failed (%v) and rolling back also failed (%v); your previous files are in %s", err, rbErr, before)
			}
			os.Remove(before)
			return fail("could not put the restored files in place: " + err.Error())
		}
		placed = append(placed, name)
	}
	if err := os.RemoveAll(pending); err != nil {
		// The restore itself succeeded; a leftover manifest is harmless but
		// would be re-processed next boot, so report it.
		return Result{Applied: true, BeforeDir: before, Message: fmt.Sprintf("Restored from backup. Your previous data is in %s. (Couldn't remove %s: %v)", before, pending, err)}, nil
	}
	return Result{
		Applied:   true,
		BeforeDir: before,
		Message:   fmt.Sprintf("Restored your database and encryption key. Your previous data is in %s. Delete it once you're happy.", before),
	}, nil
}

// moveAside renames every live file into before, returning the names moved.
func moveAside(configDir, before string) ([]string, error) {
	var moved []string
	for _, name := range liveFiles {
		src := filepath.Join(configDir, name)
		if _, err := os.Lstat(src); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return moved, fmt.Errorf("check %s: %w", name, err)
		}
		if err := os.Rename(src, filepath.Join(before, name)); err != nil {
			return moved, fmt.Errorf("move %s aside: %w", name, err)
		}
		moved = append(moved, name)
	}
	return moved, nil
}

func rollback(configDir, before string, moved []string) error {
	var errs []error
	for _, name := range moved {
		if err := os.Rename(filepath.Join(before, name), filepath.Join(configDir, name)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// removeStaleStaging deletes temporary staging/scratch folders left by a
// crash mid-upload. They only ever hold copies of an uploaded backup.
func removeStaleStaging(configDir string) {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && (strings.HasPrefix(n, PendingDir+".tmp-") || strings.HasPrefix(n, ".restore-validate-")) {
			os.RemoveAll(filepath.Join(configDir, n))
		}
		if !e.IsDir() && (strings.HasPrefix(n, ".backup-snapshot-") || strings.HasPrefix(n, ".restore-upload-")) {
			os.Remove(filepath.Join(configDir, n))
		}
	}
}

// uniqueDir returns base, or base-2, base-3... if it already exists.
func uniqueDir(base string) string {
	candidate := base
	for i := 2; ; i++ {
		if _, err := os.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}
