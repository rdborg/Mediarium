package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/fsinfo"
)

// Putting the .mediarium-root file back by hand. A library folder whose drive
// was replaced (a new volume at the same path, a moved library, a tool that
// tidied hidden files) has lost its marker although nothing is wrong, and
// every import into it is refused. An administrator who knows the folder is
// the right one can create the marker from the app instead of in a shell.

// rootMarkerPayload is what the folder check says about a library folder that
// has no marker. It is left out for any other folder.
type rootMarkerPayload struct {
	Name string `json:"name"` // the marker's file name
	// Seen is true when Mediarium has seen this folder with its marker before:
	// the file was lost, as opposed to a folder that never had one.
	Seen bool `json:"seen"`
	// Titles is how many movies or shows have files recorded under the folder.
	Titles int `json:"titles"`
	// Strong is true when titles are recorded under the folder but none of their
	// folders is there: most likely the drive is not connected and this is an
	// empty folder on the machine's own disk. The person must say the drive is
	// connected before the marker is made.
	Strong bool `json:"strong"`
	// CanCreate is true when the marker can be made: the folder exists, can be
	// written to and is not on the container's own disk.
	CanCreate bool `json:"canCreate"`
	// Why says, when CanCreate is false, what stops it.
	Why string `json:"why,omitempty"`
}

// recordedUnder counts the movies and shows with a file recorded under root,
// and reports whether any of them has its folder there now.
func (s *Server) recordedUnder(root string) (titles int, present bool) {
	root = filepath.Clean(root)
	firstDir := func(file string) string {
		rel, err := filepath.Rel(root, filepath.Clean(file))
		if err != nil {
			return ""
		}
		first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		return filepath.Join(root, first)
	}
	seen := map[string]bool{}
	check := func(file string) {
		titles++
		if present {
			return
		}
		dir := firstDir(file)
		if seen[dir] {
			return
		}
		seen[dir] = true
		if _, err := os.Stat(dir); err == nil {
			present = true
		}
	}
	if movies, err := s.MovieRepo.List(); err == nil {
		for _, m := range movies {
			if m.FilePath != "" && isWithin(root, filepath.Clean(m.FilePath)) {
				check(m.FilePath)
			}
		}
	}
	if byShow, err := s.MovieRepo.EpisodeFilesBySeries(); err == nil {
		for _, files := range byShow {
			for _, f := range files {
				if isWithin(root, filepath.Clean(f)) {
					check(f)
					break // one file is enough to count the show
				}
			}
		}
	}
	return titles, present
}

// rootMarkerInfo describes the marker of root when it is a library folder
// that lacks one; nil otherwise.
func (s *Server) rootMarkerInfo(root string) *rootMarkerPayload {
	root = filepath.Clean(root)
	if !slices.Contains(s.writableRoots(), root) || !fsinfo.RootReachable(root) || fsinfo.HasRootMarker(root) {
		return nil
	}
	p := &rootMarkerPayload{Name: fsinfo.RootMarker, Seen: slices.Contains(s.confirmedRoots(), root), CanCreate: true}
	titles, present := s.recordedUnder(root)
	p.Titles, p.Strong = titles, titles > 0 && !present
	switch {
	case onContainerDisk(root):
		p.CanCreate, p.Why = false, "This folder is on the container's own disk, not on a folder mapped from your device, so it can't be the right one. Map the folder in your compose file first."
	case !fsinfo.Inspect(root).Writable:
		p.CanCreate, p.Why = false, "Mediarium may not write to this folder, so it can't make the file. Give the user Mediarium runs as (PUID and PGID) write access to it."
	}
	return p
}

type rootMarkerRequest struct {
	Path           string `json:"path"`
	DriveConnected bool   `json:"driveConnected"`
}

// POST /api/settings/library-marker: make the .mediarium-root file in a library
// folder the administrator says is the right one.
func (s *Server) handleCreateRootMarker(w http.ResponseWriter, r *http.Request) {
	var req rootMarkerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Send the library folder.")
		return
	}
	root := filepath.Clean(strings.TrimSpace(req.Path))
	if rejectBad(w,
		checkRequired(req.Path, "Enter the library folder."),
		checkAbsPath(req.Path, "/data/tv"),
		checkMaxLen(req.Path, "The folder path", maxPathLen),
	) {
		return
	}
	if !slices.Contains(s.writableRoots(), root) {
		writeError(w, http.StatusBadRequest, "That is not one of your library folders. Add it in Settings > Library and files > Folders first.")
		return
	}
	if !fsinfo.RootReachable(root) {
		writeError(w, http.StatusConflict, fmt.Sprintf("The folder %s can't be found. If it is on a network drive, connect the drive first.", root))
		return
	}
	if fsinfo.HasRootMarker(root) {
		s.setRootConfirmed(root, true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folder": root})
		return
	}
	info := s.rootMarkerInfo(root)
	if info == nil || !info.CanCreate {
		why := "The file can't be made in this folder."
		if info != nil && info.Why != "" {
			why = info.Why
		}
		writeError(w, http.StatusConflict, why)
		return
	}
	if info.Strong && !req.DriveConnected {
		writeError(w, http.StatusConflict, fmt.Sprintf("Titles are recorded in %s, but none of their folders is there. That usually means the drive is not connected. Confirm that the drive is connected, then try again.", root))
		return
	}
	if err := fsinfo.WriteRootMarker(root); err != nil {
		slog.Warn("library folder: could not make the marker file", "folder", root, "err", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Couldn't make the file in %s: %v", root, err))
		return
	}
	s.setRootConfirmed(root, true)
	actor := "an administrator"
	if u := auth.UserFromContext(r.Context()); u != nil {
		actor = u.Username
	}
	slog.Warn("library folder: marker file made by hand", "folder", root, "by", actor, "titles", info.Titles, "drive_confirmed", req.DriveConnected)
	_ = s.QueueRepo.LogActivity(0, "library", fmt.Sprintf("%s made the %s file in the library folder %s, saying it is the right folder.", actor, fsinfo.RootMarker, root))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folder": root})
}
