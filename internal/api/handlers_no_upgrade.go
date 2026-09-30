package api

import (
	"net/http"
	"strconv"
)

type setNoUpgradeRequest struct {
	NoUpgrade bool `json:"noUpgrade"`
}

// handleSetMovieNoUpgrade and handleSetSeriesNoUpgrade switch the search for
// better versions off for one title (or back on). Titles that came in through
// an import start with it off, so an import never downloads anything by itself.
//
// Turning it off also removes the title's downloads that are only waiting (see
// clearWaitingDownloads), so it never shows up as waiting for a release.
func (s *Server) handleSetMovieNoUpgrade(w http.ResponseWriter, r *http.Request) {
	s.setNoUpgrade(w, r, "movie", s.MovieRepo.SetNoUpgrade, func(id int64) {
		s.clearWaitingDownloads(id, 0, true, betterVersionsOffNote)
	})
}

func (s *Server) handleSetSeriesNoUpgrade(w http.ResponseWriter, r *http.Request) {
	s.setNoUpgrade(w, r, "show", s.MovieRepo.SetSeriesNoUpgrade, func(id int64) {
		s.clearWaitingDownloads(0, id, true, betterVersionsOffNote)
	})
}

// betterVersionsOffNote is how a removed waiting download is recorded.
const betterVersionsOffNote = "Removed because better versions are switched off"

func (s *Server) setNoUpgrade(w http.ResponseWriter, r *http.Request, what string, set func(id int64, noUpgrade bool) error, whenOff func(id int64)) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid "+what+" ID.")
		return
	}
	var req setNoUpgradeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if err := set(id, req.NoUpgrade); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.NoUpgrade {
		whenOff(id)
	}
	writeJSON(w, http.StatusOK, nil)
}
