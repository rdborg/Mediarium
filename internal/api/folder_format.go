package api

import (
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/settings"
)

// The folder a new movie or show gets is named by the folder formats under
// Settings > Library and files > File names and importing. They are only used with the
// Custom naming style; every other style keeps the folders it always had.
// A title that already has files keeps the folder they are in, whatever the
// format says now, so changing a format never splits a show in two.

// folderFormat is the format in use for a title folder: the saved one with
// the Custom style, else the default.
func (s *Server) folderFormat(key, fallback string) string {
	if preset, _ := s.Settings.Get(settings.KeyNamingPreset); preset != "custom" {
		return fallback
	}
	if v, _ := s.Settings.Get(key); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func (s *Server) movieFolderFormat() string {
	return s.folderFormat(settings.KeyMovieFolderFormat, organizer.DefaultMovieFolder)
}

func (s *Server) seriesFolderFormat() string {
	return s.folderFormat(settings.KeySeriesFolderFormat, organizer.DefaultSeriesFolder)
}

// insideRoot reports whether dir is a folder below root (not root itself).
func insideRoot(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// keepMovieFolder puts a movie's next file next to the one it already has,
// when a custom folder format is in use: the format is for new movies, and a
// movie that already has a folder keeps it. The same applies when new folders
// are named in the info language and that name differs from the English one:
// a movie that already has files keeps the folder they are in, so changing
// the language never moves them. dest is the path the naming settings gave;
// root is the library folder it was built in. With the default folder format
// and an English name, dest comes back unchanged, as it always was.
func (s *Server) keepMovieFolder(m library.Movie, root, dest string) string {
	if m.FilePath == "" || (s.movieFolderFormat() == organizer.DefaultMovieFolder && !s.folderNameWouldChange(m.TMDBID, "movie", m.Title)) {
		return dest
	}
	dir := filepath.Dir(m.FilePath)
	if !insideRoot(root, dir) {
		return dest
	}
	return filepath.Join(dir, filepath.Base(dest))
}

// existingSeriesDir is the folder a show's episodes are already in, when it
// is below root. ok is false for a show with no files yet.
func (s *Server) existingSeriesDir(sr library.Series, root string) (string, bool) {
	eps, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return "", false
	}
	var files []string
	for _, ep := range eps {
		if ep.FilePath != "" {
			files = append(files, ep.FilePath)
		}
	}
	if len(files) == 0 {
		return "", false
	}
	dir := s.seriesFolderFor(sr, files)
	if dir == "" || !insideRoot(root, dir) {
		return "", false
	}
	return dir, true
}
