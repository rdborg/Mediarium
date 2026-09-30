// Package cleanup finds and removes what downloads leave behind in the
// downloads working folder: folders of finished or failed downloads,
// folders no download owns any more, and empty folders. It also removes a
// title's files from a library folder when the title itself is removed.
//
// It only ever deletes inside the folder it is given, and checks that on
// every single removal: a path that leads outside (through "..", or a
// symbolic link anywhere on the way) is refused, symbolic links found inside
// are removed as links and never followed, and the library folders are
// never touched by the downloads clean-up even when they sit inside the
// downloads folder. It knows nothing about the queue or the library: the
// caller decides what each folder is (see Classify).
package cleanup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Reason says why an entry can be removed.
type Reason string

const (
	// ReasonOrphaned: no download owns it (or its download failed a while
	// ago), and it has not changed for a day.
	ReasonOrphaned Reason = "orphaned"
	// ReasonImportedLeftover: its download was imported into the library
	// and nothing uses the folder any more.
	ReasonImportedLeftover Reason = "imported-leftover"
	// ReasonSeedingFinished: a torrent that was imported and has reached
	// its seeding goal.
	ReasonSeedingFinished Reason = "seeding-finished"
	// ReasonEmptyFolder: a folder with no files in it.
	ReasonEmptyFolder Reason = "empty-folder"
)

// ErrOutside is returned for a path that is not inside the area.
var ErrOutside = errors.New("outside the folder clean-up may touch")

// Area is the part of the disk the downloads clean-up works in.
type Area struct {
	// Base is the downloads folder. Reported paths are relative to it.
	Base string
	// Work is the working folder inside Base (downloads/incomplete). Only
	// entries inside it are ever removed, and never Work itself.
	Work string
	// Protected folders are never removed or entered, even when they lie
	// inside Work (the movie and TV library folders).
	Protected []string
}

// Item is one entry clean-up can remove.
type Item struct {
	Path      string // relative to Area.Base, with forward slashes
	SizeBytes int64  // space removing it frees (hardlinked files count 0)
	Reason    Reason
	abs       string
}

// Decision is what the caller decides about one entry of the working
// folder.
type Decision struct {
	Remove bool
	Reason Reason
	// Active marks an entry a download is still using (downloading,
	// importing, seeding, waiting for a decision): it is never touched,
	// not even when it is an empty folder.
	Active bool
}

// Classify decides about one top-level entry of the working folder: name
// is its name, info its Lstat result. Returning Remove false keeps it
// (something too new to judge, or Active).
type Classify func(name string, info fs.FileInfo) Decision

// Scan lists the top-level entries of the working folder that can be
// removed: what classify says to remove, and any entry that holds no files
// at all (reason empty-folder) unless classify keeps it for a running
// download. A missing working folder is not an error.
func Scan(area Area, classify Classify) ([]Item, error) {
	work, err := filepath.Abs(area.Work)
	if err != nil {
		return nil, fmt.Errorf("working folder: %w", err)
	}
	entries, err := os.ReadDir(work)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read working folder: %w", err)
	}
	var items []Item
	for _, e := range entries {
		abs := filepath.Join(work, e.Name())
		info, err := os.Lstat(abs)
		if err != nil {
			continue // removed meanwhile
		}
		if err := area.check(abs); err != nil {
			continue // a library folder inside the working folder, for example
		}
		d := classify(e.Name(), info)
		if d.Active {
			continue
		}
		empty := info.IsDir() && isEmptyTree(abs)
		if !d.Remove && !empty {
			continue
		}
		if empty {
			d.Reason = ReasonEmptyFolder
		}
		items = append(items, Item{Path: area.rel(abs), SizeBytes: Size(abs), Reason: d.Reason, abs: abs})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

// Remove deletes items found by Scan, checking each again first, and
// returns the ones it removed. It carries on past a failure and returns
// the first error.
func Remove(area Area, items []Item) ([]Item, error) {
	var removed []Item
	var firstErr error
	for _, it := range items {
		if err := area.removeEntry(it.abs); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove %s: %w", it.Path, err)
			}
			continue
		}
		removed = append(removed, it)
	}
	return removed, firstErr
}

// RemoveInside deletes one entry of the working folder (a download's
// folder), after checking it lies inside it, and returns the space freed.
// A path that does not exist is not an error.
func RemoveInside(area Area, path string) (int64, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	if _, err := os.Lstat(abs); errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	size := Size(abs)
	if err := area.removeEntry(abs); err != nil {
		return 0, err
	}
	return size, nil
}

// Rel reports path relative to the downloads folder, as Scan does.
func (a Area) Rel(path string) string { return a.rel(path) }

func (a Area) rel(abs string) string {
	base, _ := filepath.Abs(a.Base)
	if r, err := filepath.Rel(base, abs); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(abs)
}

// removeEntry checks abs and deletes it. RemoveAll never follows symbolic
// links: a link is removed as a link, whatever it points at.
func (a Area) removeEntry(abs string) error {
	if err := a.check(abs); err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

// check refuses anything that is not strictly inside the working folder,
// reached through a symbolic link, or that is (or holds) a protected folder.
func (a Area) check(abs string) error {
	work, err := filepath.Abs(a.Work)
	if err != nil {
		return err
	}
	if err := within(work, abs); err != nil {
		return err
	}
	realWork, err := filepath.EvalSymlinks(work)
	if err != nil {
		return fmt.Errorf("resolve working folder: %w", err)
	}
	realPath, err := resolveParent(work, abs)
	if err != nil {
		return err
	}
	if err := within(realWork, realPath); err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	for _, p := range a.Protected {
		if p == "" {
			continue
		}
		realP, err := filepath.EvalSymlinks(p)
		if err != nil {
			realP, _ = filepath.Abs(p)
		}
		if realP == realPath || within(realP, realPath) == nil {
			return fmt.Errorf("%s is inside the library folder %s: %w", abs, p, ErrOutside)
		}
		if info.IsDir() && within(realPath, realP) == nil {
			return fmt.Errorf("%s holds the library folder %s: %w", abs, p, ErrOutside)
		}
	}
	return nil
}

// within reports nil when path is strictly inside dir (not dir itself).
func within(dir, path string) error {
	r, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return fmt.Errorf("%s: %w", path, ErrOutside)
	}
	return nil
}

// resolveParent returns abs with every folder between root and it
// resolved, refusing when one of those folders is a symbolic link: removing
// root/link/x would delete wherever the link leads. The last element is
// left as it is (a link there is removed as a link).
func resolveParent(root, abs string) (string, error) {
	r, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	parts := strings.Split(r, string(filepath.Separator))
	cur := root
	for _, p := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, p)
		info, err := os.Lstat(cur)
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is reached through the symbolic link %s: %w", abs, cur, ErrOutside)
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(realRoot, r), nil
}

// isEmptyTree reports whether dir holds no files at any depth (only empty
// folders). Symbolic links count as files.
func isEmptyTree(dir string) bool {
	empty := true
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			empty = false
			return fs.SkipAll
		}
		if !d.IsDir() {
			empty = false
			return fs.SkipAll
		}
		return nil
	})
	return empty
}

// Size is the space deleting path would free: the size of every regular
// file below it that has no other hard link (a file hardlinked into the
// library frees nothing). Symbolic links are not followed.
func Size(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if linkCount(info) <= 1 {
			total += info.Size()
		}
		return nil
	})
	return total
}

// OlderThan reports whether info was last changed more than age ago.
func OlderThan(info fs.FileInfo, age time.Duration, now time.Time) bool {
	return now.Sub(info.ModTime()) > age
}
