package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
)

type queueItemPayload struct {
	ID           int64   `json:"id"`
	MovieID      int64   `json:"movieId"`
	SeriesID     int64   `json:"seriesId,omitempty"`
	AlbumID      int64   `json:"albumId,omitempty"` // an album grab (music module)
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
	// conflict policy. See handleResolveQueueConflict.
	DestPath string `json:"destPath,omitempty"`
	// Interrupted is a paused download nobody paused: Mediarium was restarted
	// while it was downloading.
	Interrupted bool `json:"interrupted,omitempty"`
	// Pending is "pausing" or "stopping" while the download winds down.
	Pending string `json:"pending,omitempty"`
	// KeptFiles says the partly downloaded files are still in the downloads
	// folder (a paused, stopped or failed download).
	KeptFiles bool `json:"keptFiles,omitempty"`
	// QueuePosition is the place in the download line of a waiting download: 1
	// is next. Zero for anything else.
	QueuePosition int `json:"queuePosition,omitempty"`
}

// queueRank orders the statuses on the Activity page: what is being worked
// on first, then what needs a look, then everything else.
func queueRank(st queue.Status) int {
	switch st {
	case queue.StatusDownloading, queue.StatusImporting:
		return 0
	case queue.StatusQueued:
		return 1
	case queue.StatusPaused:
		return 2
	case queue.StatusFailed:
		return 3
	case queue.StatusConflict:
		return 4
	case queue.StatusStopped:
		return 5
	case queue.StatusCompleted:
		return 6
	}
	return 7
}

func queueTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// sortQueueItems puts the queue in the order the Activity page shows it, and
// keeps it there: downloads in progress (downloading and post-processing,
// then waiting in line in the order they will start, then paused) with the one
// that started first on top, then
// failed ones, then the rest, newest first. Nothing in the order depends on
// progress, so rows do not move while they download.
func sortQueueItems(items []queue.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ra, rb := queueRank(a.Status), queueRank(b.Status); ra != rb {
			return ra < rb
		}
		if a.Status == queue.StatusQueued {
			return queue.LineOrder(a, b) // waiting downloads: the order they will start in
		}
		if queueRank(a.Status) <= 2 {
			if ta, tb := queueTime(a.AddedAt), queueTime(b.AddedAt); !ta.Equal(tb) {
				return ta.Before(tb)
			}
			return a.ID < b.ID
		}
		ta, tb := queueTime(a.CompletedAt), queueTime(b.CompletedAt)
		if ta.IsZero() {
			ta = queueTime(a.AddedAt)
		}
		if tb.IsZero() {
			tb = queueTime(b.AddedAt)
		}
		if !ta.Equal(tb) {
			return ta.After(tb)
		}
		return a.ID > b.ID
	})
}

// handleListQueue lists everything that is downloading, importing or waiting,
// for the Activity page. Each entry
// carries the name and poster of the movie or show it is for, so the page
// reads as titles rather than release names.
func (s *Server) handleListQueue(w http.ResponseWriter, r *http.Request) {
	items, err := s.QueueRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sortQueueItems(items)
	positions := waitingPositions(items)
	out := make([]queueItemPayload, len(items))
	for i, it := range items {
		title, subtitle, poster := s.describeQueueItem(it)
		p := queueItemPayload{
			ID: it.ID, MovieID: it.MovieID, SeriesID: it.SeriesID, AlbumID: it.AlbumID, Title: title, Subtitle: subtitle, PosterURL: poster,
			ReleaseTitle: it.ReleaseTitle, Protocol: string(it.Protocol), SizeBytes: it.SizeBytes, Status: string(it.Status),
			ProgressPct: it.ProgressPct, Error: plainerror.Text(it.Error), AddedAt: it.AddedAt, CompletedAt: it.CompletedAt, DestPath: it.DestPath,
			Interrupted: it.Interrupted, Pending: s.pipelines.pending(it.ID), QueuePosition: positions[it.ID],
		}
		if it.Status == queue.StatusPaused || it.Status == queue.StatusStopped || it.Status == queue.StatusFailed {
			p.KeptFiles = fileExists(s.workDirFor(it.ID))
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

// handleDeleteQueueItem removes a finished, failed, stopped, paused or parked
// entry from the list. A paused download is stopped first. Anything still
// downloading or importing is refused: stop or pause it first. With
// ?deleteFiles=1 the download's partly downloaded files are deleted from the
// downloads folder too (never anything in the library).
func (s *Server) handleDeleteQueueItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid download ID.")
		return
	}
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That download is no longer in the queue.")
		return
	}
	if isActiveStatus(item.Status) {
		if item.Status == queue.StatusQueued {
			writeError(w, http.StatusConflict, "This download is waiting in line. Stop it first.")
			return
		}
		writeError(w, http.StatusConflict, "This download is still running. Pause or stop it first.")
		return
	}
	deleteFiles := r.URL.Query().Get("deleteFiles") == "1"
	if item.Status == queue.StatusPaused {
		queueControlMu.Lock()
		s.releaseAfterPause(item)
		queueControlMu.Unlock()
	}
	if err := s.QueueRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if deleteFiles {
		s.removeWorkDir(s.workDirFor(id))
	}
	s.queueItemEvent(item, "removed", queue.LevelInfo, "Removed from the download list: "+item.ReleaseTitle)
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

// handleRetryQueueItem grabs the same release again after it failed or was
// stopped. A Usenet download whose files were all fetched and are still in its
// working folder is not downloaded again: the retry repeats post-processing
// (PAR2 verify/repair, unpack, import) on those files, and only downloads anew
// when they fail the PAR2 check. A partial download carries on from the
// articles it already has.
func (s *Server) handleRetryQueueItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid download ID.")
		return
	}
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That download is no longer in the queue.")
		return
	}
	if item.Status != queue.StatusFailed && item.Status != queue.StatusStopped {
		writeError(w, http.StatusConflict, "Only a failed or stopped download can be retried.")
		return
	}
	keptDir, reuse := s.keptDownload(item)
	if reuse {
		s.offerKeptDownload(item.NZBURL, keptDir)
	}
	newID, err := s.regrab(item)
	if err != nil {
		if reuse {
			s.withdrawKeptDownload(item.NZBURL)
		}
		writeGrabError(w, err, http.StatusBadRequest)
		return
	}
	message := "Retrying, downloading again: " + item.ReleaseTitle
	if reuse {
		message = "Retrying from the files already downloaded: " + item.ReleaseTitle
	}
	s.queueItemEvent(item, "retried", queue.LevelInfo, message)
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
			return 0, fmt.Errorf("That show isn't in your library any more.")
		}
		return s.grabTVRelease(series, item.Season, item.Episode, item.ReleaseTitle, item.NZBURL, item.SizeBytes, protocol)
	case item.MovieID > 0:
		movie, err := s.MovieRepo.Get(item.MovieID)
		if err != nil {
			return 0, fmt.Errorf("That movie isn't in your library any more.")
		}
		return s.grabRelease(movie, item.ReleaseTitle, item.NZBURL, item.SizeBytes, protocol)
	case item.AlbumID > 0:
		album, err := s.MusicRepo.GetAlbum(item.AlbumID)
		if err != nil {
			return 0, fmt.Errorf("That album isn't in your library any more.")
		}
		artist, err := s.MusicRepo.GetArtist(album.ArtistID)
		if err != nil {
			return 0, fmt.Errorf("That album isn't in your library any more.")
		}
		return s.grabAlbum(artist, album, item.ReleaseTitle, item.NZBURL, item.SizeBytes, protocol, grabPicked)
	}
	return 0, fmt.Errorf("This download isn't linked to a movie, show or album.")
}

// queueItemEvent is itemEvent for the movie, show or album a queue item
// belongs to.
func (s *Server) queueItemEvent(item queue.Item, kind string, level queue.Level, message string) {
	if item.AlbumID > 0 {
		s.albumEvent(item.AlbumID, kind, level, message)
		return
	}
	s.itemEvent(item.MovieID, item.SeriesID, kind, level, message)
}

// handleBlocklistQueueItem marks a release as bad and tries another: it
// blocklists the failed release, removes the entry and starts a fresh search
// for the same movie or show.
func (s *Server) handleBlocklistQueueItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid download ID.")
		return
	}
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That download is no longer in the queue.")
		return
	}
	if isActiveStatus(item.Status) {
		writeError(w, http.StatusConflict, "This download is still running.")
		return
	}
	if err := s.Blocklist.Add(blocklist.Entry{
		ReleaseTitle: item.ReleaseTitle, Protocol: string(item.Protocol), Reason: "Blocklisted by you", MovieID: item.MovieID, SeriesID: item.SeriesID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogItemEvent(queue.ItemEvent{MovieID: item.MovieID, SeriesID: item.SeriesID, AlbumID: item.AlbumID,
		Kind: "blocklisted", Level: queue.LevelWarn, Message: "Blocklisted \"" + item.ReleaseTitle + "\": by you"})
	_ = s.QueueRepo.Delete(id)
	switch {
	case item.MovieID > 0:
		go s.searchMovieInBackground(item.MovieID)
	case item.SeriesID > 0:
		go s.searchSeriesInBackground(item.SeriesID)
	case item.AlbumID > 0:
		s.background(func() { s.retryAlbum(item.AlbumID) })
	}
	writeJSON(w, http.StatusOK, nil)
}

type resolveConflictRequest struct {
	Overwrite bool `json:"overwrite"`
}

// handleResolveQueueConflict settles a download that is waiting because a file
// already exists (the "ask me" setting): a person decides, per item, whether
// the new file replaces the one already there or is left alone. See
// Server.resolveConflict for the file handling.
func (s *Server) handleResolveQueueConflict(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid download ID.")
		return
	}
	var req resolveConflictRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
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
	// Link is the page of the title the line is about, when there is one.
	Link string `json:"link,omitempty"`
}

// handleListActivity lists the newest activity lines: 100 unless ?limit=
// asks for more (up to 2000), narrowed by ?q= to lines containing that text.
func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, 2000)
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 100 {
		q = q[:100]
	}
	entries, err := s.QueueRepo.ListActivityFor(limit, q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	admin := isAdminRequest(r)
	out := make([]activityEntryPayload, 0, len(entries))
	for _, e := range entries {
		// Updates, restarts and shut-downs say who did it and from which
		// address: for administrators only.
		if e.EventType == "update" && !admin {
			continue
		}
		p := activityEntryPayload{ID: e.ID, EventType: e.EventType, Message: plainFailure(e.EventType, e.Message), CreatedAt: e.CreatedAt}
		switch {
		case e.MovieTMDBID > 0:
			p.Link = fmt.Sprintf("/title/%d", e.MovieTMDBID)
		case e.SeriesID > 0:
			p.Link = fmt.Sprintf("/series/%d", e.SeriesID)
		case e.ArtistID > 0:
			p.Link = fmt.Sprintf("/music/artist/%d", e.ArtistID)
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}
