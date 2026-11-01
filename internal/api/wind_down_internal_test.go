package api

import (
	"sync/atomic"
	"testing"
	"time"
)

// Pausing or stopping many downloads waits for them together, not one by one.
func TestEachAtOnceWaitsTogether(t *testing.T) {
	var done atomic.Int64
	start := time.Now()
	eachAtOnce(windDownAtOnce, func(int) {
		time.Sleep(200 * time.Millisecond)
		done.Add(1)
	})
	if done.Load() != windDownAtOnce {
		t.Fatalf("%d of %d ran", done.Load(), windDownAtOnce)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %s: the waits ran one after another", d)
	}
}
