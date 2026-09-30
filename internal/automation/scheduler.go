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
	// IntervalFunc, when set, is asked for the interval before every wait, so
	// a settings change takes effect without a restart. Zero or negative
	// falls back to Interval.
	IntervalFunc func() time.Duration
	// InitialDelay is how long after Start the first run waits. Heavy jobs
	// use it so a freshly started app answers its first requests before it
	// begins searching and refreshing. Zero runs at once.
	InitialDelay time.Duration
	Run          func(ctx context.Context)
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

// DelayFirstRuns makes every job that has no InitialDelay of its own wait d
// before its first run. Call it before Start.
func (s *Scheduler) DelayFirstRuns(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].InitialDelay == 0 {
			s.jobs[i].InitialDelay = d
		}
	}
}

// Start launches every job's ticker loop in its own goroutine. Each job
// runs once (right away, or after its InitialDelay), then again every
// Interval, until Stop is called.
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

	if job.InitialDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(job.InitialDelay):
		}
	}
	runSafely()

	for {
		if !s.waitInterval(ctx, job) {
			return
		}
		runSafely()
	}
}

// intervalRecheck is how often a waiting job re-reads its interval, so a
// shorter interval saved in Settings is honoured within this long.
var intervalRecheck = 30 * time.Second

// interval is the job's current wait between runs.
func (j Job) interval() time.Duration {
	if j.IntervalFunc != nil {
		if d := j.IntervalFunc(); d > 0 {
			return d
		}
	}
	return j.Interval
}

// waitInterval waits until the job's next run is due, counted from now. It
// reports false when ctx ended first.
func (s *Scheduler) waitInterval(ctx context.Context, job Job) bool {
	start := time.Now()
	for {
		remaining := job.interval() - time.Since(start)
		if remaining <= 0 {
			return true
		}
		if job.IntervalFunc != nil && remaining > intervalRecheck {
			remaining = intervalRecheck
		}
		t := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			t.Stop()
			return false
		case <-t.C:
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
