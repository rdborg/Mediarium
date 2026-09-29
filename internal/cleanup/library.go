package cleanup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Library removal: deleting a title's files when the title itself is
// removed from the library. Everything here is confined to one library
// folder (root): nothing outside it, nothing reached through a symbolic
// link, and never root itself.

// InsideRoot resolves path (following symbolic links) and reports an error
// unless it lies strictly inside root.
func InsideRoot(root, path string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve library folder %s: %w", root, err)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	if err := within(realRoot, realPath); err != nil {
		return "", fmt.Errorf("%s is not inside the library folder %s: %w", path, root, ErrOutside)
	}
	return realPath, nil
}

// RemoveFolder deletes a title's folder and everything in it, and returns
// the space freed. The folder must be strictly inside root, and neither it
// nor any folder between root and it may be a symbolic link (removing a
// link's target would reach outside). Links inside the folder are removed
// as links.
func RemoveFolder(root, folder string) (int64, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return 0, err
	}
	abs, err := filepath.Abs(folder)
	if err != nil {
		return 0, err
	}
	if err := within(absRoot, abs); err != nil {
		return 0, fmt.Errorf("%s is not inside the library folder %s: %w", folder, root, ErrOutside)
	}
	info, err := os.Lstat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return 0, fmt.Errorf("%s is not a folder of its own (a symbolic link or a file): %w", folder, ErrOutside)
	}
	if _, err := resolveParent(absRoot, abs); err != nil {
		return 0, err
	}
	if _, err := InsideRoot(absRoot, abs); err != nil {
		return 0, err
	}
	size := Size(abs)
	if err := os.RemoveAll(abs); err != nil {
		return 0, fmt.Errorf("remove %s: %w", folder, err)
	}
	return size, nil
}

// sidecarRest matches what may follow "<video name>." in a file that
// belongs with the video: optional language or flag parts, then a subtitle,
// metadata or artwork extension ("en.srt", "en.forced.srt", "nfo",
// "pt-BR.sdh.ass").
var sidecarRest = regexp.MustCompile(`(?i)^(?:(?:[a-z]{2,3}(?:-[a-z]{2,4})?|forced|sdh|default|cc|hi|full|foreign)\.){0,3}(?:srt|ass|ssa|sub|idx|vtt|sup|smi|nfo|jpg|jpeg|png|tbn|txt|xml)$`)

// RemoveFileWithSidecars deletes a title's video file and the files beside
// it that belong with it (the same name with a subtitle, .nfo or artwork
// extension), then folders left empty up to (not including) root. It
// returns what it removed. Each path is checked to lie inside root through
// no symbolic link; a missing file is not an error.
func RemoveFileWithSidecars(root, file string) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	if err := within(absRoot, abs); err != nil {
		return nil, fmt.Errorf("%s is not inside the library folder %s: %w", file, root, ErrOutside)
	}
	if _, err := resolveParent(absRoot, abs); err != nil {
		return nil, err
	}
	dir := filepath.Dir(abs)
	name := filepath.Base(abs)
	stem := strings.TrimSuffix(name, filepath.Ext(name))

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var removed []string
	for _, e := range entries {
		n := e.Name()
		ours := n == name || (strings.HasPrefix(n, stem+".") && sidecarRest.MatchString(n[len(stem)+1:]))
		if !ours || e.IsDir() {
			continue
		}
		p := filepath.Join(dir, n)
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, fmt.Errorf("remove %s: %w", p, err)
		}
		removed = append(removed, p)
	}
	PruneEmptyFolders(absRoot, dir)
	return removed, nil
}

// PruneEmptyFolders removes dir if it is empty, then its parent, and so on
// up to (not including) root. It stops at the first folder that is not
// empty or not inside root.
func PruneEmptyFolders(root, dir string) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return
	}
	cur, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	for within(absRoot, cur) == nil {
		info, err := os.Lstat(cur)
		if err != nil || !info.IsDir() {
			return
		}
		if os.Remove(cur) != nil { // only succeeds on an empty folder
			return
		}
		cur = filepath.Dir(cur)
	}
}
