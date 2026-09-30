// Package blocklist remembers releases that failed for a reason that is the
// release's fault, so automation does not grab the same dud again.
package blocklist

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Entry is one blocklisted release.
type Entry struct {
	ID           int64
	ReleaseTitle string
	Protocol     string
	Reason       string
	MovieID      int64 // 0 if not tied to a movie
	SeriesID     int64 // 0 if not tied to a series
	CreatedAt    time.Time
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
func (r *Repo) Add(e Entry) error {
	_, err := r.db.Exec(
		`INSERT INTO blocklist (release_title, title_key, protocol, reason, movie_id, series_id)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (title_key) DO UPDATE SET reason = excluded.reason, created_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		e.ReleaseTitle, Key(e.ReleaseTitle), e.Protocol, e.Reason, nullIfZero(e.MovieID), nullIfZero(e.SeriesID),
	)
	if err != nil {
		return fmt.Errorf("blocklist %q: %w", e.ReleaseTitle, err)
	}
	return nil
}

// Keys returns the set of blocklisted title keys, for filtering search
// results.
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

func (r *Repo) List() ([]Entry, error) {
	rows, err := r.db.Query(
		`SELECT id, release_title, protocol, reason, COALESCE(movie_id, 0), COALESCE(series_id, 0), created_at FROM blocklist ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list blocklist: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var (
			e       Entry
			created string
		)
		if err := rows.Scan(&e.ID, &e.ReleaseTitle, &e.Protocol, &e.Reason, &e.MovieID, &e.SeriesID, &created); err != nil {
			return nil, fmt.Errorf("scan blocklist entry: %w", err)
		}
		e.CreatedAt, _ = time.Parse("2006-01-02T15:04:05.000Z", created)
		out = append(out, e)
	}
	return out, rows.Err()
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
