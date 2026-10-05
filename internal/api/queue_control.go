package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/queue"
)

// Pausing, resuming and stopping downloads.
//
// Pause ends the pipeline (and with it the network transfer) but keeps the
// queue item, its partly downloaded files in the downloads folder and the
// title's claim on it. The item stays "paused" in the database, so it is still
// paused after a restart and nothing resumes it but a person. Resume starts
// the same queue item's pipeline again; a Usenet download fetches only the
// articles it does not have yet (download.ResumeFile), a torrent is added to
// the engine again and checks the pieces already on disk.
//
// Stop cancels the item for good: the title goes back to what it was, and the
// partial download can be deleted with it. Only a queue-N folder inside the
// downloads working folder is ever deleted (removeWorkDir); nothing in the
// movie, TV or music folders is touched.

var (
	// errPaused and errStopped are how a pipeline ends when a person paused or
	// stopped it. They are not failures: nothing is blocklisted or reported.
	errPaused  = errors.New("Paused.")
	errStopped = errors.New("Stopped.")
)

// stopWait is how long pausing or stopping waits for a download to wind down
// before answering. A download that is unpacking can take longer to reach the
// next safe point; it finishes stopping by itself and the list shows
// "Pausing" or "Stopping" until it has.
const stopWait = 10 * time.Second

// queueControlMu makes one pause, resume or stop happen at a time, so a
// double click or two people acting together cannot start a download twice.
var queueControlMu sync.Mutex

// settleInterrupted is called by a pipeline that ended: when a person paused
// or stopped it, it leaves the item paused or stopped and reports true along
// with the error the pipeline should end with.
func (s *Server) settleInterrupted(queueID int64, run *pipelineRun) (bool, error) {
	switch {
	case run.stopped.Load():
		item, _ := s.QueueRepo.Get(queueID)
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusStopped, "")
		s.releaseTitleClaims(item)
		if run.deleteFiles.Load() {
			s.removeWorkDir(s.workDirFor(queueID))
		}
		return true, errStopped
	case run.paused.Load():
		_ = s.QueueRepo.Pause(queueID, false)
		return true, errPaused
	}
	return false, nil
}

// pauseItem pauses one item that is queued, downloading or importing.
func (s *Server) pauseItem(item queue.Item) error {
	if !downloadInProgress(item.Status) {
		return errors.New("This download isn't running, so it can't be paused.")
	}
	if s.pipelines.interrupt(item.ID, false, false, stopWait) {
		return nil
	}
	return s.QueueRepo.Pause(item.ID, false) // nothing was running it
}

// resumeItem puts a paused item back in the download line, at the front of its
// group. It starts again from the files it has as soon as a place is free.
func (s *Server) resumeItem(item queue.Item) error {
	if item.Status != queue.StatusPaused {
		return errors.New("This download isn't paused.")
	}
	if item.Protocol == queue.ProtocolTorrent && !s.torrentsEnabled() {
		return errTorrentsDisabled
	}
	// Check that what it is for is still in the library before it goes back in line.
	if _, err := s.preparePipeline(item); err != nil {
		return err
	}
	ok, err := s.QueueRepo.Requeue(item.ID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("This download isn't paused.")
	}
	s.dispatch.Kick()
	return nil
}

// stopItem stops a download that is running or paused. With deleteFiles its
// partly downloaded files in the downloads folder are deleted too.
func (s *Server) stopItem(item queue.Item, deleteFiles bool) error {
	if item.Status != queue.StatusQueued && item.Status != queue.StatusDownloading && item.Status != queue.StatusImporting && item.Status != queue.StatusPaused {
		return errors.New("This download has already finished.")
	}
	if s.pipelines.interrupt(item.ID, true, deleteFiles, stopWait) {
		return nil
	}
	s.torrents.stopQueue(item.ID)
	if err := s.QueueRepo.SetStatus(item.ID, queue.StatusStopped, ""); err != nil {
		return err
	}
	s.releaseTitleClaims(item)
	if deleteFiles {
		s.removeWorkDir(s.workDirFor(item.ID))
	}
	return nil
}

// discardItem stops a download that is not wanted any more, deletes its
// partial files and removes it from the list. It does not wait for the
// pipeline to finish: it sets the final state itself.
func (s *Server) discardItem(item queue.Item) {
	if !s.pipelines.interrupt(item.ID, true, true, stopWait) {
		s.torrents.stopQueue(item.ID)
	}
	_ = s.QueueRepo.SetStatus(item.ID, queue.StatusStopped, "")
	s.releaseTitleClaims(item)
	s.removeWorkDir(s.workDirFor(item.ID))
	if err := s.QueueRepo.Delete(item.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("queue: remove discarded download", "queueId", item.ID, "err", err)
	}
}

// preparePipeline does the set-up of a download that is about to run, for the
// movie, episodes or album it is for: it checks they are still in the library
// and marks them as downloading. It returns the function that runs the
// pipeline, or an error that says why it cannot run. Calling it more than once
// is harmless, which is how Resume checks an item before putting it back in
// the download line.
func (s *Server) preparePipeline(item queue.Item) (func(), error) {
	protocol := indexers.Protocol(item.Protocol)
	if protocol == "" {
		protocol = indexers.ProtocolUsenet
	}
	switch {
	case item.MovieID > 0:
		movie, err := s.MovieRepo.Get(item.MovieID)
		if err != nil {
			return nil, errors.New("That movie isn't in your library any more.")
		}
		_ = s.MovieRepo.SetStatus(movie.ID, library.StatusDownloading, "", "")
		return func() {
			if err := s.runPipeline(item.ID, movie.ID, movie.Title, movie.Year, movie.TMDBID, item.ReleaseTitle, item.NZBURL, protocol); err != nil {
				slog.Info("queue: movie download ended", "queueId", item.ID, "err", err)
			}
		}, nil
	case item.SeriesID > 0:
		series, err := s.MovieRepo.GetSeries(item.SeriesID)
		if err != nil {
			return nil, errors.New("That show isn't in your library any more.")
		}
		season, episodes := resolveTVTarget(item.ReleaseTitle, item.Season, item.Episode)
		if season == 0 {
			return nil, errors.New("Couldn't tell which season this download is for.")
		}
		targets, err := s.tvGrabTargets(series.ID, season, episodes)
		if err != nil {
			return nil, errors.New("Those episodes are already in your library.")
		}
		for i := range targets {
			// An upgrade that fails leaves the old file, so those go back to downloaded.
			if fileExists(targets[i].FilePath) {
				targets[i].Status = library.StatusDownloaded
			}
		}
		s.grabMu.Lock()
		err = s.claimEpisodes(targets, false)
		s.grabMu.Unlock()
		if err != nil {
			return nil, err
		}
		return func() {
			if err := s.runTVPipeline(item.ID, series, season, targets, item.ReleaseTitle, item.NZBURL, protocol); err != nil {
				slog.Info("queue: tv download ended", "queueId", item.ID, "err", err)
			}
		}, nil
	case item.AlbumID > 0:
		album, err := s.MusicRepo.GetAlbum(item.AlbumID)
		if err != nil {
			return nil, errors.New("That album isn't in your library any more.")
		}
		restore := s.restoredAlbumStatus(album)
		_ = s.MusicRepo.SetAlbumStatus(album.ID, music.StatusDownloading)
		return func() {
			if err := s.runMusicPipeline(item.ID, album.ArtistID, album.ID, restore, item.ReleaseTitle, item.NZBURL, protocol); err != nil {
				slog.Info("queue: album download ended", "queueId", item.ID, "err", err)
			}
		}, nil
	case item.BookID > 0:
		book, err := s.BookRepo.Get(item.BookID)
		if err != nil {
			return nil, errors.New("That book isn't in your library any more.")
		}
		f := books.Format(item.BookFormat)
		if !f.Valid() {
			f = books.Ebook
		}
		_ = s.BookRepo.SetState(book.ID, f, books.StatusDownloading, "", "")
		return func() {
			if err := s.runBookPipeline(item.ID, book, f, item.ReleaseTitle, item.NZBURL, protocol); err != nil {
				slog.Info("queue: book download ended", "queueId", item.ID, "err", err)
			}
		}, nil
	}
	return nil, errors.New("This download isn't linked to a movie, show, album or book.")
}

// fileExists reports whether path names something on disk.
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// restoredMovieStatus is what a movie goes back to when a download for it
// ends without importing: downloaded when its file is still there (an
// upgrade), otherwise missing.
func restoredMovieStatus(m library.Movie) library.Status {
	if m.Status == library.StatusDownloaded || fileExists(m.FilePath) {
		return library.StatusDownloaded
	}
	return library.StatusMissing
}

func (s *Server) restoredAlbumStatus(a music.Album) music.Status {
	if a.Status == music.StatusDownloaded || (a.Path != "" && fileExists(a.Path)) {
		return music.StatusDownloaded
	}
	return music.StatusMissing
}

// releaseTitleClaims hands back what a download that will not finish had
// claimed: a movie, episodes or album marked "downloading" go back to
// downloaded (if their file is still there) or missing, unless another
// download still covers them. Call it after the item's own status has changed.
func (s *Server) releaseTitleClaims(item queue.Item) {
	switch {
	case item.MovieID > 0:
		m, err := s.MovieRepo.Get(item.MovieID)
		if err != nil || m.Status != library.StatusDownloading {
			return
		}
		if busy, err := s.QueueRepo.HasActiveForMovie(m.ID); err != nil || busy {
			return
		}
		_ = s.MovieRepo.SetStatus(m.ID, restoredMovieStatus(m), "", "")
	case item.SeriesID > 0:
		eps, err := s.MovieRepo.ListEpisodes(item.SeriesID)
		if err != nil {
			return
		}
		season, episodes := resolveTVTarget(item.ReleaseTitle, item.Season, item.Episode)
		others := s.otherOpenDownloads(item.SeriesID, item.ID)
		for _, ep := range eps {
			if ep.Status != library.StatusDownloading || ep.Season != season || !coversEpisode(season, episodes, ep) {
				continue
			}
			if downloadCovers(others, ep) {
				continue
			}
			_ = s.MovieRepo.SetEpisodeStatus(ep.ID, restoredEpisodeStatus(ep), "", "")
		}
	case item.AlbumID > 0:
		a, err := s.MusicRepo.GetAlbum(item.AlbumID)
		if err != nil || a.Status != music.StatusDownloading {
			return
		}
		if busy, err := s.QueueRepo.HasActiveForAlbum(a.ID); err != nil || busy {
			return
		}
		_ = s.MusicRepo.SetAlbumStatus(a.ID, s.restoredAlbumStatus(a))
	case item.BookID > 0:
		b, err := s.BookRepo.Get(item.BookID)
		f := books.Format(item.BookFormat)
		if err != nil || !f.Valid() || b.Status(f) != books.StatusDownloading {
			return
		}
		if busy, err := s.QueueRepo.HasActiveForBook(b.ID, string(f)); err != nil || busy {
			return
		}
		_ = s.BookRepo.SetState(b.ID, f, restoredBookStatus(b, f), "", "")
	}
}

// restoredBookStatus is what one format of a book goes back to when a
// download for it ends without importing: downloaded when its file is still
// there, otherwise missing.
func restoredBookStatus(b books.Book, f books.Format) string {
	if fileExists(b.Path(f)) {
		return books.StatusDownloaded
	}
	return books.StatusMissing
}

func restoredEpisodeStatus(ep library.Episode) library.Status {
	if fileExists(ep.FilePath) {
		return library.StatusDownloaded
	}
	return library.StatusMissing
}

// coversEpisode reports whether a release for season and episodes (none means
// a season pack) delivers ep.
func coversEpisode(season int, episodes []int, ep library.Episode) bool {
	if ep.Season != season {
		return false
	}
	if len(episodes) == 0 {
		return true
	}
	for _, n := range episodes {
		if n == ep.Episode {
			return true
		}
	}
	return false
}

// otherOpenDownloads lists the downloads of a show, apart from except, that
// still hold a claim on their episodes: running, paused or parked.
func (s *Server) otherOpenDownloads(seriesID, except int64) []queue.Item {
	items, err := s.QueueRepo.List()
	if err != nil {
		return nil
	}
	var out []queue.Item
	for _, it := range items {
		if it.SeriesID == seriesID && it.ID != except && (downloadInProgress(it.Status) || it.Status == queue.StatusPaused || it.Status == queue.StatusConflict) {
			out = append(out, it)
		}
	}
	return out
}

func downloadCovers(items []queue.Item, ep library.Episode) bool {
	for _, it := range items {
		season, episodes := resolveTVTarget(it.ReleaseTitle, it.Season, it.Episode)
		if coversEpisode(season, episodes, ep) {
			return true
		}
	}
	return false
}

// --- HTTP handlers ---

func queueID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid download ID.")
		return 0, false
	}
	return id, true
}

func (s *Server) queueItemOr404(w http.ResponseWriter, id int64) (queue.Item, bool) {
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That download is no longer in the queue.")
		return queue.Item{}, false
	}
	return item, true
}

type queueStateResponse struct {
	Status  string `json:"status"`
	Pending string `json:"pending,omitempty"` // "pausing" or "stopping" while it winds down
}

func (s *Server) queueState(id int64) queueStateResponse {
	out := queueStateResponse{Pending: s.pipelines.pending(id)}
	if it, err := s.QueueRepo.Get(id); err == nil {
		out.Status = string(it.Status)
	}
	return out
}

// handlePauseQueueItem pauses one download.
func (s *Server) handlePauseQueueItem(w http.ResponseWriter, r *http.Request) {
	id, ok := queueID(w, r)
	if !ok {
		return
	}
	item, ok := s.queueItemOr404(w, id)
	if !ok {
		return
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	if err := s.pauseItem(item); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.queueState(id))
}

// handleResumeQueueItem starts a paused download again.
func (s *Server) handleResumeQueueItem(w http.ResponseWriter, r *http.Request) {
	id, ok := queueID(w, r)
	if !ok {
		return
	}
	item, ok := s.queueItemOr404(w, id)
	if !ok {
		return
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	if err := s.resumeItem(item); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.queueState(id))
}

type stopQueueRequest struct {
	DeleteFiles bool `json:"deleteFiles"`
}

// handleStopQueueItem cancels a running or paused download, and with
// deleteFiles deletes its partly downloaded files from the downloads folder.
func (s *Server) handleStopQueueItem(w http.ResponseWriter, r *http.Request) {
	id, ok := queueID(w, r)
	if !ok {
		return
	}
	var req stopQueueRequest
	_ = decodeJSON(r, &req) // no body means keep the files
	item, ok := s.queueItemOr404(w, id)
	if !ok {
		return
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	if err := s.stopItem(item, req.DeleteFiles); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.queueState(id))
}

type pauseAllResponse struct {
	Paused int `json:"paused"`
}

// handlePauseAllQueue pauses every download that is running or waiting.
func (s *Server) handlePauseAllQueue(w http.ResponseWriter, r *http.Request) {
	items, err := s.QueueRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't read the download list. Try again.")
		return
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	// Nothing waiting starts while the running ones wind down.
	defer s.dispatch.Hold()()
	n := 0
	for _, it := range items {
		if downloadInProgress(it.Status) && s.pauseItem(it) == nil {
			n++
		}
	}
	writeJSON(w, http.StatusOK, pauseAllResponse{Paused: n})
}

type resumeAllResponse struct {
	Resumed int    `json:"resumed"`
	Skipped int    `json:"skipped"`
	Problem string `json:"problem,omitempty"` // why the first skipped one could not start
}

// handleResumeAllQueue puts every paused download back in the download line.
func (s *Server) handleResumeAllQueue(w http.ResponseWriter, r *http.Request) {
	items, err := s.QueueRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't read the download list. Try again.")
		return
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	var out resumeAllResponse
	// Each one goes to the front of the line, so the newest is put back first
	// and the oldest ends up first. Nothing starts until they are all back.
	defer s.dispatch.Hold()()
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	for _, it := range items {
		if it.Status != queue.StatusPaused {
			continue
		}
		if err := s.resumeItem(it); err != nil {
			out.Skipped++
			if out.Problem == "" {
				out.Problem = err.Error()
			}
			continue
		}
		out.Resumed++
	}
	writeJSON(w, http.StatusOK, out)
}

// --- removing what is not wanted ---

// clearWaitingDownloads removes the downloads of a movie or show that are
// only waiting: queued but not started, or paused by a restart. With
// onlyUpgrades it leaves alone the ones that are not an upgrade (the title
// has no file yet). It returns how many it removed. Used when a person turns
// off "Look for better versions" or stops monitoring a title.
func (s *Server) clearWaitingDownloads(movieID, seriesID int64, onlyUpgrades bool, reason string) int {
	items, err := s.QueueRepo.List()
	if err != nil {
		return 0
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	n := 0
	for _, it := range items {
		if (movieID == 0 || it.MovieID != movieID) && (seriesID == 0 || it.SeriesID != seriesID) {
			continue
		}
		waiting := it.Status == queue.StatusQueued || (it.Status == queue.StatusPaused && it.Interrupted)
		if !waiting {
			continue
		}
		if onlyUpgrades && !s.isUpgradeDownload(it) {
			continue
		}
		s.queueItemEvent(it, "removed", queue.LevelInfo, reason+": "+it.ReleaseTitle)
		s.discardItem(it)
		n++
	}
	return n
}

// isUpgradeDownload reports whether a download is for something already in
// the library: the movie's file, or every episode's file, is still there.
func (s *Server) isUpgradeDownload(it queue.Item) bool {
	switch {
	case it.MovieID > 0:
		m, err := s.MovieRepo.Get(it.MovieID)
		return err == nil && fileExists(m.FilePath)
	case it.SeriesID > 0:
		eps, err := s.MovieRepo.ListEpisodes(it.SeriesID)
		if err != nil {
			return false
		}
		season, episodes := resolveTVTarget(it.ReleaseTitle, it.Season, it.Episode)
		found := false
		for _, ep := range eps {
			if !coversEpisode(season, episodes, ep) {
				continue
			}
			if !fileExists(ep.FilePath) {
				return false
			}
			found = true
		}
		return found
	}
	return false
}

// tidyResult is what tidyOrphans put right.
type tidyResult struct {
	Movies, Episodes, Albums, Books, Upgrades int
}

func (t tidyResult) any() bool { return t.Movies+t.Episodes+t.Albums+t.Books+t.Upgrades > 0 }

// tidyOrphans fixes the leftovers that made titles look stuck, at startup:
//   - a movie, episode or album marked "downloading" with no download behind
//     it goes back to downloaded (its file is there) or missing;
//   - a movie or episode marked "missing" whose file is on disk goes back to
//     downloaded, so it never shows up as waiting for a release;
//   - a waiting download for a title that is downloaded and not looking for
//     better versions is removed.
//
// It only reads files in the library folders (to see they exist); it never
// changes one.
func (s *Server) tidyOrphans() tidyResult {
	var res tidyResult
	items, err := s.QueueRepo.List()
	if err != nil {
		slog.Warn("queue: tidy up", "err", err)
		return res
	}
	open := func(it queue.Item) bool {
		return downloadInProgress(it.Status) || it.Status == queue.StatusPaused || it.Status == queue.StatusConflict
	}

	// Upgrades nobody wants: the title is downloaded and better versions are off.
	for _, it := range items {
		if !(it.Status == queue.StatusQueued || (it.Status == queue.StatusPaused && it.Interrupted)) {
			continue
		}
		noUpgrade := false
		switch {
		case it.MovieID > 0:
			if m, err := s.MovieRepo.Get(it.MovieID); err == nil {
				noUpgrade = m.NoUpgrade
			}
		case it.SeriesID > 0:
			if sr, err := s.MovieRepo.GetSeries(it.SeriesID); err == nil {
				noUpgrade = sr.NoUpgrade
			}
		}
		if noUpgrade && s.isUpgradeDownload(it) {
			s.queueItemEvent(it, "removed", queue.LevelInfo, "Removed because better versions are switched off: "+it.ReleaseTitle)
			s.discardItem(it)
			res.Upgrades++
		}
	}
	if res.Upgrades > 0 {
		if items, err = s.QueueRepo.List(); err != nil {
			return res
		}
	}

	movieOpen := map[int64]bool{}
	albumOpen := map[int64]bool{}
	bookOpen := map[string]bool{} // book id + format
	seriesOpen := map[int64][]queue.Item{}
	for _, it := range items {
		if !open(it) {
			continue
		}
		switch {
		case it.MovieID > 0:
			movieOpen[it.MovieID] = true
		case it.AlbumID > 0:
			albumOpen[it.AlbumID] = true
		case it.BookID > 0:
			bookOpen[fmt.Sprintf("%d|%s", it.BookID, it.BookFormat)] = true
		case it.SeriesID > 0:
			seriesOpen[it.SeriesID] = append(seriesOpen[it.SeriesID], it)
		}
	}

	if movies, err := s.MovieRepo.List(); err == nil {
		for _, m := range movies {
			switch {
			case m.Status == library.StatusDownloading && !movieOpen[m.ID]:
				_ = s.MovieRepo.SetStatus(m.ID, restoredMovieStatus(m), "", "")
				res.Movies++
			case m.Status == library.StatusMissing && fileExists(m.FilePath):
				_ = s.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "", "")
				res.Movies++
			}
		}
	}
	if shows, err := s.MovieRepo.ListSeries(); err == nil {
		for _, sr := range shows {
			eps, err := s.MovieRepo.ListEpisodes(sr.ID)
			if err != nil {
				continue
			}
			for _, ep := range eps {
				switch {
				case ep.Status == library.StatusDownloading && !downloadCovers(seriesOpen[sr.ID], ep):
					_ = s.MovieRepo.SetEpisodeStatus(ep.ID, restoredEpisodeStatus(ep), "", "")
					res.Episodes++
				case ep.Status == library.StatusMissing && fileExists(ep.FilePath):
					_ = s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, "", "")
					res.Episodes++
				}
			}
		}
	}
	if albums, err := s.MusicRepo.ListAllAlbums(); err == nil {
		for _, a := range albums {
			if a.Status == music.StatusDownloading && !albumOpen[a.ID] {
				_ = s.MusicRepo.SetAlbumStatus(a.ID, s.restoredAlbumStatus(a))
				res.Albums++
			}
		}
	}
	if list, err := s.BookRepo.List(); err == nil {
		for _, b := range list {
			for _, f := range []books.Format{books.Ebook, books.Audiobook} {
				switch {
				case b.Status(f) == books.StatusDownloading && !bookOpen[fmt.Sprintf("%d|%s", b.ID, f)]:
					_ = s.BookRepo.SetState(b.ID, f, restoredBookStatus(b, f), "", "")
					res.Books++
				case b.Status(f) == books.StatusMissing && fileExists(b.Path(f)):
					_ = s.BookRepo.SetState(b.ID, f, books.StatusDownloaded, "", "")
					res.Books++
				}
			}
		}
	}
	return res
}

// recoverAtStartup runs once when the app starts: downloads that were running
// when it stopped come back paused (never resumed by themselves, so nothing
// that was half done starts before a person looks), downloads that were only
// waiting in line stay waiting and carry on when the line is next checked
// (never in safe mode, which pauses them too), and titles left looking stuck
// are put right.
func (s *Server) recoverAtStartup() {
	s.restoreVPN() // the VPN that was on comes back on its own; torrents wait for it
	paused, err := s.QueueRepo.RecoverInterrupted()
	if err != nil {
		slog.Warn("queue: pause interrupted downloads", "err", err)
	}
	// Safe mode starts nothing by itself, so what was waiting in line is held too.
	if s.cfg.PauseAutomation {
		if n, err := s.QueueRepo.PauseWaiting(); err != nil {
			slog.Warn("queue: pause waiting downloads", "err", err)
		} else {
			paused += n
		}
	}
	tidy := s.tidyOrphans()
	if paused > 0 || tidy.any() {
		slog.Info("queue: start-up check", "pausedDownloads", paused, "moviesFixed", tidy.Movies, "episodesFixed", tidy.Episodes, "albumsFixed", tidy.Albums, "booksFixed", tidy.Books, "upgradesRemoved", tidy.Upgrades)
	}
	if paused > 0 {
		_ = s.QueueRepo.LogActivity(0, "paused", fmt.Sprintf("Mediarium was restarted, so %s that was downloading is now paused. Press Resume in Activity to carry on.", plural(int(paused), "download")))
	}
}

// keptResume reports whether dir holds part of a Usenet download.
func keptResume(dir string) bool {
	return fileExists(filepath.Join(dir, download.ResumeFile))
}

// pausedBeforeStart reports whether queueID was paused, stopped or removed
// between being queued and its pipeline starting: it then must not run.
func (s *Server) pausedBeforeStart(queueID int64) bool {
	it, err := s.QueueRepo.Get(queueID)
	if errors.Is(err, sql.ErrNoRows) {
		return true // removed in the meantime: nothing is left to run
	}
	return err == nil && (it.Status == queue.StatusPaused || it.Status == queue.StatusStopped)
}

// releaseAfterPause is what removing a paused download does to its title: the
// download counts as stopped, so the movie, episodes or album it held go back
// to what they were.
func (s *Server) releaseAfterPause(item queue.Item) {
	_ = s.QueueRepo.SetStatus(item.ID, queue.StatusStopped, "")
	s.releaseTitleClaims(item)
}
