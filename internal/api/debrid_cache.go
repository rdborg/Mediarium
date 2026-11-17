package api

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/debrid"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/settings"
)

// Cached releases. When the debrid service is TorBox or Premiumize, Mediarium asks it once
// per search which of the torrents found it already has, so the release list
// can say "Cached" and automatic searches can take a ready release over an
// equal one that has to be downloaded first. Real-Debrid has no such check.
//
// Asking never gets in the way: a slow or failing service means "unknown", the
// answer is remembered for a few minutes, and after a failure the service is
// left alone for a minute.

const (
	cachedTTL      = 5 * time.Minute  // how long an answer is remembered
	cachedPause    = time.Minute      // how long the service is left alone after a failure
	cachedTimeout  = 5 * time.Second  // the most a search waits for the answer
	cachedMemoSize = 4000             // hashes remembered at most
	cachedLogEvery = 10 * time.Minute // a failed check is logged at most this often
)

// cachedOverScore says a cached release outranks an uncached one of the same
// quality even when the profile's preferred terms score it lower. Set from
// Settings > Downloading > Debrid service; the picking code reads it.
var cachedOverScore atomic.Bool

type cachedEntry struct {
	cached bool
	at     time.Time
}

// cachedMemo remembers what the service said about each info hash.
type cachedMemo struct {
	mu        sync.Mutex
	seen      map[string]cachedEntry
	pausedTil time.Time
	lastLog   time.Time
}

// get returns the remembered answers for hashes and the hashes still to ask.
func (m *cachedMemo) get(hashes []string, now time.Time) (known map[string]bool, missing []string, paused bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	known = make(map[string]bool, len(hashes))
	for _, h := range hashes {
		if e, ok := m.seen[h]; ok && now.Sub(e.at) < cachedTTL {
			known[h] = e.cached
		} else {
			missing = append(missing, h)
		}
	}
	return known, missing, now.Before(m.pausedTil)
}

func (m *cachedMemo) put(answers map[string]bool, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen == nil || len(m.seen)+len(answers) > cachedMemoSize {
		keep := make(map[string]cachedEntry)
		for h, e := range m.seen {
			if now.Sub(e.at) < cachedTTL && len(keep) < cachedMemoSize/2 {
				keep[h] = e
			}
		}
		m.seen = keep
	}
	for h, c := range answers {
		m.seen[h] = cachedEntry{cached: c, at: now}
	}
}

// fail pauses asking and says whether this failure is worth a log line.
func (m *cachedMemo) fail(now time.Time) (log bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pausedTil = now.Add(cachedPause)
	if now.Sub(m.lastLog) < cachedLogEvery {
		return false
	}
	m.lastLog = now
	return true
}

// setCachedPolicy reads the "accept a lower score for a cached release" setting
// into the flag the picking code uses.
func (s *Server) setCachedPolicy() {
	v, _ := s.Settings.Get(settings.KeyDebridCachedLower)
	cachedOverScore.Store(v == "1")
}

// cacheChecker is the debrid service when it is set, has a key, can say what
// it has cached, and the person has not switched the check off.
func (s *Server) cacheChecker() debrid.CacheChecker {
	if s.Settings == nil {
		return nil
	}
	service, key := s.debridConfig()
	if service == "" || key == "" {
		return nil
	}
	if v, _ := s.Settings.Get(settings.KeyDebridPreferCached); v == "0" {
		return nil
	}
	svc, err := s.debridService(service, key)
	if err != nil {
		return nil
	}
	cc, _ := svc.(debrid.CacheChecker)
	return cc
}

// preferCachedOn reports whether the cache check is in use right now.
func (s *Server) preferCachedOn() bool { return s.cacheChecker() != nil }

// cachedAnswers returns, for the info hashes the service has said something
// about, whether each is cached. Hashes it could not be asked about are left
// out ("unknown"). It never takes longer than cachedTimeout and never fails.
func (s *Server) cachedAnswers(ctx context.Context, hashes []string) map[string]bool {
	if len(hashes) == 0 {
		return nil
	}
	cc := s.cacheChecker()
	if cc == nil {
		return nil
	}
	now := time.Now()
	known, missing, paused := s.debridCache.get(hashes, now)
	if len(missing) == 0 || paused {
		return known
	}
	ctx, cancel := context.WithTimeout(ctx, cachedTimeout)
	defer cancel()
	got, err := cc.Cached(ctx, missing)
	if err != nil {
		if s.debridCache.fail(time.Now()) {
			slog.Warn("debrid: couldn't ask which releases are cached, carrying on without", "err", err)
		}
		return known
	}
	s.debridCache.put(got, time.Now())
	for h, c := range got {
		known[h] = c
	}
	return known
}

var (
	hex40Re  = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	btihHash = regexp.MustCompile(`(?i)xt=urn:btih:([a-z0-9]+)`)
)

// releaseHash is the torrent's info hash in lower-case hex, from the indexer's
// own info hash or the magnet link, "" when the result has neither. A .torrent
// link is not downloaded to find out: that would cost a request per release.
func releaseHash(res indexers.Result) string {
	if res.Protocol != indexers.ProtocolTorrent && !res.Torrent {
		return ""
	}
	if h := normalizeHash(res.InfoHash); h != "" {
		return h
	}
	if strings.HasPrefix(strings.ToLower(res.DownloadURL), "magnet:") {
		if m := btihHash.FindStringSubmatch(res.DownloadURL); m != nil {
			return normalizeHash(m[1])
		}
	}
	return ""
}

// normalizeHash accepts a 40 character hex hash or the 32 character base32
// form some magnet links use, and returns lower-case hex ("" for anything else).
func normalizeHash(h string) string {
	h = strings.TrimSpace(h)
	switch {
	case hex40Re.MatchString(h):
		return strings.ToLower(h)
	case len(h) == 32:
		raw, err := base32.StdEncoding.DecodeString(strings.ToUpper(h))
		if err != nil || len(raw) != 20 {
			return ""
		}
		return hex.EncodeToString(raw)
	}
	return ""
}

// markCached returns results with Cached set on the torrents the service
// already has. It leaves the slice it was given alone. With fewer than two
// torrents to tell apart there is nothing to rank, so it asks nobody (the
// release list asks anyway, to show the tag: see markCachedPayloads).
func (s *Server) markCached(ctx context.Context, results []indexers.Result) []indexers.Result {
	if !s.preferCachedOn() {
		return results
	}
	var hashes []string
	torrents := 0
	for _, res := range results {
		if h := releaseHash(res); h != "" {
			hashes = append(hashes, h)
			torrents++
		}
	}
	if torrents < 2 {
		return results
	}
	answers := s.cachedAnswers(ctx, hashes)
	var out []indexers.Result
	for i, res := range results {
		if !answers[releaseHash(res)] || res.Cached {
			continue
		}
		if out == nil {
			out = append([]indexers.Result(nil), results...)
		}
		out[i].Cached = true
	}
	if out == nil {
		return results
	}
	return out
}

// markCachedPayloads sets Cached on the rows of an interactive search with one
// question for the whole list.
func (s *Server) markCachedPayloads(ctx context.Context, rows []searchResultPayload) {
	if !s.preferCachedOn() {
		return
	}
	var hashes []string
	for _, p := range rows {
		if p.hash != "" {
			hashes = append(hashes, p.hash)
		}
	}
	answers := s.cachedAnswers(ctx, hashes)
	for i := range rows {
		rows[i].Cached = answers[rows[i].hash]
		if rows[i].Cached {
			rows[i].BelowMinSeeders = false // a release the service has needs no seeders
		}
	}
}

// preferOver reports whether candidate should replace best when the two have
// the same pickKey. The indexer's priority and the preferred protocol come
// first, as they always did; a cached release wins what is left.
func preferOver(best, candidate *indexers.Result) bool {
	// The same name posted twice: the later post is the one that was fixed.
	if newer := newerSameName(best, candidate); newer != nil {
		return newer == candidate
	}
	if preferUsenet(best, candidate) {
		return true
	}
	if preferUsenet(candidate, best) {
		return false
	}
	if best == nil {
		return false
	}
	if candidate.Cached != best.Cached {
		return candidate.Cached
	}
	// Two torrents that are otherwise the same: the one with more seeders.
	return candidate.SeedersKnown && best.SeedersKnown && candidate.Seeders > best.Seeders
}
