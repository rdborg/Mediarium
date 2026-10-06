package library

import (
	"database/sql"
	"errors"
	"fmt"
)

// Series is a TV show in the library. Metadata is keyed by TMDB id (TMDB
// covers movies and TV), not TVDB.
type Series struct {
	ID           int64
	TMDBID       int
	Title        string
	Year         int
	Overview     string
	PosterPath   string
	FirstAirDate string
	Monitored    bool
	ProfileID    int64  // quality profile; 0 = the default profile
	SourcePref   string // "" = follow Settings; else usenet, torrent or both
	AddedBy      int64  // account that added it; 0 = unknown
	// Genres are TMDB genre names; nil means not fetched yet (as for Movie).
	Genres []string
	// NoUpgrade leaves every episode out of the search for better versions.
	NoUpgrade bool
	// DetailsState and DetailsNote work as on Movie.
	DetailsState string
	DetailsNote  string
	// SeriesType is how release names number the episodes: SeriesStandard,
	// SeriesAnime or SeriesDaily.
	SeriesType string

	// Populated by GetSeries/ListSeries only, for the library views.
	EpisodeCount    int
	DownloadedCount int
}

// Episode is one episode of a Series, tracked individually with the same
// missing -> downloading -> downloaded lifecycle as a Movie.
type Episode struct {
	ID        int64
	SeriesID  int64
	Season    int
	Episode   int
	Title     string
	Overview  string
	AirDate   string // "YYYY-MM-DD", empty if TMDB has no date yet
	Status    Status
	Quality   string
	FilePath  string
	Monitored bool
}

const seriesSelect = `
	SELECT s.id, s.tmdb_id, s.title, COALESCE(s.year, 0), COALESCE(s.overview, ''), COALESCE(s.poster_path, ''),
	       COALESCE(s.first_air_date, ''), s.monitored,
	       (SELECT COUNT(*) FROM episodes e WHERE e.series_id = s.id AND (e.season > 0 OR e.monitored = 1 OR e.status = 'downloaded')),
	       (SELECT COUNT(*) FROM episodes e WHERE e.series_id = s.id AND e.status = 'downloaded'),
	       COALESCE(s.profile_id, 0), s.source_pref, COALESCE(s.added_by, 0), s.genres, s.no_upgrade, s.details_state, s.details_note, s.series_type
	FROM series s`

func scanSeries(scan func(dest ...any) error) (Series, error) {
	var (
		s      Series
		genres sql.NullString
	)
	if err := scan(&s.ID, &s.TMDBID, &s.Title, &s.Year, &s.Overview, &s.PosterPath, &s.FirstAirDate, &s.Monitored, &s.EpisodeCount, &s.DownloadedCount, &s.ProfileID, &s.SourcePref, &s.AddedBy, &genres, &s.NoUpgrade, &s.DetailsState, &s.DetailsNote, &s.SeriesType); err != nil {
		return Series{}, fmt.Errorf("scan series: %w", err)
	}
	s.Genres = decodeGenres(genres)
	return s, nil
}

// AddSeries inserts a series and its full episode list in one transaction,
// so a failure halfway through never leaves a series with a partial
// episode list.
func (r *Repo) AddSeries(s Series, episodes []Episode) (Series, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return Series{}, fmt.Errorf("begin add series: %w", err)
	}
	defer tx.Rollback()

	genres, err := encodeGenres(s.Genres)
	if err != nil {
		return Series{}, err
	}
	res, err := tx.Exec(
		`INSERT INTO series (tmdb_id, title, year, overview, poster_path, first_air_date, monitored, added_by, genres, no_upgrade, series_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.TMDBID, s.Title, s.Year, s.Overview, s.PosterPath, s.FirstAirDate, s.Monitored, nullID(s.AddedBy), genres, s.NoUpgrade, ValidSeriesType(s.SeriesType),
	)
	if err != nil {
		return Series{}, fmt.Errorf("insert series: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Series{}, fmt.Errorf("get inserted series id: %w", err)
	}
	if err := upsertEpisodes(tx, id, episodes, s.Monitored); err != nil {
		return Series{}, err
	}
	if err := tx.Commit(); err != nil {
		return Series{}, fmt.Errorf("commit add series: %w", err)
	}
	return r.GetSeries(id)
}

// UpsertEpisodes inserts episodes that don't exist yet and refreshes the
// title/overview/air date of ones that do (new seasons appear and air
// dates get announced/moved over a show's life) — never touching an
// existing episode's status, quality, or file, which are download state.
func (r *Repo) UpsertEpisodes(seriesID int64, episodes []Episode, monitorNew bool) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin upsert episodes: %w", err)
	}
	defer tx.Rollback()
	if err := upsertEpisodes(tx, seriesID, episodes, monitorNew); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertEpisodes(tx *sql.Tx, seriesID int64, episodes []Episode, monitorNew bool) error {
	stmt, err := tx.Prepare(`
		INSERT INTO episodes (series_id, season, episode, title, overview, air_date, monitored)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (series_id, season, episode) DO UPDATE SET
			title = excluded.title, overview = excluded.overview, air_date = excluded.air_date`)
	if err != nil {
		return fmt.Errorf("prepare upsert episode: %w", err)
	}
	defer stmt.Close()
	for _, e := range episodes {
		// Specials (season 0) start unmonitored: they are rarely posted in a
		// searchable form, so they would only ever show as missing.
		if _, err := stmt.Exec(seriesID, e.Season, e.Episode, e.Title, e.Overview, e.AirDate, monitorNew && e.Season > 0); err != nil {
			return fmt.Errorf("upsert episode S%02dE%02d: %w", e.Season, e.Episode, err)
		}
	}
	return nil
}

func (r *Repo) GetSeries(id int64) (Series, error) {
	return scanSeries(r.db.QueryRow(seriesSelect+` WHERE s.id = ?`, id).Scan)
}

// GetSeriesByTMDBID reports whether a TMDB show is already in the library.
func (r *Repo) GetSeriesByTMDBID(tmdbID int) (Series, bool, error) {
	s, err := scanSeries(r.db.QueryRow(seriesSelect+` WHERE s.tmdb_id = ?`, tmdbID).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Series{}, false, nil
		}
		return Series{}, false, err
	}
	return s, true, nil
}

func (r *Repo) ListSeries() ([]Series, error) {
	rows, err := r.db.Query(seriesSelect + ` ORDER BY s.added_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list series: %w", err)
	}
	defer rows.Close()
	var out []Series
	for rows.Next() {
		s, err := scanSeries(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetSeriesMonitored turns automatic searching for a whole series on or off.
func (r *Repo) SetSeriesMonitored(id int64, monitored bool) error {
	_, err := r.db.Exec(`UPDATE series SET monitored = ? WHERE id = ?`, monitored, id)
	if err != nil {
		return fmt.Errorf("set series %d monitored: %w", id, err)
	}
	return nil
}

// SetEpisodeMonitored turns automatic searching for one episode on or off.
func (r *Repo) SetEpisodeMonitored(id int64, monitored bool) error {
	_, err := r.db.Exec(`UPDATE episodes SET monitored = ? WHERE id = ?`, monitored, id)
	if err != nil {
		return fmt.Errorf("set episode %d monitored: %w", id, err)
	}
	return nil
}

// SetSeasonMonitored sets the monitored flag on every episode of a season.
func (r *Repo) SetSeasonMonitored(seriesID int64, season int, monitored bool) error {
	_, err := r.db.Exec(`UPDATE episodes SET monitored = ? WHERE series_id = ? AND season = ?`, monitored, seriesID, season)
	if err != nil {
		return fmt.Errorf("set series %d season %d monitored: %w", seriesID, season, err)
	}
	return nil
}

// SetSeriesSourcePref sets which downloaders a series may use ("" = the default).
func (r *Repo) SetSeriesSourcePref(id int64, pref string) error {
	_, err := r.db.Exec(`UPDATE series SET source_pref = ? WHERE id = ?`, pref, id)
	if err != nil {
		return fmt.Errorf("set series %d source preference: %w", id, err)
	}
	return nil
}

// MonitorFromDate sets every episode of a series monitored when it airs on or
// after date (or has no date yet) and unmonitored when it aired earlier, so
// a newly added show can track only what is still to come.
func (r *Repo) MonitorFromDate(seriesID int64, date string) error {
	_, err := r.db.Exec(
		`UPDATE episodes SET monitored = CASE WHEN COALESCE(air_date, '') = '' OR air_date >= ? THEN 1 ELSE 0 END WHERE series_id = ? AND season > 0`,
		date, seriesID)
	if err != nil {
		return fmt.Errorf("set series %d episode monitoring: %w", seriesID, err)
	}
	return nil
}

// SetAllEpisodesMonitored sets the monitored flag on every episode of a series.
func (r *Repo) SetAllEpisodesMonitored(seriesID int64, monitored bool) error {
	// Switching a whole show on leaves its specials as they are.
	_, err := r.db.Exec(`UPDATE episodes SET monitored = ? WHERE series_id = ? AND (season > 0 OR ? = 0)`, monitored, seriesID, monitored)
	if err != nil {
		return fmt.Errorf("set series %d episodes monitored: %w", seriesID, err)
	}
	return nil
}

// SetSeriesNoUpgrade turns the search for better versions of a show's
// episodes off (or back on).
func (r *Repo) SetSeriesNoUpgrade(id int64, noUpgrade bool) error {
	_, err := r.db.Exec(`UPDATE series SET no_upgrade = ? WHERE id = ?`, noUpgrade, id)
	if err != nil {
		return fmt.Errorf("set series %d no-upgrade: %w", id, err)
	}
	return nil
}

// SetSeriesProfile assigns a quality profile to a series (0 = use the default).
func (r *Repo) SetSeriesProfile(id, profileID int64) error {
	_, err := r.db.Exec(`UPDATE series SET profile_id = NULLIF(?, 0) WHERE id = ?`, profileID, id)
	if err != nil {
		return fmt.Errorf("set series %d profile: %w", id, err)
	}
	return nil
}

// DeleteSeries removes a show with its episodes and blocklist entries (via
// foreign keys) and the detailed events shown only on its own page. Lines in
// the Activity feed are kept, detached.
func (r *Repo) DeleteSeries(id int64) error {
	return r.deleteWithItemEvents("series", "series_id", id)
}

const episodeSelect = `
	SELECT id, series_id, season, episode, COALESCE(title, ''), COALESCE(overview, ''), COALESCE(air_date, ''),
	       status, COALESCE(quality, ''), COALESCE(file_path, ''), monitored
	FROM episodes`

func scanEpisode(scan func(dest ...any) error) (Episode, error) {
	var e Episode
	var status string
	if err := scan(&e.ID, &e.SeriesID, &e.Season, &e.Episode, &e.Title, &e.Overview, &e.AirDate, &status, &e.Quality, &e.FilePath, &e.Monitored); err != nil {
		return Episode{}, fmt.Errorf("scan episode: %w", err)
	}
	e.Status = Status(status)
	return e, nil
}

// ListEpisodes returns every episode of a series ordered by season then
// episode number.
func (r *Repo) ListEpisodes(seriesID int64) ([]Episode, error) {
	rows, err := r.db.Query(episodeSelect+` WHERE series_id = ? ORDER BY season, episode`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}
	defer rows.Close()
	var out []Episode
	for rows.Next() {
		e, err := scanEpisode(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetEpisodeByID returns one episode by its own id.
func (r *Repo) GetEpisodeByID(id int64) (Episode, error) {
	return scanEpisode(r.db.QueryRow(episodeSelect+` WHERE id = ?`, id).Scan)
}

// MoveEpisodeFile points every episode whose file is from at to instead
// (after the file was renamed on disk).
func (r *Repo) MoveEpisodeFile(from, to string) error {
	if _, err := r.db.Exec(`UPDATE episodes SET file_path = ? WHERE file_path = ?`, to, from); err != nil {
		return fmt.Errorf("update episode file: %w", err)
	}
	return nil
}

func (r *Repo) GetEpisode(seriesID int64, season, episode int) (Episode, error) {
	return scanEpisode(r.db.QueryRow(episodeSelect+` WHERE series_id = ? AND season = ? AND episode = ?`, seriesID, season, episode).Scan)
}

// SetEpisodeStatus mirrors Repo.SetStatus for movies: empty quality/
// filePath leave the existing values alone.
func (r *Repo) SetEpisodeStatus(id int64, status Status, quality, filePath string) error {
	_, err := r.db.Exec(
		`UPDATE episodes SET status = ?, quality = COALESCE(NULLIF(?, ''), quality), file_path = COALESCE(NULLIF(?, ''), file_path) WHERE id = ?`,
		string(status), quality, filePath, id,
	)
	if err != nil {
		return fmt.Errorf("update episode %d status: %w", id, err)
	}
	return nil
}
