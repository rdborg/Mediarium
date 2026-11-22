package api

import (
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/library"
)

// DELETE /episodefile/{id}, /episodefile/bulk and the Radarr twins
// (/moviefile/{id}, /moviefile/bulk): the way cleanup apps such as
// Maintainerr remove a watched season or film. They do what
// DELETE /api/episodes/{id}/file and /api/movies/{id}/file do: the file goes
// to the recycle bin when it is on, and the episode or movie is wanted again.
// The file ids are the episode and movie ids, the same ones GET /episodefile
// and GET /moviefile hand out.

// arrDeleteEpisodeFile answers DELETE /episodefile/{id}.
func (s *Server) arrDeleteEpisodeFile(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	if !s.arrDeleteEpisodeFileIDs(w, []int64{id}) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// arrDeleteEpisodeFilesBulk answers DELETE /episodefile/bulk.
func (s *Server) arrDeleteEpisodeFilesBulk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EpisodeFileIDs []int64 `json:"episodeFileIds"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.EpisodeFileIDs) == 0 {
		arrBadRequest(w, "EpisodeFileIds", "Give the ids of the files to delete in episodeFileIds.")
		return
	}
	if !s.arrDeleteEpisodeFileIDs(w, body.EpisodeFileIDs) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// arrDeleteEpisodeFileIDs deletes the files of those episodes, show by show.
// It answers the request itself and reports false when it did.
func (s *Server) arrDeleteEpisodeFileIDs(w http.ResponseWriter, ids []int64) bool {
	bySeries := map[int64][]library.Episode{}
	var order []int64
	for _, id := range ids {
		ep, err := s.MovieRepo.GetEpisodeByID(id)
		if err != nil || ep.Status != library.StatusDownloaded || ep.FilePath == "" {
			arrNotFound(w)
			return false
		}
		if _, seen := bySeries[ep.SeriesID]; !seen {
			order = append(order, ep.SeriesID)
		}
		bySeries[ep.SeriesID] = append(bySeries[ep.SeriesID], ep)
	}
	for _, sid := range order {
		series, err := s.MovieRepo.GetSeries(sid)
		if err != nil {
			arrNotFound(w)
			return false
		}
		if _, err := s.deleteEpisodeFiles(series, bySeries[sid]); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"message": err.Error()})
			return false
		}
	}
	return true
}

// arrDeleteMovieFile answers DELETE /moviefile/{id}.
func (s *Server) arrDeleteMovieFile(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	if !s.arrDeleteMovieFileIDs(w, []int64{id}) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// arrDeleteMovieFilesBulk answers DELETE /moviefile/bulk.
func (s *Server) arrDeleteMovieFilesBulk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MovieFileIDs []int64 `json:"movieFileIds"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.MovieFileIDs) == 0 {
		arrBadRequest(w, "MovieFileIds", "Give the ids of the files to delete in movieFileIds.")
		return
	}
	if !s.arrDeleteMovieFileIDs(w, body.MovieFileIDs) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) arrDeleteMovieFileIDs(w http.ResponseWriter, ids []int64) bool {
	for _, id := range ids {
		_, code, msg, err := s.deleteMovieFile(id)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"message": err.Error()})
			return false
		}
		if msg == "" {
			continue
		}
		// No such movie, or no file on it: the file isn't there either way.
		if code == http.StatusNotFound || strings.Contains(msg, "no file") {
			arrNotFound(w)
			return false
		}
		writeJSON(w, code, map[string]string{"message": msg})
		return false
	}
	return true
}
