package library

import (
	"context"
	"fmt"
	"strings"
)

// The queries in this file answer the questions the dashboard, the calendar
// and the download list ask often ("how many", "which ones are coming up",
// "what is this id called"), so those pages never have to read whole tables
// into memory just to count or to look up a title.

// MovieCounts is how the main movies (second copies left out) stand.
type MovieCounts struct {
	Total       int
	Downloaded  int
	Downloading int
	// Missing is everything else: neither downloaded nor downloading.
	Missing int
}

// MovieCounts counts the library's movies by where they stand.
func (r *Repo) MovieCounts(ctx context.Context) (MovieCounts, error) {
	var c MovieCounts
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'downloaded' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'downloading' THEN 1 ELSE 0 END), 0)
		FROM movies WHERE copy_of IS NULL`).Scan(&c.Total, &c.Downloaded, &c.Downloading)
	if err != nil {
		return MovieCounts{}, fmt.Errorf("count movies: %w", err)
	}
	c.Missing = c.Total - c.Downloaded - c.Downloading
	return c, nil
}

// SeriesCounts is the show and episode totals of the library (second copies
// left out).
type SeriesCounts struct {
	Series             int
	Anime              int // of those, the anime shows
	Episodes           int // the episodes a show counts: not unmonitored specials without a file
	EpisodesDownloaded int
}

// SeriesCounts counts shows and their episodes. An episode counts like it
// does in a show's own totals: every regular episode, and a special only when
// it is monitored or has its file.
func (r *Repo) SeriesCounts(ctx context.Context) (SeriesCounts, error) {
	var c SeriesCounts
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN series_type = 'anime' THEN 1 ELSE 0 END), 0) FROM series WHERE copy_of IS NULL`).Scan(&c.Series, &c.Anime); err != nil {
		return SeriesCounts{}, fmt.Errorf("count series: %w", err)
	}
	err := r.db.QueryRowContext(ctx, `SELECT
			COALESCE(SUM(CASE WHEN e.season > 0 OR e.monitored = 1 OR e.status = 'downloaded' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN e.status = 'downloaded' THEN 1 ELSE 0 END), 0)
		FROM series s CROSS JOIN episodes e ON e.series_id = s.id WHERE s.copy_of IS NULL`).Scan(&c.Episodes, &c.EpisodesDownloaded)
	if err != nil {
		return SeriesCounts{}, fmt.Errorf("count episodes: %w", err)
	}
	return c, nil
}

// MissingMovies is how many monitored main movies are marked missing, with
// the file paths the few that still name one carry. The caller decides
// whether a file that is still on disk keeps such a movie off the Wanted
// list; the others are wanted either way.
func (r *Repo) MissingMovies(ctx context.Context) (noFile int, withFile []string, err error) {
	rows, err := r.db.QueryContext(ctx, `SELECT COALESCE(file_path, ''), COUNT(*) FROM movies
		WHERE copy_of IS NULL AND monitored = 1 AND status = 'missing' GROUP BY COALESCE(file_path, '')`)
	if err != nil {
		return 0, nil, fmt.Errorf("count missing movies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		var n int
		if err := rows.Scan(&path, &n); err != nil {
			return 0, nil, fmt.Errorf("scan missing movies: %w", err)
		}
		if path == "" {
			noFile += n
			continue
		}
		for range n {
			withFile = append(withFile, path)
		}
	}
	return noFile, withFile, rows.Err()
}

// The Wanted > Missing rules for an episode, as SQL over "series s CROSS JOIN
// episodes e": the show and the episode are monitored, the episode is marked
// missing and it has aired by the date given as the argument (and, when its
// exact air time is known, that moment is less than AirSlack ahead). The status and
// monitored terms are written out (not passed as arguments) so SQLite can use
// the episodes_wanted index, which holds exactly those episodes; CROSS JOIN
// makes it start from the shows, so when no show is monitored nothing is read.
const wantedEpisodeWhere = `s.monitored = 1 AND e.status = 'missing' AND e.monitored = 1
	AND COALESCE(e.air_date, '') <> '' AND e.air_date <= ?
	AND (e.air_at IS NULL OR e.air_at <= strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '+3 hours'))`

func wantedWhere(mainOnly bool) string {
	if mainOnly {
		return wantedEpisodeWhere + ` AND s.copy_of IS NULL`
	}
	return wantedEpisodeWhere
}

func wantedShowsQuery(mainOnly bool) string {
	return `SELECT s.id, s.title, COALESCE(s.year, 0), COALESCE(s.poster_path, ''), COALESCE(s.profile_id, 0), COALESCE(s.copy_of, 0),
			COUNT(*), COALESCE(SUM(CASE WHEN COALESCE(e.file_path, '') <> '' THEN 1 ELSE 0 END), 0)
		FROM series s CROSS JOIN episodes e ON e.series_id = s.id
		WHERE ` + wantedWhere(mainOnly) + `
		GROUP BY s.id ORDER BY s.added_at DESC, s.id`
}

func wantedEpisodeFilesQuery(mainOnly bool) string {
	return `SELECT s.id, e.file_path FROM series s CROSS JOIN episodes e ON e.series_id = s.id
		WHERE ` + wantedWhere(mainOnly) + ` AND COALESCE(e.file_path, '') <> ''`
}

// WantedShow is a show with episodes in Wanted > Missing, and how many.
type WantedShow struct {
	ID         int64
	Title      string
	Year       int
	PosterPath string
	ProfileID  int64
	CopyOf     int64
	// Episodes is how many of its episodes are wanted; WithFile how many of
	// those still name a file, which the caller checks on disk (one that is
	// there is not wanted).
	Episodes int
	WithFile int
}

// WantedShows lists the shows (second copies too, unless mainOnly) with
// missing episodes that have aired by today ("YYYY-MM-DD"), newest added
// first, each with its count. It only counts: no episode is read in full.
func (r *Repo) WantedShows(ctx context.Context, today string, mainOnly bool) ([]WantedShow, error) {
	rows, err := r.db.QueryContext(ctx, wantedShowsQuery(mainOnly), today)
	if err != nil {
		return nil, fmt.Errorf("count wanted episodes: %w", err)
	}
	defer rows.Close()
	var out []WantedShow
	for rows.Next() {
		var w WantedShow
		if err := rows.Scan(&w.ID, &w.Title, &w.Year, &w.PosterPath, &w.ProfileID, &w.CopyOf, &w.Episodes, &w.WithFile); err != nil {
			return nil, fmt.Errorf("scan wanted show: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// EpisodeFile is the file an episode names.
type EpisodeFile struct {
	SeriesID int64
	Path     string
}

// WantedEpisodeFiles lists the file paths that missing, wanted episodes
// still name (see WantedShows), so the caller can check whether they are on
// disk. There are few: an episode marked missing normally has no file.
func (r *Repo) WantedEpisodeFiles(ctx context.Context, today string, mainOnly bool) ([]EpisodeFile, error) {
	rows, err := r.db.QueryContext(ctx, wantedEpisodeFilesQuery(mainOnly), today)
	if err != nil {
		return nil, fmt.Errorf("list wanted episode files: %w", err)
	}
	defer rows.Close()
	var out []EpisodeFile
	for rows.Next() {
		var f EpisodeFile
		if err := rows.Scan(&f.SeriesID, &f.Path); err != nil {
			return nil, fmt.Errorf("scan wanted episode file: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FileInfo is a downloaded file's path and the quality it was filed under.
type FileInfo struct {
	Path    string
	Quality string
}

// DownloadedMovieFiles lists the main movies that are marked downloaded.
func (r *Repo) DownloadedMovieFiles() ([]FileInfo, error) {
	return r.fileInfos(`SELECT COALESCE(file_path, ''), COALESCE(quality, '') FROM movies WHERE copy_of IS NULL AND status = 'downloaded'`)
}

// DownloadedEpisodeFiles lists the episodes of main shows that are marked
// downloaded, in show and episode order (so a file shared by several episodes
// comes out together).
func (r *Repo) DownloadedEpisodeFiles() ([]FileInfo, error) {
	return r.fileInfos(`SELECT COALESCE(e.file_path, ''), COALESCE(e.quality, '') FROM episodes e JOIN series s ON s.id = e.series_id
		WHERE s.copy_of IS NULL AND e.status = 'downloaded' ORDER BY e.series_id, e.season, e.episode`)
}

func (r *Repo) fileInfos(query string) ([]FileInfo, error) {
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()
	var out []FileInfo
	for rows.Next() {
		var f FileInfo
		if err := rows.Scan(&f.Path, &f.Quality); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CalendarMovie is a movie as the calendar shows it.
type CalendarMovie struct {
	ID          int64
	TMDBID      int
	Title       string
	PosterPath  string
	ReleaseDate string
	Status      Status
}

// CalendarMovies lists the monitored main movies with a release date on or
// after since ("" for all of them), newest added first.
func (r *Repo) CalendarMovies(since string) ([]CalendarMovie, error) {
	rows, err := r.db.Query(`SELECT id, tmdb_id, title, COALESCE(poster_path, ''), release_date, status FROM movies
		WHERE copy_of IS NULL AND monitored = 1 AND COALESCE(release_date, '') <> '' AND release_date >= ?
		ORDER BY added_at DESC`, since)
	if err != nil {
		return nil, fmt.Errorf("list calendar movies: %w", err)
	}
	defer rows.Close()
	var out []CalendarMovie
	for rows.Next() {
		var m CalendarMovie
		var status string
		if err := rows.Scan(&m.ID, &m.TMDBID, &m.Title, &m.PosterPath, &m.ReleaseDate, &status); err != nil {
			return nil, fmt.Errorf("scan calendar movie: %w", err)
		}
		m.Status = Status(status)
		out = append(out, m)
	}
	return out, rows.Err()
}

// CalendarEpisode is an episode as the calendar shows it.
type CalendarEpisode struct {
	ID         int64
	SeriesID   int64
	Season     int
	Episode    int
	Title      string
	AirDate    string
	AirAt      string // RFC 3339 UTC, "" when no time is known
	Status     Status
	SeriesName string
	PosterPath string
	SeriesTMDB int // the show's TMDB id
}

// CalendarEpisodes lists the monitored episodes of monitored main shows that
// air from from to to (both "YYYY-MM-DD", inclusive), show by show.
func (r *Repo) CalendarEpisodes(from, to string) ([]CalendarEpisode, error) {
	rows, err := r.db.Query(`SELECT e.id, e.series_id, e.season, e.episode, COALESCE(e.title, ''), e.air_date, COALESCE(e.air_at, ''), e.status, s.title, COALESCE(s.poster_path, ''), s.tmdb_id
		FROM episodes e JOIN series s ON s.id = e.series_id
		WHERE s.copy_of IS NULL AND s.monitored = 1 AND e.monitored = 1 AND COALESCE(e.air_date, '') <> '' AND e.air_date >= ? AND e.air_date <= ?
		ORDER BY s.added_at DESC, s.id, e.season, e.episode`, from, to)
	if err != nil {
		return nil, fmt.Errorf("list calendar episodes: %w", err)
	}
	defer rows.Close()
	var out []CalendarEpisode
	for rows.Next() {
		var e CalendarEpisode
		var status string
		if err := rows.Scan(&e.ID, &e.SeriesID, &e.Season, &e.Episode, &e.Title, &e.AirDate, &e.AirAt, &status, &e.SeriesName, &e.PosterPath, &e.SeriesTMDB); err != nil {
			return nil, fmt.Errorf("scan calendar episode: %w", err)
		}
		e.Status = Status(status)
		out = append(out, e)
	}
	return out, rows.Err()
}

// TitleBrief is just enough of a movie or show to name it in a list.
type TitleBrief struct {
	ID         int64
	TMDBID     int
	Title      string
	Year       int
	PosterPath string
	Status     Status // movies only
	Quality    string // movies only
	CopyOf     int64
}

// idChunk is how many ids go into one IN (...) list.
const idChunk = 400

// MovieBriefs looks up many movies at once.
func (r *Repo) MovieBriefs(ids []int64) (map[int64]TitleBrief, error) {
	return r.briefs("movies", `id, tmdb_id, title, COALESCE(year, 0), COALESCE(poster_path, ''), status, COALESCE(copy_of, 0), COALESCE(quality, '')`, ids)
}

// SeriesBriefs looks up many shows at once.
func (r *Repo) SeriesBriefs(ids []int64) (map[int64]TitleBrief, error) {
	return r.briefs("series", `id, tmdb_id, title, COALESCE(year, 0), COALESCE(poster_path, ''), '', COALESCE(copy_of, 0), ''`, ids)
}

func (r *Repo) briefs(table, columns string, ids []int64) (map[int64]TitleBrief, error) {
	out := make(map[int64]TitleBrief, len(ids))
	for start := 0; start < len(ids); start += idChunk {
		end := min(start+idChunk, len(ids))
		chunk := ids[start:end]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := r.db.Query(`SELECT `+columns+` FROM `+table+` WHERE id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("look up %s: %w", table, err)
		}
		for rows.Next() {
			var b TitleBrief
			var status string
			if err := rows.Scan(&b.ID, &b.TMDBID, &b.Title, &b.Year, &b.PosterPath, &status, &b.CopyOf, &b.Quality); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan %s: %w", table, err)
			}
			b.Status = Status(status)
			out[b.ID] = b
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// ListMonitoredWithStatus lists the monitored movies, second copies included,
// that have the given status, newest added first. The Wanted page starts from
// these instead of reading every movie.
func (r *Repo) ListMonitoredWithStatus(ctx context.Context, status Status) ([]Movie, error) {
	return r.listMoviesContext(ctx, `WHERE monitored = 1 AND status = ? ORDER BY added_at DESC`, string(status))
}

// wantedEpisodesQuery is ListWantedEpisodes' query. Its arguments are today
// (status missing) or the status (other than downloaded), then seriesID when
// it is not 0.
func wantedEpisodesQuery(status Status, seriesID int64) string {
	var where string
	switch status {
	case StatusMissing:
		where = wantedEpisodeWhere
	case StatusDownloaded:
		where = `s.monitored = 1 AND e.monitored = 1 AND e.status = 'downloaded' AND s.no_upgrade = 0`
	default:
		where = `s.monitored = 1 AND e.monitored = 1 AND e.status = ?`
	}
	if seriesID != 0 {
		where += ` AND s.id = ?`
	}
	// CROSS JOIN: start from the (few) monitored shows, see wantedEpisodeWhere.
	return `SELECT e.id, e.series_id, e.season, e.episode, COALESCE(e.title, ''), COALESCE(e.air_date, ''), e.status, COALESCE(e.quality, ''), COALESCE(e.file_path, ''), e.release_title,
			s.title, COALESCE(s.year, 0), COALESCE(s.poster_path, ''), COALESCE(s.profile_id, 0), s.no_upgrade, COALESCE(s.copy_of, 0)
		FROM series s CROSS JOIN episodes e ON e.series_id = s.id
		WHERE ` + where + `
		ORDER BY s.added_at DESC, s.id, e.season, e.episode`
}

// WantedEpisode is an episode of a monitored show together with what the
// Wanted page needs to know about the show.
type WantedEpisode struct {
	Episode
	SeriesTitle  string
	SeriesYear   int
	SeriesPoster string
	ProfileID    int64
	NoUpgrade    bool
	CopyOf       int64
}

// ListWantedEpisodes lists the monitored episodes of monitored shows (second
// copies included) with the given status, show by show, newest added show
// first. Only status "missing" ones that have aired by today ("YYYY-MM-DD")
// are returned for that status, since an episode that has not aired cannot be
// wanted yet; shows set to leave their files alone are left out for
// "downloaded". seriesID, when not 0, keeps it to that one show.
func (r *Repo) ListWantedEpisodes(ctx context.Context, status Status, today string, seriesID int64) ([]WantedEpisode, error) {
	var args []any
	switch status {
	case StatusMissing:
		args = append(args, today)
	case StatusDownloaded:
	default:
		args = append(args, string(status))
	}
	if seriesID != 0 {
		args = append(args, seriesID)
	}
	rows, err := r.db.QueryContext(ctx, wantedEpisodesQuery(status, seriesID), args...)
	if err != nil {
		return nil, fmt.Errorf("list wanted episodes: %w", err)
	}
	defer rows.Close()
	var out []WantedEpisode
	for rows.Next() {
		var w WantedEpisode
		var st string
		if err := rows.Scan(&w.ID, &w.SeriesID, &w.Season, &w.Episode.Episode, &w.Title, &w.AirDate, &st, &w.Quality, &w.FilePath, &w.ReleaseTitle,
			&w.SeriesTitle, &w.SeriesYear, &w.SeriesPoster, &w.ProfileID, &w.NoUpgrade, &w.CopyOf); err != nil {
			return nil, fmt.Errorf("scan wanted episode: %w", err)
		}
		w.Status = Status(st)
		w.Monitored = true
		out = append(out, w)
	}
	return out, rows.Err()
}
