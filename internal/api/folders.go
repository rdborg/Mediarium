package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/settings"
)

// moviesRoot is where movies are organized: the Settings value, falling back
// to the MOVIES_DIR container default so a fresh Docker install works.
func (s *Server) moviesRoot() string {
	if v, _ := s.Settings.Get(settings.KeyMoviesPath); v != "" {
		return v
	}
	return s.cfg.MoviesDir
}

// downloadsRoot is the parent of the incomplete/ working folder.
func (s *Server) downloadsRoot() string {
	if v, _ := s.Settings.Get(settings.KeyDownloadsPath); v != "" {
		return v
	}
	return s.cfg.DownloadsDir
}

// ebooksRoot and audiobooksRoot are the ebooks and audiobooks libraries: the
// Settings value, falling back to EBOOKS_DIR / AUDIOBOOKS_DIR.
func (s *Server) ebooksRoot() string {
	if v, _ := s.Settings.Get(settings.KeyEbooksPath); v != "" {
		return v
	}
	return s.cfg.EbooksDir
}

func (s *Server) audiobooksRoot() string {
	if v, _ := s.Settings.Get(settings.KeyAudiobooksPath); v != "" {
		return v
	}
	return s.cfg.AudiobooksDir
}

type folderPayload struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Writable   bool   `json:"writable"`
	FreeBytes  uint64 `json:"freeBytes"`
	TotalBytes uint64 `json:"totalBytes"`
	Mounted    bool   `json:"mounted"`
	MountKnown bool   `json:"mountKnown"`
	// InDocker is true when Mediarium runs in a container. Only then does
	// "map this folder in your compose file" make sense.
	InDocker bool     `json:"inDocker"`
	Warnings []string `json:"warnings"`
	// CanCreate is true for a folder that doesn't exist yet but can be made
	// safely: its parent exists, can be written, and is mapped to the device
	// (so it isn't lost when the container is updated).
	CanCreate bool `json:"canCreate,omitempty"`
}

func toFolderPayload(f fsinfo.Folder) folderPayload {
	warnings := f.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return folderPayload{
		Path: f.Path, Exists: f.Exists && f.IsDir, Writable: f.Writable, FreeBytes: f.FreeBytes, TotalBytes: f.TotalBytes,
		Mounted: f.Mounted, MountKnown: f.MountKnown, InDocker: fsinfo.InDocker(), Warnings: warnings,
	}
}

// handleFolderCheck inspects one folder for the setup screens: does it
// exist, can Mediarium write to it, how much room is left and — in Docker —
// was it actually mapped in from the host. It only reads (plus one probe file
// that is created and removed); it never changes existing files.
func (s *Server) handleFolderCheck(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if rejectBad(w,
		checkRequired(path, "Enter a folder path to check, for example /media/movies."),
		checkAbsPath(path, "/media/movies"),
		checkMaxLen(path, "The folder path", maxPathLen),
	) {
		return
	}
	writeJSON(w, http.StatusOK, s.folderPayloadFor(path))
}

// folderPayloadFor is toFolderPayload plus whether a missing folder can be created.
func (s *Server) folderPayloadFor(path string) folderPayload {
	p := toFolderPayload(fsinfo.Inspect(path))
	if !p.Exists {
		if parent, ok := creatableParent(path); ok {
			p.CanCreate = true
			p.Warnings = []string{fmt.Sprintf("This folder doesn't exist yet. Mediarium can create it inside %s, which is mapped to your device.", parent)}
		}
	}
	return p
}

// creatableParent reports whether path can be created safely: one level
// below a folder that exists, is writable and (in Docker) is mapped from the
// host. A folder made anywhere else would live inside the container and
// disappear with the next update.
func creatableParent(path string) (string, bool) {
	clean := filepath.Clean(path)
	parent := filepath.Dir(clean)
	if !filepath.IsAbs(clean) || parent == clean || parent == "/" || parent == "." {
		return parent, false
	}
	f := fsinfo.Inspect(parent)
	if !f.Exists || !f.IsDir || !f.Writable {
		return parent, false
	}
	if f.MountKnown && !f.Mounted && !insideMount(parent) {
		return parent, false
	}
	return parent, true
}

// insideMount reports whether dir is below a folder mapped from the device
// (a mapped /data makes /data/Media/Books safe too).
func insideMount(dir string) bool {
	for d := filepath.Dir(dir); d != "/" && d != "." && d != filepath.Dir(d); d = filepath.Dir(d) {
		if f := fsinfo.Inspect(d); f.MountKnown && f.Mounted {
			return true
		}
	}
	return false
}

type createFolderRequest struct {
	Path string `json:"path"`
}

// handleCreateFolder makes a missing library folder (administrators), only
// where creatableParent allows. It answers with the folder's new check.
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var req createFolderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Send the folder to create.")
		return
	}
	path := strings.TrimSpace(req.Path)
	if rejectBad(w,
		checkRequired(path, "Enter the folder to create, for example /data/Ebooks."),
		checkAbsPath(path, "/data/Ebooks"),
		checkMaxLen(path, "The folder path", maxPathLen),
	) {
		return
	}
	if _, err := os.Stat(path); err == nil {
		writeJSON(w, http.StatusOK, s.folderPayloadFor(path))
		return
	}
	parent, ok := creatableParent(path)
	if !ok {
		writeError(w, http.StatusConflict, fmt.Sprintf("Mediarium can only create a folder inside one that is mapped to your device and writable, and %s isn't. Map a folder for it in your compose file instead.", parent))
		return
	}
	if err := os.Mkdir(filepath.Clean(path), 0o775); err != nil && !errors.Is(err, os.ErrExist) {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Couldn't create %s: %v", path, err))
		return
	}
	writeJSON(w, http.StatusOK, s.folderPayloadFor(path))
}
