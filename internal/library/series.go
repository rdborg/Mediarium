package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/origin"
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
	// Origin is how it came into the library, as on Movie.
	Origin origin.Origin
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
	// EpisodeOrder is the list a show's regular seasons follow:
	// EpisodeOrderTMDB (the default) or EpisodeOrderTVDB.
	EpisodeOrder string
	// CopyOf is the main show's id when this row is a second copy of it,
	// with its own episodes, kept in another quality; 0 for the main show.
	CopyOf int64
	// TVDBID and IMDBID are the show's ids elsewhere, from TMDB (0 / "" until
	// the show is next refreshed).
	TVDBID int
	IMDBID string
	// TMDBStatus is TMDB's status for the show ("Returning Series", "Ended",
	// "Canceled"…), "" until the show is next refreshed.
	TMDBStatus string
	// StatusCheckedAt is when TMDB's details for the show were last read
	// (RFC 3339, UTC), "" if never.
	StatusCheckedAt string
	// TVmazeID is the show's id on TVmaze: 0 until looked up, -1 when TVmaze
	// does not know the show. TVmazeCheckedAt is when TVmaze was last asked
	// about it (RFC 3339, UTC), "" if never.
	TVmazeID        int
	TVmazeCheckedAt string
	// RootPath is the library folder the show's files go to (one of the TV
	// folders in Settings); "" = the default folder.
	RootPath string
	// SeasonPack is the show's own choice about taking a season pack once a
	// season has ended: "" follows the setting in Settings, "on" or "off" decides
	// for this show.
	SeasonPack string

	// OriginCountry lists the countries TMDB gives for the show, as two-letter
	// codes ("US", "GB"). nil means not read yet (the next refresh fills it
	// in); an empty, non-nil list means TMDB names none.
	OriginCountry []string

	// YieldsUntagged is set when the list was loaded for the automatic searches
	// (never stored): another show in the library has the same name and takes
	// the releases that carry no country tag, so this one does not. See
	// markSameName in the api package.
	YieldsUntagged bool

	// Aliases are other names the show is known by, found with its numbering
	// (see aliases.go); release names that use one are taken for the show.
	Aliases []string

	// AltTitles are other titles of the show (its title where a release was
	// made, such as a German one), as for Movie.AltTitles.
	AltTitles []string

	// Populated by GetSeries/ListSeries only, for the library views.
	EpisodeCount    int
	DownloadedCount int
}

// Episode is one episode of a Series, tracked individually with the same
// missing -> downloading -> downloaded lifecycle as a Movie.
type Episode struct {
	ID       int64
	SeriesID int64
	Season   int
	Episode  int
	Title    string
	Overview string
	AirDate  string // "YYYY-MM-DD", empty if TMDB has no date yet
	// AirAt is the exact moment the episode airs (RFC 3339, UTC), from
	// TVmaze; "" when no time is known. AirDate stays the day TMDB gives.
	AirAt     string
	Status    Status
	Quality   string
	FilePath  string
	Monitored bool
	// ReleaseTitle is the release the file came from ("" when unknown).
	ReleaseTitle string
}

// AirSlack is how far ahead an exact air time may be before an episode whose
// date has come still counts as not aired yet: a few hours either way are
// not worth holding a search for.
const AirSlack = 3 * time.Hour

// AirsLater reports whether the episode's exact air time is known and is
// still more than AirSlack after now. Such an episode is not missing yet, even
// when its date (the day TMDB gives, which can be a day early for a show that
// airs in the evening in the Americas) is today. Without a known time this is
// false, so the date alone decides, as it always did.
func (e Episode) AirsLater(now time.Time) bool {
	if e.AirAt == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, e.AirAt)
	if err != nil {
		return false
	}
	return at.After(now.Add(AirSlack))
}

const seriesSelect = `
	SELECT s.id, s.tmdb_id, s.title, COALESCE(s.year, 0), COALESCE(s.overview, ''), COALESCE(s.poster_path, ''),
	       COALESCE(s.first_air_date, ''), s.monitored,
	       (SELECT COUNT(*) FROM episodes e WHERE e.series_id = s.id AND (e.season > 0 OR e.monitored = 1 OR e.status = 'downloaded')),
	       (SELECT COUNT(*) FROM episodes e WHERE e.series_id = s.id AND e.status = 'downloaded'),
	       COALESCE(s.profile_id, 0), s.source_pref, COALESCE(s.added_by, 0), s.genres, s.no_upgrade, s.details_state, s.details_note, s.series_type,
	       COALESCE(s.copy_of, 0), s.tvdb_id, s.imdb_id, s.tmdb_status, s.root_path, s.season_pack,
	       COALESCE((SELECT names FROM title_names WHERE kind = 'tv' AND tmdb_id = s.tmdb_id), ''),
	       s.origin_country, s.status_checked_at, s.tvmaze_id, s.tvmaze_checked_at, s.episode_order, s.origin_kind, s.origin_name
	FROM series s`

func scanSeries(scan func(dest ...any) error) (Series, error) {
	var (
		s      Series
		genres sql.NullString
		alts   string
		origin sql.NullString
	)
	if err := scan(&s.ID, &s.TMDBID, &s.Title, &s.Year, &s.Overview, &s.PosterPath, &s.FirstAirDate, &s.Monitored, &s.EpisodeCount, &s.DownloadedCount, &s.ProfileID, &s.SourcePref, &s.AddedBy, &genres, &s.NoUpgrade, &s.DetailsState, &s.DetailsNote, &s.SeriesType, &s.CopyOf, &s.TVDBID, &s.IMDBID, &s.TMDBStatus, &s.RootPath, &s.SeasonPack, &alts, &origin, &s.StatusCheckedAt, &s.TVmazeID, &s.TVmazeCheckedAt, &s.EpisodeOrder, &s.Origin.Kind, &s.Origin.Name); err != nil {
		return Series{}, fmt.Errorf("scan series: %w", err)
	}
	s.EpisodeOrder = ValidEpisodeOrder(s.EpisodeOrder)
	s.OriginCountry = decodeCountries(origin)
	s.Genres = decodeGenres(genres)
	s.AltTitles = decodeTitleNames(alts)
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
		`INSERT INTO series (tmdb_id, title, year, overview, poster_path, first_air_date, monitored, added_by, genres, no_upgrade, series_type, root_path, origin_kind, origin_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.TMDBID, s.Title, s.Year, s.Overview, s.PosterPath, s.FirstAirDate, s.Monitored, nullID(s.AddedBy), genres, s.NoUpgrade, ValidSeriesType(s.SeriesType), s.RootPath, s.Origin.Clean().Kind, s.Origin.Clean().Name,
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

// PruneEpisodes removes the episodes of a show that TMDB no longer lists
// (merged, renumbered or dropped) and that never got anything: still missing,
// with no file. keep is TMDB's current list. An episode with a file or being
// downloaded stays whatever TMDB says. An empty list, or one that covers
// less than half of what is stored, is treated as a bad answer and prunes
// nothing. It returns how many rows went.
func (r *Repo) PruneEpisodes(seriesID int64, keep []Episode) (int, error) {
	if len(keep) == 0 {
		return 0, nil
	}
	listed := make(map[[2]int]bool, len(keep))
	for _, e := range keep {
		listed[[2]int{e.Season, e.Episode}] = true
	}
	rows, err := r.db.Query(`SELECT id, season, episode, status, COALESCE(file_path, '') FROM episodes WHERE series_id = ?`, seriesID)
	if err != nil {
		return 0, fmt.Errorf("prune episodes: %w", err)
	}
	var stale []int64
	total := 0
	for rows.Next() {
		var id int64
		var season, episode int
		var status, file string
		if err := rows.Scan(&id, &season, &episode, &status, &file); err != nil {
			rows.Close()
			return 0, fmt.Errorf("prune episodes: %w", err)
		}
		total++
		if !listed[[2]int{season, episode}] && status == string(StatusMissing) && file == "" {
			stale = append(stale, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("prune episodes: %w", err)
	}
	if len(stale) == 0 || len(keep)*2 < total {
		return 0, nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("prune episodes: %w", err)
	}
	defer tx.Rollback()
	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM episodes WHERE id = ? AND status = 'missing' AND COALESCE(file_path, '') = ''`, id); err != nil {
			return 0, fmt.Errorf("prune episode %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("prune episodes: %w", err)
	}
	return len(stale), nil
}

func upsertEpisodes(tx *sql.Tx, seriesID int64, episodes []Episode, monitorNew bool) error {
	stmt, err := tx.Prepare(`
		INSERT INTO episodes (series_id, season, episode, title, overview, air_date, monitored)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (series_id, season, episode) DO UPDATE SET
			title = excluded.title, overview = excluded.overview, air_date = excluded.air_date,
			air_at = CASE WHEN excluded.air_date = episodes.air_date THEN episodes.air_at ELSE NULL END`)
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
	s, err := scanSeries(r.db.QueryRow(seriesSelect+` WHERE s.id = ?`, id).Scan)
	if err == nil {
		r.loadAliases(&s)
	}
	return s, err
}

// GetSeriesByTMDBID reports whether a TMDB show is already in the library.
func (r *Repo) GetSeriesByTMDBID(tmdbID int) (Series, bool, error) {
	s, err := scanSeries(r.db.QueryRow(seriesSelect+` WHERE s.tmdb_id = ? AND s.copy_of IS NULL`, tmdbID).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Series{}, false, nil
		}
		return Series{}, false, err
	}
	r.loadAliases(&s)
	return s, true, nil
}

// ListSeries lists the library's shows, second copies left out.
func (r *Repo) ListSeries() ([]Series, error) {
	return r.listSeries(` WHERE s.copy_of IS NULL ORDER BY s.added_at DESC`)
}

// ListSeriesNames lists every main show with only what tells same-named shows
// apart (id, title, year, origin countries). The other fields stay empty. It
// is one plain table read, for screens that only need the names and must not
// pay for the episode counts and aliases ListSeries works out.
func (r *Repo) ListSeriesNames() ([]Series, error) {
	rows, err := r.db.Query(`SELECT id, title, COALESCE(year, 0), origin_country FROM series WHERE copy_of IS NULL ORDER BY added_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list show names: %w", err)
	}
	defer rows.Close()
	var out []Series
	for rows.Next() {
		var (
			s      Series
			origin sql.NullString
		)
		if err := rows.Scan(&s.ID, &s.Title, &s.Year, &origin); err != nil {
			return nil, fmt.Errorf("scan show name: %w", err)
		}
		s.OriginCountry = decodeCountries(origin)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListSeriesWithCopies lists every show row, second copies included.
func (r *Repo) ListSeriesWithCopies() ([]Series, error) {
	return r.listSeries(` ORDER BY s.added_at DESC`)
}

// ListSeriesCopies lists the second copies of a show, oldest first.
func (r *Repo) ListSeriesCopies(mainID int64) ([]Series, error) {
	return r.listSeries(` WHERE s.copy_of = ? ORDER BY s.id`, mainID)
}

// AddSeriesCopy adds a second copy of a show in another quality profile,
// with the same episodes (missing, monitored as on the main show), to be
// downloaded into the copies folder.
func (r *Repo) AddSeriesCopy(mainID, profileID int64) (Series, error) {
	main, err := r.GetSeries(mainID)
	if err != nil {
		return Series{}, err
	}
	if main.CopyOf != 0 {
		return Series{}, errors.New("that is already a second copy")
	}
	if main.ProfileID == profileID {
		return Series{}, ErrCopySameProfile
	}
	tx, err := r.db.Begin()
	if err != nil {
		return Series{}, fmt.Errorf("begin adding a copy: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO series (tmdb_id, title, year, overview, poster_path, first_air_date, monitored, profile_id, source_pref, added_by, genres, series_type, origin_country, episode_order, copy_of)
		 SELECT tmdb_id, title, year, overview, poster_path, first_air_date, monitored, ?, source_pref, added_by, genres, series_type, origin_country, episode_order, id FROM series WHERE id = ?
		 AND NOT EXISTS (SELECT 1 FROM series WHERE copy_of = ? AND profile_id = ?)`, profileID, mainID, mainID, profileID)
	if err != nil {
		return Series{}, fmt.Errorf("add a second copy: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Series{}, ErrCopyExists
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Series{}, fmt.Errorf("get the copy's id: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO episodes (series_id, season, episode, title, overview, air_date, air_at, monitored)
		 SELECT ?, season, episode, title, overview, air_date, air_at, monitored FROM episodes WHERE series_id = ?`, id, mainID); err != nil {
		return Series{}, fmt.Errorf("copy the episode list: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Series{}, fmt.Errorf("add a second copy: %w", err)
	}
	return r.GetSeries(id)
}

func (r *Repo) listSeries(where string, args ...any) ([]Series, error) {
	rows, err := r.db.Query(seriesSelect+where, args...)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	r.loadAllAliases(out)
	return out, nil
}

// SetSeriesMonitored turns automatic searching for a whole series on or off.
func (r *Repo) SetSeriesMonitored(id int64, monitored bool) error {
	_, err := r.db.Exec(`UPDATE series SET monitored = ? WHERE id = ?`, monitored, id)
	if err != nil {
		return fmt.Errorf("set series %d monitored: %w", id, err)
	}
	return nil
}

// EnableSeriesOfEpisode switches a show's own Monitored switch on, given one
// of its episodes. A show with an episode that is monitored is monitored: the
// switch is what automation and the Wanted lists look at first.
func (r *Repo) EnableSeriesOfEpisode(episodeID int64) error {
	_, err := r.db.Exec(`UPDATE series SET monitored = 1 WHERE monitored = 0 AND id = (SELECT series_id FROM episodes WHERE id = ?)`, episodeID)
	if err != nil {
		return fmt.Errorf("switch on the show of episode %d: %w", episodeID, err)
	}
	return nil
}

// RepairFutureOnlyMonitoring switches monitoring back on for shows that were
// set to "only episodes that haven't aired yet" while their own Monitored
// switch ended up off. Such a show looks like this: the switch is off, at
// least one real episode that has not aired (or has no date) is monitored,
// no aired episode is monitored, and at least one aired episode is not. A
// show someone turned off on purpose keeps all its episodes monitored, so it
// does not look like that and is left alone. It does nothing the second time
// (the switch is on by then). today is a YYYY-MM-DD date. It returns how many
// shows were changed.
func (r *Repo) RepairFutureOnlyMonitoring(today string) (int, error) {
	res, err := r.db.Exec(`UPDATE series SET monitored = 1
		WHERE monitored = 0 AND copy_of IS NULL
		AND EXISTS (SELECT 1 FROM episodes e WHERE e.series_id = series.id AND e.season > 0 AND e.monitored = 1 AND (COALESCE(e.air_date, '') = '' OR e.air_date >= ?))
		AND NOT EXISTS (SELECT 1 FROM episodes e WHERE e.series_id = series.id AND e.season > 0 AND e.monitored = 1 AND COALESCE(e.air_date, '') <> '' AND e.air_date < ?)
		AND EXISTS (SELECT 1 FROM episodes e WHERE e.series_id = series.id AND e.season > 0 AND e.monitored = 0 AND COALESCE(e.air_date, '') <> '' AND e.air_date < ?)`, today, today, today)
	if err != nil {
		return 0, fmt.Errorf("repair shows set to future episodes only: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
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
	_, err := r.ApplyMonitorMode(seriesID, MonitorFuture, date, false)
	return err
}

// The ways to choose which episodes of a show are monitored, when adding it
// and from its page afterwards.
const (
	MonitorAll    = "all"
	MonitorFuture = "future"
	MonitorNone   = "none"
)

// MonitorChange is how many episodes stopped being monitored and how many
// started.
type MonitorChange struct {
	Stopped int `json:"stopped"`
	Started int `json:"started"`
}

// ApplyMonitorMode sets which episodes of a show are monitored and returns
// how many stopped and how many started. all monitors every episode but the specials, future
// monitors the ones that air on or after today (a YYYY-MM-DD date) or have
// no air date yet and stops monitoring the ones that aired earlier (specials
// stay as they are), none stops monitoring everything. Only the monitored
// flag changes: files, status and downloads are left alone. dryRun counts
// without changing anything.
func (r *Repo) ApplyMonitorMode(seriesID int64, mode, today string, dryRun bool) (MonitorChange, error) {
	var set, scope string
	var setArgs []any
	switch mode {
	case MonitorAll:
		set, scope = "1", "season > 0"
	case MonitorFuture:
		set, scope = "CASE WHEN COALESCE(air_date, '') = '' OR air_date >= ? THEN 1 ELSE 0 END", "season > 0"
		setArgs = []any{today}
	case MonitorNone:
		set, scope = "0", "1 = 1"
	default:
		return MonitorChange{}, fmt.Errorf("unknown monitor mode %q", mode)
	}
	tx, err := r.db.Begin()
	if err != nil {
		return MonitorChange{}, fmt.Errorf("begin series %d episode monitoring: %w", seriesID, err)
	}
	defer tx.Rollback()
	var change MonitorChange
	rows, err := tx.Query(`SELECT monitored, COUNT(*) FROM episodes WHERE series_id = ? AND `+scope+` AND monitored <> (`+set+`) GROUP BY monitored`,
		append([]any{seriesID}, setArgs...)...)
	if err != nil {
		return MonitorChange{}, fmt.Errorf("count series %d episode monitoring: %w", seriesID, err)
	}
	for rows.Next() {
		var was bool
		var n int
		if err := rows.Scan(&was, &n); err != nil {
			rows.Close()
			return MonitorChange{}, fmt.Errorf("count series %d episode monitoring: %w", seriesID, err)
		}
		if was {
			change.Stopped = n
		} else {
			change.Started = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return MonitorChange{}, fmt.Errorf("count series %d episode monitoring: %w", seriesID, err)
	}
	if dryRun || change.Stopped+change.Started == 0 {
		return change, nil
	}
	if _, err := tx.Exec(`UPDATE episodes SET monitored = `+set+` WHERE series_id = ? AND `+scope, append(setArgs, seriesID)...); err != nil {
		return MonitorChange{}, fmt.Errorf("set series %d episode monitoring: %w", seriesID, err)
	}
	if err := tx.Commit(); err != nil {
		return MonitorChange{}, fmt.Errorf("commit series %d episode monitoring: %w", seriesID, err)
	}
	return change, nil
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

// Values of a show's own season pack choice (Series.SeasonPack).
const (
	SeasonPackDefault = ""
	SeasonPackOn      = "on"
	SeasonPackOff     = "off"
)

// ValidSeasonPack reports whether v is one of the three choices.
func ValidSeasonPack(v string) bool {
	return v == SeasonPackDefault || v == SeasonPackOn || v == SeasonPackOff
}

// SetSeriesSeasonPack sets the show's own choice about season packs ("" = follow
// the setting in Settings).
func (r *Repo) SetSeriesSeasonPack(id int64, choice string) error {
	if !ValidSeasonPack(choice) {
		return fmt.Errorf("set series %d season pack: %q is not a choice", id, choice)
	}
	_, err := r.db.Exec(`UPDATE series SET season_pack = ? WHERE id = ?`, choice, id)
	if err != nil {
		return fmt.Errorf("set series %d season pack: %w", id, err)
	}
	// A changed choice is a reason to look for packs again.
	return r.ResetSeasonPackTries(id)
}

// SeasonPackGiveUpDays is how long a season that kept finding no season pack is
// left alone after its last search.
const SeasonPackGiveUpDays = 60

// PackSearch is what is remembered about the searches for a season pack of one
// season.
type PackSearch struct {
	At       string // when it was last searched for (RFC 3339)
	Tries    int    // searches in a row that found no pack
	Episodes int    // how many episodes the season had then
}

// SeasonPackSearches returns what is remembered about the searches for a season
// pack of each season of the show, keyed by season.
func (r *Repo) SeasonPackSearches(seriesID int64) (map[int]PackSearch, error) {
	rows, err := r.db.Query(`SELECT season, searched_at, tries, episodes FROM season_pack_searches WHERE series_id = ?`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("list season pack searches: %w", err)
	}
	defer rows.Close()
	out := map[int]PackSearch{}
	for rows.Next() {
		var season int
		var p PackSearch
		if err := rows.Scan(&season, &p.At, &p.Tries, &p.Episodes); err != nil {
			return nil, fmt.Errorf("scan season pack search: %w", err)
		}
		out[season] = p
	}
	return out, rows.Err()
}

// NoteSeasonPackSearch records that a season (with this many episodes) was
// searched for a season pack at the given time (RFC 3339). found says a pack
// was taken, which clears the count of searches that found nothing; otherwise
// the count goes up by one, or starts again at one when the season has
// another number of episodes than at the last search or that search was more
// than SeasonPackGiveUpDays ago.
func (r *Repo) NoteSeasonPackSearch(seriesID int64, season int, at string, episodes int, found bool) error {
	tries := 1
	if found {
		tries = 0
	}
	cutoff := ""
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		cutoff = t.UTC().AddDate(0, 0, -SeasonPackGiveUpDays).Format(time.RFC3339)
	}
	_, err := r.db.Exec(`INSERT INTO season_pack_searches (series_id, season, searched_at, tries, episodes) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(series_id, season) DO UPDATE SET
			tries = CASE WHEN ? = 0 THEN 0
				WHEN season_pack_searches.episodes <> excluded.episodes OR season_pack_searches.searched_at <= ? THEN 1
				ELSE season_pack_searches.tries + 1 END,
			searched_at = excluded.searched_at, episodes = excluded.episodes`,
		seriesID, season, at, tries, episodes, tries, cutoff)
	if err != nil {
		return fmt.Errorf("note season pack search: %w", err)
	}
	return nil
}

// ResetSeasonPackTries lets the seasons of a show (every show when seriesID is
// 0) be searched for a season pack again, as after a pack setting was changed.
// When each season was last searched stays as it is.
func (r *Repo) ResetSeasonPackTries(seriesID int64) error {
	var err error
	if seriesID == 0 {
		_, err = r.db.Exec(`UPDATE season_pack_searches SET tries = 0`)
	} else {
		_, err = r.db.Exec(`UPDATE season_pack_searches SET tries = 0 WHERE series_id = ?`, seriesID)
	}
	if err != nil {
		return fmt.Errorf("reset season pack searches: %w", err)
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
	       status, COALESCE(quality, ''), COALESCE(file_path, ''), monitored, release_title, COALESCE(air_at, '')
	FROM episodes`

func scanEpisode(scan func(dest ...any) error) (Episode, error) {
	var e Episode
	var status string
	if err := scan(&e.ID, &e.SeriesID, &e.Season, &e.Episode, &e.Title, &e.Overview, &e.AirDate, &status, &e.Quality, &e.FilePath, &e.Monitored, &e.ReleaseTitle, &e.AirAt); err != nil {
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

// SetEpisodeReleaseTitle records the release an episode's file came from.
func (r *Repo) SetEpisodeReleaseTitle(id int64, release string) error {
	if _, err := r.db.Exec(`UPDATE episodes SET release_title = ? WHERE id = ?`, release, id); err != nil {
		return fmt.Errorf("set episode %d release: %w", id, err)
	}
	return nil
}

// episodeFilesQuery is answered from the episodes_with_file index alone, which
// holds only the episodes that name a file.
const episodeFilesQuery = `SELECT DISTINCT series_id, file_path FROM episodes WHERE file_path IS NOT NULL AND file_path != ''`

// EpisodeFilesBySeries lists each show's episode files, every file once
// (a file holding two episodes is listed once).
func (r *Repo) EpisodeFilesBySeries() (map[int64][]string, error) {
	rows, err := r.db.Query(episodeFilesQuery)
	if err != nil {
		return nil, fmt.Errorf("list episode files: %w", err)
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, fmt.Errorf("scan episode file: %w", err)
		}
		out[id] = append(out[id], path)
	}
	return out, rows.Err()
}

// SetSeriesExternalIDs stores a show's TheTVDB and IMDb ids and TMDB status.
// A zero or empty value leaves the saved one alone.
func (r *Repo) SetSeriesExternalIDs(id int64, tvdbID int, imdbID, status string) error {
	if _, err := r.db.Exec(`UPDATE series SET tvdb_id = CASE WHEN ? > 0 THEN ? ELSE tvdb_id END, imdb_id = CASE WHEN ? != '' THEN ? ELSE imdb_id END,
		tmdb_status = CASE WHEN ? != '' THEN ? ELSE tmdb_status END WHERE id = ? OR copy_of = ?`,
		tvdbID, tvdbID, imdbID, imdbID, status, status, id, id); err != nil {
		return fmt.Errorf("set series external ids: %w", err)
	}
	return nil
}

// SetSeriesTVmaze records a show's id on TVmaze (0 to keep what is stored,
// -1 for "TVmaze does not know it") and that TVmaze was asked at t (the zero
// time keeps the stored one). A second copy of the show shares the main
// show's answer.
func (r *Repo) SetSeriesTVmaze(id int64, tvmazeID int, t time.Time) error {
	checked := ""
	if !t.IsZero() {
		checked = t.UTC().Format(time.RFC3339)
	}
	if _, err := r.db.Exec(`UPDATE series SET tvmaze_id = CASE WHEN ? != 0 THEN ? ELSE tvmaze_id END, tvmaze_checked_at = CASE WHEN ? != '' THEN ? ELSE tvmaze_checked_at END WHERE id = ? OR copy_of = ?`,
		tvmazeID, tvmazeID, checked, checked, id, id); err != nil {
		return fmt.Errorf("set series tvmaze: %w", err)
	}
	return nil
}

// AirTime is the moment one episode airs, for SetEpisodeAirTimes.
type AirTime struct {
	Season, Episode int
	At              time.Time
}

// SetEpisodeAirTimes stores when episodes air (UTC). An episode that is not
// in the show is skipped. It returns how many rows changed.
func (r *Repo) SetEpisodeAirTimes(seriesID int64, times []AirTime) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin air times: %w", err)
	}
	defer tx.Rollback()
	changed := 0
	for _, a := range times {
		res, err := tx.Exec(`UPDATE episodes SET air_at = ? WHERE series_id = ? AND season = ? AND episode = ? AND COALESCE(air_at, '') != ?`,
			a.At.UTC().Format(time.RFC3339), seriesID, a.Season, a.Episode, a.At.UTC().Format(time.RFC3339))
		if err != nil {
			return 0, fmt.Errorf("set air time S%02dE%02d: %w", a.Season, a.Episode, err)
		}
		n, _ := res.RowsAffected()
		changed += int(n)
	}
	return changed, tx.Commit()
}

// MarkSeriesChecked records that TMDB's details for a show were read at t.
func (r *Repo) MarkSeriesChecked(id int64, t time.Time) error {
	if _, err := r.db.Exec(`UPDATE series SET status_checked_at = ? WHERE id = ?`, t.UTC().Format(time.RFC3339), id); err != nil {
		return fmt.Errorf("mark series checked: %w", err)
	}
	return nil
}

// decodeCountries reads the stored country list: NULL is nil (not read yet),
// "" an empty list, otherwise the comma-separated codes.
func decodeCountries(v sql.NullString) []string {
	if !v.Valid {
		return nil
	}
	out := []string{}
	for _, c := range strings.Split(v.String, ",") {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// SetSeriesOriginCountry stores the countries TMDB gives for a show (an empty
// list says it names none, so the show is not asked about again). A second
// copy shares the main show's countries.
func (r *Repo) SetSeriesOriginCountry(id int64, countries []string) error {
	clean := make([]string, 0, len(countries))
	for _, c := range countries {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			clean = append(clean, c)
		}
	}
	if _, err := r.db.Exec(`UPDATE series SET origin_country = ? WHERE id = ? OR copy_of = ?`, strings.Join(clean, ","), id, id); err != nil {
		return fmt.Errorf("set series %d country: %w", id, err)
	}
	return nil
}

// SeriesGap is a show whose origin country has not been read yet.
type SeriesGap struct {
	ID     int64
	TMDBID int
	Title  string
}

// MissingOriginCountry lists up to limit shows (newest first, second copies
// left out) whose origin country is still unknown.
func (r *Repo) MissingOriginCountry(limit int) ([]SeriesGap, error) {
	rows, err := r.db.Query(`SELECT id, tmdb_id, title FROM series WHERE origin_country IS NULL AND copy_of IS NULL ORDER BY added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list shows without a country: %w", err)
	}
	defer rows.Close()
	var out []SeriesGap
	for rows.Next() {
		var g SeriesGap
		if err := rows.Scan(&g.ID, &g.TMDBID, &g.Title); err != nil {
			return nil, fmt.Errorf("scan show without a country: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SetSeriesRoot chooses the library folder a show's next files go to ("" = the
// default folder). Files already there stay where they are.
func (r *Repo) SetSeriesRoot(id int64, root string) error {
	_, err := r.db.Exec(`UPDATE series SET root_path = ? WHERE id = ?`, root, id)
	if err != nil {
		return fmt.Errorf("set series %d folder: %w", id, err)
	}
	return nil
}
