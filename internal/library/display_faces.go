package library

import (
	"database/sql"
	"fmt"
)

// DisplayFaceGap is a library title whose translated title and poster have
// not been stored for one language yet.
type DisplayFaceGap struct {
	Kind   string // "movie" or "tv"
	TMDBID int
	Title  string
	Poster string
}

// DisplayFace reads the stored title and poster for one title in one
// language. ok is false when that language has not been stored yet.
func (r *Repo) DisplayFace(kind string, tmdbID int, language string) (title, poster string, ok bool, err error) {
	err = r.db.QueryRow(
		`SELECT title, poster FROM display_faces WHERE kind = ? AND tmdb_id = ? AND language = ?`,
		kind, tmdbID, language,
	).Scan(&title, &poster)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return title, poster, true, nil
}

// SaveDisplayFace remembers the title and poster shown for one title in one
// language. Saving again replaces the previous pair.
func (r *Repo) SaveDisplayFace(kind string, tmdbID int, language, title, poster string) error {
	_, err := r.db.Exec(
		`INSERT INTO display_faces (kind, tmdb_id, language, title, poster) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (kind, tmdb_id, language) DO UPDATE SET title = excluded.title, poster = excluded.poster`,
		kind, tmdbID, language, title, poster,
	)
	if err != nil {
		return fmt.Errorf("save display face %s %d %s: %w", kind, tmdbID, language, err)
	}
	return nil
}

// MissingDisplayFaces lists up to limit library titles that have no stored
// face for language. Movies come first, newest first. Copies share the main
// title's face, so they are not listed.
func (r *Repo) MissingDisplayFaces(language string, limit int) ([]DisplayFaceGap, error) {
	rows, err := r.db.Query(`
		SELECT kind, tmdb_id, title, poster FROM (
			SELECT 'movie' AS kind, tmdb_id, title, COALESCE(poster_path, '') AS poster, added_at, 0 AS ord
			FROM movies
			WHERE copy_of IS NULL AND tmdb_id > 0
			  AND NOT EXISTS (
			    SELECT 1 FROM display_faces f
			    WHERE f.kind = 'movie' AND f.tmdb_id = movies.tmdb_id AND f.language = ?
			  )
			UNION ALL
			SELECT 'tv', tmdb_id, title, COALESCE(poster_path, ''), added_at, 1
			FROM series
			WHERE copy_of IS NULL AND tmdb_id > 0
			  AND NOT EXISTS (
			    SELECT 1 FROM display_faces f
			    WHERE f.kind = 'tv' AND f.tmdb_id = series.tmdb_id AND f.language = ?
			  )
		) ORDER BY ord, added_at DESC LIMIT ?`, language, language, limit)
	if err != nil {
		return nil, fmt.Errorf("list display faces still to fetch: %w", err)
	}
	defer rows.Close()
	var out []DisplayFaceGap
	for rows.Next() {
		var g DisplayFaceGap
		if err := rows.Scan(&g.Kind, &g.TMDBID, &g.Title, &g.Poster); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
