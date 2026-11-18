package indexers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
)

// Back-off: an indexer that keeps failing is left out of searches for a while
// instead of being asked again and again. Every failure in a row makes the
// wait longer, up to six hours; the first answer that works clears it.

const (
	// FailuresBeforePause is how many failures in a row it takes before an
	// indexer is paused. One or two can be a bad moment.
	FailuresBeforePause = 3
	// BaseBackoff is the first pause. It doubles with every failure after that.
	BaseBackoff = 5 * time.Minute
	// MaxBackoff is the longest pause, whatever the indexer asks for.
	MaxBackoff = 6 * time.Hour
)

// StatusError is an indexer's answer that was not a success, with the wait it
// asked for (the Retry-After header) when it sent one.
type StatusError struct {
	Status     int
	RetryAfter time.Duration
	msg        string
}

func (e *StatusError) Error() string { return e.msg }

// NewStatusError builds the error for an answer with this HTTP status. msg is
// what the error says (it must not hold the API key).
func NewStatusError(status int, header http.Header, msg string, now time.Time) *StatusError {
	return &StatusError{Status: status, RetryAfter: ParseRetryAfter(header.Get("Retry-After"), now), msg: msg}
}

// ParseRetryAfter reads a Retry-After header: a number of seconds or a date.
// It gives 0 when the header is missing, unreadable or in the past.
func ParseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		// A silly number must not overflow the duration (and wrap to a short
		// or negative wait): nothing is waited for longer than MaxBackoff.
		if secs > int(MaxBackoff/time.Second) {
			return MaxBackoff
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// BackoffFor is how long an indexer waits after its n-th failure in a row (n
// counts the one that just happened). rateLimited failures pause from the
// first one, and retryAfter is the wait the indexer itself asked for.
// It gives 0 when the indexer is not paused yet.
func BackoffFor(n int, rateLimited bool, retryAfter time.Duration) time.Duration {
	var d time.Duration
	step := n - FailuresBeforePause
	if rateLimited {
		step = n - 1
	}
	if step >= 0 {
		d = BaseBackoff
		for i := 0; i < step && d < MaxBackoff; i++ {
			d *= 2
		}
	}
	if retryAfter > d {
		d = retryAfter
	}
	if d > MaxBackoff {
		d = MaxBackoff
	}
	return d
}

// failureReason says in a few plain words why an indexer failed.
func failureReason(err error) string {
	var se *StatusError
	if errors.As(err, &se) && se.Status == http.StatusTooManyRequests {
		return "it said you made too many requests"
	}
	if _, ok := FlareSolverrDown(err); ok {
		return "FlareSolverr isn't running or can't be reached"
	}
	if status, ok := GoneStatus(err); ok {
		return fmt.Sprintf("the site says the page is gone (HTTP %d)", status)
	}
	switch problems.IndexerCode(err) {
	case problems.CodeIndexerRateLimited:
		return "it said you made too many requests"
	case problems.CodeIndexerBlocked:
		return "its site blocked the request"
	case problems.CodeIndexerAuthRefused:
		return "it did not accept the API key or login"
	case problems.CodeIndexerUnreachable:
		return "it could not be reached"
	}
	if se != nil && se.Status >= 500 {
		return fmt.Sprintf("it answered with an error (HTTP %d)", se.Status)
	}
	return "it returned an error"
}

// Health is what is known about one indexer's recent answers.
type Health struct {
	Failures    int       // failures in a row; 0 when it works
	PausedUntil time.Time // searches skip it until then; zero when it is not paused
	Reason      string    // why the last failure happened, in plain words
	LastFailure time.Time
}

// Paused reports whether the indexer is being left out of searches at now.
func (h Health) Paused(now time.Time) bool { return now.Before(h.PausedUntil) }

// Tracker keeps every indexer's health and search counters. It remembers the
// pauses in the database so a restart does not start asking a struggling
// indexer again at once. A nil *Tracker does nothing.
type Tracker struct {
	db  *sql.DB
	now func() time.Time

	mu     sync.Mutex
	health map[int64]Health
	loaded bool
	// sources remembers which indexer a release came from, so a grab can be
	// counted for it.
	sources map[string]sourceNote
	// prunedOn is the day old counters were last cleared.
	prunedOn string

	// order lists sources by when they were noted (oldest first, from
	// orderHead), so old ones are dropped without scanning the map.
	order     []sourceKey
	orderHead int

	// limits is what is known about daily request limits (see limits.go).
	limits limitState

	// paceMu guards lanes: each indexer's waiting searches (see Pace).
	paceMu sync.Mutex
	lanes  map[int64]*paceLane
}

type sourceKey struct {
	url    string
	expiry time.Time
}

type sourceNote struct {
	id     int64
	guid   string // the id the indexer gave the release ("" when none)
	expiry time.Time
}

const (
	sourceTTL = 24 * time.Hour
	maxSource = 50000
)

// NewTracker makes a tracker that stores in db (nil keeps everything in memory
// only). now is the clock (nil: the real one).
func NewTracker(db *sql.DB, now func() time.Time) *Tracker {
	if now == nil {
		now = time.Now
	}
	return &Tracker{db: db, now: now, health: map[int64]Health{}, sources: map[string]sourceNote{}}
}

// MinSearchGap is the least time between two searches on one indexer. A
// search of the whole library (the automatic one asks for every missing
// movie and episode in turn) would otherwise send hundreds of requests in a
// minute, and indexers answer that with "too many requests". An indexer that
// has already said so is asked more slowly.
const (
	MinSearchGap    = 1500 * time.Millisecond
	RateLimitedGap  = 6 * time.Second
	maxPacedWaiting = 10 * time.Minute // a wait longer than this is not queued: the search is skipped
)

// ErrSearchQueueFull is returned by Pace when so many searches are already
// waiting for an indexer that this one would wait longer than
// maxPacedWaiting: it is skipped instead of queued for hours.
var ErrSearchQueueFull = errors.New("too many searches are already waiting for this indexer")

// paceLane is one indexer's line of waiting searches.
type paceLane struct {
	turn    chan struct{} // held by the search whose start is being timed
	last    time.Time     // when the last search started
	waiting int           // searches in the line, the one being timed included
}

// Pace waits until this indexer may be asked again, one search after another
// at least gap apart (more than one waiting at once line up behind each other).
// It gives ctx's error when ctx ended first, and ErrSearchQueueFull without
// waiting when the line is already longer than maxPacedWaiting. A search that
// gives up never holds back the ones behind it: only searches that really
// start count. A nil tracker never waits.
func (t *Tracker) Pace(ctx context.Context, id int64, gap time.Duration) error {
	if t == nil || gap <= 0 {
		return ctx.Err()
	}
	t.paceMu.Lock()
	if t.lanes == nil {
		t.lanes = map[int64]*paceLane{}
	}
	lane := t.lanes[id]
	if lane == nil {
		lane = &paceLane{turn: make(chan struct{}, 1)}
		t.lanes[id] = lane
	}
	ahead := time.Duration(lane.waiting) * gap
	if !lane.last.IsZero() {
		ahead += max(0, lane.last.Add(gap).Sub(t.now()))
	}
	if ahead > maxPacedWaiting {
		t.paceMu.Unlock()
		return ErrSearchQueueFull
	}
	lane.waiting++
	t.paceMu.Unlock()
	defer func() {
		t.paceMu.Lock()
		lane.waiting--
		t.paceMu.Unlock()
	}()

	select {
	case lane.turn <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-lane.turn }()

	t.paceMu.Lock()
	var wait time.Duration
	if !lane.last.IsZero() {
		wait = lane.last.Add(gap).Sub(t.now())
	}
	t.paceMu.Unlock()
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	t.paceMu.Lock()
	lane.last = t.now()
	t.paceMu.Unlock()
	return nil
}

// SearchGap is how far apart searches on this indexer are kept: longer for
// one that recently said it was being asked too often.
func (t *Tracker) SearchGap(id int64) time.Duration {
	if h, ok := t.Health(id); ok && h.Failures > 0 && strings.Contains(h.Reason, "too many requests") {
		return gapLimited
	}
	return gapNormal
}

var (
	gapNormal  = MinSearchGap
	gapLimited = RateLimitedGap
)

// SetSearchGaps changes the gaps. It is for tests, which would otherwise wait
// a second and a half between every search.
func SetSearchGaps(normal, limited time.Duration) (restore func()) {
	oldN, oldL := gapNormal, gapLimited
	gapNormal, gapLimited = normal, limited
	return func() { gapNormal, gapLimited = oldN, oldL }
}

const timeFmt = time.RFC3339

func (t *Tracker) load() {
	if t.loaded {
		return
	}
	t.loaded = true
	if t.db == nil {
		return
	}
	rows, err := t.db.Query(`SELECT indexer_id, failures, paused_until, reason, last_failure_at FROM indexer_health`)
	if err != nil {
		slog.Warn("indexers: read indexer health", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id            int64
			h             Health
			until, lastAt string
		)
		if err := rows.Scan(&id, &h.Failures, &until, &h.Reason, &lastAt); err != nil {
			slog.Warn("indexers: read indexer health", "err", err)
			return
		}
		h.PausedUntil, _ = time.Parse(timeFmt, until)
		h.LastFailure, _ = time.Parse(timeFmt, lastAt)
		t.health[id] = h
	}
}

// Health returns what is known about an indexer. ok is false when it has no
// failures on record.
func (t *Tracker) Health(id int64) (Health, bool) {
	if t == nil {
		return Health{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	h, ok := t.health[id]
	return h, ok
}

// PausedUntil says whether searches should skip the indexer now, and until
// when.
func (t *Tracker) PausedUntil(id int64) (time.Time, bool) {
	h, ok := t.Health(id)
	if !ok || !h.Paused(t.now()) {
		return time.Time{}, false
	}
	return h.PausedUntil, true
}

// Succeeded clears an indexer's failures: it answered.
func (t *Tracker) Succeeded(id int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	if _, ok := t.health[id]; !ok {
		return
	}
	delete(t.health, id)
	t.forget(id)
}

// Resume lets searches ask the indexer again now ("Try again now"). The
// failures are forgotten too, so a new failure starts the count again.
func (t *Tracker) Resume(id int64) {
	t.Succeeded(id)
	if t != nil {
		t.clearQuotaWait(id)
	}
}

// Failed records a failed search and returns the indexer's health after it.
func (t *Tracker) Failed(id int64, err error) Health {
	if t == nil {
		return Health{}
	}
	now := t.now()
	var retryAfter time.Duration
	var se *StatusError
	rateLimited := problems.IndexerCode(err) == problems.CodeIndexerRateLimited
	if errors.As(err, &se) {
		retryAfter = se.RetryAfter
		rateLimited = rateLimited || retryAfter > 0 || se.Status == http.StatusTooManyRequests
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	h := t.health[id]
	h.Failures++
	h.Reason = failureReason(err)
	h.LastFailure = now
	if d := BackoffFor(h.Failures, rateLimited, retryAfter); d > 0 {
		h.PausedUntil = now.Add(d)
	}
	t.health[id] = h
	t.save(id, h)
	return h
}

func (t *Tracker) save(id int64, h Health) {
	if t.db == nil {
		return
	}
	until := ""
	if !h.PausedUntil.IsZero() {
		until = h.PausedUntil.UTC().Format(timeFmt)
	}
	_, err := t.db.Exec(`INSERT INTO indexer_health (indexer_id, failures, paused_until, reason, last_failure_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(indexer_id) DO UPDATE SET failures = excluded.failures, paused_until = excluded.paused_until, reason = excluded.reason, last_failure_at = excluded.last_failure_at`,
		id, h.Failures, until, h.Reason, h.LastFailure.UTC().Format(timeFmt))
	if err != nil {
		slog.Warn("indexers: save indexer health", "indexer", id, "err", err)
	}
}

func (t *Tracker) forget(id int64) {
	if t.db == nil {
		return
	}
	if _, err := t.db.Exec(`DELETE FROM indexer_health WHERE indexer_id = ?`, id); err != nil {
		slog.Warn("indexers: clear indexer health", "indexer", id, "err", err)
	}
}

// Delete drops everything kept about an indexer that was removed.
func (t *Tracker) Delete(id int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	delete(t.health, id)
}

// PausedMessage is the plain sentence for a paused indexer, for the indexer
// list, Health and a search's status: "Example is paused until 14:20 after 3
// failures in a row, because it could not be reached."
func PausedMessage(name string, h Health, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	when := h.PausedUntil.In(loc).Format("15:04")
	if h.PausedUntil.Sub(h.LastFailure) > 12*time.Hour {
		when = h.PausedUntil.In(loc).Format("2 Jan 15:04")
	}
	return fmt.Sprintf("%s is paused until %s after %s in a row, because %s.", name, when, plural(h.Failures, "failure"), h.Reason)
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
