package metadata

import (
	"strconv"
	"testing"
	"time"
)

func TestPageCacheExpiryAndBound(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	p := &pageCache{now: func() time.Time { return now }}

	p.put("a", 1)
	if v, ok := p.get("a"); !ok || v.(int) != 1 {
		t.Fatalf("fresh entry: %v %v", v, ok)
	}
	now = now.Add(pageCacheTTL)
	if _, ok := p.get("a"); ok {
		t.Fatal("an entry should expire after the TTL")
	}

	// Filling past the bound drops the oldest entry and keeps the size fixed.
	for i := 0; i < pageCacheEntries; i++ {
		now = now.Add(time.Millisecond)
		p.put(strconv.Itoa(i), i)
	}
	now = now.Add(time.Millisecond)
	p.put("new", -1)
	if len(p.entries) != pageCacheEntries {
		t.Fatalf("cache holds %d entries, want %d", len(p.entries), pageCacheEntries)
	}
	if _, ok := p.get("0"); ok {
		t.Fatal("the oldest entry should have been dropped")
	}
	if _, ok := p.get("new"); !ok {
		t.Fatal("the newest entry should be kept")
	}
}
