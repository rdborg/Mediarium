package auth

import (
	"sync"
	"time"
)

// LoginLimiter blunts brute-force login attempts ("a
// handful of attempts per IP/window") with a simple in-memory sliding
// window. Deliberately not persisted to the DB: a restart resetting
// attempt counts is an acceptable tradeoff for avoiding a write on every
// single login attempt, and this only needs to survive for the duration
// of an actual brute-force burst, not across restarts.
type LoginLimiter struct {
	mu          sync.Mutex
	attempts    map[string][]time.Time
	maxAttempts int
	window      time.Duration
}

func NewLoginLimiter(maxAttempts int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{
		attempts:    make(map[string][]time.Time),
		maxAttempts: maxAttempts,
		window:      window,
	}
}

// Allow reports whether key (typically a client IP) is still under the
// attempt limit for the current window.
func (l *LoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	return len(l.attempts[key]) < l.maxAttempts
}

// RecordFailure counts one failed attempt against key.
func (l *LoginLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	l.attempts[key] = append(l.attempts[key], time.Now())
}

// RecordSuccess clears key's attempt history — a successful login isn't
// itself an attack signal, and shouldn't count toward the next window.
func (l *LoginLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// prune drops attempts older than the window. Caller must hold l.mu.
func (l *LoginLimiter) prune(key string) {
	cutoff := time.Now().Add(-l.window)
	kept := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.attempts, key)
	} else {
		l.attempts[key] = kept
	}
}
