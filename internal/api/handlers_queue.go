package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/ryanborg/mediarium/internal/blocklist"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/queue"
)

type queueItemPayload struct {
	ID           int64   `json:"id"`
	MovieID      int64   `json:"movieId"`
	SeriesID     int64   `json:"seriesId,omitempty"`
	TMDBID       int     `json:"tmdbId,omitempty"`
	Title        string  `json:"title"`
	Subtitle     string  `json:"subtitle,omitempty"`
	PosterURL    string  `json:"posterUrl,omitempty"`
	ReleaseTitle string  `json:"releaseTitle"`
	Protocol     string  `json:"protocol"`
	SizeBytes    int64   `json:"sizeBytes"`
	Status       string  `json:"status"`
	ProgressPct  float64 `json:"progressPct"`
	Error        string  `json:"error,omitempty"`
	AddedAt      string  `json:"addedAt,omitempty"`
	CompletedAt  string  `json:"completedAt,omitempty"`
	// DestPath is only populated for a StatusConflict item — the path a
	// naming collision was found at, under the "always ask" import
	// conflict policy (PRD §4.8). See handleResolveQueueConflict.
	DestPath string `json:"destPath,omitempty"`
}

// handleListQueue is the Activity/Queue view (PRD §6 — "one live view of
// everything currently downloading/importing/post-processing"). Each entry
// carries the name and poster of the movie or show it is for, so the page
// reads as titles rather than release names.
func (s *Server) handleListQueue(w http.ResponseWriter, r *http.Request) {
	items, err := s.QueueRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]queueItemPayload, len(items))
	for i, it := range items {
		title, subtitle, poster := s.describeQueueItem(it)
		p := queueItemPayload{
			ID: it.ID, MovieID: it.MovieID, SeriesID: it.SeriesID, Title: title, Subtitle: subtitle, PosterURL: poster,
			ReleaseTitle: it.ReleaseTitle, Protocol: string(it.Protocol), SizeBytes: it.SizeBytes, Status: string(it.Status),
			ProgressPct: it.ProgressPct, Error: it.Error, AddedAt: it.AddedAt, CompletedAt: it.CompletedAt, DestPath: it.DestPath,
		}
		if it.MovieID > 0 {
			if m, err := s.MovieRepo.Get(it.MovieID); err == nil {
				p.TMDBID = m.TMDBID
			}
		}
		out[i] = p
	}
	writeJSON(w, http.StatusOK, out)
}

func isActiveStatus(status queue.Status) bool {
	return status == queue.StatusQueued || status == queue.StatusDownloading || status == queue.StatusImporting
}

// handleDeleteQueueItem removes a finished, failed or parked entry from the
// list. Anything still downloading or importing is refused: its pipeline is
// running and cannot be cancelled yet.
func (s *Server) handleDeleteQueueItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid queue item id")
		return
	}
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "queue item not found")
		return
	}
	if isActiveStatus(item.Status) {
		writeError(w, http.StatusConflict, "this download is still running; wait for it to finish or fail")
		return
	}
	if err := s.QueueRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// handleClearFinishedQueue empties the list of completed and failed entries.
func (s *Server) handleClearFinishedQueue(w http.ResponseWriter, r *http.Request) {
	n, err := s.QueueRepo.ClearFinished()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"removed": n})
}

// handleRetryQueueItem grabs the same release again after it failed.
func (s *Server) handleRetryQueueItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid queue item id")
		return
	}
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "queue item not found")
		return
	}
	if item.Status != queue.StatusFailed {
		writeError(w, http.StatusConflict, "only a failed download can be retried")
		return
	}
	newID, err := s.regrab(item)
	if err != nil {
		writeGrabError(w, err, http.StatusBadRequest)
		return
	}
	_ = s.QueueRepo.Delete(id) // the retry replaces the failed entry
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": newID})
}

// regrab queues the same release again for the same movie or episode(s).
func (s *Server) regrab(item queue.Item) (int64, error) {
	protocol := indexers.Protocol(item.Protocol)
	switch {
	case item.SeriesID > 0:
		series, err := s.MovieRepo.GetSeries(item.SeriesID)
		if err != nil {
			return 0, fmt.Errorf("that show is no longer in your library")
		}
		return s.grabTVRelease(series, item.Season, item.Episode, item.ReleaseTitle, item.NZBURL, item.SizeBytes, protocol)
	case item.MovieID > 0:
		movie, err := s.MovieRepo.Get(item.MovieID)
		if err != nil {
			return 0, fmt.Errorf("that movie is no longer in your library")
		}
		return s.grabRelease(movie, item.ReleaseTitle, item.NZBURL, item.SizeBytes, protocol)
	}
	return 0, fmt.Errorf("this download is not tied to a movie or show")
}

// handleBlocklistQueueItem is "this release is bad, try another": it
// blocklists the failed release, removes the entry and starts a fresh search
// for the same movie or show.
func (s *Server) handleBlocklistQueueItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid queue item id")
		return
	}
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "queue item not found")
		return
	}
	if isActiveStatus(item.Status) {
		writeError(w, http.StatusConflict, "this download is still running")
		return
	}
	if err := s.Blocklist.Add(blocklist.Entry{
		ReleaseTitle: item.ReleaseTitle, Protocol: string(item.Protocol), Reason: "Blocklisted by you", MovieID: item.MovieID, SeriesID: item.SeriesID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(item.MovieID, "blocklisted", "Blocklisted \""+item.ReleaseTitle+"\": by you")
	_ = s.QueueRepo.Delete(id)
	switch {
	case item.MovieID > 0:
		go s.searchMovieInBackground(item.MovieID)
	case item.SeriesID > 0:
		go s.searchSeriesInBackground(item.SeriesID)
	}
	writeJSON(w, http.StatusOK, nil)
}

type resolveConflictRequest struct {
	Overwrite bool `json:"overwrite"`
}

// handleResolveQueueConflict is the manual-import review action PRD §4.8's
// "always ask" conflict policy defers to: a person decides, per item,
// whether the newly-downloaded file should replace the one already at the
// destination, or be left alone (skip). See Server.resolveConflict for the
// actual file-move logic.
func (s *Server) handleResolveQueueConflict(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid queue item id")
		return
	}
	var req resolveConflictRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.resolveConflict(id, req.Overwrite); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

type activityEntryPayload struct {
	ID        int64  `json:"id"`
	EventType string `json:"eventType"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	entries, err := s.QueueRepo.ListActivity(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]activityEntryPayload, len(entries))
	for i, e := range entries {
		out[i] = activityEntryPayload{ID: e.ID, EventType: e.EventType, Message: e.Message, CreatedAt: e.CreatedAt}
	}
	writeJSON(w, http.StatusOK, out)
}
