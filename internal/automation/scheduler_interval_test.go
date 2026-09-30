package automation

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntervalFuncIsReadAgainWhileWaiting(t *testing.T) {
	old := intervalRecheck
	intervalRecheck = 5 * time.Millisecond
	defer func() { intervalRecheck = old }()

	var interval atomic.Int64
	interval.Store(int64(time.Hour))
	var runs atomic.Int64
	sched := NewScheduler(Job{
		Name:         "adjustable",
		Interval:     time.Hour,
		IntervalFunc: func() time.Duration { return time.Duration(interval.Load()) },
		Run:          func(context.Context) { runs.Add(1) },
	})
	sched.Start()
	defer sched.Stop()

	waitFor(t, func() bool { return runs.Load() >= 1 }) // the first run is immediate
	time.Sleep(30 * time.Millisecond)
	if got := runs.Load(); got != 1 {
		t.Fatalf("ran %d times while the interval was an hour, want 1", got)
	}

	interval.Store(int64(10 * time.Millisecond)) // saved in Settings: no restart
	waitFor(t, func() bool { return runs.Load() >= 3 })
}

func TestJobIntervalFallsBack(t *testing.T) {
	tests := []struct {
		name string
		job  Job
		want time.Duration
	}{
		{"fixed", Job{Interval: time.Minute}, time.Minute},
		{"from the function", Job{Interval: time.Minute, IntervalFunc: func() time.Duration { return time.Hour }}, time.Hour},
		{"function gives zero", Job{Interval: time.Minute, IntervalFunc: func() time.Duration { return 0 }}, time.Minute},
		{"function gives negative", Job{Interval: time.Minute, IntervalFunc: func() time.Duration { return -time.Second }}, time.Minute},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.job.interval(); got != tc.want {
				t.Fatalf("interval() = %v, want %v", got, tc.want)
			}
		})
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
