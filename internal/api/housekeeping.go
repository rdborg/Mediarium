package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

const (
	// orphanAge is how long an entry nobody owns (or a failed download's
	// folder) is left alone before clean-up removes it: long enough to
	// retry a failed download or look inside it.
	orphanAge = 24 * time.Hour
	// cleanupEvery is how often the automatic clean-up runs.
	cleanupEvery = 24 * time.Hour
	// cleanupCheckInterval is how often the automation loop checks whether
	// the automatic clean-up is due.
	cleanupCheckInterval = time.Hour
	// defaultHistoryRetentionDays applies while historyRetentionDays is unset.
	defaultHistoryRetentionDays = 90
)

// errRemovedFromLibrary is how a download cancelled because its title was
// removed from the library ends.
var errRemovedFromLibrary = errors.New("Cancelled because the title was removed from your library.")

// cleanupMu makes one clean-up run at a time.
var cleanupMu sync.Mutex

// downloadsArea is the part of the disk clean-up may touch: the working
// folder inside the downloads folder, never the movie or TV library.
func (s *Server) downloadsArea() cleanup.Area {
	return cleanup.Area{
		Base:      s.downloadsRoot(),
		Work:      s.downloadsIncompleteDir(),
		Protected: append(append(s.movieRoots(), s.tvRoots()...), s.musicRoot()),
	}
}

// workDirFor is the working folder of queue item id.
func (s *Server) workDirFor(id int64) string {
	return filepath.Join(s.downloadsIncompleteDir(), fmt.Sprintf("queue-%d", id))
}

// queueDirID parses a working folder name ("queue-12") into its queue item
// id.
func queueDirID(name string) (int64, bool) {
	rest, ok := strings.CutPrefix(name, "queue-")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil && id > 0
}

// cleanupWorkDir removes a download's working folder once its files are in
// the library. A torrent that is still seeding keeps its folder until its
// seeding goal is met (seedingGoalMet removes it then).
func (s *Server) cleanupWorkDir(dir string, protocol indexers.Protocol) {
	if protocol == indexers.ProtocolTorrent && s.torrents.activeDir(dir) {
		return
	}
	s.removeWorkDir(dir)
}

// removeWorkDir deletes one queue-N working folder directly inside the
// working folder (anything else is refused) and returns the space freed.
func (s *Server) removeWorkDir(dir string) int64 {
	area := s.downloadsArea()
	dir = filepath.Clean(dir)
	if _, ok := queueDirID(filepath.Base(dir)); !ok || filepath.Dir(dir) != filepath.Clean(area.Work) {
		log.Printf("cleanup: refused to remove path=%s: not a download's working folder", dir)
		return 0
	}
	freed, err := cleanup.RemoveInside(area, dir)
	if err != nil {
		log.Printf("cleanup: remove working folder path=%s: %v", dir, err)
		return 0
	}
	return freed
}

// seedingGoalMet is called once queueID's torrent, saved in dir, has
// reached its seeding goal and been stopped: its data is removed as soon as
// the download has been imported (if the import is still running, the
// pipeline removes it when it finishes).
func (s *Server) seedingGoalMet(queueID int64, dir string) {
	s.torrents.markGoalMet(queueID)
	item, err := s.QueueRepo.Get(queueID)
	if err != nil || item.Status != queue.StatusCompleted {
		return
	}
	freed := s.removeWorkDir(dir)
	_ = s.QueueRepo.LogActivity(item.MovieID, "cleanup", fmt.Sprintf("Seeding goal reached for %q: stopped seeding and removed its download data (%s)", item.ReleaseTitle, humanBytes(freed)))
}

// classifyWorkEntry decides what one entry of the working folder is, for
// the clean-up scan.
func (s *Server) classifyWorkEntry(items map[int64]queue.Item, now time.Time) cleanup.Classify {
	return func(name string, info fs.FileInfo) cleanup.Decision {
		old := cleanup.OlderThan(info, orphanAge, now)
		id, isQueueDir := queueDirID(name)
		if !isQueueDir {
			if old {
				return cleanup.Decision{Remove: true, Reason: cleanup.ReasonOrphaned}
			}
			return cleanup.Decision{}
		}
		if s.torrents.active(id) || s.pipelines.isRunning(id) {
			return cleanup.Decision{Active: true}
		}
		it, known := items[id]
		switch {
		case !known:
			if old {
				return cleanup.Decision{Remove: true, Reason: cleanup.ReasonOrphaned}
			}
			return cleanup.Decision{}
		case isActiveStatus(it.Status) || it.Status == queue.StatusConflict || it.Status == queue.StatusPaused:
			return cleanup.Decision{Active: true} // a paused download keeps its files until resumed or stopped
		case it.Status == queue.StatusCompleted:
			if s.torrents.seedingDone(id) {
				return cleanup.Decision{Remove: true, Reason: cleanup.ReasonSeedingFinished}
			}
			return cleanup.Decision{Remove: true, Reason: cleanup.ReasonImportedLeftover}
		default: // failed: kept a day, to retry it or look inside
			if finished, err := time.Parse(time.RFC3339Nano, it.CompletedAt); err == nil && now.Sub(finished) > orphanAge {
				return cleanup.Decision{Remove: true, Reason: cleanup.ReasonOrphaned}
			}
			if it.CompletedAt == "" && old {
				return cleanup.Decision{Remove: true, Reason: cleanup.ReasonOrphaned}
			}
			return cleanup.Decision{}
		}
	}
}

// scanDownloads lists what clean-up would remove from the working folder.
func (s *Server) scanDownloads(now time.Time) ([]cleanup.Item, error) {
	list, err := s.QueueRepo.List()
	if err != nil {
		return nil, err
	}
	items := make(map[int64]queue.Item, len(list))
	for _, it := range list {
		items[it.ID] = it
	}
	return cleanup.Scan(s.downloadsArea(), s.classifyWorkEntry(items, now))
}

// historyRetentionDays is how many days finished downloads and activity are
// kept; 0 keeps them forever.
func (s *Server) historyRetentionDays() int {
	v, _ := s.Settings.Get(settings.KeyHistoryRetentionDays)
	if v == "" {
		return defaultHistoryRetentionDays
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultHistoryRetentionDays
	}
	return n
}

// cleanupAutoEnabled reports the automatic clean-up switch (on unless
// turned off).
func (s *Server) cleanupAutoEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyCleanupAuto)
	return v != "0"
}

func (s *Server) cleanupLastRun() time.Time {
	v, _ := s.Settings.Get(settings.KeyCleanupLastRunAt)
	t, _ := time.Parse(time.RFC3339, v)
	return t
}

// cleanupResult is what one clean-up run did.
type cleanupResult struct {
	Removed        []cleanup.Item
	RemovedBytes   int64
	PrunedQueue    int64
	PrunedActivity int64
	Errors         []string
	RanAt          time.Time
}

// runCleanup removes everything the scan finds in the working folder and
// prunes history older than the retention setting.
func (s *Server) runCleanup(now time.Time) cleanupResult {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	res := cleanupResult{RanAt: now}

	items, err := s.scanDownloads(now)
	if err != nil {
		res.Errors = append(res.Errors, plainerror.Message(err))
	}
	removed, err := cleanup.Remove(s.downloadsArea(), items)
	if err != nil {
		res.Errors = append(res.Errors, plainerror.Message(err))
	}
	res.Removed = removed
	for _, it := range removed {
		res.RemovedBytes += it.SizeBytes
	}

	if days := s.historyRetentionDays(); days > 0 {
		cutoff := now.AddDate(0, 0, -days)
		if res.PrunedQueue, err = s.QueueRepo.PruneFinished(cutoff); err != nil {
			res.Errors = append(res.Errors, plainerror.Message(err))
		}
		if res.PrunedActivity, err = s.QueueRepo.PruneActivity(cutoff); err != nil {
			res.Errors = append(res.Errors, plainerror.Message(err))
		}
	}

	s.pruneProblems(now)
	_, _ = s.QueueRepo.ClearSettledFailures()

	if n, freed, errs := s.purgeTrash(now); n > 0 || len(errs) > 0 {
		res.Errors = append(res.Errors, errs...)
		if n > 0 {
			_ = s.QueueRepo.LogActivity(0, "cleanup", fmt.Sprintf("Emptied %s from the recycle bin (older than %d days) and freed %s", plural(n, "item"), s.trashDays(), humanBytes(freed)))
		}
	}

	_ = s.Settings.Set(settings.KeyCleanupLastRunAt, now.UTC().Format(time.RFC3339), false)
	if len(removed) > 0 {
		_ = s.QueueRepo.LogActivity(0, "cleanup", fmt.Sprintf("Clean-up removed %s from the downloads folder and freed %s", plural(len(removed), "item"), humanBytes(res.RemovedBytes)))
	}
	log.Printf("cleanup: removed=%d freed_bytes=%d pruned_queue=%d pruned_activity=%d errors=%d",
		len(removed), res.RemovedBytes, res.PrunedQueue, res.PrunedActivity, len(res.Errors))
	return res
}

// cleanupJob is the automation loop's hourly check: it runs the clean-up
// when the automatic switch is on and the last run is a day old.
func (s *Server) cleanupJob(context.Context) {
	if !s.cleanupAutoEnabled() {
		return
	}
	now := time.Now()
	if last := s.cleanupLastRun(); !last.IsZero() && now.Sub(last) < cleanupEvery {
		return
	}
	s.runCleanup(now)
}

type cleanupItemPayload struct {
	Path      string `json:"path"` // relative to the downloads folder
	SizeBytes int64  `json:"sizeBytes"`
	Reason    string `json:"reason"` // orphaned | imported-leftover | seeding-finished | empty-folder
}

func toCleanupItems(items []cleanup.Item) []cleanupItemPayload {
	out := make([]cleanupItemPayload, 0, len(items))
	for _, it := range items {
		out = append(out, cleanupItemPayload{Path: it.Path, SizeBytes: it.SizeBytes, Reason: string(it.Reason)})
	}
	return out
}

type cleanupStatusPayload struct {
	ReclaimableBytes     int64                `json:"reclaimableBytes"`
	Items                []cleanupItemPayload `json:"items"`
	LastRunAt            string               `json:"lastRunAt,omitempty"`
	Auto                 bool                 `json:"auto"`
	HistoryRetentionDays int                  `json:"historyRetentionDays"`
	TrashDays            int                  `json:"trashDays"`
}

// handleCleanupStatus lists what clean-up would remove from the downloads
// working folder right now (never the library), with the space it frees, when
// it last ran and whether it runs daily.
func (s *Server) handleCleanupStatus(w http.ResponseWriter, r *http.Request) {
	items, err := s.scanDownloads(time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := cleanupStatusPayload{Items: toCleanupItems(items), Auto: s.cleanupAutoEnabled(), HistoryRetentionDays: s.historyRetentionDays(), TrashDays: s.trashDays()}
	for _, it := range items {
		out.ReclaimableBytes += it.SizeBytes
	}
	if last := s.cleanupLastRun(); !last.IsZero() {
		out.LastRunAt = last.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, out)
}

type cleanupRunPayload struct {
	RemovedBytes   int64                `json:"removedBytes"`
	Removed        []cleanupItemPayload `json:"removed"`
	PrunedQueue    int64                `json:"prunedQueueItems"` // finished downloads dropped from the history
	PrunedActivity int64                `json:"prunedActivity"`   // activity entries dropped
	LastRunAt      string               `json:"lastRunAt"`
	Errors         []string             `json:"errors,omitempty"`
}

// handleCleanupRun runs the clean-up now and reports what it removed and how
// much history it pruned.
func (s *Server) handleCleanupRun(w http.ResponseWriter, r *http.Request) {
	res := s.runCleanup(time.Now())
	writeJSON(w, http.StatusOK, cleanupRunPayload{
		RemovedBytes: res.RemovedBytes, Removed: toCleanupItems(res.Removed),
		PrunedQueue: res.PrunedQueue, PrunedActivity: res.PrunedActivity,
		LastRunAt: res.RanAt.UTC().Format(time.RFC3339), Errors: res.Errors,
	})
}

// humanBytes renders a size for activity messages ("1.4 GB").
func humanBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
