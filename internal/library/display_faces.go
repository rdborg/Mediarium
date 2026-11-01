package library

import (
	"database/sql"
	"fmt"
)

// DisplayFaceGap is a library title whose translated title, poster or plot
// has not been stored for one language yet.
type DisplayFaceGap struct {
	Kind     string // "movie" or "tv"
	TMDBID   int
	Title    string
	Poster   string
	Overview string
}

// StoredDisplayFace is the title, poster and plot kept for one title in one
// language. HasOverview is false when the row was saved before plots were
// stored, so the background job still has to ask for the plot.
type StoredDisplayFace struct {
	Title       string
	Poster      string
	Overview    string
	HasOverview bool
}

// DisplayFace reads the stored face for one title in one language. ok is
// false when that language has not been stored yet.
func (r *Repo) DisplayFace(kind string, tmdbID int, language string) (StoredDisplayFace, bool, error) {
	var face StoredDisplayFace
	var overview sql.NullString
	err := r.db.QueryRow(
		`SELECT title, poster, overview FROM display_faces WHERE kind = ? AND tmdb_id = ? AND language = ?`,
		kind, tmdbID, language,
	).Scan(&face.Title, &face.Poster, &overview)
	if err == sql.ErrNoRows {
		return StoredDisplayFace{}, false, nil
	}
	if err != nil {
		return StoredDisplayFace{}, false, err
	}
	face.Overview = overview.String
	face.HasOverview = overview.Valid
	return face, true, nil
}

// SaveDisplayFace remembers the title, poster and plot shown for one title in
// one language. Saving again replaces the previous row.
func (r *Repo) SaveDisplayFace(kind string, tmdbID int, language, title, poster, overview string) error {
	_, err := r.db.Exec(
		`INSERT INTO display_faces (kind, tmdb_id, language, title, poster, overview) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (kind, tmdb_id, language) DO UPDATE SET
		   title = excluded.title, poster = excluded.poster, overview = excluded.overview`,
		kind, tmdbID, language, title, poster, overview,
	)
	if err != nil {
		return fmt.Errorf("save display face %s %d %s: %w", kind, tmdbID, language, err)
	}
	return nil
}

// MissingDisplayFaces lists up to limit library titles that have no stored
// face for language, or a face saved before the plot was kept. Movies come
// first, newest first. Copies share the main title's face, so they are not
// listed.
func (r *Repo) MissingDisplayFaces(language string, limit int) ([]DisplayFaceGap, error) {
	rows, err := r.db.Query(`
		SELECT kind, tmdb_id, title, poster, overview FROM (
			SELECT 'movie' AS kind, tmdb_id, title, COALESCE(poster_path, '') AS poster, COALESCE(overview, '') AS overview, added_at, 0 AS ord
			FROM movies
			WHERE copy_of IS NULL AND tmdb_id > 0
			  AND NOT EXISTS (
			    SELECT 1 FROM display_faces f
			    WHERE f.kind = 'movie' AND f.tmdb_id = movies.tmdb_id AND f.language = ?
			      AND f.overview IS NOT NULL
			  )
			UNION ALL
			SELECT 'tv', tmdb_id, title, COALESCE(poster_path, ''), COALESCE(overview, ''), added_at, 1
			FROM series
			WHERE copy_of IS NULL AND tmdb_id > 0
			  AND NOT EXISTS (
			    SELECT 1 FROM display_faces f
			    WHERE f.kind = 'tv' AND f.tmdb_id = series.tmdb_id AND f.language = ?
			      AND f.overview IS NOT NULL
			  )
		) ORDER BY ord, added_at DESC LIMIT ?`, language, language, limit)
	if err != nil {
		return nil, fmt.Errorf("list display faces still to fetch: %w", err)
	}
	defer rows.Close()
	var out []DisplayFaceGap
	for rows.Next() {
		var g DisplayFaceGap
		if err := rows.Scan(&g.Kind, &g.TMDBID, &g.Title, &g.Poster, &g.Overview); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
