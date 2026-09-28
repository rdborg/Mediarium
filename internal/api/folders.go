package api

import (
	"net/http"
	"strings"

	"github.com/ryanborg/mediarium/internal/fsinfo"
	"github.com/ryanborg/mediarium/internal/settings"
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

type folderPayload struct {
	Path       string   `json:"path"`
	Exists     bool     `json:"exists"`
	Writable   bool     `json:"writable"`
	FreeBytes  uint64   `json:"freeBytes"`
	TotalBytes uint64   `json:"totalBytes"`
	Mounted    bool     `json:"mounted"`
	MountKnown bool     `json:"mountKnown"`
	Warnings   []string `json:"warnings"`
}

func toFolderPayload(f fsinfo.Folder) folderPayload {
	warnings := f.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return folderPayload{
		Path: f.Path, Exists: f.Exists && f.IsDir, Writable: f.Writable, FreeBytes: f.FreeBytes, TotalBytes: f.TotalBytes,
		Mounted: f.Mounted, MountKnown: f.MountKnown, Warnings: warnings,
	}
}

// handleFolderCheck inspects one folder for the setup screens: does it
// exist, can Mediarium write to it, how much room is left and — in Docker —
// was it actually mapped in from the host. It only reads (plus one probe file
// that is created and removed); it never changes existing files.
func (s *Server) handleFolderCheck(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, "query param path is required")
		return
	}
	writeJSON(w, http.StatusOK, toFolderPayload(fsinfo.Inspect(path)))
}
