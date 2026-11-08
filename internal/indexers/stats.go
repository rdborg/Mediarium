package indexers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
)

// Per-indexer counters for the Statistics page: how many searches each
// indexer got, how many failed, how many releases it returned and how many of
// its releases were grabbed. One row per indexer per day.

const dayFmt = "2006-01-02"

// StatsKeepDays is how long the daily counters are kept.
const StatsKeepDays = 90

// Stat is one indexer's counters over a period.
type Stat struct {
	IndexerID int64
	Name      string
	Queries   int
	Failures  int
	Results   int
	Grabs     int
	GrabBytes int64 // the size of what was grabbed
}

// SuccessRate is the share of searches that got an answer, 0 to 1. It is -1
// when there were no searches.
func (s Stat) SuccessRate() float64 {
	if s.Queries == 0 {
		return -1
	}
	ok := s.Queries - s.Failures
	if ok < 0 {
		ok = 0
	}
	return float64(ok) / float64(s.Queries)
}

func (t *Tracker) bump(id int64, queries, failures, results, grabs int) {
	t.bumpBytes(id, queries, failures, results, grabs, 0)
}

// bumpBytes is bump with the size of what was grabbed.
func (t *Tracker) bumpBytes(id int64, queries, failures, results, grabs int, grabBytes int64) {
	if t == nil || t.db == nil {
		return
	}
	day := t.now().UTC().Format(dayFmt)
	t.mu.Lock()
	prune := t.prunedOn != day
	t.prunedOn = day
	t.mu.Unlock()
	if prune { // once a day
		t.Prune()
	}
	_, err := t.db.Exec(`INSERT INTO indexer_stats (indexer_id, day, queries, failures, results, grabs, grab_bytes) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(indexer_id, day) DO UPDATE SET queries = queries + excluded.queries, failures = failures + excluded.failures,
		  results = results + excluded.results, grabs = grabs + excluded.grabs, grab_bytes = grab_bytes + excluded.grab_bytes`,
		id, day, queries, failures, results, grabs, max(grabBytes, 0))
	if err != nil {
		slog.Warn("indexers: count indexer search", "indexer", id, "err", err)
	}
}

// RecordSearch counts one search of an indexer: how many releases it gave, or
// the error when it failed. It also keeps the back-off up to date.
func (t *Tracker) RecordSearch(id int64, results []Result, err error) {
	if t == nil {
		return
	}
	if err != nil {
		// Over the daily limit is not a failure: the indexer works, it is used
		// up for today. It is left out until its requests are back.
		if t.LearnLimit(id, err.Error()) {
			t.bump(id, 1, 0, 0, 0)
			return
		}
		t.Failed(id, err)
		t.noteFailed(id)
		t.bump(id, 1, 1, 0, 0)
		return
	}
	t.Succeeded(id)
	t.noteAnswered(id)
	t.bump(id, 1, 0, len(results), 0)
	t.remember(id, results)
}

// RecordLaterPage takes in what a later page of a search that did answer said:
// a daily limit it reports is learned and "too many requests" pauses the
// indexer, as on a first page. Other errors there (a page that times out)
// say little about the indexer and are left alone.
func (t *Tracker) RecordLaterPage(id int64, err error) {
	if t == nil || err == nil {
		return
	}
	if t.LearnLimit(id, err.Error()) {
		return
	}
	var se *StatusError
	if problems.IndexerCode(err) == problems.CodeIndexerRateLimited || (errors.As(err, &se) && se.Status == http.StatusTooManyRequests) {
		t.Failed(id, err)
	}
}

func (t *Tracker) remember(id int64, results []Result) {
	if len(results) == 0 {
		return
	}
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range results {
		if r.DownloadURL != "" {
			exp := now.Add(sourceTTL)
			t.sources[r.DownloadURL] = sourceNote{id: id, expiry: exp}
			t.order = append(t.order, sourceKey{url: r.DownloadURL, expiry: exp})
		}
	}
	t.dropOldSources(now)
}

// dropOldSources forgets the oldest notes while they have expired or there
// are too many. Notes are kept in the order they were made, which is also the
// order they expire in, so each one is looked at once: no scan of the map.
func (t *Tracker) dropOldSources(now time.Time) {
	for t.orderHead < len(t.order) {
		k := t.order[t.orderHead]
		live := len(t.order) - t.orderHead
		if !now.After(k.expiry) && len(t.sources) <= maxSource && live <= 2*maxSource {
			break
		}
		// a URL noted again later has a newer note: only drop the one this is
		if n, ok := t.sources[k.url]; ok && n.expiry.Equal(k.expiry) {
			delete(t.sources, k.url)
		}
		t.order[t.orderHead] = sourceKey{}
		t.orderHead++
	}
	// reuse the space of the dropped notes once they are half of it
	if t.orderHead > 0 && t.orderHead*2 >= len(t.order) {
		t.order = append(t.order[:0], t.order[t.orderHead:]...)
		t.orderHead = 0
	}
}

// SourceOf is the indexer that gave the release at url, when a search in the
// last day found it. ok is false for a release the tracker never saw (or saw
// too long ago, or before a restart).
func (t *Tracker) SourceOf(url string) (id int64, ok bool) {
	if t == nil || url == "" {
		return 0, false
	}
	t.mu.Lock()
	n, found := t.sources[url]
	t.mu.Unlock()
	if !found || t.now().After(n.expiry) {
		return 0, false
	}
	return n.id, true
}

// NoteGrab counts a grab for the indexer that gave the release at url, with
// its size (0 when not known). A release from a search long ago, or one the
// tracker never saw, is not counted.
func (t *Tracker) NoteGrab(url string, size int64) {
	if t == nil || url == "" {
		return
	}
	t.mu.Lock()
	n, ok := t.sources[url]
	t.mu.Unlock()
	if !ok || t.now().After(n.expiry) {
		return
	}
	t.bumpBytes(n.id, 0, 0, 0, 1, size)
	t.CountRequest(n.id, 1) // the release file is fetched from the indexer
}

// Stats adds up the counters of every indexer since the given day (UTC). An
// indexer with no searches in the period is listed with zeros.
func (t *Tracker) Stats(since time.Time) ([]Stat, error) {
	if t == nil || t.db == nil {
		return nil, nil
	}
	rows, err := t.db.Query(`SELECT i.id, i.name, COALESCE(SUM(s.queries), 0), COALESCE(SUM(s.failures), 0), COALESCE(SUM(s.results), 0), COALESCE(SUM(s.grabs), 0), COALESCE(SUM(s.grab_bytes), 0)
		FROM indexers i LEFT JOIN indexer_stats s ON s.indexer_id = i.id AND s.day >= ?
		GROUP BY i.id, i.name ORDER BY i.id`, since.UTC().Format(dayFmt))
	if err != nil {
		return nil, fmt.Errorf("read indexer statistics: %w", err)
	}
	defer rows.Close()
	var out []Stat
	for rows.Next() {
		var s Stat
		if err := rows.Scan(&s.IndexerID, &s.Name, &s.Queries, &s.Failures, &s.Results, &s.Grabs, &s.GrabBytes); err != nil {
			return nil, fmt.Errorf("read indexer statistics: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Prune forgets counters older than StatsKeepDays.
func (t *Tracker) Prune() {
	if t == nil || t.db == nil {
		return
	}
	cut := t.now().UTC().AddDate(0, 0, -StatsKeepDays).Format(dayFmt)
	if _, err := t.db.Exec(`DELETE FROM indexer_stats WHERE day < ?`, cut); err != nil {
		slog.Warn("indexers: prune indexer statistics", "err", err)
	}
}
