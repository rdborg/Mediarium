// Package library owns the movies table — the Phase 1 library (TV follows
// the same shape in Phase 2 per the `media_type` column in the schema).
package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rdborg/mediarium/internal/origin"
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
	// Origin is how it came into the library: by a person, a list, a library
	// scan... (only Kind and Name are kept; the person is AddedBy). The zero
	// value is a title from before this was recorded.
	Origin origin.Origin
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
	// CopyOf is the main movie's id when this row is a second copy of it (the
	// same film kept again in another quality, in the copies folder); 0 for
	// the main movie.
	CopyOf int64
	// ReleaseTitle is the release the file came from ("" when unknown, as for
	// files that were imported from an existing library).
	ReleaseTitle string
	// RootPath is the library folder the movie's files go to (one of the
	// movie folders in Settings); "" = the default folder.
	RootPath string
	// AltTitles are other titles of the film (its title where a release was
	// made, such as a German one), picked for the audio languages wanted: at
	// most four, empty when English is all that is wanted. Releases named by
	// one are taken for this movie, and the indexers are asked for each.
	AltTitles []string
}

const movieColumns = `id, tmdb_id, title, year, overview, poster_path, status, quality, file_path, monitored, COALESCE(release_date, ''), COALESCE(profile_id, 0), source_pref, COALESCE(added_by, 0), genres, no_upgrade, details_state, details_note, COALESCE(copy_of, 0), release_title, root_path, COALESCE((SELECT names FROM title_names WHERE kind = 'movie' AND tmdb_id = movies.tmdb_id), ''), origin_kind, origin_name, COALESCE(origin_list_id, 0)`

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
		`INSERT INTO movies (tmdb_id, title, year, overview, poster_path, status, monitored, release_date, added_by, genres, no_upgrade, root_path, origin_kind, origin_name, origin_list_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.TMDBID, m.Title, m.Year, m.Overview, m.PosterPath, string(m.Status), m.Monitored, m.ReleaseDate, nullID(m.AddedBy), genres, m.NoUpgrade, m.RootPath, m.Origin.Clean().Kind, m.Origin.Clean().Name, nullID(m.Origin.Clean().ListID),
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
// needing its own library id (which doesn't exist until it's added). It
// finds the main movie, never a second copy.
func (r *Repo) GetByTMDBID(tmdbID int) (Movie, bool, error) {
	m, err := r.scanOne(r.db.QueryRow(
		`SELECT `+movieColumns+` FROM movies WHERE tmdb_id = ? AND copy_of IS NULL`, tmdbID))
	if err == sql.ErrNoRows {
		return Movie{}, false, nil
	}
	if err != nil {
		return Movie{}, false, err
	}
	return m, true, nil
}

// List lists the library's movies, second copies left out.
func (r *Repo) List() ([]Movie, error) {
	return r.listMovies(`WHERE copy_of IS NULL ORDER BY added_at DESC`)
}

// ListWithCopies lists every movie row, second copies included: for the
// jobs that work on files (searching, renaming, subtitles, sizes).
func (r *Repo) ListWithCopies() ([]Movie, error) {
	return r.listMovies(`ORDER BY added_at DESC`)
}

// ListCopies lists the second copies of a movie, oldest first.
func (r *Repo) ListCopies(mainID int64) ([]Movie, error) {
	return r.listMovies(`WHERE copy_of = ? ORDER BY id`, mainID)
}

// AddCopy adds a second copy of a movie in another quality profile: the
// same film, missing and monitored, to be downloaded into the copies
// folder. ErrCopyExists when that profile already has a copy.
func (r *Repo) AddCopy(mainID, profileID int64) (Movie, error) {
	m, err := r.Get(mainID)
	if err != nil {
		return Movie{}, err
	}
	if m.CopyOf != 0 {
		return Movie{}, errors.New("that is already a second copy")
	}
	if m.ProfileID == profileID {
		return Movie{}, ErrCopySameProfile
	}
	res, err := r.db.Exec(
		`INSERT INTO movies (tmdb_id, title, year, overview, poster_path, status, monitored, release_date, added_by, genres, copy_of, profile_id, source_pref)
		 SELECT tmdb_id, title, year, overview, poster_path, 'missing', 1, release_date, added_by, genres, id, ?, source_pref FROM movies WHERE id = ?
		 AND NOT EXISTS (SELECT 1 FROM movies WHERE copy_of = ? AND profile_id = ?)`, profileID, mainID, mainID, profileID)
	if err != nil {
		return Movie{}, fmt.Errorf("add a second copy: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Movie{}, ErrCopyExists
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Movie{}, fmt.Errorf("get the copy's id: %w", err)
	}
	return r.Get(id)
}

// CopyCounts maps each main title to how many second copies it has. table
// is "movies" or "series".
func (r *Repo) CopyCounts(table string) (map[int64]int, error) {
	if table != "movies" && table != "series" {
		return nil, fmt.Errorf("unknown table %q", table)
	}
	rows, err := r.db.Query(`SELECT copy_of, COUNT(*) FROM ` + table + ` WHERE copy_of IS NOT NULL GROUP BY copy_of`)
	if err != nil {
		return nil, fmt.Errorf("count copies: %w", err)
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

var (
	// ErrCopyExists means the title already has a copy in that profile.
	ErrCopyExists = errors.New("there's already a copy in that quality")
	// ErrCopySameProfile means the copy would use the main title's own profile.
	ErrCopySameProfile = errors.New("the main copy already uses that quality")
)

func (r *Repo) listMovies(where string, args ...any) ([]Movie, error) {
	return r.listMoviesContext(context.Background(), where, args...)
}

func (r *Repo) listMoviesContext(ctx context.Context, where string, args ...any) ([]Movie, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+movieColumns+` FROM movies `+where, args...)
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

// SetRoot chooses the library folder a movie's next files go to ("" = the
// default folder). Files already there stay where they are.
func (r *Repo) SetRoot(id int64, root string) error {
	_, err := r.db.Exec(`UPDATE movies SET root_path = ? WHERE id = ?`, root, id)
	if err != nil {
		return fmt.Errorf("set movie %d folder: %w", id, err)
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
	// State is where the title stands: "downloaded", "downloading",
	// "partial" (a show with some episodes), "missing" (monitored, waiting
	// for a release) or "unmonitored".
	State string
}

// RecentlyAdded returns the newest movies and series together, newest first.
func (r *Repo) RecentlyAdded(limit int) ([]RecentItem, error) {
	rows, err := r.db.Query(
		`SELECT kind, id, tmdb_id, title, year, poster, added_at, state FROM (
			SELECT 'movie' AS kind, id, tmdb_id, title, COALESCE(year, 0) AS year, COALESCE(poster_path, '') AS poster, added_at,
				CASE WHEN status IN ('downloaded', 'downloading') THEN status WHEN monitored = 0 THEN 'unmonitored' ELSE 'missing' END AS state
			FROM movies WHERE copy_of IS NULL
			UNION ALL
			SELECT 'series', s.id, s.tmdb_id, s.title, COALESCE(s.year, 0), COALESCE(s.poster_path, ''), s.added_at,
				CASE
					WHEN EXISTS (SELECT 1 FROM episodes e WHERE e.series_id = s.id AND e.status = 'downloading') THEN 'downloading'
					WHEN (SELECT COUNT(*) FROM episodes e WHERE e.series_id = s.id AND e.status = 'downloaded') > 0
						AND NOT EXISTS (SELECT 1 FROM episodes e WHERE e.series_id = s.id AND e.season > 0 AND e.monitored = 1 AND e.status = 'missing') THEN 'downloaded'
					WHEN (SELECT COUNT(*) FROM episodes e WHERE e.series_id = s.id AND e.status = 'downloaded') > 0 THEN 'partial'
					WHEN s.monitored = 0 THEN 'unmonitored'
					ELSE 'missing'
				END
			FROM series s WHERE s.copy_of IS NULL
		) ORDER BY added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recently added: %w", err)
	}
	defer rows.Close()
	var out []RecentItem
	for rows.Next() {
		var it RecentItem
		if err := rows.Scan(&it.Kind, &it.ID, &it.TMDBID, &it.Title, &it.Year, &it.PosterPath, &it.AddedAt, &it.State); err != nil {
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

// SetReleaseTitle records the release a movie's file came from.
func (r *Repo) SetReleaseTitle(id int64, release string) error {
	if _, err := r.db.Exec(`UPDATE movies SET release_title = ? WHERE id = ?`, release, id); err != nil {
		return fmt.Errorf("set movie %d release: %w", id, err)
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
		altNames string
	)
	err := scanner.Scan(&m.ID, &m.TMDBID, &m.Title, &m.Year, &m.Overview, &m.PosterPath, &status, &quality, &filePath, &m.Monitored, &m.ReleaseDate, &m.ProfileID, &m.SourcePref, &m.AddedBy, &genres, &m.NoUpgrade, &m.DetailsState, &m.DetailsNote, &m.CopyOf, &m.ReleaseTitle, &m.RootPath, &altNames, &m.Origin.Kind, &m.Origin.Name, &m.Origin.ListID)
	if err == sql.ErrNoRows {
		return Movie{}, err
	}
	if err != nil {
		return Movie{}, fmt.Errorf("scan movie: %w", err)
	}
	m.Genres = decodeGenres(genres)
	m.AltTitles = decodeTitleNames(altNames)
	m.Status = Status(status)
	m.Quality = quality.String
	m.FilePath = filePath.String
	return m, nil
}
