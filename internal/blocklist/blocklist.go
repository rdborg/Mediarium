// Package blocklist remembers releases that failed for a reason that is the
// release's fault, so automation does not grab the same dud again.
package blocklist

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// What an entry is blocked for: every title, or the one it is tied to.
const (
	ScopeAll  = "all"
	ScopeItem = "item"
)

// Entry is one blocklisted release.
type Entry struct {
	ID           int64
	ReleaseTitle string
	Protocol     string
	Reason       string
	MovieID      int64 // 0 if not tied to a movie
	SeriesID     int64 // 0 if not tied to a series
	AlbumID      int64 // 0 if not tied to an album
	BookID       int64 // 0 if not tied to a book
	// ItemOnly makes the entry count for the title named by the ids above and
	// for no other. Without it the release is blocked everywhere, as before.
	ItemOnly  bool
	CreatedAt time.Time
}

// Scope names the one title (a movie, a series, an album or a book) a search
// is for, so entries made for that title alone apply to it. The zero Scope is
// a search that is for no particular title: only entries for everywhere apply.
type Scope struct {
	MovieID, SeriesID, AlbumID, BookID int64
}

// tags lists the scope's targets in the form used inside keys.
func (sc Scope) tags() []string {
	var out []string
	if sc.MovieID != 0 {
		out = append(out, fmt.Sprintf("m%d", sc.MovieID))
	}
	if sc.SeriesID != 0 {
		out = append(out, fmt.Sprintf("s%d", sc.SeriesID))
	}
	if sc.AlbumID != 0 {
		out = append(out, fmt.Sprintf("a%d", sc.AlbumID))
	}
	if sc.BookID != 0 {
		out = append(out, fmt.Sprintf("b%d", sc.BookID))
	}
	return out
}

// Scope is the title an entry is tied to (zero when it is not tied to one).
func (e Entry) Scope() Scope {
	return Scope{MovieID: e.MovieID, SeriesID: e.SeriesID, AlbumID: e.AlbumID, BookID: e.BookID}
}

// scopedKey is the key of a release blocklisted for one title only. The NUL
// bytes keep it from ever matching a real release title.
func scopedKey(tag, releaseTitle string) string {
	return "\x00item\x00" + tag + "\x00" + Key(releaseTitle)
}

// Blocked reports whether the release is blocklisted for a search for sc: on
// the blocklist for everywhere, or for that title alone. set is from Keys.
func Blocked(set map[string]bool, releaseTitle string, sc Scope) bool {
	if set[Key(releaseTitle)] {
		return true
	}
	for _, tag := range sc.tags() {
		if set[scopedKey(tag, releaseTitle)] {
			return true
		}
	}
	return false
}

// Key is how titles are compared: case and surrounding whitespace ignored.
func Key(releaseTitle string) string {
	return strings.ToLower(strings.TrimSpace(releaseTitle))
}

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func nullIfZero(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// Add blocklists a release; adding one that is already blocklisted just
// refreshes its reason and timestamp.
//
// With ItemOnly and a title to tie it to, the release is blocklisted for that
// title alone; an entry for everywhere that the same title made for the same
// release is dropped, or it would still block the release for every title.
// ItemOnly without a title is blocklisted everywhere, as there is nothing to
// limit it to.
func (r *Repo) Add(e Entry) error {
	tags := e.Scope().tags()
	key, scope := Key(e.ReleaseTitle), ScopeAll
	if e.ItemOnly && len(tags) > 0 {
		key, scope = scopedKey(tags[0], e.ReleaseTitle), ScopeItem
	}
	_, err := r.db.Exec(
		`INSERT INTO blocklist (release_title, title_key, protocol, reason, movie_id, series_id, album_id, book_id, scope)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (title_key) DO UPDATE SET reason = excluded.reason, created_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		e.ReleaseTitle, key, e.Protocol, e.Reason, nullIfZero(e.MovieID), nullIfZero(e.SeriesID), nullIfZero(e.AlbumID), nullIfZero(e.BookID), scope,
	)
	if err != nil {
		return fmt.Errorf("blocklist %q: %w", e.ReleaseTitle, err)
	}
	if scope == ScopeItem {
		// Only the entry this same title made: another title's stays.
		if _, err := r.db.Exec(
			`DELETE FROM blocklist WHERE title_key = ? AND scope = ? AND COALESCE(movie_id, 0) = ? AND COALESCE(series_id, 0) = ? AND COALESCE(album_id, 0) = ? AND COALESCE(book_id, 0) = ?`,
			Key(e.ReleaseTitle), ScopeAll, e.MovieID, e.SeriesID, e.AlbumID, e.BookID,
		); err != nil {
			return fmt.Errorf("narrow blocklist entry %q: %w", e.ReleaseTitle, err)
		}
	}
	return nil
}

// ErrNoTitle is what SetScope answers when an entry is not tied to a title, so
// there is no title to limit it to.
var ErrNoTitle = errors.New("blocklist entry is not tied to a title")

// SetScope switches an entry between everywhere and its own title only. An
// entry of the same release and scope that is already there is replaced by
// this one.
func (r *Repo) SetScope(id int64, itemOnly bool) error {
	var (
		e                          Entry
		scope                      string
		movie, series, album, book sql.NullInt64
	)
	err := r.db.QueryRow(`SELECT release_title, scope, movie_id, series_id, album_id, book_id FROM blocklist WHERE id = ?`, id).
		Scan(&e.ReleaseTitle, &scope, &movie, &series, &album, &book)
	if errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil {
		return fmt.Errorf("read blocklist entry %d: %w", id, err)
	}
	e.MovieID, e.SeriesID, e.AlbumID, e.BookID = movie.Int64, series.Int64, album.Int64, book.Int64
	key, want := Key(e.ReleaseTitle), ScopeAll
	if itemOnly {
		tags := e.Scope().tags()
		if len(tags) == 0 {
			return ErrNoTitle
		}
		key, want = scopedKey(tags[0], e.ReleaseTitle), ScopeItem
	}
	if scope == want {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("switch blocklist entry %d: %w", id, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM blocklist WHERE title_key = ? AND id != ?`, key, id); err != nil {
		return fmt.Errorf("switch blocklist entry %d: %w", id, err)
	}
	if _, err := tx.Exec(`UPDATE blocklist SET title_key = ?, scope = ? WHERE id = ?`, key, want, id); err != nil {
		return fmt.Errorf("switch blocklist entry %d: %w", id, err)
	}
	return tx.Commit()
}

// Reason returns the reason a release is blocklisted for everywhere, and false
// when it is not on the blocklist like that.
func (r *Repo) Reason(releaseTitle string) (string, bool, error) {
	var reason string
	err := r.db.QueryRow(`SELECT reason FROM blocklist WHERE title_key = ?`, Key(releaseTitle)).Scan(&reason)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("look up blocklist reason of %q: %w", releaseTitle, err)
	}
	return reason, true, nil
}

// Keys returns the set of blocklisted title keys, for filtering search
// results. A release blocked for one title alone is in it under a key of its
// own, so look releases up with Blocked and not by Key alone.
func (r *Repo) Keys() (map[string]bool, error) {
	rows, err := r.db.Query(`SELECT title_key FROM blocklist`)
	if err != nil {
		return nil, fmt.Errorf("list blocklist keys: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

const entryColumns = `id, release_title, protocol, reason, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(album_id, 0), COALESCE(book_id, 0), scope, created_at`

func scanEntry(rows *sql.Rows) (Entry, error) {
	var (
		e       Entry
		scope   string
		created string
	)
	if err := rows.Scan(&e.ID, &e.ReleaseTitle, &e.Protocol, &e.Reason, &e.MovieID, &e.SeriesID, &e.AlbumID, &e.BookID, &scope, &created); err != nil {
		return e, fmt.Errorf("scan blocklist entry: %w", err)
	}
	e.ItemOnly = scope == ScopeItem
	e.CreatedAt, _ = time.Parse("2006-01-02T15:04:05.000Z", created)
	return e, nil
}

func (r *Repo) List() ([]Entry, error) {
	rows, err := r.db.Query(`SELECT ` + entryColumns + ` FROM blocklist ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list blocklist: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Page returns one page of the blocklist, newest first, and how many entries
// there are in all.
func (r *Repo) Page(limit, offset int) ([]Entry, int, error) {
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM blocklist`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count blocklist: %w", err)
	}
	rows, err := r.db.Query(`SELECT `+entryColumns+` FROM blocklist ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list blocklist: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (r *Repo) Remove(id int64) error {
	res, err := r.db.Exec(`DELETE FROM blocklist WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("remove blocklist entry %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repo) Clear() error {
	if _, err := r.db.Exec(`DELETE FROM blocklist`); err != nil {
		return fmt.Errorf("clear blocklist: %w", err)
	}
	return nil
}

// CountRecent counts entries for a movie (movieID) or series (seriesID)
// added since the given time — the guard that stops automatic retries from
// chewing through an endless list of bad releases.
func (r *Repo) CountRecent(movieID, seriesID int64, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM blocklist WHERE created_at >= ? AND ((? != 0 AND movie_id = ?) OR (? != 0 AND series_id = ?))`,
		since.UTC().Format("2006-01-02T15:04:05.000Z"), movieID, movieID, seriesID, seriesID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count recent blocklist entries: %w", err)
	}
	return n, nil
}
