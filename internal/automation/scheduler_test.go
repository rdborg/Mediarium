package automation_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/automation"
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
