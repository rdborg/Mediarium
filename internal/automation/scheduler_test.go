package automation_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/automation"
)

func TestSchedulerRunsImmediatelyAndRepeatedly(t *testing.T) {
	var count int64
	job := automation.Job{
		Name:     "test-job",
		Interval: 20 * time.Millisecond,
		Run: func(ctx context.Context) {
			atomic.AddInt64(&count, 1)
		},
	}
	sched := automation.NewScheduler(job)
	sched.Start()
	defer sched.Stop()

	// Should have run at least once immediately, without waiting a full tick.
	deadline := time.Now().Add(200 * time.Millisecond)
	for atomic.LoadInt64(&count) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&count); got < 3 {
		t.Fatalf("expected job to run at least 3 times within 200ms, ran %d times", got)
	}
}

func TestSchedulerStopWaitsForInFlightRun(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	job := automation.Job{
		Name:     "slow-job",
		Interval: time.Hour, // won't fire a second time during this test
		Run: func(ctx context.Context) {
			close(started)
			time.Sleep(50 * time.Millisecond)
			close(finished)
		},
	}
	sched := automation.NewScheduler(job)
	sched.Start()

	<-started
	sched.Stop() // should block until the in-flight run completes

	select {
	case <-finished:
	default:
		t.Fatal("expected Stop() to wait for the in-flight run to finish")
	}
}

func TestSchedulerJobPanicDoesNotKillScheduler(t *testing.T) {
	var count int64
	job := automation.Job{
		Name:     "panicky-job",
		Interval: 15 * time.Millisecond,
		Run: func(ctx context.Context) {
			atomic.AddInt64(&count, 1)
			panic("boom")
		},
	}
	sched := automation.NewScheduler(job)
	sched.Start()
	defer sched.Stop()

	deadline := time.Now().Add(200 * time.Millisecond)
	for atomic.LoadInt64(&count) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&count); got < 3 {
		t.Fatalf("expected the scheduler to keep retrying after a panic, only ran %d times", got)
	}
}

func TestSchedulerWaitsForInitialDelay(t *testing.T) {
	var runs atomic.Int32
	s := automation.NewScheduler(automation.Job{Name: "late", Interval: time.Hour, InitialDelay: 150 * time.Millisecond, Run: func(context.Context) { runs.Add(1) }})
	s.Start()
	defer s.Stop()
	time.Sleep(50 * time.Millisecond)
	if n := runs.Load(); n != 0 {
		t.Fatalf("job ran %d times before its delay was over", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runs.Load() != 1 {
		t.Fatalf("job ran %d times after its delay, want 1", runs.Load())
	}
}

func TestSchedulerStopsDuringInitialDelay(t *testing.T) {
	var runs atomic.Int32
	s := automation.NewScheduler(automation.Job{Name: "late", Interval: time.Hour, InitialDelay: time.Hour, Run: func(context.Context) { runs.Add(1) }})
	s.Start()
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return while a job was waiting out its delay")
	}
	if runs.Load() != 0 {
		t.Fatal("job ran")
	}
}

func TestSchedulerDelayFirstRunsKeepsAJobsOwnDelay(t *testing.T) {
	var early, late atomic.Int32
	s := automation.NewScheduler(
		automation.Job{Name: "own", Interval: time.Hour, InitialDelay: 10 * time.Millisecond, Run: func(context.Context) { early.Add(1) }},
		automation.Job{Name: "default", Interval: time.Hour, Run: func(context.Context) { late.Add(1) }},
	)
	s.DelayFirstRuns(time.Hour)
	s.Start()
	defer s.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for early.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if early.Load() != 1 {
		t.Fatalf("the job with its own delay ran %d times, want 1", early.Load())
	}
	if late.Load() != 0 {
		t.Fatal("a job without its own delay ran before the general delay was over")
	}
}
