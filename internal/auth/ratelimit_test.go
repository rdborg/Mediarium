package auth_test

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
)

func TestLoginLimiterBlocksAfterMaxAttempts(t *testing.T) {
	limiter := auth.NewLoginLimiter(3, time.Minute)
	key := "1.2.3.4"

	for i := 0; i < 3; i++ {
		if !limiter.Allow(key) {
			t.Fatalf("expected attempt %d to be allowed", i+1)
		}
		limiter.RecordFailure(key)
	}
	if limiter.Allow(key) {
		t.Fatal("expected the 4th attempt to be blocked")
	}
}

func TestLoginLimiterSuccessClearsHistory(t *testing.T) {
	limiter := auth.NewLoginLimiter(2, time.Minute)
	key := "1.2.3.4"

	limiter.RecordFailure(key)
	limiter.RecordFailure(key)
	if limiter.Allow(key) {
		t.Fatal("expected to be blocked after 2 failures with a limit of 2")
	}

	limiter.RecordSuccess(key)
	if !limiter.Allow(key) {
		t.Fatal("expected a successful login to clear the attempt history")
	}
}

func TestLoginLimiterWindowExpires(t *testing.T) {
	limiter := auth.NewLoginLimiter(1, 20*time.Millisecond)
	key := "1.2.3.4"

	limiter.RecordFailure(key)
	if limiter.Allow(key) {
		t.Fatal("expected to be blocked immediately after 1 failure with a limit of 1")
	}

	time.Sleep(40 * time.Millisecond)
	if !limiter.Allow(key) {
		t.Fatal("expected the window to have expired, allowing another attempt")
	}
}

func TestLoginLimiterKeysAreIndependent(t *testing.T) {
	limiter := auth.NewLoginLimiter(1, time.Minute)
	limiter.RecordFailure("1.2.3.4")
	if !limiter.Allow("5.6.7.8") {
		t.Fatal("expected a different key to be unaffected by another key's failures")
	}
}
