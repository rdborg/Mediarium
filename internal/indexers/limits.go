package indexers

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Daily limits. Free indexers allow a number of requests a day and answer
// "Request limit reached (26/25)" or "Request limit reached. Retry in 894
// minutes." after that. Spacing searches apart does nothing against a daily
// count, so the tracker counts every request it makes to each indexer per UTC
// day, learns the limit from those answers, and automation leaves the indexer
// alone before it is used up.

const (
	// maxDailyLimit is the largest daily limit taken from an indexer's answer
	// (the same as the largest a person can set). A bigger number is nonsense.
	maxDailyLimit = 100000
	// maxRetryWait is the longest "Retry in N" wait that is believed.
	maxRetryWait = 48 * time.Hour
	// learnedLimitKeep is how long a limit learned from an answer is trusted
	// without hearing it again.
	learnedLimitKeep = 7 * 24 * time.Hour
	// learnedLimitFloor is the smallest limit taken at the first answer. A
	// smaller one has to be heard twice.
	learnedLimitFloor = 5
)

// ProbeInterval is the least time between two connection checks of one
// indexer, whatever the schedule of the monitor is.
const ProbeInterval = 6 * time.Hour

// PausedProbeInterval is the least time between two checks of an indexer that
// is paused after failures (other than being asked too often). The check is the
// small "what do you support" question, and when it works the pause is lifted,
// so an indexer that came back, or whose key was refused for a while, does not
// stay left out for hours until a person presses Test.
const PausedProbeInterval = 30 * time.Minute

// DailyLimit is what an indexer's "limit reached" answer says.
type DailyLimit struct {
	Used   int       // requests used today, 0 when the answer does not say
	Limit  int       // requests allowed a day, 0 when the answer does not say
	BackAt time.Time // when the requests are back
}

var (
	limitCountRe = regexp.MustCompile(`\((\d+)\s*/\s*(\d+)\)`)
	limitRetryRe = regexp.MustCompile(`(?i)retry in (\d+)\s*(second|sec|minute|min|hour|hr)`)
)

// ParseDailyLimit reads an indexer's answer to a request over its daily
// limit. It gives false for any other message, including a plain "too many
// requests" (that one is about speed, not the day).
func ParseDailyLimit(msg string, now time.Time) (DailyLimit, bool) {
	low := strings.ToLower(msg)
	if !strings.Contains(low, "request limit reached") && !strings.Contains(low, "daily limit") {
		return DailyLimit{}, false
	}
	var dl DailyLimit
	if m := limitCountRe.FindStringSubmatch(msg); m != nil {
		// numbers that do not fit, or that no daily limit could be, are left out
		if n, err := strconv.ParseInt(m[1], 10, 32); err == nil && n <= maxDailyLimit {
			dl.Used = int(n)
		}
		if n, err := strconv.ParseInt(m[2], 10, 32); err == nil && n <= maxDailyLimit {
			dl.Limit = int(n)
		}
	}
	dl.BackAt = nextMidnight(now)
	if m := limitRetryRe.FindStringSubmatch(msg); m != nil {
		n, _ := strconv.ParseInt(m[1], 10, 32)
		unit := time.Minute
		switch strings.ToLower(m[2]) {
		case "second", "sec":
			unit = time.Second
		case "hour", "hr":
			unit = time.Hour
		}
		if n > 0 && n < 100000 {
			dl.BackAt = now.Add(min(time.Duration(n)*unit, maxRetryWait))
		}
	}
	return dl, true
}

// IsDailyLimit reports whether err is an indexer's answer over its daily
// limit.
func IsDailyLimit(err error) bool {
	if err == nil {
		return false
	}
	_, ok := ParseDailyLimit(err.Error(), time.Now())
	return ok
}

func isMidnight(t time.Time) bool {
	u := t.UTC()
	return u.Hour() == 0 && u.Minute() == 0 && u.Second() == 0
}

func nextMidnight(now time.Time) time.Time {
	u := now.UTC()
	return time.Date(u.Year(), u.Month(), u.Day()+1, 0, 0, 0, 0, time.UTC)
}

// limitMargin is how many requests automation leaves unused, so a search
// started by hand still has room.
func limitMargin(limit int) int {
	if limit > 5 {
		return 2
	}
	return 0
}

// Quota is where an indexer stands against its daily limit.
type Quota struct {
	Limit   int       // requests allowed a day; 0 = no limit known
	Told    bool      // the indexer said its requests are used up, and when they are back
	Learned bool      // the limit came from the indexer's own answers, not from a setting
	Used    int       // requests made today (UTC)
	Reached bool      // no more requests are allowed today
	Blocked bool      // automation leaves it alone: the limit, less the margin, is reached
	BackAt  time.Time // when requests are allowed again (set when Reached or Blocked)
}

// Message says in plain words why automation leaves the indexer alone, in
// loc's time ("" when it does not).
func (q Quota) Message(loc *time.Location) string {
	if !q.Blocked {
		return ""
	}
	if loc == nil {
		loc = time.Local
	}
	midnight := isMidnight(q.BackAt)
	switch {
	case q.Limit > 0 && midnight:
		return fmt.Sprintf("Daily limit of %d reached. It is used again after midnight (UTC).", q.Limit)
	case q.Limit > 0:
		return fmt.Sprintf("Daily limit of %d reached. It is used again at %s.", q.Limit, q.BackAt.In(loc).Format("15:04"))
	case midnight:
		return "Daily limit reached. It is used again after midnight (UTC)."
	}
	return fmt.Sprintf("Daily limit reached. It is used again at %s.", q.BackAt.In(loc).Format("15:04"))
}

// limitState is everything the tracker keeps for daily limits, guarded by
// Tracker.mu.
type limitState struct {
	quota     map[int64]*quotaNote
	used      map[int64]*usedNote
	lastOK    map[int64]time.Time // when a real search or feed read last worked
	lastProbe map[int64]time.Time // when a connection check last ran
	lastFail  map[int64]time.Time // when a real search or feed read last failed
	rssLast   map[int64]time.Time // when an RSS run last read the indexer (rss_budget.go)
	rssUsed   map[int64]*usedNote // the RSS requests made today
}

type quotaNote struct {
	limit     int
	backAt    time.Time
	learnedAt time.Time // when the indexer last told the limit
	pending   int       // a small limit heard once, not believed yet
}

type usedNote struct {
	day string
	n   int
}

func (ls *limitState) init() {
	if ls.quota == nil {
		ls.quota = map[int64]*quotaNote{}
		ls.used = map[int64]*usedNote{}
		ls.lastOK = map[int64]time.Time{}
		ls.lastProbe = map[int64]time.Time{}
		ls.lastFail = map[int64]time.Time{}
	}
}

// quotaNoteLocked returns what is known about the indexer's limit, reading the
// database once. t.mu must be held.
func (t *Tracker) quotaNoteLocked(id int64) *quotaNote {
	t.limits.init()
	if q, ok := t.limits.quota[id]; ok {
		return q
	}
	q := &quotaNote{}
	if t.db != nil {
		var back, learned string
		if err := t.db.QueryRow(`SELECT learned_limit, quota_back_at, learned_at FROM indexers WHERE id = ?`, id).Scan(&q.limit, &back, &learned); err == nil {
			q.backAt, _ = time.Parse(timeFmt, back)
			q.learnedAt, _ = time.Parse(timeFmt, learned)
			if q.limit > 0 && q.learnedAt.IsZero() {
				// a limit learned before the time was kept: its week starts now
				q.learnedAt = t.now()
				if _, err := t.db.Exec(`UPDATE indexers SET learned_at = ? WHERE id = ?`, q.learnedAt.UTC().Format(timeFmt), id); err != nil {
					slog.Warn("indexers: save indexer daily limit time", "indexer", id, "err", err)
				}
			}
		}
	}
	t.limits.quota[id] = q
	return q
}

// usedNoteLocked returns today's request count, reading the database once a
// day. t.mu must be held.
func (t *Tracker) usedNoteLocked(id int64) *usedNote {
	t.limits.init()
	day := t.now().UTC().Format(dayFmt)
	u := t.limits.used[id]
	if u != nil && u.day == day {
		return u
	}
	u = &usedNote{day: day}
	if t.db != nil {
		_ = t.db.QueryRow(`SELECT requests FROM indexer_stats WHERE indexer_id = ? AND day = ?`, id, day).Scan(&u.n)
	}
	t.limits.used[id] = u
	return u
}

// CountRequest counts requests made to an indexer today: a search, a feed
// read, a connection check, a release download.
func (t *Tracker) CountRequest(id int64, n int) {
	if t == nil || id == 0 || n <= 0 {
		return
	}
	t.mu.Lock()
	u := t.usedNoteLocked(id)
	u.n = min(u.n+n, 4*maxDailyLimit) // the count of a day cannot run away
	day := u.day
	t.mu.Unlock()
	if t.db == nil {
		return
	}
	_, err := t.db.Exec(`INSERT INTO indexer_stats (indexer_id, day, requests) VALUES (?, ?, ?)
		ON CONFLICT(indexer_id, day) DO UPDATE SET requests = requests + excluded.requests`, id, day, n)
	if err != nil {
		slog.Warn("indexers: count indexer requests", "indexer", id, "err", err)
	}
}

// Quota says where the indexer stands today. setLimit is the limit a person
// set for it (0 = none), which wins over what the indexer reported.
func (t *Tracker) Quota(id int64, setLimit int) Quota {
	if t == nil {
		return Quota{}
	}
	now := t.now()
	t.mu.Lock()
	qn := *t.quotaNoteLocked(id)
	used := t.usedNoteLocked(id).n
	t.mu.Unlock()

	q := Quota{Used: used, Limit: setLimit}
	if q.Limit <= 0 && qn.limit > 0 && now.Sub(qn.learnedAt) < learnedLimitKeep {
		// a learned limit is trusted for a week without hearing it again
		q.Limit, q.Learned = qn.limit, true
	}
	told := now.Before(qn.backAt)
	q.Told = told
	atLimit := q.Limit > 0 && used >= q.Limit
	q.Reached = told || atLimit
	q.Blocked = told || (q.Limit > 0 && used >= q.Limit-limitMargin(q.Limit))
	if q.Blocked {
		q.BackAt = nextMidnight(now)
		if told {
			q.BackAt = qn.backAt
		}
	}
	return q
}

// LearnLimit reads an indexer's answer for a daily limit. When it is one, the
// limit and the time the requests are back are kept, and the count of the day
// is raised to what the indexer says it has counted. It reports whether msg
// was such an answer.
func (t *Tracker) LearnLimit(id int64, msg string) bool {
	if t == nil || id == 0 {
		return false
	}
	now := t.now()
	dl, ok := ParseDailyLimit(msg, now)
	if !ok {
		return false
	}
	t.mu.Lock()
	qn := t.quotaNoteLocked(id)
	if dl.Limit > 0 {
		switch {
		case dl.Limit >= learnedLimitFloor || dl.Limit == qn.limit || dl.Limit == qn.pending:
			// a very small limit is only believed when it is heard twice
			qn.limit, qn.pending = dl.Limit, 0
			qn.learnedAt = now
		default:
			qn.pending = dl.Limit
			dl.Limit, dl.Used = 0, 0
		}
	}
	qn.backAt = dl.BackAt
	limit, backAt, learnedAt := qn.limit, qn.backAt, qn.learnedAt
	u := t.usedNoteLocked(id)
	raise := 0
	if floor := max(dl.Used, dl.Limit); floor > u.n {
		raise = floor - u.n
	}
	t.mu.Unlock()
	if raise > 0 {
		t.CountRequest(id, raise)
	}
	if t.db != nil {
		_, err := t.db.Exec(`UPDATE indexers SET learned_limit = ?, quota_back_at = ?, learned_at = ? WHERE id = ?`, limit, backAt.UTC().Format(timeFmt), formatTime(learnedAt), id)
		if err != nil {
			slog.Warn("indexers: save indexer daily limit", "indexer", id, "err", err)
		}
	}
	slog.Info("indexers: an indexer reached its daily limit", "indexer", id, "limit", limit, "back", backAt.UTC().Format(timeFmt))
	return true
}

// clearQuotaWait forgets that the indexer said its requests were used up: it
// has just answered, so they are back.
func (t *Tracker) clearQuotaWait(id int64) {
	t.mu.Lock()
	qn := t.quotaNoteLocked(id)
	had := !qn.backAt.IsZero()
	qn.backAt = time.Time{}
	t.mu.Unlock()
	if had && t.db != nil {
		if _, err := t.db.Exec(`UPDATE indexers SET quota_back_at = '' WHERE id = ?`, id); err != nil {
			slog.Warn("indexers: clear indexer daily limit", "indexer", id, "err", err)
		}
	}
}

// noteAnswered remembers that a real search or feed read worked.
func (t *Tracker) noteAnswered(id int64) {
	now := t.now()
	t.mu.Lock()
	t.limits.init()
	t.limits.lastOK[id] = now
	qn := t.quotaNoteLocked(id)
	wrong := qn.limit > 0 && t.usedNoteLocked(id).n > qn.limit
	if wrong {
		// the search was counted over the learned limit and still worked, so
		// the limit is not what it was
		qn.limit, qn.pending, qn.learnedAt = 0, 0, time.Time{}
	}
	t.mu.Unlock()
	t.clearQuotaWait(id)
	if wrong && t.db != nil {
		if _, err := t.db.Exec(`UPDATE indexers SET learned_limit = 0, learned_at = '' WHERE id = ?`, id); err != nil {
			slog.Warn("indexers: forget indexer daily limit", "indexer", id, "err", err)
		}
	}
}

// noteFailed remembers that a real search or feed read failed.
func (t *Tracker) noteFailed(id int64) {
	now := t.now()
	t.mu.Lock()
	t.limits.init()
	t.limits.lastFail[id] = now
	t.mu.Unlock()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeFmt)
}

// Probe says what a connection check of the indexer should do now.
type Probe int

const (
	// ProbeNow: ask the indexer.
	ProbeNow Probe = iota
	// ProbeSkipWorks: a real search or feed read worked since the last check,
	// and none has failed since, so the indexer is known to work and costs no
	// request.
	ProbeSkipWorks
	// ProbeSkipWait: the last check was less than ProbeInterval ago, or the
	// daily limit is used up. Keep the earlier result.
	ProbeSkipWait
)

// ProbeAdvice decides whether the monitor should ask the indexer. A real
// search or feed read that worked since the last check is as good as a check.
// Otherwise the indexer is asked at most once every ProbeInterval, and not at
// all while its daily limit is used up. setLimit is the limit set by a person.
func (t *Tracker) ProbeAdvice(id int64, setLimit int) Probe {
	if t == nil {
		return ProbeNow
	}
	// Only what the indexer itself said, or a limit a person set, keeps the
	// check away. A limit learned from earlier answers may be out of date, and
	// must not hide an indexer that is broken.
	if q := t.Quota(id, setLimit); q.Told || (q.Blocked && !q.Learned) {
		return ProbeSkipWait
	}
	now := t.now()
	interval := ProbeInterval
	if h, _ := t.Health(id); h.Paused(now) && !strings.Contains(h.Reason, "too many requests") {
		interval = PausedProbeInterval
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.limits.init()
	ok, probed, failed := t.limits.lastOK[id], t.limits.lastProbe[id], t.limits.lastFail[id]
	if !ok.IsZero() && ok.After(probed) && !failed.After(ok) && now.Sub(ok) < ProbeInterval {
		return ProbeSkipWorks
	}
	if !probed.IsZero() && now.Sub(probed) < interval {
		return ProbeSkipWait
	}
	return ProbeNow
}

// NoteProbe records that a connection check of the indexer was made now; it
// counts as a request.
func (t *Tracker) NoteProbe(id int64) {
	if t == nil {
		return
	}
	now := t.now()
	t.mu.Lock()
	t.limits.init()
	t.limits.lastProbe[id] = now
	t.mu.Unlock()
	t.CountRequest(id, 1)
}

type manualKey struct{}

// AsManual marks a search as one a person started. Such a search still asks an
// indexer that has used up its automatic share of the day.
func AsManual(ctx context.Context) context.Context {
	return context.WithValue(ctx, manualKey{}, true)
}

func isManual(ctx context.Context) bool {
	v, _ := ctx.Value(manualKey{}).(bool)
	return v
}
