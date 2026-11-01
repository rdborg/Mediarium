package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/queue"
)

// One active download per library item, and retrying a failed Usenet
// download from the files it already has.

// alreadyDownloadingError refuses a grab for a movie or episode that
// already has a download in progress (writeGrabError answers it with 409).
type alreadyDownloadingError struct {
	release string
	paused  bool
	// item is the download in the way (zero when it is not known), so a person
	// can be offered to cancel it and grab the other release in one step.
	item queue.Item
}

func (e alreadyDownloadingError) Error() string {
	if e.paused {
		return fmt.Sprintf("A download is paused: %s. Resume it, or stop it first to pick another.", e.release)
	}
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

// downloadOpen is downloadInProgress plus a paused download, which still holds
// its title until it is resumed or stopped.
func downloadOpen(st queue.Status) bool {
	return downloadInProgress(st) || st == queue.StatusPaused
}

// checkMovieNotDownloading refuses a grab for a movie that already has a
// download in progress. Only the unfinished downloads are read: these checks
// run under the grab lock, and the whole history (every row with its address
// decrypted) grows without end.
func (s *Server) checkMovieNotDownloading(movieID int64) error {
	items, err := s.QueueRepo.ListUnfinished()
	if err != nil {
		return fmt.Errorf("check downloads in progress: %w", err)
	}
	for _, it := range items {
		if it.MovieID == movieID && downloadOpen(it.Status) {
			return alreadyDownloadingError{release: it.ReleaseTitle, paused: it.Status == queue.StatusPaused, item: it}
		}
	}
	return nil
}

// checkEpisodesNotDownloading refuses a TV grab when any episode it would
// deliver already has a download in progress. A season pack covers every
// episode of its season, both as the new grab and as a running download.
func (s *Server) checkEpisodesNotDownloading(seriesID int64, hintSeason, hintEpisode int, releaseTitle string) error {
	season, episodes := resolveTVTarget(releaseTitle, hintSeason, hintEpisode)
	if season == 0 && len(episodes) == 0 {
		return nil // the grab itself reports this
	}
	items, err := s.QueueRepo.ListUnfinished()
	if err != nil {
		return fmt.Errorf("check downloads in progress: %w", err)
	}
	for _, it := range items {
		if it.SeriesID != seriesID || !downloadOpen(it.Status) {
			continue
		}
		busySeason, busyEpisodes := resolveTVTarget(it.ReleaseTitle, it.Season, it.Episode)
		if busySeason == season && episodesOverlap(episodes, busyEpisodes) {
			return alreadyDownloadingError{release: it.ReleaseTitle, paused: it.Status == queue.StatusPaused, item: it}
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

// keptDownload returns the working folder of a failed or stopped Usenet
// download whose files are still there, so a retry can carry on from them: a
// finished download is post-processed again, a partial one fetches only the
// articles it is missing.
func (s *Server) keptDownload(item queue.Item) (string, bool) {
	if item.Protocol != queue.ProtocolUsenet && item.Protocol != "" {
		return "", false
	}
	dir, ok := s.existingWorkDir(item.ID)
	if !ok {
		return "", false
	}
	if fileExists(filepath.Join(dir, downloadedMarker)) || keptResume(dir) {
		return dir, true
	}
	return "", false
}

// moveKeptDownload moves the working folder of an earlier attempt to the
// retry's own folder, so the retry starts from those files (see
// reuseDownloaded). The new folder is made beside the old one, in the same work
// folder, so the move is a rename even when that is the temporary download
// folder. Only queue folders directly inside a work folder are ever moved. It
// reports whether the files are now in place.
func (s *Server) moveKeptDownload(old string, newID int64) bool {
	old = filepath.Clean(old)
	root := filepath.Dir(old)
	known := false
	for _, r := range s.workRoots() {
		if r == root {
			known = true
		}
	}
	if !known {
		return false
	}
	dir := filepath.Join(root, fmt.Sprintf("queue-%d", newID))
	if err := os.Rename(old, dir); err != nil {
		slog.Warn("retry: move the earlier download's files", "from", old, "to", dir, "err", err)
		return false
	}
	return true
}

// verifiedDirs are working folders reuseDownloaded has just checked with
// PAR2, so verifyStep need not check them a second time.
var verifiedDirs sync.Map

// reuseDownloaded checks a finished download already in this queue item's
// working folder (moved there by a retry, see moveKeptDownload, or left by a
// pause) with PAR2. It reports false, and leaves an empty folder for a fresh
// download, when there is nothing to reuse or the files fail the check.
func (s *Server) reuseDownloaded(ctx context.Context, queueID int64, incompleteDir string) bool {
	// Whether the files came from an earlier attempt or this item was paused
	// and resumed, only a finished download is reused as it is; a partial one
	// carries on from what it has (download.ResumeFile).
	if !fileExists(filepath.Join(incompleteDir, downloadedMarker)) {
		return false
	}
	missing := 0
	if b, err := os.ReadFile(filepath.Join(incompleteDir, downloadedMarker)); err == nil {
		missing, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	s.queueEvent(queueID, "retried", queue.LevelInfo, "Reusing the files already downloaded instead of downloading again. Checking them first.")
	if err := verifyAndRepair(ctx, incompleteDir, missing); err != nil {
		s.queueEvent(queueID, "retried", queue.LevelWarn, fmt.Sprintf("The files already downloaded didn't pass the check (%v). Downloading again.", err))
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
func (s *Server) verifyStep(ctx context.Context, queueID int64, dir string, missing int) error {
	if _, done := verifiedDirs.LoadAndDelete(filepath.Clean(dir)); done {
		s.queueEvent(queueID, "postprocess", queue.LevelInfo, "Files verified")
		return nil
	}
	par2, _ := organizer.FindMainPar2Files(dir)
	if len(par2) == 0 && missing == 0 {
		return verifyAndRepair(ctx, dir, missing)
	}
	if missing > 0 {
		s.queueEvent(queueID, "postprocess", queue.LevelInfo, fmt.Sprintf("Repairing %s with PAR2", plural(missing, "missing article")))
	} else {
		s.queueEvent(queueID, "postprocess", queue.LevelInfo, "Verifying with PAR2")
	}
	if err := verifyAndRepair(ctx, dir, missing); err != nil {
		s.queueEvent(queueID, "postprocess", queue.LevelError, fmt.Sprintf("PAR2 repair failed (%v)", err))
		return err
	}
	s.queueEvent(queueID, "postprocess", queue.LevelInfo, "Files verified")
	return nil
}

// unpackStep is the unpack step of post-processing, recorded in the item's
// activity.
//
// While it runs, the Activity page shows how much has been unpacked (see
// unpackTracker), and the log says so when it takes long or gets stuck.
func (s *Server) unpackStep(ctx context.Context, queueID int64, dir string) error {
	archives, _ := organizer.FindPrimaryArchivesDeep(dir)
	if len(archives) == 0 {
		return unpackArchives(dir)
	}
	s.queueEvent(queueID, "postprocess", queue.LevelInfo, fmt.Sprintf("Unpacking %s", plural(len(archives), "archive")))
	w := newUnpackWatch(queueID, func(st unpackStatus) { s.unpacking.set(queueID, st) })
	defer s.unpacking.clear(queueID)
	bytes, took, err := unpackArchivesWatched(ctx, dir, w)
	if err != nil {
		if ctx.Err() == nil {
			s.queueEvent(queueID, "postprocess", queue.LevelError, fmt.Sprintf("Unpacking failed (%v)", err))
		}
		return err
	}
	s.queueEvent(queueID, "postprocess", queue.LevelInfo, unpackedMessage(bytes, took))
	return nil
}

// unpackedMessage is the activity line for a finished unpack: just "Unpacked"
// when it was quick, and how much and how long when it was not.
func unpackedMessage(bytes int64, took time.Duration) string {
	if took < 10*time.Second || bytes <= 0 {
		return "Unpacked"
	}
	return fmt.Sprintf("Unpacked %s in %s", humanBytes(bytes), humanDuration(took))
}

// humanDuration is a length of time in words: "45 seconds", "5 minutes",
// "1 hour 20 minutes".
func humanDuration(d time.Duration) string {
	secs := int(d.Round(time.Second).Seconds())
	switch {
	case secs < 60:
		return plural(max(secs, 1), "second")
	case secs < 3570:
		return plural((secs+30)/60, "minute")
	case secs < 3600:
		return "1 hour"
	}
	h, m := secs/3600, (secs%3600+30)/60
	if m == 60 {
		h, m = h+1, 0
	}
	if m == 0 {
		return plural(h, "hour")
	}
	return plural(h, "hour") + " " + plural(m, "minute")
}
