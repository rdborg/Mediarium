package api

import (
	"net/http"
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

// ebooksRoot and audiobooksRoot are the folders kept for those modules (not
// built yet): the Settings value, falling back to EBOOKS_DIR / AUDIOBOOKS_DIR.
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
	writeJSON(w, http.StatusOK, toFolderPayload(fsinfo.Inspect(path)))
}
