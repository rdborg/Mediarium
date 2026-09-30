package library

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// nullID stores 0 (unknown) as NULL.
func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id > 0} }

// encodeGenres stores nil as NULL (not fetched yet) and anything else,
// including an empty list, as a JSON array.
func encodeGenres(genres []string) (sql.NullString, error) {
	if genres == nil {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(genres)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encode genres: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

// decodeGenres reverses encodeGenres. A value that is not a JSON array is
// treated as not fetched, so the background job fetches it again.
func decodeGenres(v sql.NullString) []string {
	if !v.Valid {
		return nil
	}
	out := []string{}
	if err := json.Unmarshal([]byte(v.String), &out); err != nil {
		return nil
	}
	return out
}

func (r *Repo) setGenres(table string, id int64, genres []string) error {
	if genres == nil {
		genres = []string{}
	}
	enc, err := encodeGenres(genres)
	if err != nil {
		return err
	}
	if _, err := r.db.Exec(`UPDATE `+table+` SET genres = ? WHERE id = ?`, enc, id); err != nil {
		return fmt.Errorf("set %s %d genres: %w", table, id, err)
	}
	return nil
}

// SetMovieGenres stores a movie's genre names (nil is stored as an empty list,
// so the title is not fetched again).
func (r *Repo) SetMovieGenres(id int64, genres []string) error {
	return r.setGenres("movies", id, genres)
}

// SetSeriesGenres is SetMovieGenres for a show.
func (r *Repo) SetSeriesGenres(id int64, genres []string) error {
	return r.setGenres("series", id, genres)
}

// GenreGap is a library title whose genres have not been fetched yet.
type GenreGap struct {
	Kind   string // "movie" or "series"
	ID     int64
	TMDBID int
	Title  string
}

// MissingGenres lists up to limit titles (movies first, newest first) whose
// genres are still unknown.
func (r *Repo) MissingGenres(limit int) ([]GenreGap, error) {
	rows, err := r.db.Query(`
		SELECT kind, id, tmdb_id, title FROM (
			SELECT 'movie' AS kind, id, tmdb_id, title, added_at, 0 AS ord FROM movies WHERE genres IS NULL
			UNION ALL
			SELECT 'series', id, tmdb_id, title, added_at, 1 FROM series WHERE genres IS NULL
		) ORDER BY ord, added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list titles without genres: %w", err)
	}
	defer rows.Close()
	var out []GenreGap
	for rows.Next() {
		var g GenreGap
		if err := rows.Scan(&g.Kind, &g.ID, &g.TMDBID, &g.Title); err != nil {
			return nil, fmt.Errorf("scan title without genres: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
