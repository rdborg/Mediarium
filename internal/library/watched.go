package library

import (
	"fmt"
	"time"
)

// What has been watched, as read from the media servers (see
// internal/api/watched.go). One row per watched movie or episode.

// Watched is the play state of a movie (Season and Episode 0) or an episode.
type Watched struct {
	Kind       string // "movie" or "episode"
	TitleID    int64  // movie id, or series id for an episode
	Season     int
	Episode    int
	Plays      int
	LastPlayed time.Time // zero when unknown
}

// ReplaceWatched swaps the whole watched list for a fresh one.
func (r *Repo) ReplaceWatched(list []Watched) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("replace watched: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM watched`); err != nil {
		return fmt.Errorf("replace watched: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO watched (kind, title_id, season, episode, plays, last_played) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("replace watched: %w", err)
	}
	defer stmt.Close()
	for _, w := range list {
		last := ""
		if !w.LastPlayed.IsZero() {
			last = w.LastPlayed.UTC().Format(time.RFC3339)
		}
		if _, err := stmt.Exec(w.Kind, w.TitleID, w.Season, w.Episode, w.Plays, last); err != nil {
			return fmt.Errorf("replace watched: %w", err)
		}
	}
	return tx.Commit()
}

// ListWatched returns every watched row.
func (r *Repo) ListWatched() ([]Watched, error) {
	rows, err := r.db.Query(`SELECT kind, title_id, season, episode, plays, last_played FROM watched`)
	if err != nil {
		return nil, fmt.Errorf("list watched: %w", err)
	}
	defer rows.Close()
	var out []Watched
	for rows.Next() {
		var w Watched
		var last string
		if err := rows.Scan(&w.Kind, &w.TitleID, &w.Season, &w.Episode, &w.Plays, &last); err != nil {
			return nil, fmt.Errorf("scan watched: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, last); err == nil {
			w.LastPlayed = t
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MovieAddedDates maps every movie to when it was added to the library.
func (r *Repo) MovieAddedDates() (map[int64]time.Time, error) {
	rows, err := r.db.Query(`SELECT id, added_at FROM movies WHERE copy_of IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("movie added dates: %w", err)
	}
	defer rows.Close()
	out := map[int64]time.Time{}
	for rows.Next() {
		var id int64
		var at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("scan movie added date: %w", err)
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, at); err == nil {
				out[id] = t
				break
			}
		}
	}
	return out, rows.Err()
}

// ClearMovieFile marks a movie as missing and forgets its file (after the
// file was deleted).
func (r *Repo) ClearMovieFile(id int64) error {
	if _, err := r.db.Exec(`UPDATE movies SET status = 'missing', quality = '', file_path = NULL, release_title = '', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, id); err != nil {
		return fmt.Errorf("clear movie %d file: %w", id, err)
	}
	return nil
}

// ClearEpisodeFile marks an episode as missing and forgets its file.
func (r *Repo) ClearEpisodeFile(id int64) error {
	if _, err := r.db.Exec(`UPDATE episodes SET status = 'missing', quality = '', file_path = NULL, release_title = '' WHERE id = ?`, id); err != nil {
		return fmt.Errorf("clear episode %d file: %w", id, err)
	}
	return nil
}

// UnlinkMovieFile detaches a movie from its file without touching the file:
// the movie is missing again, and what was recorded about that file for the
// movie (downloaded subtitles, subtitle lookups) is forgotten so it can't be
// mistaken for the next file's.
func (r *Repo) UnlinkMovieFile(id int64) error {
	return r.unlink("movie", "movies", id)
}

// UnlinkEpisodeFile is UnlinkMovieFile for one episode.
func (r *Repo) UnlinkEpisodeFile(id int64) error {
	return r.unlink("episode", "episodes", id)
}

func (r *Repo) unlink(kind, table string, id int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("unlink %s %d: %w", kind, id, err)
	}
	defer func() { _ = tx.Rollback() }()
	set := "status = 'missing', quality = '', file_path = NULL, release_title = ''"
	if kind == "movie" {
		set += ", updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')"
	}
	if _, err := tx.Exec("UPDATE "+table+" SET "+set+" WHERE id = ?", id); err != nil {
		return fmt.Errorf("unlink %s %d: %w", kind, id, err)
	}
	for _, t := range []string{"subtitle_files", "subtitle_attempts"} {
		if _, err := tx.Exec("DELETE FROM "+t+" WHERE kind = ? AND media_id = ?", kind, id); err != nil {
			return fmt.Errorf("unlink %s %d: forget %s: %w", kind, id, t, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("unlink %s %d: %w", kind, id, err)
	}
	return nil
}
