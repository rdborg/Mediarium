package mediaservers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Pending holds sign-ins in progress (a Plex PIN, a Quick Connect code) in
// memory only, each for a limited time. What they hold (account tokens,
// Quick Connect secrets) never goes to the browser or the database.
type Pending[T any] struct {
	TTL time.Duration
	Now func() time.Time // time.Now; tests replace it
	Max int              // most entries kept at once; the oldest go first (default 64)

	mu    sync.Mutex
	items map[string]pendingEntry[T]
}

type pendingEntry[T any] struct {
	value   T
	added   time.Time
	expires time.Time
}

func (p *Pending[T]) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// sweep drops expired entries; p.mu is held.
func (p *Pending[T]) sweep(now time.Time) {
	for k, e := range p.items {
		if !now.Before(e.expires) {
			delete(p.items, k)
		}
	}
}

// Put stores v under key, replacing what was there.
func (p *Pending[T]) Put(key string, v T) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.items == nil {
		p.items = map[string]pendingEntry[T]{}
	}
	p.sweep(now)
	max := p.Max
	if max <= 0 {
		max = 64
	}
	for len(p.items) >= max {
		oldest, when := "", time.Time{}
		for k, e := range p.items {
			if oldest == "" || e.added.Before(when) {
				oldest, when = k, e.added
			}
		}
		delete(p.items, oldest)
	}
	ttl := p.TTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	p.items[key] = pendingEntry[T]{value: v, added: now, expires: now.Add(ttl)}
}

// Get returns the value under key, unless it expired.
func (p *Pending[T]) Get(key string) (T, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var zero T
	e, ok := p.items[key]
	if !ok {
		return zero, false
	}
	if !p.now().Before(e.expires) {
		delete(p.items, key)
		return zero, false
	}
	return e.value, true
}

// Take returns the value under key and removes it, so only one caller gets it.
func (p *Pending[T]) Take(key string) (T, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var zero T
	e, ok := p.items[key]
	if !ok {
		return zero, false
	}
	delete(p.items, key)
	if !p.now().Before(e.expires) {
		return zero, false
	}
	return e.value, true
}

// Delete forgets key.
func (p *Pending[T]) Delete(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.items, key)
}

// NewID returns a random id for a pending sign-in.
func NewID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("make a random id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
