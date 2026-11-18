package api

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// A finished download is not imported when a file is already at its place and
// "If a file already exists" says to keep that one. When the title has no file
// on record (it was taken out of the library with its files left on the disk
// and then added again, or the file was copied there by hand) the kept file is
// the one it should have. Left unlinked, the title stays wanted, so every later
// search downloads the same release again just to keep the file once more.

// keptFileTier is the quality the name of a kept file says, "" when it says none.
func keptFileTier(path string) string {
	tier := string(quality.Classify(parser.Parse(filepath.Base(path))))
	if tier == string(quality.TierUnknown) {
		return ""
	}
	return tier
}

// adoptKeptMovieFile links the file that was kept at a movie's place to the
// movie when it has no file of its own, and says so in its log. It reports
// whether it did.
func (s *Server) adoptKeptMovieFile(movieID int64, kept string) bool {
	if !fileExists(kept) || !s.insideLibrary(kept) {
		return false
	}
	m, err := s.MovieRepo.Get(movieID)
	if err != nil || fileExists(m.FilePath) {
		return false
	}
	if owner := otherFileOwner(s.trackedFileOwners(), kept, movieID, nil); owner != "" {
		return false
	}
	if err := s.MovieRepo.SetStatus(movieID, library.StatusDownloaded, keptFileTier(kept), kept); err != nil {
		slog.Warn("import: couldn't link the kept file to the movie", "movie", m.Title, "err", err)
		return false
	}
	_ = s.QueueRepo.LogActivity(movieID, "imported", fmt.Sprintf("%s: the file %s was already in your library, so Mediarium linked it to the movie.", movieLabel(m), filepath.Base(kept)))
	s.mediaServersImported(mediaservers.MediaMovie, kept)
	s.kickMediaInfo()
	return true
}

// adoptKeptEpisodeFile is adoptKeptMovieFile for an episode.
func (s *Server) adoptKeptEpisodeFile(series library.Series, ep library.Episode, kept string) bool {
	if !fileExists(kept) || !s.insideLibrary(kept) || fileExists(ep.FilePath) {
		return false
	}
	if owner := otherFileOwner(s.trackedFileOwners(), kept, 0, []int64{ep.ID}); owner != "" {
		return false
	}
	if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, keptFileTier(kept), kept); err != nil {
		slog.Warn("import: couldn't link the kept file to the episode", "show", series.Title, "err", err)
		return false
	}
	_ = s.QueueRepo.LogSeriesActivity(series.ID, "imported", fmt.Sprintf("%s S%02dE%02d: the file %s was already in your library, so Mediarium linked it to the episode.", series.Title, ep.Season, ep.Episode, filepath.Base(kept)))
	s.mediaServersImported(mediaservers.MediaTV, kept)
	s.kickMediaInfo()
	return true
}
