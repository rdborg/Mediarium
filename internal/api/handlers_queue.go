package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
)

type queueItemPayload struct {
	ID           int64   `json:"id"`
	MovieID      int64   `json:"movieId"`
	SeriesID     int64   `json:"seriesId,omitempty"`
	AlbumID      int64   `json:"albumId,omitempty"` // an album grab (music module)
	BookID       int64   `json:"bookId,omitempty"`  // a book grab (ebooks and audiobooks)
	BookFormat   string  `json:"bookFormat,omitempty"`
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
	// KeptFile and KeptData are set on a download that finished but was not
	// imported because the file the title already had was kept: that file, and
	// what became of the downloaded data ("deleted", "seeding" or "client").
	KeptFile string `json:"keptFile,omitempty"`
	KeptData string `json:"keptData,omitempty"`
	// Interrupted is a paused download nobody paused: Mediarium was restarted
	// while it was downloading.
	Interrupted bool `json:"interrupted,omitempty"`
	// Pending is "pausing" or "stopping" while the download winds down.
	Pending string `json:"pending,omitempty"`
	// Peers and Seeds are how many the built-in torrent client is connected to
	// (only for a torrent it is running right now).
	Peers *int `json:"peers,omitempty"`
	Seeds *int `json:"seeds,omitempty"`
	// Category is the torrent category the built-in client filed the download
	// under (empty for none).
	Category string `json:"category,omitempty"`
	// KeptFiles says the partly downloaded files are still in the downloads
	// folder (a paused, stopped or failed download).
	KeptFiles bool `json:"keptFiles,omitempty"`
	// IndexerName is the indexer the release came from, when it is still set up.
	IndexerName string `json:"indexerName,omitempty"`
	// QueuePosition is the place in the download line of a waiting download: 1
	// is next. Zero for anything else.
	QueuePosition int `json:"queuePosition,omitempty"`
	// What the release name says it is (movies and shows): the picture size,
	// where it came from, the video codec and the audio languages.
	Resolution string `json:"resolution,omitempty"`
	Source     string `json:"source,omitempty"`
	Codec      string `json:"codec,omitempty"`
	Language   string `json:"language,omitempty"`
	// Note is a sentence for the row that its status alone does not say: why a
	// waiting download does not start (the disk is nearly full), or that a
	// failed release is held back from searches for now.
	Note string `json:"note,omitempty"`
	// Waiting says an album's files are downloaded and its import is waiting
	// for MusicBrainz; Note says so and the page offers Retry now.
	Waiting bool `json:"waiting,omitempty"`
	// Unpack is how far the unpacking of an archive has come, while a download
	// is being unpacked: the page shows it instead of a bar stuck at 100%.
	Unpack *queueUnpack `json:"unpack,omitempty"`
}

// queueUnpack is the progress of unpacking one download. Index is the archive
// being unpacked out of Count; DoneBytes and TotalBytes are those of that
// archive (TotalBytes is 0 when it is not known).
type queueUnpack struct {
	DoneBytes  int64 `json:"doneBytes"`
	TotalBytes int64 `json:"totalBytes"`
	Index      int   `json:"index"`
	Count      int   `json:"count"`
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
	// ?active=1 asks only for what is waiting, downloading or importing: the
	// menu's count and the dashboard need no more, and it is far smaller than
	// a list that also holds every finished download.
	list := s.QueueRepo.List
	if r.URL.Query().Get("active") == "1" {
		list = s.QueueRepo.ListActive
	}
	items, err := list()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sortQueueItems(items)
	positions := waitingPositions(items)
	out := make([]queueItemPayload, len(items))
	indexerNames := s.indexerNames()
	categories, _ := s.TorrentOpts.Categories()
	namer := s.newQueueNamer(items)
	preferred := s.preferredLanguage()
	// Asked once for the whole list, and only when a row needs it.
	var diskNote string
	var held map[string]bool
	for _, it := range items {
		if it.Status == queue.StatusQueued && diskNote == "" {
			if _, message, short := s.diskSpaceShort(); short {
				diskNote = message
			} else {
				diskNote = "-"
			}
		}
		if it.Status == queue.StatusFailed && held == nil {
			held = s.activeNZBLinkHolds(time.Now())
		}
	}
	for i, it := range items {
		title, subtitle, poster := namer.describe(it)
		p := queueItemPayload{
			ID: it.ID, MovieID: it.MovieID, SeriesID: it.SeriesID, AlbumID: it.AlbumID, BookID: it.BookID, BookFormat: it.BookFormat, Title: title, Subtitle: subtitle, PosterURL: poster,
			ReleaseTitle: it.ReleaseTitle, Protocol: string(it.Protocol), SizeBytes: it.SizeBytes, Status: string(it.Status),
			ProgressPct: it.ProgressPct, Error: memberText(r, plainerror.Text(it.Error)), AddedAt: it.AddedAt, CompletedAt: it.CompletedAt, DestPath: fileLabel(r, it.DestPath), KeptFile: fileLabel(r, it.KeptFile), KeptData: it.KeptData,
			Interrupted: it.Interrupted, Pending: s.pipelines.pending(it.ID), QueuePosition: positions[it.ID],
		}
		if it.IndexerID.Valid {
			p.IndexerName = indexerNames[it.IndexerID.Int64]
		}
		if it.Protocol == queue.ProtocolTorrent {
			p.Category = categories[it.ID]
		}
		if it.Status == queue.StatusImporting {
			if st, ok := s.unpacking.get(it.ID); ok {
				p.Unpack = &queueUnpack{DoneBytes: st.Done, TotalBytes: st.Total, Index: st.Index, Count: st.Count}
				if st.Total > 0 {
					p.ProgressPct = st.Percent() // the stored 100% only says the download is in
				}
			}
		}
		if it.Protocol == queue.ProtocolTorrent && it.Status == queue.StatusDownloading {
			if peers, seeds, ok := s.torrents.swarm(it.ID); ok {
				p.Peers, p.Seeds = &peers, &seeds
			}
		}
		if it.Status == queue.StatusPaused || it.Status == queue.StatusStopped || it.Status == queue.StatusFailed {
			p.KeptFiles = fileExists(s.workDirFor(it.ID))
		}
		if it.MovieID > 0 {
			if m, ok := namer.movie(it.MovieID); ok {
				p.TMDBID = m.TMDBID
			}
		}
		if it.AlbumID == 0 && it.BookID == 0 {
			rel := parser.Parse(it.ReleaseTitle)
			p.Resolution, p.Source, p.Codec = rel.Resolution, rel.Source, rel.Codec
			p.Language = quality.FitLanguage(rel, preferred).Label
		}
		if s.musicWaitingNow(it) {
			p.Waiting, p.Note = true, musicWaitNote
		}
		switch {
		case p.Waiting:
		case it.Status == queue.StatusQueued && diskNote != "-":
			p.Note = diskNote
		case it.Status == queue.StatusFailed && held[nzbLinkHoldKey(it.NZBURL, it.ReleaseTitle)]:
			p.Note = nzbLinkHeldNote
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
	if err := s.removeQueueEntry(item, r.URL.Query().Get("deleteFiles") == "1", false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// removeQueueEntry takes a finished, failed, stopped or paused download off
// the list (a paused one is let go first), and with deleteFiles also deletes
// its partly downloaded files from the downloads folder. controlLocked says
// the caller already holds queueControlMu.
func (s *Server) removeQueueEntry(item queue.Item, deleteFiles, controlLocked bool) error {
	if item.Status == queue.StatusPaused || item.Status == queue.StatusConflict {
		// A paused download, or one parked on a file that already exists, still
		// holds its title as "downloading": hand it back.
		if !controlLocked {
			queueControlMu.Lock()
			defer queueControlMu.Unlock()
		}
		s.releaseAfterPause(item)
	}
	// A torrent that is still seeding for this entry stops with it, before its
	// files can be deleted underneath it.
	s.torrents.stopQueue(item.ID)
	work := s.workDirFor(item.ID) // before the row goes: a torrent may be filed in a category folder
	ref, inClient := s.clientRefFor(item)
	if err := s.QueueRepo.Delete(item.ID); err != nil {
		return err
	}
	s.removeFromClient(ref, inClient, false)
	if deleteFiles {
		s.removeWorkDir(work)
	}
	s.queueItemEvent(item, "removed", queue.LevelInfo, "Removed from the download list: "+item.ReleaseTitle)
	return nil
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
	newID, err := s.retryQueueItem(item)
	if errors.Is(err, errNotRetryable) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeGrabError(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": newID})
}

// errNotRetryable answers a retry of a download that is not failed or stopped.
var errNotRetryable = errors.New("Only a failed or stopped download can be retried.")

// retryQueueItem is Retry on one download (see handleRetryQueueItem), shared
// with the bulk action. It answers the new queue item.
func (s *Server) retryQueueItem(item queue.Item) (int64, error) {
	// An album that is only waiting for MusicBrainz keeps its files: Retry
	// repeats the import from them, it does not download again.
	if s.retryMusicImport(item) {
		return item.ID, nil
	}
	if item.Status != queue.StatusFailed && item.Status != queue.StatusStopped {
		return 0, errNotRetryable
	}
	keptDir, reuse := s.keptDownload(item)
	// The new item must not start before the earlier files are in its folder.
	release := s.dispatch.Hold()
	newID, err := s.regrab(item)
	if err != nil {
		release()
		return 0, err
	}
	if reuse {
		// Moved now, not when the new item starts: while it waits in line the
		// files belong to it, so a restart or the daily clean-up can't lose them.
		reuse = s.moveKeptDownload(keptDir, newID)
	}
	release()
	message := "Retrying, downloading again: " + item.ReleaseTitle
	if reuse {
		message = "Retrying from the files already downloaded: " + item.ReleaseTitle
	}
	s.queueItemEvent(item, "retried", queue.LevelInfo, message)
	_ = s.QueueRepo.Delete(item.ID) // the retry replaces the failed entry
	return newID, nil
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
		return s.grabTVOpts(series, item.Season, item.Episode, item.ReleaseTitle, item.NZBURL, item.SizeBytes, protocol, grabPicked, item.ReplaceByHand)
	case item.MovieID > 0:
		movie, err := s.MovieRepo.Get(item.MovieID)
		if err != nil {
			return 0, fmt.Errorf("That movie isn't in your library any more.")
		}
		return s.grabMovieOpts(movie, item.ReleaseTitle, item.NZBURL, item.SizeBytes, protocol, grabPicked, grabOpts{byHand: item.ReplaceByHand})
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
	case item.BookID > 0:
		book, err := s.BookRepo.Get(item.BookID)
		if err != nil {
			return 0, fmt.Errorf("That book isn't in your library any more.")
		}
		return s.grabBook(book, books.Format(item.BookFormat), item.ReleaseTitle, item.NZBURL, item.SizeBytes, protocol, queue.PriorityManual)
	}
	return 0, fmt.Errorf("This download isn't linked to a movie, show, album or book.")
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
	if err := s.blocklistQueueItem(item); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errStillRunning) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

var errStillRunning = errors.New("This download is still running.")

// blocklistQueueItem blocklists the release of a download that is not running,
// removes the download and looks for another release for its title. One
// download from the Activity buttons, or each of several picked together.
func (s *Server) blocklistQueueItem(item queue.Item) error {
	if isActiveStatus(item.Status) {
		return errStillRunning
	}
	reason, err := s.personBlocklistReason(item)
	if err != nil {
		return err
	}
	if err := s.Blocklist.Add(blocklist.Entry{
		ReleaseTitle: item.ReleaseTitle, Protocol: string(item.Protocol), Reason: reason, MovieID: item.MovieID, SeriesID: item.SeriesID,
	}); err != nil {
		return err
	}
	_ = s.QueueRepo.LogItemEvent(queue.ItemEvent{MovieID: item.MovieID, SeriesID: item.SeriesID, AlbumID: item.AlbumID,
		Kind: "blocklisted", Level: queue.LevelWarn, Message: "Blocklisted \"" + item.ReleaseTitle + "\": by you"})
	s.dropBlocklistedItem(item)
	switch {
	case item.MovieID > 0:
		go s.searchMovieInBackground(item.MovieID)
	case item.SeriesID > 0:
		go s.searchSeriesInBackground(item.SeriesID)
	case item.AlbumID > 0:
		s.background(func() { s.retryAlbum(item.AlbumID) })
	case item.BookID > 0:
		s.background(func() { s.searchBook(item.BookID, books.Format(item.BookFormat), true) })
	}
	return nil
}

// personBlocklistReason is the blocklist reason for a release a person
// blocklists from Activity. It says it was them, and keeps why the release
// was bad: the reason it was already blocklisted for (a failed download is
// often blocklisted by itself first), or else the failure on its row.
func (s *Server) personBlocklistReason(item queue.Item) (string, error) {
	const byYou = "Blocklisted by you"
	why, listed, err := s.Blocklist.Reason(item.ReleaseTitle)
	if err != nil {
		return "", err
	}
	if listed && strings.HasPrefix(why, byYou) {
		return why, nil // blocklisted by a person before: nothing new to say
	}
	if !listed || why == "" {
		why = item.Error
	}
	if why == "" {
		return byYou, nil
	}
	return fmt.Sprintf("Blocklisted by you: %s", why), nil
}

// dropBlocklistedItem takes a download whose release a person blocklisted off
// the list. A paused or parked one still holds its title as "downloading":
// that is handed back first, or the new search would find the title busy. Its
// files are not wanted any more, so its working folder goes too.
func (s *Server) dropBlocklistedItem(item queue.Item) {
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	item = s.freshItem(item)
	if isActiveStatus(item.Status) {
		return // resumed in the meantime: never delete a running download's row
	}
	work := s.workDirFor(item.ID) // before the row goes: a torrent may be filed in a category folder
	// The release is being thrown away, so its torrent goes even when it had
	// finished downloading (an item that was imported never gets here).
	ref, inClient := s.clientRefFor(item)
	if item.Status == queue.StatusPaused || item.Status == queue.StatusConflict {
		_ = s.QueueRepo.SetStatus(item.ID, queue.StatusStopped, "")
		s.releaseTitleClaims(item)
	}
	s.torrents.stopQueue(item.ID)
	_ = s.QueueRepo.Delete(item.ID)
	s.removeFromClient(ref, inClient, true)
	s.removeWorkDir(work)
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
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "That download isn't in the list any more.")
			return
		}
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
		// Updates, restarts and shut-downs, and changes to accounts, say who
		// did it and from which address: for administrators only.
		if (e.EventType == "update" || e.EventType == "account" || e.EventType == "library") && !admin {
			continue
		}
		msg := plainFailure(e.EventType, e.Message)
		if !admin {
			msg = hideLibraryPaths(msg) // where files lie on the server is for administrators
		}
		p := activityEntryPayload{ID: e.ID, EventType: e.EventType, Message: msg, CreatedAt: e.CreatedAt}
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

// indexerNames maps an indexer's id to its name, so a download can say where
// its release came from.
func (s *Server) indexerNames() map[int64]string {
	out := map[int64]string{}
	list, err := s.IndexerRepo.List()
	if err != nil {
		return out
	}
	for _, inst := range list {
		out[inst.ID] = inst.Name
	}
	return out
}
