package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/rdborg/mediarium/internal/library"
)

// PUT /api/series/{id}/episode-order {"order": "tmdb" | "tvdb"}
//
// Which list the show's regular seasons follow (#184). "tvdb" needs a saved
// TheTVDB key; the show's episodes are then put in TheTVDB's aired order
// right away (see library.PlanEpisodeOrder: an episode with a file is never
// removed, and a season where one can't be placed is left as it was). The
// answer lists those seasons in "leftAsIs". Specials and anime are not
// affected: anime has its own numbering (see handleSetSeriesType).
func (s *Server) handleSetEpisodeOrder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	var req struct {
		Order string `json:"order"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Order != library.EpisodeOrderTMDB && req.Order != library.EpisodeOrderTVDB) {
		writeError(w, http.StatusBadRequest, `Choose "tmdb" or "tvdb".`)
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sr.CopyOf != 0 {
		writeError(w, http.StatusBadRequest, "Change the episode order on the main show. Its second copies follow it.")
		return
	}
	if sr.SeriesType == library.SeriesAnime {
		writeError(w, http.StatusBadRequest, "An anime show has its own episode numbering. Change its Episode numbering instead.")
		return
	}
	if sr.EpisodeOrder == req.Order {
		writeJSON(w, http.StatusOK, map[string]any{"order": req.Order, "leftAsIs": []int{}})
		return
	}
	if req.Order == library.EpisodeOrderTVDB && s.tvdbKeyClient() == nil {
		writeError(w, http.StatusBadRequest, "Save a TheTVDB key in Settings first. TheTVDB's order needs it.")
		return
	}
	if err := s.MovieRepo.SetEpisodeOrder(id, req.Order); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sr.EpisodeOrder = req.Order
	blocked, synced, err := s.refreshSeriesOrder(r.Context(), sr, true)
	if err != nil && !synced {
		// Nothing was changed in the episodes: put the old choice back.
		if rerr := s.MovieRepo.SetEpisodeOrder(id, library.ValidEpisodeOrder(opposite(req.Order))); rerr != nil {
			writeError(w, http.StatusInternalServerError, rerr.Error())
			return
		}
		if errors.Is(err, errNoTVDBOrder) {
			writeError(w, http.StatusBadGateway, "TheTVDB's episode list for this show couldn't be read. Check the key in Settings and that TheTVDB knows the show, then try again.")
			return
		}
		writeUpstreamError(w, "change the episode order", err)
		return
	}
	if err != nil {
		writeUpstreamError(w, "finish changing the episode order", err)
		return
	}
	if blocked == nil {
		blocked = []int{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": req.Order, "leftAsIs": blocked})
}

func opposite(order string) string {
	if order == library.EpisodeOrderTVDB {
		return library.EpisodeOrderTMDB
	}
	return library.EpisodeOrderTVDB
}
