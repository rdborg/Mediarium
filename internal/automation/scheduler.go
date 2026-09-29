// Package automation provides a generic interval-based background job
// runner ("RSS sync + scheduled automatic search/
// grab", "missing/upgrade hunting loop"). It deliberately knows nothing
// about indexers, movies, or grabbing — internal/api wires the actual
// hunt/RSS logic in as a plain func() so this package stays a reusable,
// easily-testable ticker, matching the project's package-boundary rule
// (this package shouldn't import the feature packages it schedules work
// for).
package automation

import (
	"context"
	"log"
	"sync"
	"time"
)

// Job is one named unit of scheduled work.
type Job struct {
	Name     string
	Interval time.Duration
	Run      func(ctx context.Context)
}

// Scheduler runs a set of Jobs on their own tickers until Stop is called.
type Scheduler struct {
	jobs []Job

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewScheduler(jobs ...Job) *Scheduler {
	return &Scheduler{jobs: jobs}
}

// Start launches every job's ticker loop in its own goroutine. Each job
// runs once immediately, then again every Interval, until Stop is called.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return // already started
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	for _, job := range s.jobs {
		s.wg.Add(1)
		go s.runJob(ctx, job)
	}
}

func (s *Scheduler) runJob(ctx context.Context, job Job) {
	defer s.wg.Done()

	runSafely := func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("automation: job %s panicked: %v", job.Name, r)
			}
		}()
		job.Run(ctx)
	}

	runSafely()

	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runSafely()
		}
	}
}

// Stop cancels every job and waits for their current run (if any) to
// return.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	s.wg.Wait()
}
