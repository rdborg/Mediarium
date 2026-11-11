package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/library"
)

// Deleting one file of a title (a movie's file, an episode's, a season's)
// without removing the title: the files go to the recycle bin like any
// removal, and the movie or episodes are wanted again, so a new release can
// take their place. Nothing outside the library folders is ever touched.

type deletedFilesAnswer struct {
	Files    int  `json:"files"`    // files moved or deleted (with their subtitles and the like)
	Episodes int  `json:"episodes"` // movies or episodes that are now wanted again
	Trashed  bool `json:"trashed"`  // in the recycle bin rather than deleted for good
}

// handleDeleteMovieFile deletes a movie's file and makes the movie wanted again.
func (s *Server) handleDeleteMovieFile(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if m.Status != library.StatusDownloaded || m.FilePath == "" {
		writeError(w, http.StatusConflict, "This movie has no file to delete.")
		return
	}
	n, trashed, err := s.discardFileWithSidecars(s.fileRoot(s.movieRootFor(m), m.FilePath), m.FilePath, movieLabel(m))
	if err != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("Couldn't delete the file: %s", err.Error()))
		return
	}
	if err := s.MovieRepo.ClearMovieFile(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(id, "removed", fmt.Sprintf("%s: its file %s was deleted%s. The movie is wanted again.", movieLabel(m), filepath.Base(m.FilePath), binNote(trashed)))
	writeJSON(w, http.StatusOK, deletedFilesAnswer{Files: n, Episodes: 1, Trashed: trashed})
}

// handleDeleteEpisodeFile deletes one episode's file.
func (s *Server) handleDeleteEpisodeFile(w http.ResponseWriter, r *http.Request) {
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
	s.deleteEpisodeFilesAnswer(w, ep.SeriesID, []library.Episode{ep})
}

// handleDeleteSeasonFiles deletes the files of every downloaded episode of a season.
func (s *Server) handleDeleteSeasonFiles(w http.ResponseWriter, r *http.Request) {
	id, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	season, err2 := strconv.Atoi(r.PathValue("season"))
	if err1 != nil || err2 != nil || season < 0 {
		writeError(w, http.StatusBadRequest, "That isn't a valid show or season.")
		return
	}
	all, err := s.MovieRepo.ListEpisodes(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var eps []library.Episode
	for _, e := range all {
		if e.Season == season {
			eps = append(eps, e)
		}
	}
	s.deleteEpisodeFilesAnswer(w, id, eps)
}

func (s *Server) deleteEpisodeFilesAnswer(w http.ResponseWriter, seriesID int64, eps []library.Episode) {
	series, err := s.MovieRepo.GetSeries(seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	answer, err := s.deleteEpisodeFiles(series, eps)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if answer.Files == 0 && answer.Episodes == 0 {
		writeError(w, http.StatusConflict, "There is no downloaded file here to delete.")
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// deleteEpisodeFiles removes the files of the downloaded episodes in eps. A
// file that holds several episodes goes once, and all of its episodes become
// wanted again, whether or not they were asked for. A file that can't be
// removed stops the work there and the episodes already done stay done.
func (s *Server) deleteEpisodeFiles(series library.Series, eps []library.Episode) (deletedFilesAnswer, error) {
	var answer deletedFilesAnswer
	var files []string
	seen := map[string]bool{}
	for _, e := range eps {
		if e.Status != library.StatusDownloaded || e.FilePath == "" || seen[e.FilePath] {
			continue
		}
		seen[e.FilePath] = true
		files = append(files, e.FilePath)
	}
	if len(files) == 0 {
		return answer, nil
	}
	users, err := s.MovieRepo.EpisodesByFiles(files)
	if err != nil {
		return answer, err
	}
	root := s.seriesRootFor(series)
	label := seriesLabel(series)
	for _, f := range files {
		n, trashed, err := s.discardFileWithSidecars(s.fileRoot(root, f), f, label)
		if err != nil {
			return answer, fmt.Errorf("couldn't delete %s: %w", filepath.Base(f), err)
		}
		answer.Files += n
		answer.Trashed = answer.Trashed || trashed
		for _, e := range users {
			if e.FilePath != f {
				continue
			}
			if err := s.MovieRepo.ClearEpisodeFile(e.ID); err != nil {
				return answer, err
			}
			answer.Episodes++
		}
		_ = s.QueueRepo.LogSeriesActivity(series.ID, "removed", fmt.Sprintf("%s: the file %s was deleted%s. Its episodes are wanted again.", label, filepath.Base(f), binNote(trashed)))
		// What is left of a season or show folder that holds no video any more
		// (its .nfo and artwork files) goes too, so no empty folder stays behind.
		cleanup.ClearLeftovers(s.fileRoot(root, f), filepath.Dir(f))
	}
	return answer, nil
}

func binNote(trashed bool) string {
	if trashed {
		return " (it is in the recycle bin)"
	}
	return ""
}
