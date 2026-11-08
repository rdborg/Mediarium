package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// keepSafetyCopies is how many of the copies made before schema changes are
// kept next to the database.
const keepSafetyCopies = 2

const safetyCopyPrefix = "pre-migration-"

// appliedVersions returns the schema changes the database says it has had.
func appliedVersions(conn *sql.DB) (map[string]bool, error) {
	rows, err := conn.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("read schema_migrations: %w", err)
		}
		out[v] = true
	}
	return out, rows.Err()
}

// NewerMessage is the plain sentence for a database that a newer version of
// Mediarium has already changed. versions are the changes this program does not
// know.
func NewerMessage(versions []string) string {
	return fmt.Sprintf("This database was updated by a newer version of Mediarium (it has changes this version doesn't know, the newest is %s). Update to the newer version again to keep using it safely, or restore a backup made before that update.", versions[len(versions)-1])
}

func isMemory(path string) bool {
	return path == ":memory:" || path == "" || strings.Contains(path, "mode=memory")
}

// snapshotBeforeMigrating writes a copy of the database to
// pre-migration-<version>-<YYYYMMDD-HHMMSS>.db next to it, readable by the
// owner only, and keeps the newest keepSafetyCopies of them. version is the
// last change about to be applied. The time stamp keeps a second update to the
// same change (after a backup is restored, say) from replacing the copy made
// at the first one.
func snapshotBeforeMigrating(conn *sql.DB, path, version string) (string, error) {
	final, err := writeCopy(conn, filepath.Dir(path), safetyCopyPrefix+version+"-"+time.Now().Format("20060102-150405"))
	if err != nil {
		return "", err
	}
	pruneSafetyCopies(filepath.Dir(path))
	return final, nil
}

// writeCopy makes <base>.db (or <base>-2.db, and so on, when that name is
// taken) in dir as a copy of the open database.
func writeCopy(conn *sql.DB, dir, base string) (string, error) {
	final := filepath.Join(dir, base+".db")
	for i := 2; ; i++ { // two starts within the same second
		if _, err := os.Lstat(final); os.IsNotExist(err) {
			break
		}
		final = filepath.Join(dir, fmt.Sprintf("%s-%d.db", base, i))
	}
	// VACUUM INTO makes the file itself with the default mode, so it goes into
	// a folder only the owner can open and is moved out once it is complete.
	work, err := os.MkdirTemp(dir, ".pre-migration-")
	if err != nil {
		return "", fmt.Errorf("make a folder for the copy: %w", err)
	}
	defer os.RemoveAll(work)
	tmp := filepath.Join(work, "copy.db")
	if _, err := conn.Exec("VACUUM INTO '" + strings.ReplaceAll(tmp, "'", "''") + "'"); err != nil {
		return "", fmt.Errorf("copy the database: %w", err)
	}
	_ = os.Chmod(tmp, 0o600)
	if err := os.Rename(tmp, final); err != nil {
		return "", fmt.Errorf("save the copy: %w", err)
	}
	return final, nil
}

const configCopyPrefix = "pre-config-"

// keepConfigCopies is how many of the copies made before a settings file is
// applied are kept.
const keepConfigCopies = 3

// SnapshotBeforeConfig writes a copy of the database to
// pre-config-<YYYYMMDD-HHMMSS>.db next to it, readable by the owner only, and
// keeps the newest few. It is made before a settings file is applied. It is
// not one of the copies "going back after an update" offers.
func SnapshotBeforeConfig(conn *sql.DB, path string) (string, error) {
	if isMemory(path) {
		return "", nil
	}
	dir := filepath.Dir(path)
	final, err := writeCopy(conn, dir, configCopyPrefix+time.Now().Format("20060102-150405"))
	if err != nil {
		return "", err
	}
	matches, _ := filepath.Glob(filepath.Join(dir, configCopyPrefix+"*.db"))
	sort.Strings(matches) // the time stamp sorts oldest first
	for _, m := range matches[:max(0, len(matches)-keepConfigCopies)] {
		_ = os.Remove(m)
	}
	return final, nil
}

// pruneSafetyCopies removes all but the newest keepSafetyCopies copies.
func pruneSafetyCopies(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, safetyCopyPrefix+"*.db"))
	type file struct {
		path string
		mod  int64
	}
	var files []file
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil {
			files = append(files, file{m, info.ModTime().UnixNano()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod > files[j].mod })
	for _, f := range files[min(keepSafetyCopies, len(files)):] {
		_ = os.Remove(f.path)
	}
}

// SafetyCopy is one of the copies made before an update changed the tables.
type SafetyCopy struct {
	Name string    `json:"name"`
	Path string    `json:"-"`
	At   time.Time `json:"at"` // when the update started, the moment the copy shows
	Size int64     `json:"size"`
}

// copyStamp finds the time in a copy's name: ...-20261101-190412.db, maybe
// with -2 after it for a second copy within the same second.
var copyStamp = regexp.MustCompile(`-(\d{8}-\d{6})(?:-\d+)?\.db$`)

// SafetyCopies lists the copies next to the database at dbPath, newest first.
func SafetyCopies(dbPath string) []SafetyCopy {
	if isMemory(dbPath) {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(dbPath), safetyCopyPrefix+"*.db"))
	var out []SafetyCopy
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		at := info.ModTime()
		if sm := copyStamp.FindStringSubmatch(filepath.Base(m)); sm != nil {
			if t, err := time.ParseInLocation("20060102-150405", sm[1], time.Local); err == nil {
				at = t
			}
		}
		out = append(out, SafetyCopy{Name: filepath.Base(m), Path: m, At: at, Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// SafetyCopyPath is the copy called name next to the database at dbPath. Only
// a name SafetyCopies lists is accepted, so it can't point anywhere else.
func SafetyCopyPath(dbPath, name string) (string, bool) {
	for _, c := range SafetyCopies(dbPath) {
		if c.Name == name {
			return c.Path, true
		}
	}
	return "", false
}

// CopySetting reads one setting that isn't encrypted from a database copy,
// without changing the file. It is "" when the copy doesn't have it.
func CopySetting(path, key string) string {
	escaped := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(path)
	conn, err := sql.Open("sqlite", "file:"+escaped+"?mode=ro&immutable=1")
	if err != nil {
		return ""
	}
	defer conn.Close()
	var v string
	if err := conn.QueryRow(`SELECT value FROM settings WHERE key = ? AND encrypted = 0`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}
