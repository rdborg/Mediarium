package metadata

import (
	"sync"
	"time"
)

// Discover lists change slowly, and the same pages are asked for again and
// again as people scroll rails back and forth, so list pages are kept for a
// while instead of asking TMDB every time.
const (
	pageCacheTTL     = 10 * time.Minute
	pageCacheEntries = 256
)

// pageCache is a small, size-bounded TTL cache of decoded list pages keyed
// by request path and query (never the API key). Cached values are shared
// between callers and must not be modified. The zero value is ready to use.
type pageCache struct {
	mu      sync.Mutex
	entries map[string]pageCacheEntry
	now     func() time.Time
	// max is how many entries are kept; zero means pageCacheEntries.
	max int
}

type pageCacheEntry struct {
	value   any
	expires time.Time
}

func (p *pageCache) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *pageCache) limit() int {
	if p.max > 0 {
		return p.max
	}
	return pageCacheEntries
}

func (p *pageCache) get(key string) (any, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[key]
	if !ok {
		return nil, false
	}
	if !p.clock().Before(e.expires) {
		delete(p.entries, key)
		return nil, false
	}
	return e.value, true
}

func (p *pageCache) put(key string, value any) { p.putFor(key, value, pageCacheTTL) }

// putFor is put with its own lifetime, for what changes more slowly than lists.
func (p *pageCache) putFor(key string, value any, ttl time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock()
	if p.entries == nil {
		p.entries = map[string]pageCacheEntry{}
	}
	if _, exists := p.entries[key]; !exists && len(p.entries) >= p.limit() {
		// Drop what has expired; if the cache is still full, drop the entry
		// closest to expiring (the oldest).
		var oldestKey string
		var oldest time.Time
		for k, e := range p.entries {
			if !now.Before(e.expires) {
				delete(p.entries, k)
				continue
			}
			if oldestKey == "" || e.expires.Before(oldest) {
				oldestKey, oldest = k, e.expires
			}
		}
		if len(p.entries) >= p.limit() {
			delete(p.entries, oldestKey)
		}
	}
	p.entries[key] = pageCacheEntry{value: value, expires: now.Add(ttl)}
}
