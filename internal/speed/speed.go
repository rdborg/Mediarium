// Package speed holds the one download speed limit that Usenet and torrent
// downloads share. With no limit set it costs nothing.
package speed

import (
	"context"
	"sync/atomic"

	"golang.org/x/time/rate"
)

// burst is the most bytes taken in one step. A Usenet article is under 1 MB,
// so one article never has to be split.
const burst = 4 << 20

var (
	limiter = rate.NewLimiter(rate.Inf, burst)
	current atomic.Int64
)

// Limiter is the shared limiter, for the torrent engine's own config.
func Limiter() *rate.Limiter { return limiter }

// Set changes the limit to bytesPerSecond, or removes it when that is 0 or
// less. Downloads that are running follow the new limit at once.
func Set(bytesPerSecond int64) {
	if bytesPerSecond <= 0 {
		current.Store(0)
		limiter.SetLimit(rate.Inf)
		return
	}
	current.Store(bytesPerSecond)
	limiter.SetLimit(rate.Limit(bytesPerSecond))
}

// Current is the limit in bytes per second, 0 for none.
func Current() int64 { return current.Load() }

// Wait holds the caller until n more bytes may be taken under the limit, or
// ctx ends.
func Wait(ctx context.Context, n int) error {
	if current.Load() == 0 {
		return nil
	}
	for n > 0 {
		step := min(n, burst)
		if err := limiter.WaitN(ctx, step); err != nil {
			return err
		}
		n -= step
	}
	return nil
}
