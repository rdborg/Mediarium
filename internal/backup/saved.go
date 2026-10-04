package backup

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// Backups Mediarium makes by itself (every night, and before an update) are
// kept in a folder inside the config folder. They are the same zip as a
// downloaded backup, so any of them can be restored the usual way.

// SavedDir is the folder, inside the config folder, that holds saved backups.
const SavedDir = "backups"

// savedName matches the file names Save writes (and nothing else, so a
// request can never name another file).
var savedName = regexp.MustCompile(`^mediarium-backup-\d{8}-\d{6}\.zip$`)

// ValidName reports whether name is the name of a saved backup.
func ValidName(name string) bool { return savedName.MatchString(name) }

// Saved is one backup in the folder.
type Saved struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

// Save writes a backup into configDir/backups and returns its path. The file
// is written under a temporary name and renamed when complete, so the folder
// never holds half a backup. It holds every stored secret, so only the owner
// may read it.
func Save(ctx context.Context, db *sql.DB, configDir, version string, now time.Time) (string, error) {
	dir := filepath.Join(configDir, SavedDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make the backups folder: %w", err)
	}
	final := filepath.Join(dir, FileName(now))
	tmp, err := os.CreateTemp(dir, ".saving-*.zip")
	if err != nil {
		return "", fmt.Errorf("create backup file: %w", err)
	}
	defer os.Remove(tmp.Name()) // gone already once renamed
	_ = tmp.Chmod(0o600)
	if err := Create(ctx, db, configDir, version, now, tmp); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("finish backup file: %w", err)
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", fmt.Errorf("save backup: %w", err)
	}
	return final, nil
}

// ListSaved returns the saved backups, newest first. A missing folder is an
// empty list.
func ListSaved(configDir string) ([]Saved, error) {
	entries, err := os.ReadDir(filepath.Join(configDir, SavedDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the backups folder: %w", err)
	}
	var out []Saved
	for _, e := range entries {
		if e.IsDir() || !savedName.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		created, err := time.ParseInLocation(timeLayout, e.Name()[len("mediarium-backup-"):len(e.Name())-len(".zip")], time.Local)
		if err != nil {
			created = info.ModTime()
		}
		out = append(out, Saved{Name: e.Name(), Size: info.Size(), CreatedAt: created})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// PathOf returns the path of the saved backup called name, or an error when
// there is none by that name.
func PathOf(configDir, name string) (string, error) {
	if !savedName.MatchString(name) {
		return "", os.ErrNotExist
	}
	p := filepath.Join(configDir, SavedDir, name)
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	return p, nil
}

// Prune keeps the newest keep backups and deletes the rest. It returns how
// many it deleted.
func Prune(configDir string, keep int) (int, error) {
	if keep < 1 {
		keep = 1
	}
	list, err := ListSaved(configDir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range list[min(keep, len(list)):] {
		if err := os.Remove(filepath.Join(configDir, SavedDir, s.Name)); err != nil && !os.IsNotExist(err) {
			return n, fmt.Errorf("delete old backup %s: %w", s.Name, err)
		}
		n++
	}
	return n, nil
}
