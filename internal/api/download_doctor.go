package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/queue"
)

// The download doctor (V4 D1). A download whose import failed because of
// something on this computer (a folder Mediarium may not write to, a full
// disk, a network share that dropped away) is tried again by itself a few
// times, from the files it already has, instead of waiting for someone to
// press Retry. A release that is bad goes the other way (blocklisted and
// searched for again, see blocklist.go), so it never comes here.

// doctorWaits is how long each automatic retry waits after a failure: three
// tries in all, then the download is left for a person. A variable for tests.
var doctorWaits = []time.Duration{10 * time.Minute, 30 * time.Minute, 2 * time.Hour}

// doctorForget is how long a download that was retried and then went quiet
// (it worked, or was removed) is remembered.
const doctorForget = 24 * time.Hour

type doctorEntry struct {
	queueID int64     // the failed download waiting for its retry; 0 while the retry runs
	due     time.Time // when it is retried
	tries   int       // automatic retries so far
	seen    time.Time // the last failure
}

type downloadDoctor struct {
	mu       sync.Mutex
	byKey    map[string]*doctorEntry
	progress map[int64]progressMark // running downloads: their progress and since when
}

func (s *Server) doctor() *downloadDoctor {
	s.doctorOnce.Do(func() { s.doctorState = &downloadDoctor{byKey: map[string]*doctorEntry{}} })
	return s.doctorState
}

// doctorKey names what a download is for and which release, so a retry (a
// new queue item) counts against the same tries.
func doctorKey(item queue.Item) string {
	return fmt.Sprintf("%d/%d/%d/%d/%d/%d|%s", item.MovieID, item.SeriesID, item.Season, item.Episode, item.AlbumID, item.BookID, item.ReleaseTitle)
}

// localFault reports whether err is trouble on this computer rather than with
// the release: trying again later can work once it is fixed or comes back.
func localFault(err error) bool {
	if err == nil {
		return false
	}
	switch problems.CodeFor(err, "") {
	case problems.CodeFolderPermission, problems.CodeDiskFull:
		return true
	}
	if errors.Is(err, syscall.ESTALE) || errors.Is(err, syscall.ENOTCONN) || errors.Is(err, syscall.EHOSTDOWN) || errors.Is(err, syscall.EIO) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"stale file handle", "transport endpoint is not connected", "host is down", "input/output error"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// doctorNoteFailure is told about every failed download. One that failed for
// a local reason is put down to be tried again; after the last try it is left
// for a person, with a line in its history that says so.
func (s *Server) doctorNoteFailure(queueID int64, err error) {
	if !localFault(err) || isBadRelease(err) {
		return
	}
	item, gerr := s.QueueRepo.Get(queueID)
	if gerr != nil {
		return
	}
	d := s.doctor()
	d.mu.Lock()
	key := doctorKey(item)
	e := d.byKey[key]
	if e == nil {
		e = &doctorEntry{}
		d.byKey[key] = e
	}
	e.seen = time.Now()
	if e.tries >= len(doctorWaits) {
		delete(d.byKey, key)
		d.mu.Unlock()
		s.queueEvent(queueID, "failed", queue.LevelWarn, fmt.Sprintf("Tried again %d times by itself and it still fails. Fix the cause, then press Retry", e.tries))
		return
	}
	e.queueID, e.due = queueID, time.Now().Add(doctorWaits[e.tries])
	due := e.due
	d.mu.Unlock()
	s.queueEvent(queueID, "failed", queue.LevelInfo, "The trouble is on this computer, not with the release, so Mediarium tries again by itself at "+due.Format("15:04"))
}

// downloadDoctorJob notes the progress of running downloads and runs the
// retries that are due. A download someone already
// retried, removed or blocklisted is left alone.
func (s *Server) downloadDoctorJob(context.Context) {
	d := s.doctor()
	now := time.Now()
	s.watchProgress(now)
	type dueRetry struct {
		key string
		id  int64
	}
	var due []dueRetry
	d.mu.Lock()
	for key, e := range d.byKey {
		switch {
		case e.queueID != 0 && !now.Before(e.due):
			due = append(due, dueRetry{key, e.queueID})
			e.queueID = 0
		case e.queueID == 0 && now.Sub(e.seen) > doctorForget:
			delete(d.byKey, key)
		}
	}
	d.mu.Unlock()

	for _, r := range due {
		item, err := s.QueueRepo.Get(r.id)
		if err != nil || item.Status != queue.StatusFailed {
			d.forget(r.key)
			continue
		}
		retry := s.retryQueueItem
		if s.doctorRetry != nil {
			retry = s.doctorRetry
		}
		newID, err := retry(item)
		if err != nil {
			slog.Warn("download doctor: couldn't try the download again", "queueId", r.id, "err", err)
			d.forget(r.key)
			continue
		}
		d.mu.Lock()
		tries := 0
		if e := d.byKey[r.key]; e != nil {
			e.tries++
			e.seen = time.Now()
			tries = e.tries
		}
		d.mu.Unlock()
		s.queueEvent(newID, "retried", queue.LevelInfo, fmt.Sprintf("Tried again by itself (%d of %d)", tries, len(doctorWaits)))
	}
}

func (d *downloadDoctor) forget(key string) {
	d.mu.Lock()
	delete(d.byKey, key)
	d.mu.Unlock()
}

// stuckAfter is how long a running download may go without any progress
// before Activity says so.
const stuckAfter = 30 * time.Minute

type progressMark struct {
	pct   float64
	since time.Time // when pct was first seen
}

// watchProgress notes the progress of every running download, once a minute,
// so Activity can say when one hasn't moved for a while (in any client: the
// built-in ones, a download client or a debrid service).
func (s *Server) watchProgress(now time.Time) {
	items, err := s.QueueRepo.ListActive()
	if err != nil {
		return
	}
	d := s.doctor()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.progress == nil {
		d.progress = map[int64]progressMark{}
	}
	running := map[int64]bool{}
	for _, it := range items {
		if it.Status != queue.StatusDownloading {
			continue
		}
		running[it.ID] = true
		if m, ok := d.progress[it.ID]; !ok || m.pct != it.ProgressPct {
			d.progress[it.ID] = progressMark{pct: it.ProgressPct, since: now}
		}
	}
	for id := range d.progress {
		if !running[id] {
			delete(d.progress, id)
		}
	}
}

// stuckNote is the sentence for a running download that hasn't moved for a
// while, or "".
func (s *Server) stuckNote(id int64, now time.Time) string {
	d := s.doctor()
	d.mu.Lock()
	m, ok := d.progress[id]
	d.mu.Unlock()
	if !ok || now.Sub(m.since) < stuckAfter {
		return ""
	}
	if h := int(now.Sub(m.since).Hours()); h >= 2 {
		return fmt.Sprintf("Nothing has moved for %d hours. Check the download client or your Usenet servers, or try another release.", h)
	}
	return fmt.Sprintf("Nothing has moved for %d minutes. Check the download client or your Usenet servers, or try another release.", int(now.Sub(m.since).Minutes()))
}
