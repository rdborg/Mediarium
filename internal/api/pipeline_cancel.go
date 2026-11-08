package api

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// pipelineRegistry tracks the download pipelines that are running, so one
// can be cancelled when its movie or show is removed from the library. The
// zero value is ready to use.
type pipelineRegistry struct {
	mu      sync.Mutex
	running map[int64]*pipelineRun
	down    bool // set by shutdown: pipelines that start after it end at once
}

// Shutdown is called once when Mediarium is stopping, after the web server has
// stopped taking requests and before the database is closed. It ends the running
// download pipelines and the torrent engine without marking anything failed:
// what was downloading stays as it is, and the next start finds it interrupted.
//
// wait is all the time it may take, torrents and pipelines together: the stop
// has to fit in the 10 seconds Docker gives before it kills the container.
// Closing a torrent engine can hang on a bad connection, so it gets half of
// it at most and is left behind after that; the program is exiting anyway.
func (s *Server) Shutdown(wait time.Duration) {
	deadline := time.Now().Add(wait)
	if s.resumeStop != nil {
		s.resumeStop()
	}
	// Nothing new starts, and a download that ends while the rest wind down
	// does not look for the next one in a database that is about to close.
	if s.dispatch != nil {
		s.dispatch.Stop()
	}
	s.archiverStop(time.Second) // ends a running Channel archiver check; yt-dlp is stopped with it
	s.pipelines.shutdown(0) // flag and cancel them first, so stopping the torrents is not read as a failure
	stopped := make(chan struct{})
	go func() {
		s.torrents.stopAll()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(wait / 2):
		slog.Warn("shutdown: the torrent engine was slow to stop; closing anyway")
	}
	s.pipelines.shutdown(max(time.Until(deadline), 0))
}

// shutdown tells every running pipeline that Mediarium is stopping, cancels
// them, and waits up to wait for them to wind down. Items they were working on
// keep their status, so the next start sees them as interrupted.
func (r *pipelineRegistry) shutdown(wait time.Duration) {
	r.mu.Lock()
	r.down = true
	runs := make([]*pipelineRun, 0, len(r.running))
	for _, run := range r.running {
		run.shutdown.Store(true)
		run.cancel()
		runs = append(runs, run)
	}
	r.mu.Unlock()
	deadline := time.After(wait)
	for _, run := range runs {
		select {
		case <-run.done:
		case <-deadline:
			return
		}
	}
}

type pipelineRun struct {
	cancel    context.CancelFunc
	done      chan struct{}
	cancelled atomic.Bool
	// paused and stopped say a person interrupted the download: the pipeline
	// ends without failing, and the item is left paused (files kept) or
	// stopped. deleteFiles makes a stop also delete the partial download.
	paused      atomic.Bool
	stopped     atomic.Bool
	deleteFiles atomic.Bool
	// shutdown says Mediarium itself is stopping: the pipeline ends and leaves
	// the item as it is, so the next start finds it interrupted (see
	// Repo.RecoverInterrupted) instead of failed.
	shutdown atomic.Bool
}

// begin registers the pipeline of queueID and returns the context it runs
// under; call end when it finishes.
func (r *pipelineRegistry) begin(queueID int64) (context.Context, *pipelineRun) {
	ctx, cancel := context.WithCancel(context.Background())
	run := &pipelineRun{cancel: cancel, done: make(chan struct{})}
	r.mu.Lock()
	if r.running == nil {
		r.running = map[int64]*pipelineRun{}
	}
	r.running[queueID] = run
	if r.down {
		run.shutdown.Store(true)
		cancel()
	}
	r.mu.Unlock()
	return ctx, run
}

// end unregisters a finished pipeline. Its context is deliberately left
// alive: a torrent keeps seeding under it after the pipeline has imported
// it (the torrent registry stops it).
func (r *pipelineRegistry) end(queueID int64, run *pipelineRun) {
	r.mu.Lock()
	if r.running[queueID] == run {
		delete(r.running, queueID)
	}
	r.mu.Unlock()
	close(run.done)
}

// running reports whether queueID's pipeline is still running.
func (r *pipelineRegistry) isRunning(queueID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[queueID] != nil
}

// cancel stops queueID's pipeline, if one is running, and waits up to wait
// for it to finish. It reports whether a pipeline was running.
func (r *pipelineRegistry) cancel(queueID int64, wait time.Duration) bool {
	r.mu.Lock()
	run := r.running[queueID]
	r.mu.Unlock()
	if run == nil {
		return false
	}
	run.cancelled.Store(true)
	run.cancel()
	select {
	case <-run.done:
	case <-time.After(wait):
	}
	return true
}

// interrupt pauses or stops queueID's pipeline, if one is running, and waits up
// to wait for it to wind down. It reports whether a pipeline was running. The
// pipeline itself leaves the item paused or stopped (settleInterrupted).
func (r *pipelineRegistry) interrupt(queueID int64, stop, deleteFiles bool, wait time.Duration) bool {
	r.mu.Lock()
	run := r.running[queueID]
	if run != nil {
		if stop {
			run.stopped.Store(true)
			run.deleteFiles.Store(deleteFiles)
		} else {
			run.paused.Store(true)
		}
	}
	r.mu.Unlock()
	if run == nil {
		return false
	}
	run.cancel()
	select {
	case <-run.done:
	case <-time.After(wait):
	}
	return true
}

// pending says what a running pipeline is being asked to do: "pausing",
// "stopping", or "" when nothing.
func (r *pipelineRegistry) pending(queueID int64) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.running[queueID]
	switch {
	case run == nil:
		return ""
	case run.stopped.Load():
		return "stopping"
	case run.paused.Load():
		return "pausing"
	}
	return ""
}
