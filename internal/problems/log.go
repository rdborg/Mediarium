package problems

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/logbuf"
)

const (
	// Window is how long after a problem the same one is folded into its row
	// instead of getting a new row.
	Window = 10 * time.Minute
	// KeepDays is how long problems are kept.
	KeepDays = 30
	// MaxRows is the most rows kept, whatever their age.
	MaxRows = 5000

	maxDetail  = 2000
	maxMessage = 300
	maxTitle   = 200
	maxPending = 500
)

// timeFormat is fixed width, so stored times sort as text.
const timeFormat = "2006-01-02T15:04:05Z"

// Problem is what a caller reports. Only Code is needed; everything else has a
// sensible default.
type Problem struct {
	Code       string // one of the Code constants (an unknown code is still kept)
	Level      Level  // default: the level of the code
	Subject    string // what the problem is about when one code can happen to several things (a server, a source, a download): repeats fold together only for the same subject
	Message    string // a short plain sentence; default: the title of the code
	Err        error  // the underlying error, kept as technical detail
	Detail     string // more technical detail
	Title      string // the movie, show or album it concerns, when known
	Link       string // page in the app for that title, for example /title/603
	DownloadID int64  // the download it concerns, when known
	Quiet      bool   // do not send a notification for it, even when those are switched on
}

// Entry is one row of the log.
type Entry struct {
	ID         int64
	FirstAt    time.Time
	LastAt     time.Time
	Level      Level
	Area       Area
	Code       string
	Subject    string
	Message    string
	Detail     string
	Title      string
	Link       string
	DownloadID int64
	Count      int
	Read       bool
}

// Log is the stored problem log. Record is safe to call from anywhere and never
// blocks: the write happens on a short-lived goroutine of its own.
type Log struct {
	db  *sql.DB
	now func() time.Time

	// OnNew, when set, is told about every new error row (not about repeats
	// folded into an existing one). It must not block.
	OnNew func(Entry)

	mu      sync.Mutex
	cond    *sync.Cond
	pending []queued
	working bool
	dropped int
}

type queued struct {
	p  Problem
	at time.Time
}

// Open returns a log stored in db, which must have the problems table.
func Open(db *sql.DB) *Log {
	l := &Log{db: db, now: time.Now}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// SetClock replaces the clock (tests).
func (l *Log) SetClock(now func() time.Time) { l.now = now }

var std atomic.Pointer[Log]

// SetDefault chooses the log that Record writes to. Pass nil to switch
// recording off.
func SetDefault(l *Log) { std.Store(l) }

// Default returns the log Record writes to, or nil.
func Default() *Log { return std.Load() }

// Record reports a problem to the default log. It does nothing until the API
// has set one, and it never blocks or fails.
func Record(p Problem) {
	if l := std.Load(); l != nil {
		l.Record(p)
	}
}

// Record queues the problem to be written.
func (l *Log) Record(p Problem) {
	if l == nil || l.db == nil || strings.TrimSpace(p.Code) == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pending) >= maxPending {
		l.dropped++
		return
	}
	l.pending = append(l.pending, queued{p: p, at: l.now()})
	if !l.working {
		l.working = true
		go l.work()
	}
}

// Flush waits until everything recorded so far is written.
func (l *Log) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for l.working || len(l.pending) > 0 {
		l.cond.Wait()
	}
}

func (l *Log) work() {
	for {
		l.mu.Lock()
		if len(l.pending) == 0 {
			l.working = false
			l.cond.Broadcast()
			l.mu.Unlock()
			return
		}
		batch := l.pending
		l.pending = nil
		l.mu.Unlock()
		for _, q := range batch {
			l.save(q)
		}
	}
}

// Clip shortens s to at most about n bytes without cutting a character in half.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// Clean turns a problem into what is stored: secrets taken out, long text cut.
func Clean(p Problem) (message, detail string) {
	message = Clip(strings.TrimSpace(logbuf.Scrub(p.Message)), maxMessage)
	var parts []string
	if p.Err != nil {
		parts = append(parts, p.Err.Error())
	}
	if d := strings.TrimSpace(p.Detail); d != "" {
		parts = append(parts, d)
	}
	// Scrub each line, as the log buffer does, so a pattern cannot straddle two.
	lines := strings.Split(strings.Join(parts, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = logbuf.Scrub(strings.TrimRight(ln, "\r"))
	}
	detail = Clip(strings.TrimSpace(strings.Join(lines, "\n")), maxDetail)
	return message, detail
}

// saving counts the writes to the log that are running now.
var saving atomic.Int32

// Saving reports whether the log is writing a row right now. The database
// reports statements that take long to the log; the statements of the log's
// own write are left out, or a slow database would fill the log with the log's
// own trouble and keep writing.
func Saving() bool { return saving.Load() > 0 }

func (l *Log) save(q queued) {
	saving.Add(1)
	defer saving.Add(-1)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("problems: could not save a problem: %v", r)
		}
	}()
	p := q.p
	help, known := Lookup(p.Code)
	level, area := p.Level, AreaSystem
	if known {
		area = help.Area
		if level == "" {
			level = help.Level
		}
	}
	if level != LevelWarning {
		level = LevelError
	}
	message, detail := Clean(p)
	if message == "" {
		if known {
			message = help.Title
		} else {
			message = p.Code
		}
	}
	title := Clip(strings.TrimSpace(p.Title), maxTitle)
	at := q.at.UTC()

	tx, err := l.db.Begin()
	if err != nil {
		log.Printf("problems: could not save %s: %v", p.Code, err)
		return
	}
	defer tx.Rollback()

	var (
		id    int64
		prevL string
	)
	err = tx.QueryRow(`SELECT id, level FROM problems WHERE code = ? AND subject = ? AND last_at >= ? ORDER BY last_at DESC, id DESC LIMIT 1`,
		p.Code, p.Subject, at.Add(-Window).Format(timeFormat)).Scan(&id, &prevL)
	switch {
	case err == nil:
		if prevL == string(LevelError) {
			level = LevelError
		}
		_, err = tx.Exec(`UPDATE problems SET count = count + 1, last_at = ?, level = ?, message = ?, detail = ?,
			title = CASE WHEN ? <> '' THEN ? ELSE title END,
			link = CASE WHEN ? <> '' THEN ? ELSE link END,
			download_id = CASE WHEN ? <> 0 THEN ? ELSE download_id END,
			is_read = 0 WHERE id = ?`,
			at.Format(timeFormat), string(level), message, detail,
			title, title, p.Link, p.Link, p.DownloadID, p.DownloadID, id)
		if err != nil {
			log.Printf("problems: could not save %s: %v", p.Code, err)
			return
		}
		if err := tx.Commit(); err != nil {
			log.Printf("problems: could not save %s: %v", p.Code, err)
		}
		return
	case errors.Is(err, sql.ErrNoRows):
	default:
		log.Printf("problems: could not save %s: %v", p.Code, err)
		return
	}

	res, err := tx.Exec(`INSERT INTO problems (first_at, last_at, level, area, code, subject, message, detail, title, link, download_id, count, is_read)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 0)`,
		at.Format(timeFormat), at.Format(timeFormat), string(level), string(area), p.Code, p.Subject, message, detail, title, p.Link, p.DownloadID)
	if err != nil {
		log.Printf("problems: could not save %s: %v", p.Code, err)
		return
	}
	id, _ = res.LastInsertId()
	// Keep the table to its size limit.
	if _, err := tx.Exec(`DELETE FROM problems WHERE id IN (SELECT id FROM problems ORDER BY last_at DESC, id DESC LIMIT -1 OFFSET ?)`, MaxRows); err != nil {
		log.Printf("problems: could not trim the log: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("problems: could not save %s: %v", p.Code, err)
		return
	}
	if level == LevelError && !p.Quiet && l.OnNew != nil {
		l.OnNew(Entry{ID: id, FirstAt: at, LastAt: at, Level: level, Area: area, Code: p.Code, Subject: p.Subject,
			Message: message, Detail: detail, Title: title, Link: p.Link, DownloadID: p.DownloadID, Count: 1})
	}
}

// Filter narrows a list of problems. The zero value matches everything.
type Filter struct {
	Level      Level
	Area       Area
	Code       string
	Since      time.Time // last happened at or after this
	Until      time.Time // last happened before this
	Text       string    // words to find in the message, detail, title or code
	UnreadOnly bool
	Limit      int // default 50, at most 200
	Offset     int
}

func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	if f.Level != "" {
		conds = append(conds, "level = ?")
		args = append(args, string(f.Level))
	}
	if f.Area != "" {
		conds = append(conds, "area = ?")
		args = append(args, string(f.Area))
	}
	if f.Code != "" {
		conds = append(conds, "code = ?")
		args = append(args, f.Code)
	}
	if !f.Since.IsZero() {
		conds = append(conds, "last_at >= ?")
		args = append(args, f.Since.UTC().Format(timeFormat))
	}
	if !f.Until.IsZero() {
		conds = append(conds, "last_at < ?")
		args = append(args, f.Until.UTC().Format(timeFormat))
	}
	if f.UnreadOnly {
		conds = append(conds, "is_read = 0")
	}
	for _, word := range strings.Fields(f.Text) {
		like := "%" + escapeLike(strings.ToLower(word)) + "%"
		conds = append(conds, `(lower(message) LIKE ? ESCAPE '\' OR lower(detail) LIKE ? ESCAPE '\' OR lower(title) LIKE ? ESCAPE '\' OR lower(code) LIKE ? ESCAPE '\' OR lower(subject) LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like, like)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

const columns = `id, first_at, last_at, level, area, code, subject, message, detail, title, link, download_id, count, is_read`

func scanEntry(rows interface{ Scan(...any) error }) (Entry, error) {
	var (
		e           Entry
		first, last string
		level, area string
		read        int
	)
	if err := rows.Scan(&e.ID, &first, &last, &level, &area, &e.Code, &e.Subject, &e.Message, &e.Detail, &e.Title, &e.Link, &e.DownloadID, &e.Count, &read); err != nil {
		return e, err
	}
	e.FirstAt, _ = time.Parse(timeFormat, first)
	e.LastAt, _ = time.Parse(timeFormat, last)
	e.Level, e.Area, e.Read = Level(level), Area(area), read != 0
	return e, nil
}

// List returns the problems matching f, newest first, and how many match in all.
func (l *Log) List(f Filter) ([]Entry, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	where, args := f.where()
	var total int
	if err := l.db.QueryRow(`SELECT COUNT(*) FROM problems`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count problems: %w", err)
	}
	rows, err := l.db.Query(`SELECT `+columns+` FROM problems`+where+` ORDER BY last_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list problems: %w", err)
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("read a problem: %w", err)
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// Counts are the numbers on the cards at the top of the page.
type Counts struct {
	ErrorsToday    int `json:"errorsToday"`
	WarningsToday  int `json:"warningsToday"`
	ErrorsWeek     int `json:"errorsWeek"`
	WarningsWeek   int `json:"warningsWeek"`
	Unread         int `json:"unread"`
	UnreadErrors24 int `json:"unreadErrors24h"` // unread errors from the last 24 hours: the red dot and the dashboard card
}

// Counts totals the log. Today runs from midnight in now's time zone; the week
// is the last seven days.
func (l *Log) Counts(now time.Time) (Counts, error) {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := now.AddDate(0, 0, -7)
	day := now.Add(-24 * time.Hour)
	var c Counts
	err := l.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN level = 'error' AND last_at >= ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN level = 'warning' AND last_at >= ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN level = 'error' AND last_at >= ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN level = 'warning' AND last_at >= ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN is_read = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN is_read = 0 AND level = 'error' AND last_at >= ? THEN 1 ELSE 0 END), 0)
		FROM problems`,
		midnight.UTC().Format(timeFormat), midnight.UTC().Format(timeFormat),
		week.UTC().Format(timeFormat), week.UTC().Format(timeFormat), day.UTC().Format(timeFormat),
	).Scan(&c.ErrorsToday, &c.WarningsToday, &c.ErrorsWeek, &c.WarningsWeek, &c.Unread, &c.UnreadErrors24)
	if err != nil {
		return c, fmt.Errorf("count problems: %w", err)
	}
	return c, nil
}

// MarkRead marks the given problems as read and returns how many changed.
func (l *Log) MarkRead(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	res, err := l.db.Exec(`UPDATE problems SET is_read = 1 WHERE is_read = 0 AND id IN (`+marks+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("mark problems read: %w", err)
	}
	return res.RowsAffected()
}

// MarkAllRead marks every problem matching f as read (only the filter's
// conditions are used, not its paging).
func (l *Log) MarkAllRead(f Filter) (int64, error) {
	f.UnreadOnly = false
	where, args := f.where()
	if where == "" {
		where = " WHERE 1=1"
	}
	res, err := l.db.Exec(`UPDATE problems SET is_read = 1`+where+` AND is_read = 0`, args...)
	if err != nil {
		return 0, fmt.Errorf("mark problems read: %w", err)
	}
	return res.RowsAffected()
}

// Prune removes problems older than the retention and anything beyond the row
// limit. days is how many days to keep; 0 or less means the default (30), and a
// longer time than that is never kept.
func (l *Log) Prune(now time.Time, days int) (int64, error) {
	if days <= 0 || days > KeepDays {
		days = KeepDays
	}
	cutoff := now.AddDate(0, 0, -days).UTC().Format(timeFormat)
	res, err := l.db.Exec(`DELETE FROM problems WHERE last_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune problems: %w", err)
	}
	n, _ := res.RowsAffected()
	res, err = l.db.Exec(`DELETE FROM problems WHERE id IN (SELECT id FROM problems ORDER BY last_at DESC, id DESC LIMIT -1 OFFSET ?)`, MaxRows)
	if err != nil {
		return n, fmt.Errorf("trim problems: %w", err)
	}
	m, _ := res.RowsAffected()
	return n + m, nil
}

// Dropped is how many reports were thrown away because too many arrived at
// once.
func (l *Log) Dropped() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dropped
}
