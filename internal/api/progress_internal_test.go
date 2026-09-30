package api

import (
	"sync"
	"testing"
	"time"
)

func TestProgressSaverWritesAtMostOncePerInterval(t *testing.T) {
	type report struct {
		at          time.Duration // time since start
		done, total int64
	}
	tests := []struct {
		name    string
		reports []report
		flush   bool
		want    []float64 // percentages saved, in order
	}{
		{
			name:    "a burst is saved once",
			reports: []report{{0, 1, 100}, {10 * time.Millisecond, 2, 100}, {20 * time.Millisecond, 3, 100}},
			want:    []float64{1},
		},
		{
			name:    "the next one after the interval is saved",
			reports: []report{{0, 1, 100}, {500 * time.Millisecond, 2, 100}, {1100 * time.Millisecond, 3, 100}},
			want:    []float64{1, 3},
		},
		{
			name:    "the same percentage is not saved twice",
			reports: []report{{0, 5, 100}, {2 * time.Second, 5, 100}, {4 * time.Second, 5, 100}},
			want:    []float64{5},
		},
		{
			name:    "flush saves the last value that was skipped",
			reports: []report{{0, 1, 100}, {10 * time.Millisecond, 99, 100}},
			flush:   true,
			want:    []float64{1, 99},
		},
		{
			name:    "flush after a saved value writes nothing more",
			reports: []report{{0, 40, 100}},
			flush:   true,
			want:    []float64{40},
		},
		{
			name:    "an unknown total saves zero",
			reports: []report{{0, 500, 0}},
			want:    []float64{0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			var now time.Time
			var saved []float64
			p := newProgressSaver(func(pct float64) error {
				saved = append(saved, pct)
				return nil
			})
			p.now = func() time.Time { return now }
			for _, r := range tc.reports {
				now = start.Add(r.at)
				p.Report(r.done, r.total)
			}
			if tc.flush {
				p.Flush()
			}
			if len(saved) != len(tc.want) {
				t.Fatalf("saved %v, want %v", saved, tc.want)
			}
			for i := range saved {
				if saved[i] != tc.want[i] {
					t.Fatalf("saved %v, want %v", saved, tc.want)
				}
			}
		})
	}
}

// Many download connections report at once; the writes stay rare.
func TestProgressSaverIsSafeForManyConnections(t *testing.T) {
	var mu sync.Mutex
	writes := 0
	p := newProgressSaver(func(float64) error {
		mu.Lock()
		writes++
		mu.Unlock()
		return nil
	})
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int64(1); i <= 500; i++ {
				p.Report(i, 500)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if writes > 3 {
		t.Fatalf("%d database writes for 8000 reports in well under a second", writes)
	}
}
