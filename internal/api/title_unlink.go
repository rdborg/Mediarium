package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/rdborg/mediarium/internal/library"
)

// Unlinking is the opposite of linking a file by hand: the movie or episode
// lets go of its file, which stays exactly where it is on the disk, and is
// missing again. The file can then be linked to a different episode or movie.

// unlinkedAnswer says which file was let go of.
type unlinkedAnswer struct {
	File string `json:"file"`
}

// handleUnlinkMovieFile detaches a movie from its file without deleting it.
func (s *Server) handleUnlinkMovieFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return
	}
	if err != nil {
		slog.Warn("unlink: couldn't read a movie", "movie", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the movie. Check the Mediarium log for details.")
		return
	}
	if m.Status != library.StatusDownloaded || m.FilePath == "" {
		writeError(w, http.StatusConflict, "This movie has no file to unlink.")
		return
	}
	if err := s.MovieRepo.UnlinkMovieFile(m.ID); err != nil {
		slog.Warn("unlink: couldn't unlink a movie's file", "movie", m.Title, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't unlink the file. Check the Mediarium log for details.")
		return
	}
	name := filepath.Base(m.FilePath)
	slog.Info("unlink: a movie was detached from its file", "movie", m.Title, "file", name)
	_ = s.QueueRepo.LogActivity(m.ID, "removed", fmt.Sprintf("%s: the file %s was unlinked by hand. The file is still on the disk. The movie is wanted again.", movieLabel(m), name))
	writeJSON(w, http.StatusOK, unlinkedAnswer{File: name})
}

// handleUnlinkEpisodeFile detaches an episode from its file without deleting it.
func (s *Server) handleUnlinkEpisodeFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid episode ID.")
		return
	}
	ep, err := s.MovieRepo.GetEpisodeByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That episode isn't in your library.")
		return
	}
	sr, err := s.MovieRepo.GetSeries(ep.SeriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	if ep.Status != library.StatusDownloaded || ep.FilePath == "" {
		writeError(w, http.StatusConflict, "This episode has no file to unlink.")
		return
	}
	if err := s.MovieRepo.UnlinkEpisodeFile(ep.ID); err != nil {
		slog.Warn("unlink: couldn't unlink an episode's file", "series", sr.Title, "season", ep.Season, "episode", ep.Episode, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't unlink the file. Check the Mediarium log for details.")
		return
	}
	name := filepath.Base(ep.FilePath)
	slog.Info("unlink: an episode was detached from its file", "series", sr.Title, "season", ep.Season, "episode", ep.Episode, "file", name)
	_ = s.QueueRepo.LogSeriesActivity(sr.ID, "removed", fmt.Sprintf("%s S%02dE%02d: the file %s was unlinked by hand. The file is still on the disk. The episode is wanted again.", seriesLabel(sr), ep.Season, ep.Episode, name))
	writeJSON(w, http.StatusOK, unlinkedAnswer{File: name})
}
