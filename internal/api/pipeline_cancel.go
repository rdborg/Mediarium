package api

import (
	"context"
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
