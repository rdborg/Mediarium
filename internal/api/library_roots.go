package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/settings"
)

// A library folder on a network drive that isn't connected is an empty folder
// on the machine's own disk. Importing into it would fill that disk, and the
// files would vanish from view the moment the drive comes back. So before
// anything is written under a library folder, the folder has to be there, and
// one that has been seen with its .mediarium-root marker has to still have it.
// A folder that was never marked keeps working as it always did.

// libraryRootError is the refusal for a library folder that isn't usable now.
// Its message is written for the person reading it.
type libraryRootError struct {
	root string
	gone bool // the folder is there but its marker is not
	err  error
}

func (e *libraryRootError) Error() string { return e.err.Error() }

func (e *libraryRootError) Unwrap() error { return e.err }

// isLibraryRootError reports whether err is a refusal from checkLibraryRoot.
func isLibraryRootError(err error) bool {
	var e *libraryRootError
	return errors.As(err, &e)
}

var rootsMu sync.Mutex

// writableRoots lists every folder files are written into: the movie and TV
// folders (with the extra ones and the second-copy ones), music, ebooks and
// audiobooks.
func (s *Server) writableRoots() []string {
	var out []string
	add := func(p string) {
		if p = strings.TrimSpace(p); p != "" && !slices.Contains(out, filepath.Clean(p)) {
			out = append(out, filepath.Clean(p))
		}
	}
	for _, p := range s.movieFolders() {
		add(p)
	}
	for _, p := range s.tvFolders() {
		add(p)
	}
	add(s.movieCopiesRoot())
	add(s.tvCopiesRoot())
	add(s.musicRoot())
	add(s.ebooksRoot())
	add(s.audiobooksRoot())
	add(s.otherRoot())
	return out
}

// confirmedRoots reads the folders that have been seen with their marker.
func (s *Server) confirmedRoots() []string {
	v, _ := s.Settings.Get(settings.KeyRootsConfirmed)
	var list []string
	if v != "" {
		_ = json.Unmarshal([]byte(v), &list)
	}
	return list
}

func (s *Server) setRootConfirmed(root string, on bool) {
	rootsMu.Lock()
	defer rootsMu.Unlock()
	list := s.confirmedRoots()
	has := slices.Contains(list, root)
	if has == on {
		return
	}
	if on {
		list = append(list, root)
	} else {
		list = slices.DeleteFunc(list, func(p string) bool { return p == root })
	}
	b, _ := json.Marshal(list)
	_ = s.Settings.Set(settings.KeyRootsConfirmed, string(b), false)
}

// onContainerDisk reports whether root is known to be on the container's own
// disk, not on a folder mapped in from outside. Tests replace it.
var onContainerDisk = func(root string) bool {
	f := fsinfo.Inspect(root)
	return f.MountKnown && !f.Mounted
}

// confirmLibraryRoot marks root as the real folder: the marker goes in (when
// the folder can be written to) and the folder is remembered. force skips the
// check that the folder already holds something, for a folder a person has just
// chosen. A folder that sits on the container's own disk is never marked: it
// isn't the drive it is meant to be.
func (s *Server) confirmLibraryRoot(root string, force bool) {
	root = filepath.Clean(root)
	if !fsinfo.RootReachable(root) {
		return
	}
	if !fsinfo.HasRootMarker(root) {
		if !force && !fsinfo.RootHasContent(root) {
			return
		}
		if onContainerDisk(root) {
			return
		}
		if fsinfo.WriteRootMarker(root) != nil {
			return
		}
	}
	s.setRootConfirmed(root, true)
}

// libraryRootProblem looks at one library folder: nil when files may be
// written into it, else why not.
func (s *Server) libraryRootProblem(root string) *libraryRootError {
	root = filepath.Clean(root)
	if !fsinfo.RootReachable(root) {
		return &libraryRootError{root: root, err: fmt.Errorf("The library folder %s can't be found, so nothing was put into it. If it is on a network drive, check that the drive is connected. If the folder is new, create it first (Settings > Library and files > Folders), then try again.", root)}
	}
	missing, readErr := fsinfo.RootMarkerState(root)
	if !missing && readErr == nil {
		s.setRootConfirmed(root, true)
		return nil
	}
	if readErr != nil {
		// The folder is there but could not be read to look for the file (a
		// network drive that stumbled). That is not the same as a lost file.
		slog.Warn("library folder: could not look for the marker file", "folder", root, "err", readErr)
		return &libraryRootError{root: root, err: fmt.Errorf("The library folder %s could not be read just now (%v), so nothing was put into it. If it is on a network drive, check that the drive is connected and working, then try again.", root, readErr)}
	}
	if slices.Contains(s.confirmedRoots(), root) {
		return &libraryRootError{root: root, gone: true, err: fmt.Errorf("The library folder %s has lost its %s file. That usually means its drive isn't connected and this is an empty folder on the machine's own disk, so nothing was put into it. Connect the drive and try again. If the folder is the right one, add an empty file named %s to it.", root, fsinfo.RootMarker, fsinfo.RootMarker)}
	}
	s.confirmLibraryRoot(root, false)
	return nil
}

// checkLibraryRoot is called before anything is written to dest. It does
// nothing for a path outside the library folders.
func (s *Server) checkLibraryRoot(dest string) error {
	dest = filepath.Clean(dest)
	// The innermost library folder holding dest is the one that has to be real.
	root := ""
	for _, r := range s.writableRoots() {
		if rootContaining([]string{r}, dest) != "" && len(r) > len(root) {
			root = r
		}
	}
	if root == "" {
		return nil
	}
	if p := s.libraryRootProblem(root); p != nil {
		return p
	}
	return nil
}
