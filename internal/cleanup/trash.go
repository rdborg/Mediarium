package cleanup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The recycle bin. When a title is removed with its files, the files are
// moved into a hidden folder inside the same library folder instead of being
// deleted, so a slip can be undone for a few days. Moving within one library
// folder is a rename on the same disk: instant, whatever the size. Media
// servers and Mediarium's own scans skip hidden folders, so nothing in the
// bin shows up anywhere.

// TrashDir is the recycle bin folder inside each library folder.
const TrashDir = ".mediarium-trash"

// ErrTrashEntry is returned for an entry that does not exist or whose name
// is not one the bin makes.
var ErrTrashEntry = errors.New("that item is not in the recycle bin")

// ErrRestoreBlocked is returned when something already sits where a file
// would be put back.
var ErrRestoreBlocked = errors.New("something is already in the way")

// trashID is the shape of an entry's folder name: when it was removed and a
// random part.
var trashID = regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{8}$`)

// TrashEntry is one removal in the bin: a title's folder, or its files.
type TrashEntry struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	DeletedAt time.Time `json:"deletedAt"`
	Paths     []string  `json:"paths"` // where each item was, to put it back
	Size      int64     `json:"size"`
	Files     int       `json:"files"`
}

// TrashFolder moves a title's folder into root's bin. It follows the same
// rules as RemoveFolder: strictly inside root, not a symbolic link, and no
// symbolic link on the way.
func TrashFolder(root, folder, label string, now time.Time) (TrashEntry, error) {
	absRoot, abs, err := checkedPath(root, folder)
	if err != nil {
		return TrashEntry{}, err
	}
	info, err := os.Lstat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return TrashEntry{}, nil
	}
	if err != nil {
		return TrashEntry{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return TrashEntry{}, fmt.Errorf("%s is not a folder of its own (a symbolic link or a file): %w", folder, ErrOutside)
	}
	if _, err := InsideRoot(absRoot, abs); err != nil {
		return TrashEntry{}, err
	}
	return moveToTrash(absRoot, label, []string{abs}, now)
}

// TrashFileWithSidecars moves a video file and the files that belong with it
// (the ones RemoveFileWithSidecars would delete) into root's bin as one
// entry, then removes folders left empty.
func TrashFileWithSidecars(root, file, label string, now time.Time) (TrashEntry, error) {
	absRoot, abs, err := checkedPath(root, file)
	if err != nil {
		return TrashEntry{}, err
	}
	files, err := fileAndSidecars(abs)
	if err != nil || len(files) == 0 {
		return TrashEntry{}, err
	}
	entry, err := moveToTrash(absRoot, label, files, now)
	if err != nil {
		return entry, err
	}
	PruneEmptyFolders(absRoot, filepath.Dir(abs))
	return entry, nil
}

// checkedPath makes root and path absolute and refuses a path that is not
// strictly inside root, or that is reached through a symbolic link.
func checkedPath(root, path string) (string, string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	if err := within(absRoot, abs); err != nil {
		return "", "", fmt.Errorf("%s is not inside the library folder %s: %w", path, root, ErrOutside)
	}
	if strings.HasPrefix(filepath.ToSlash(mustRel(absRoot, abs)), TrashDir+"/") {
		return "", "", fmt.Errorf("%s is already in the recycle bin: %w", path, ErrOutside)
	}
	if _, err := resolveParent(absRoot, abs); err != nil {
		return "", "", err
	}
	return absRoot, abs, nil
}

func mustRel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}

// moveToTrash renames each path into a new entry folder, keeping its place
// relative to root, and writes the entry's record. If one move fails, the
// ones already made are put back.
func moveToTrash(absRoot, label string, paths []string, now time.Time) (TrashEntry, error) {
	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return TrashEntry{}, err
	}
	id := now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(rnd[:])
	dir := filepath.Join(absRoot, TrashDir, id)
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
		return TrashEntry{}, fmt.Errorf("make the recycle bin folder: %w", err)
	}
	entry := TrashEntry{ID: id, Label: label, DeletedAt: now.UTC()}
	var moved [][2]string
	undo := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			_ = os.Rename(moved[i][1], moved[i][0])
		}
		_ = os.RemoveAll(dir)
	}
	for _, p := range paths {
		dest := filepath.Join(dir, "files", mustRel(absRoot, p))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			undo()
			return TrashEntry{}, fmt.Errorf("make the recycle bin folder: %w", err)
		}
		size, files := Size(p), countFiles(p)
		if err := os.Rename(p, dest); err != nil {
			undo()
			return TrashEntry{}, fmt.Errorf("move %s to the recycle bin: %w", p, err)
		}
		moved = append(moved, [2]string{p, dest})
		entry.Paths = append(entry.Paths, p)
		entry.Size += size
		entry.Files += files
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "entry.json"), data, 0o644)
	}
	if err != nil {
		undo()
		return TrashEntry{}, fmt.Errorf("write the recycle bin record: %w", err)
	}
	return entry, nil
}

// countFiles counts the files at path (1 for a file).
func countFiles(path string) int {
	n := 0
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// entryDir returns the folder of entry id in root's bin, refusing any id the
// bin did not make (so an id can never point somewhere else).
func entryDir(root, id string) (string, error) {
	if !trashID.MatchString(id) {
		return "", ErrTrashEntry
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(absRoot, TrashDir, id)
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return "", ErrTrashEntry
	}
	return dir, nil
}

func readEntry(dir string) (TrashEntry, error) {
	data, err := os.ReadFile(filepath.Join(dir, "entry.json"))
	if err != nil {
		return TrashEntry{}, err
	}
	var e TrashEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return TrashEntry{}, err
	}
	e.ID = filepath.Base(dir)
	return e, nil
}

// ListTrash returns the entries in root's bin, newest first. A missing bin
// is an empty list.
func ListTrash(root string) ([]TrashEntry, error) {
	if root == "" {
		return nil, nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(filepath.Join(absRoot, TrashDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the recycle bin: %w", err)
	}
	var out []TrashEntry
	for _, d := range dirs {
		if !d.IsDir() || !trashID.MatchString(d.Name()) {
			continue
		}
		e, err := readEntry(filepath.Join(absRoot, TrashDir, d.Name()))
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeletedAt.After(out[j].DeletedAt) })
	return out, nil
}

// RestoreTrash puts an entry's files back where they were and removes the
// entry. Nothing is moved when any original place is taken.
func RestoreTrash(root, id string) (TrashEntry, error) {
	dir, err := entryDir(root, id)
	if err != nil {
		return TrashEntry{}, err
	}
	e, err := readEntry(dir)
	if err != nil {
		return TrashEntry{}, fmt.Errorf("read the recycle bin record: %w", err)
	}
	absRoot, _ := filepath.Abs(root)
	for _, p := range e.Paths {
		if within(absRoot, p) != nil {
			return e, fmt.Errorf("%s is not inside the library folder: %w", p, ErrOutside)
		}
		if _, err := os.Lstat(p); err == nil {
			return e, fmt.Errorf("%s: %w", p, ErrRestoreBlocked)
		}
	}
	for _, p := range e.Paths {
		src := filepath.Join(dir, "files", mustRel(absRoot, p))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return e, err
		}
		if err := os.Rename(src, p); err != nil {
			return e, fmt.Errorf("put back %s: %w", p, err)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return e, fmt.Errorf("tidy the recycle bin: %w", err)
	}
	return e, nil
}

// DeleteTrash deletes one entry for good.
func DeleteTrash(root, id string) error {
	dir, err := entryDir(root, id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete from the recycle bin: %w", err)
	}
	return nil
}

// PurgeTrash deletes the entries removed before cutoff and returns how many
// and the space freed.
func PurgeTrash(root string, cutoff time.Time) (int, int64, error) {
	entries, err := ListTrash(root)
	if err != nil {
		return 0, 0, err
	}
	n, freed := 0, int64(0)
	for _, e := range entries {
		if !e.DeletedAt.Before(cutoff) {
			continue
		}
		if err := DeleteTrash(root, e.ID); err != nil {
			return n, freed, err
		}
		n++
		freed += e.Size
	}
	return n, freed, nil
}
