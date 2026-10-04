package speed

import (
	"context"
	"testing"
	"time"
)

func TestWaitFollowsTheLimit(t *testing.T) {
	cases := []struct {
		name    string
		limit   int64
		bytes   int
		atLeast time.Duration
		atMost  time.Duration
	}{
		{"no limit is instant", 0, 50 << 20, 0, 50 * time.Millisecond},
		{"a limit slows a large read down", 1 << 20, 2 << 20, 1500 * time.Millisecond, 4 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Set(tc.limit)
			t.Cleanup(func() { Set(0) })
			// Use up the starting burst so the test measures the steady rate.
			_ = Wait(context.Background(), burst)
			start := time.Now()
			if err := Wait(context.Background(), tc.bytes); err != nil {
				t.Fatal(err)
			}
			took := time.Since(start)
			if took < tc.atLeast || took > tc.atMost {
				t.Fatalf("took %v, want between %v and %v", took, tc.atLeast, tc.atMost)
			}
		})
	}
}

func TestWaitStopsWithTheContext(t *testing.T) {
	Set(1)
	t.Cleanup(func() { Set(0) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := Wait(ctx, 10<<20); err == nil {
		t.Fatal("a cancelled wait should return an error")
	}
	if Current() != 1 {
		t.Fatalf("current = %d", Current())
	}
}
