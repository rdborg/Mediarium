package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// The download line.
//
// Every download, whatever started it, is added to the line with status
// "queued" and starts when a place is free. How many places there are is the
// setting "Downloads at the same time" (one by default), for Usenet and
// torrents together. A download keeps its place from the moment it starts
// until its pipeline has ended: downloaded, repaired, unpacked and imported (or
// failed, paused or stopped). A torrent that is only seeding afterwards does
// not hold a place. The dispatcher (internal/queue) does the counting; this
// file connects it to the rest of the app.

const (
	minDownloadsAtOnce = 1
	maxDownloadsAtOnce = 5

	// downloadLineCheck is how often the line is looked at even when nothing
	// has happened, in case a wake-up was missed. The first check also starts
	// what was waiting when the app was stopped.
	downloadLineCheck = time.Minute
)

// grabKind says who asked for a download, which decides whether it may be
// refused because the title is already being downloaded, and where it goes in
// the download line.
type grabKind int

const (
	// grabPicked is a release a person chose (Choose release, Retry). It is
	// never refused for the automatic checks, and goes before automatic ones.
	grabPicked grabKind = iota
	// grabSearchNow is a release Mediarium picked because a person asked it to
	// search (Search now, adding a title, a bulk search). It goes before
	// automatic ones, but is skipped when the title is already being fetched.
	grabSearchNow
	// grabAutomatic is a release the scheduled searches picked. It waits
	// behind everything a person asked for.
	grabAutomatic
)

// searchKind is the kind of a grab made by a search: force is set when a
// person started the search.
func searchKind(force bool) grabKind {
	if force {
		return grabSearchNow
	}
	return grabAutomatic
}

// refusesDuplicates says the grab is skipped when the title is already being
// downloaded (a person choosing a release is told so instead).
func (k grabKind) refusesDuplicates() bool { return k != grabPicked }

func (k grabKind) priority() queue.Priority {
	if k == grabAutomatic {
		return queue.PriorityAutomatic
	}
	return queue.PriorityManual
}

// downloadsAtOnce is how many downloads may run at the same time.
func (s *Server) downloadsAtOnce() int {
	v, _ := s.Settings.Get(settings.KeyDownloadsConcurrent)
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < minDownloadsAtOnce {
		return minDownloadsAtOnce
	}
	return min(n, maxDownloadsAtOnce)
}

func (s *Server) newDispatcher() *queue.Dispatcher {
	return queue.NewDispatcher(s.QueueRepo, queue.DispatchConfig{
		Limit: s.downloadsAtOnce,
		// In safe mode only what a person asked for is ever started.
		ManualOnly:   func() bool { return s.cfg.PauseAutomation },
		Start:        s.startQueued,
		OnStartError: s.startRefused,
	})
}

// startQueued begins the pipeline of an item the dispatcher took out of the
// line. done is called when the pipeline has ended, which frees the place.
func (s *Server) startQueued(item queue.Item, done func()) error {
	run, err := s.preparePipeline(item)
	if err != nil {
		return err
	}
	// The item may have been stopped, or removed, in the moment between the
	// line handing it over and the title being marked as downloading. The
	// stop already looked at the title before it was marked, so what
	// preparePipeline just claimed is handed back here; the pipeline itself
	// sees the item is not to run (pausedBeforeStart).
	if it, err := s.QueueRepo.Get(item.ID); errors.Is(err, sql.ErrNoRows) || (err == nil && it.Status == queue.StatusStopped) {
		s.releaseTitleClaims(item)
	}
	s.launch(item, done, run)
	return nil
}

// launch runs a download's pipeline in the background and gives its place in
// the line back when it has ended, however it ended.
func (s *Server) launch(item queue.Item, done func(), run func()) {
	s.background(func() {
		defer done()
		s.runGuarded(item, run)
	})
}

// runGuarded runs a pipeline and turns a crash in it into a failed download.
// The pipelines run in their own goroutines, where an unrecovered panic would
// end the whole app; here the item is failed, the title it held is handed
// back, and the details go to the log.
func (s *Server) runGuarded(item queue.Item, run func()) {
	defer func() {
		if p := recover(); p != nil {
			s.pipelinePanicked(item, p)
		}
	}()
	run()
}

func (s *Server) pipelinePanicked(item queue.Item, p any) {
	slog.Error("queue: a download crashed", "queueId", item.ID, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
	const plain = "Something went wrong while handling this download. Try again."
	s.torrents.stopQueue(item.ID)
	_ = s.QueueRepo.SetStatus(item.ID, queue.StatusFailed, plain)
	s.releaseTitleClaims(item)
	s.queueItemEvent(item, "failed", queue.LevelError, fmt.Sprintf("%s: %s", item.ReleaseTitle, plain))
}

// startRefused settles an item that could not start, for example because its
// movie was removed while it waited. It fails, the next one starts.
func (s *Server) startRefused(item queue.Item, err error) {
	plain := plainerror.Message(err)
	_ = s.QueueRepo.SetStatus(item.ID, queue.StatusFailed, plain)
	s.releaseTitleClaims(item)
	s.queueItemEvent(item, "failed", queue.LevelError, fmt.Sprintf("%s: %s", item.ReleaseTitle, plain))
	slog.Info("queue: a waiting download could not start", "queueId", item.ID, "err", err)
}

// grabbedMessage is what the activity feed says when a release is added: that
// it started, or that it is waiting for a place.
func (s *Server) grabbedMessage(release, title string) string {
	if s.dispatch.FreePlace() {
		return fmt.Sprintf("Started downloading %q for %s", release, title)
	}
	return fmt.Sprintf("Added %q for %s. It is waiting in line.", release, title)
}

// kickDownloads starts waiting downloads if there are free places. It is the
// scheduled safety check (see StartAutomation).
func (s *Server) kickDownloads() { s.dispatch.Kick() }

// waitingPositions numbers the downloads waiting in line from 1, in the order
// they will start.
func waitingPositions(items []queue.Item) map[int64]int {
	var waiting []queue.Item
	for _, it := range items {
		if it.Status == queue.StatusQueued {
			waiting = append(waiting, it)
		}
	}
	sortLine(waiting)
	out := make(map[int64]int, len(waiting))
	for i, it := range waiting {
		out[it.ID] = i + 1
	}
	return out
}

func sortLine(items []queue.Item) {
	sort.SliceStable(items, func(i, j int) bool { return queue.LineOrder(items[i], items[j]) })
}
