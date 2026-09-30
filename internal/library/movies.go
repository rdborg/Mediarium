// Package library owns the movies table — the Phase 1 library (TV follows
// the same shape in Phase 2 per the `media_type` column in the schema).
package library

import (
	"database/sql"
	"fmt"
)

type Status string

const (
	StatusMissing     Status = "missing"
	StatusDownloading Status = "downloading"
	StatusDownloaded  Status = "downloaded"
)

type Movie struct {
	ID          int64
	TMDBID      int
	Title       string
	Year        int
	Overview    string
	PosterPath  string
	Status      Status
	Quality     string
	FilePath    string
	Monitored   bool
	ReleaseDate string // "YYYY-MM-DD" from TMDB, empty if unknown — powers the calendar
	ProfileID   int64  // quality profile; 0 = the default profile
	SourcePref  string // "" = follow Settings; else usenet, torrent or both
	AddedBy     int64  // account that added it; 0 = unknown
	// Genres are TMDB genre names. nil means not fetched yet (see
	// MissingGenres); an empty, non-nil slice means TMDB lists none.
	Genres []string
	// NoUpgrade leaves the movie out of the search for better versions
	// (set for titles that came in through an import).
	NoUpgrade bool
	// DetailsState is "pending" while an import is still fetching the
	// movie's details, "problem" when that failed (DetailsNote says why in
	// plain words) and empty otherwise.
	DetailsState string
	DetailsNote  string
}

const movieColumns = `id, tmdb_id, title, year, overview, poster_path, status, quality, file_path, monitored, COALESCE(release_date, ''), COALESCE(profile_id, 0), source_pref, COALESCE(added_by, 0), genres, no_upgrade, details_state, details_note`

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// Add inserts a movie into the library as "missing"
// (added via search/Discover, not yet downloaded).
func (r *Repo) Add(m Movie) (Movie, error) {
	if m.Status == "" {
		m.Status = StatusMissing
	}
	genres, err := encodeGenres(m.Genres)
	if err != nil {
		return Movie{}, err
	}
	res, err := r.db.Exec(
		`INSERT INTO movies (tmdb_id, title, year, overview, poster_path, status, monitored, release_date, added_by, genres, no_upgrade) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.TMDBID, m.Title, m.Year, m.Overview, m.PosterPath, string(m.Status), m.Monitored, m.ReleaseDate, nullID(m.AddedBy), genres, m.NoUpgrade,
	)
	if err != nil {
		return Movie{}, fmt.Errorf("insert movie: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Movie{}, fmt.Errorf("get inserted movie id: %w", err)
	}
	m.ID = id
	return m, nil
}

func (r *Repo) Get(id int64) (Movie, error) {
	return r.scanOne(r.db.QueryRow(
		`SELECT `+movieColumns+` FROM movies WHERE id = ?`, id))
}

// GetByTMDBID looks up a movie by its TMDB id — used by the movie detail
// page to check whether a Discover title has already been added, without
// needing its own library id (which doesn't exist until it's added).
func (r *Repo) GetByTMDBID(tmdbID int) (Movie, bool, error) {
	m, err := r.scanOne(r.db.QueryRow(
		`SELECT `+movieColumns+` FROM movies WHERE tmdb_id = ?`, tmdbID))
	if err == sql.ErrNoRows {
		return Movie{}, false, nil
	}
	if err != nil {
		return Movie{}, false, err
	}
	return m, true, nil
}

func (r *Repo) List() ([]Movie, error) {
	rows, err := r.db.Query(
		`SELECT ` + movieColumns + ` FROM movies ORDER BY added_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list movies: %w", err)
	}
	defer rows.Close()

	var out []Movie
	for rows.Next() {
		m, err := r.scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMonitored turns automatic searching for a movie on or off.
func (r *Repo) SetMonitored(id int64, monitored bool) error {
	_, err := r.db.Exec(`UPDATE movies SET monitored = ? WHERE id = ?`, monitored, id)
	if err != nil {
		return fmt.Errorf("set movie %d monitored: %w", id, err)
	}
	return nil
}

// SetSourcePref sets which downloaders a movie may use ("" = the default).
func (r *Repo) SetSourcePref(id int64, pref string) error {
	_, err := r.db.Exec(`UPDATE movies SET source_pref = ? WHERE id = ?`, pref, id)
	if err != nil {
		return fmt.Errorf("set movie %d source preference: %w", id, err)
	}
	return nil
}

// SetProfile assigns a quality profile to a movie (0 = use the default).
func (r *Repo) SetProfile(id, profileID int64) error {
	_, err := r.db.Exec(`UPDATE movies SET profile_id = NULLIF(?, 0) WHERE id = ?`, profileID, id)
	if err != nil {
		return fmt.Errorf("set movie %d profile: %w", id, err)
	}
	return nil
}

// SetNoUpgrade turns the search for better versions of a movie off (or back on).
func (r *Repo) SetNoUpgrade(id int64, noUpgrade bool) error {
	_, err := r.db.Exec(`UPDATE movies SET no_upgrade = ? WHERE id = ?`, noUpgrade, id)
	if err != nil {
		return fmt.Errorf("set movie %d no-upgrade: %w", id, err)
	}
	return nil
}

// RecentItem is a movie or series, for "recently added" lists.
type RecentItem struct {
	Kind       string // "movie" or "series"
	ID         int64
	TMDBID     int
	Title      string
	Year       int
	PosterPath string
	AddedAt    string
}

// RecentlyAdded returns the newest movies and series together, newest first.
func (r *Repo) RecentlyAdded(limit int) ([]RecentItem, error) {
	rows, err := r.db.Query(
		`SELECT kind, id, tmdb_id, title, year, poster, added_at FROM (
			SELECT 'movie' AS kind, id, tmdb_id, title, COALESCE(year, 0) AS year, COALESCE(poster_path, '') AS poster, added_at FROM movies
			UNION ALL
			SELECT 'series', id, tmdb_id, title, COALESCE(year, 0), COALESCE(poster_path, ''), added_at FROM series
		) ORDER BY added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recently added: %w", err)
	}
	defer rows.Close()
	var out []RecentItem
	for rows.Next() {
		var it RecentItem
		if err := rows.Scan(&it.Kind, &it.ID, &it.TMDBID, &it.Title, &it.Year, &it.PosterPath, &it.AddedAt); err != nil {
			return nil, fmt.Errorf("scan recently added: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Delete removes a movie and, via foreign keys, its queue items. The lines
// in the Activity feed are kept, detached; the detailed events that are shown
// only on the movie's own page go with it, since nothing could show them again.
func (r *Repo) Delete(id int64) error {
	return r.deleteWithItemEvents("movies", "movie_id", id)
}

// deleteWithItemEvents deletes row id of table and the item-only activity
// events that belong to it, in one step.
func (r *Repo) deleteWithItemEvents(table, eventColumn string, id int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("delete %s %d: %w", table, id, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM activity WHERE item_only = 1 AND `+eventColumn+` = ?`, id); err != nil {
		return fmt.Errorf("delete %s %d: remove its events: %w", table, id, err)
	}
	if _, err := tx.Exec(`DELETE FROM `+table+` WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete %s %d: %w", table, id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete %s %d: %w", table, id, err)
	}
	return nil
}

// SetStatus updates a movie's lifecycle status (missing -> downloading ->
// downloaded), and optionally its resolved quality/file path once known.
func (r *Repo) SetStatus(id int64, status Status, quality, filePath string) error {
	_, err := r.db.Exec(
		`UPDATE movies SET status = ?, quality = COALESCE(NULLIF(?, ''), quality), file_path = COALESCE(NULLIF(?, ''), file_path), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`,
		string(status), quality, filePath, id,
	)
	if err != nil {
		return fmt.Errorf("update movie %d status: %w", id, err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *Repo) scanOne(row *sql.Row) (Movie, error) {
	return r.scanRow(row)
}

func (r *Repo) scanRow(scanner rowScanner) (Movie, error) {
	var (
		m        Movie
		status   string
		quality  sql.NullString
		filePath sql.NullString
		genres   sql.NullString
	)
	err := scanner.Scan(&m.ID, &m.TMDBID, &m.Title, &m.Year, &m.Overview, &m.PosterPath, &status, &quality, &filePath, &m.Monitored, &m.ReleaseDate, &m.ProfileID, &m.SourcePref, &m.AddedBy, &genres, &m.NoUpgrade, &m.DetailsState, &m.DetailsNote)
	if err == sql.ErrNoRows {
		return Movie{}, err
	}
	if err != nil {
		return Movie{}, fmt.Errorf("scan movie: %w", err)
	}
	m.Genres = decodeGenres(genres)
	m.Status = Status(status)
	m.Quality = quality.String
	m.FilePath = filePath.String
	return m, nil
}
