package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// How long a movie or an episode is meant to be (TMDB's runtime, in minutes),
// kept per TMDB id, and the files whose real length is far below it.

// RuntimeKind names which TMDB id a runtime belongs to.
const (
	RuntimeMovie = "movie"
	RuntimeTV    = "tv"
)

// GetRuntime returns the saved runtime of a title. found is false when it was
// never looked up; minutes is 0 when TMDB did not know it.
func (r *Repo) GetRuntime(kind string, tmdbID int) (minutes float64, checked time.Time, found bool, err error) {
	var at string
	err = r.db.QueryRow(`SELECT minutes, checked_at FROM tmdb_runtime WHERE kind = ? AND tmdb_id = ?`, kind, tmdbID).Scan(&minutes, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, fmt.Errorf("get runtime: %w", err)
	}
	checked, _ = time.Parse(time.RFC3339, at)
	return minutes, checked, true, nil
}

// SaveRuntime keeps the runtime of a title (0 when TMDB does not know it).
func (r *Repo) SaveRuntime(kind string, tmdbID int, minutes float64, at time.Time) error {
	if _, err := r.db.Exec(`INSERT OR REPLACE INTO tmdb_runtime (kind, tmdb_id, minutes, checked_at) VALUES (?, ?, ?, ?)`,
		kind, tmdbID, minutes, at.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("save runtime: %w", err)
	}
	return nil
}

// RuntimeWanted is a title whose runtime should be looked up.
type RuntimeWanted struct {
	Kind   string
	TMDBID int
}

// RuntimesWanted lists up to limit titles that have a downloaded file whose
// length is known and whose runtime was never looked up, or was not known
// when last looked at before. It is what fills the table for a library that
// existed before the check did.
func (r *Repo) RuntimesWanted(retryBefore time.Time, limit int) ([]RuntimeWanted, error) {
	rows, err := r.db.Query(`SELECT kind, tmdb_id FROM (
		SELECT 'movie' AS kind, m.tmdb_id AS tmdb_id FROM movies m JOIN media_info mi ON mi.path = m.file_path
			WHERE m.status = 'downloaded' AND mi.failed = 0 AND m.tmdb_id > 0
		UNION
		SELECT 'tv', s.tmdb_id FROM episodes e JOIN series s ON s.id = e.series_id JOIN media_info mi ON mi.path = e.file_path
			WHERE e.status = 'downloaded' AND mi.failed = 0 AND s.tmdb_id > 0
	) w WHERE NOT EXISTS (SELECT 1 FROM tmdb_runtime t WHERE t.kind = w.kind AND t.tmdb_id = w.tmdb_id AND (t.minutes > 0 OR t.checked_at >= ?))
	ORDER BY kind, tmdb_id LIMIT ?`, retryBefore.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("list runtimes to look up: %w", err)
	}
	defer rows.Close()
	var out []RuntimeWanted
	for rows.Next() {
		var w RuntimeWanted
		if err := rows.Scan(&w.Kind, &w.TMDBID); err != nil {
			return nil, fmt.Errorf("scan runtime to look up: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// FileLength is a downloaded file whose saved length is short enough to be
// worth judging, with the runtime its title is meant to have.
type FileLength struct {
	Episode  bool  // else a movie
	ID       int64 // the movie's or the episode's id
	SeriesID int64 // episodes only
	Season   int   // episodes only
	Path     string
	Seconds  float64
	Expected float64 // minutes TMDB gives, 0 when not known
	Archive  string  // set by ArchiveFiles: the archive the "video" really is ("rar", "zip", "7z")
}

// The lengths are the "duration" column, copied from the saved details JSON when
// a file is saved (so the lists never read the JSON, which is large). A row
// whose details could not be read, or hold no length, is left out.
const lengthExpr = `CASE WHEN mi.failed = 0 THEN mi.duration ELSE 0 END`

// ShortFileCandidates lists the downloaded files whose length is under
// maxSeconds, or under half their title's runtime, whichever is longer. The
// caller decides with mediainfo.TooShort which of them are really too short.
func (r *Repo) ShortFileCandidates(maxSeconds float64) ([]FileLength, error) {
	var out []FileLength
	movies, err := r.db.Query(`SELECT id, path, dur, minutes FROM (
		SELECT m.id AS id, m.file_path AS path, `+lengthExpr+` AS dur, COALESCE(rt.minutes, 0) AS minutes
		FROM movies m JOIN media_info mi ON mi.path = m.file_path
		LEFT JOIN tmdb_runtime rt ON rt.kind = 'movie' AND rt.tmdb_id = m.tmdb_id
		WHERE m.status = 'downloaded') q
		WHERE dur > 0 AND (dur < ? OR dur < minutes * 30)`, maxSeconds)
	if err != nil {
		return nil, fmt.Errorf("short movie files: %w", err)
	}
	for movies.Next() {
		f := FileLength{}
		if err := movies.Scan(&f.ID, &f.Path, &f.Seconds, &f.Expected); err != nil {
			movies.Close()
			return nil, fmt.Errorf("scan short movie file: %w", err)
		}
		out = append(out, f)
	}
	if err := movies.Err(); err != nil {
		movies.Close()
		return nil, fmt.Errorf("short movie files: %w", err)
	}
	movies.Close()
	eps, err := r.db.Query(`SELECT id, series_id, season, path, dur, minutes FROM (
		SELECT e.id AS id, e.series_id AS series_id, e.season AS season, e.file_path AS path, `+lengthExpr+` AS dur, COALESCE(rt.minutes, 0) AS minutes
		FROM episodes e JOIN series s ON s.id = e.series_id JOIN media_info mi ON mi.path = e.file_path
		LEFT JOIN tmdb_runtime rt ON rt.kind = 'tv' AND rt.tmdb_id = s.tmdb_id
		WHERE e.status = 'downloaded') q
		WHERE dur > 0 AND (dur < ? OR dur < minutes * 30)`, maxSeconds)
	if err != nil {
		return nil, fmt.Errorf("short episode files: %w", err)
	}
	defer eps.Close()
	for eps.Next() {
		f := FileLength{Episode: true}
		if err := eps.Scan(&f.ID, &f.SeriesID, &f.Season, &f.Path, &f.Seconds, &f.Expected); err != nil {
			return nil, fmt.Errorf("scan short episode file: %w", err)
		}
		out = append(out, f)
	}
	return out, eps.Err()
}

// ArchiveFiles lists the downloaded movie and episode files whose saved details
// say they are an archive, not a video (a release that was never unpacked):
// the container column holds one of containers (mediainfo.ArchiveContainers).
func (r *Repo) ArchiveFiles(containers []string) ([]FileLength, error) {
	if len(containers) == 0 {
		return nil, nil
	}
	in := strings.TrimSuffix(strings.Repeat("?,", len(containers)), ",")
	args := make([]any, len(containers))
	for i, c := range containers {
		args[i] = c
	}
	rows, err := r.db.Query(`SELECT 0, m.id, 0, 0, m.file_path, mi.container FROM movies m JOIN media_info mi ON mi.path = m.file_path
		WHERE m.status = 'downloaded' AND mi.failed = 0 AND mi.container IN (`+in+`)
		UNION ALL
		SELECT 1, e.id, e.series_id, e.season, e.file_path, mi.container FROM episodes e JOIN media_info mi ON mi.path = e.file_path
		WHERE e.status = 'downloaded' AND mi.failed = 0 AND mi.container IN (`+in+`)`, append(args, args...)...)
	if err != nil {
		return nil, fmt.Errorf("archive files: %w", err)
	}
	defer rows.Close()
	var out []FileLength
	for rows.Next() {
		var f FileLength
		var episode int
		if err := rows.Scan(&episode, &f.ID, &f.SeriesID, &f.Season, &f.Path, &f.Archive); err != nil {
			return nil, fmt.Errorf("scan archive file: %w", err)
		}
		f.Episode = episode == 1
		out = append(out, f)
	}
	return out, rows.Err()
}
