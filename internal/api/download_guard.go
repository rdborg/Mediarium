package api

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ryanborg/mediarium/internal/organizer"
	"github.com/ryanborg/mediarium/internal/queue"
)

// One active download per library item, and retrying a failed Usenet
// download from the files it already has.

// alreadyDownloadingError refuses a grab for a movie or episode that
// already has a download in progress (writeGrabError answers it with 409).
type alreadyDownloadingError struct{ release string }

func (e alreadyDownloadingError) Error() string {
	return fmt.Sprintf("Already downloading: %s. Cancel it first to pick another.", e.release)
}

func isAlreadyDownloading(err error) bool {
	var e alreadyDownloadingError
	return errors.As(err, &e)
}

// downloadInProgress reports whether a queue item is still being worked on:
// waiting, downloading or post-processing.
func downloadInProgress(st queue.Status) bool {
	return st == queue.StatusQueued || st == queue.StatusDownloading || st == queue.StatusImporting
}

// checkMovieNotDownloading refuses a grab for a movie that already has a
// download in progress.
func (s *Server) checkMovieNotDownloading(movieID int64) error {
	items, err := s.QueueRepo.List()
	if err != nil {
		return fmt.Errorf("check downloads in progress: %w", err)
	}
	for _, it := range items {
		if it.MovieID == movieID && downloadInProgress(it.Status) {
			return alreadyDownloadingError{release: it.ReleaseTitle}
		}
	}
	return nil
}

// checkEpisodesNotDownloading refuses a TV grab when any episode it would
// deliver already has a download in progress. A season pack covers every
// episode of its season, both as the new grab and as a running download.
func (s *Server) checkEpisodesNotDownloading(seriesID int64, hintSeason, hintEpisode int, releaseTitle string) error {
	season, episodes := resolveTVTarget(releaseTitle, hintSeason, hintEpisode)
	if season == 0 {
		return nil // the grab itself reports this
	}
	items, err := s.QueueRepo.List()
	if err != nil {
		return fmt.Errorf("check downloads in progress: %w", err)
	}
	for _, it := range items {
		if it.SeriesID != seriesID || !downloadInProgress(it.Status) {
			continue
		}
		busySeason, busyEpisodes := resolveTVTarget(it.ReleaseTitle, it.Season, it.Episode)
		if busySeason == season && episodesOverlap(episodes, busyEpisodes) {
			return alreadyDownloadingError{release: it.ReleaseTitle}
		}
	}
	return nil
}

// episodesOverlap reports whether two grabs of the same season share an
// episode; an empty list is a season pack, which covers them all.
func episodesOverlap(a, b []int) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// downloadedMarker is written into a Usenet download's working folder once
// every article has been fetched. It holds the number of articles no server
// had, which PAR2 has to make up for.
const downloadedMarker = ".mediarium-downloaded"

// markDownloaded records that a working folder holds a finished download.
func markDownloaded(dir string, missing int) {
	if err := os.WriteFile(filepath.Join(dir, downloadedMarker), []byte(strconv.Itoa(missing)), 0o644); err != nil {
		slog.Warn("pipeline: mark download finished", "dir", dir, "err", err)
	}
}

// keptDownload returns the working folder of a failed Usenet download whose
// files were all fetched and are still there, so a retry can post-process
// them again instead of downloading everything anew.
func (s *Server) keptDownload(item queue.Item) (string, bool) {
	if item.Protocol != queue.ProtocolUsenet && item.Protocol != "" {
		return "", false
	}
	dir := filepath.Join(s.downloadsIncompleteDir(), fmt.Sprintf("queue-%d", item.ID))
	if _, err := os.Stat(filepath.Join(dir, downloadedMarker)); err != nil {
		return "", false
	}
	return dir, true
}

// reprocessDirs maps a retried release (downloads folder + download URL) to
// the working folder of the earlier attempt, from the retry until the new
// pipeline picks it up in prepareDownload.
var reprocessDirs sync.Map

func (s *Server) reprocessKey(downloadURL string) string {
	return filepath.Clean(s.downloadsIncompleteDir()) + "\x00" + downloadURL
}

// offerKeptDownload lets the next pipeline for downloadURL start from dir.
func (s *Server) offerKeptDownload(downloadURL, dir string) {
	reprocessDirs.Store(s.reprocessKey(downloadURL), dir)
}

// withdrawKeptDownload undoes offerKeptDownload when the retry did not start.
func (s *Server) withdrawKeptDownload(downloadURL string) {
	reprocessDirs.Delete(s.reprocessKey(downloadURL))
}

// verifiedDirs are working folders reuseDownloaded has just checked with
// PAR2, so verifyStep need not check them a second time.
var verifiedDirs sync.Map

// reuseDownloaded moves the files of an earlier attempt (offered by a retry)
// into this queue item's working folder and checks them with PAR2. It
// reports false, and leaves an empty folder for a fresh download, when
// there is nothing to reuse or the files fail the check.
func (s *Server) reuseDownloaded(queueID int64, downloadURL, incompleteDir string) bool {
	v, ok := reprocessDirs.LoadAndDelete(s.reprocessKey(downloadURL))
	if !ok {
		return false
	}
	old := v.(string)
	root := filepath.Clean(s.downloadsIncompleteDir())
	if filepath.Dir(filepath.Clean(old)) != root || filepath.Dir(filepath.Clean(incompleteDir)) != root {
		return false // only ever move queue folders inside the downloads folder
	}
	if err := os.Rename(old, incompleteDir); err != nil {
		slog.Warn("pipeline: reuse earlier download", "from", old, "to", incompleteDir, "err", err)
		s.queueEvent(queueID, "retried", queue.LevelWarn, "The files of the earlier attempt could not be reused, downloading again")
		return false
	}
	missing := 0
	if b, err := os.ReadFile(filepath.Join(incompleteDir, downloadedMarker)); err == nil {
		missing, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	s.queueEvent(queueID, "retried", queue.LevelInfo, "Reusing the files already downloaded (no new download), checking them again")
	if err := verifyAndRepair(incompleteDir, missing); err != nil {
		s.queueEvent(queueID, "retried", queue.LevelWarn, fmt.Sprintf("The files already downloaded failed verification (%v), downloading again", err))
		if err := os.RemoveAll(incompleteDir); err != nil {
			slog.Warn("pipeline: clear failed earlier download", "dir", incompleteDir, "err", err)
		}
		return false
	}
	verifiedDirs.Store(filepath.Clean(incompleteDir), true)
	return true
}

// verifyStep is the PAR2 verify/repair step of post-processing, recorded in
// the item's activity.
func (s *Server) verifyStep(queueID int64, dir string, missing int) error {
	if _, done := verifiedDirs.LoadAndDelete(filepath.Clean(dir)); done {
		s.queueEvent(queueID, "postprocess", queue.LevelInfo, "Files verified")
		return nil
	}
	par2, _ := organizer.FindMainPar2Files(dir)
	if len(par2) == 0 && missing == 0 {
		return verifyAndRepair(dir, missing)
	}
	if missing > 0 {
		s.queueEvent(queueID, "postprocess", queue.LevelInfo, fmt.Sprintf("Repairing %d missing article(s) with PAR2", missing))
	} else {
		s.queueEvent(queueID, "postprocess", queue.LevelInfo, "Verifying with PAR2")
	}
	if err := verifyAndRepair(dir, missing); err != nil {
		s.queueEvent(queueID, "postprocess", queue.LevelError, fmt.Sprintf("PAR2 repair failed (%v)", err))
		return err
	}
	s.queueEvent(queueID, "postprocess", queue.LevelInfo, "Files verified")
	return nil
}

// unpackStep is the unpack step of post-processing, recorded in the item's
// activity.
func (s *Server) unpackStep(queueID int64, dir string) error {
	archives, _ := organizer.FindPrimaryArchives(dir)
	if len(archives) == 0 {
		return unpackArchives(dir)
	}
	s.queueEvent(queueID, "postprocess", queue.LevelInfo, fmt.Sprintf("Unpacking %d archive(s)", len(archives)))
	if err := unpackArchives(dir); err != nil {
		s.queueEvent(queueID, "postprocess", queue.LevelError, fmt.Sprintf("Unpacking failed (%v)", err))
		return err
	}
	s.queueEvent(queueID, "postprocess", queue.LevelInfo, "Unpacked")
	return nil
}
