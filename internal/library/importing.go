package library

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Importing an existing library happens in two steps so the person is never
// left waiting on the network. RegisterImport adds every chosen title at
// once with only what the scan already knows (one database transaction for
// the whole batch), and a background worker then fills in the details
// (Complete*Import), title by title. Each title has a row in import_items
// that says what is still to do, so unfinished work survives a restart.

// Import states of one title.
const (
	ImportPending = "pending"
	ImportDone    = "done"
	ImportProblem = "problem"
)

// Import outcomes of one title at registration.
const (
	ImportAdded   = "added"
	ImportAlready = "already"
	ImportFailed  = "failed"
)

// ImportFile is an episode file found on disk for a show.
type ImportFile struct {
	Path     string `json:"path"`
	Quality  string `json:"quality"`
	Season   int    `json:"season"`
	Episodes []int  `json:"episodes"`
}

// ImportEntry is one title the person confirmed.
type ImportEntry struct {
	Kind       string // "movie" or "series"
	TMDBID     int
	Title      string
	Year       int
	PosterPath string
	Quality    string       // movies: quality of the main file
	FilePath   string       // movies: the main file
	Files      []ImportFile // shows: the episode files found on disk
	AddedAt    time.Time    // zero means now
}

// ImportOptions are the choices made for a whole import.
type ImportOptions struct {
	Kind           string // "movie" or "tv"
	Root           string
	Monitor        bool // watch the titles for new episodes and better versions
	NoUpgrade      bool // do not look for better versions of what is already there
	MonitorMissing bool // shows: look for the episodes that are missing
}

// ImportOutcome says what registering one entry did.
type ImportOutcome struct {
	ItemID  int64 // the row in import_items
	TitleID int64 // the movie or series that was added (0 when nothing was)
	Outcome string
	Note    string
}

// ImportItem is one title of an import.
type ImportItem struct {
	ID       int64
	BatchID  int64
	Kind     string // "movie" or "series"
	TitleID  int64
	TMDBID   int
	Title    string
	Outcome  string
	State    string
	Note     string
	Attempts int
	NextTry  string
	Files    []ImportFile
	Imported int
	Skipped  int
}

// ImportBatch is one confirmed import, with its counts.
type ImportBatch struct {
	ID             int64
	Kind           string
	Root           string
	CreatedAt      string
	FinishedAt     string
	Dismissed      bool
	Monitor        bool
	NoUpgrade      bool
	MonitorMissing bool
	Added          int // titles added (they all need details)
	Already        int // titles that were in the library already
	Failed         int // titles that could not be added at all
	Pending        int // added titles still waiting for their details
	Problems       int // failed titles plus added titles whose details failed
	// Elapsed is how long the import has run (or ran), by the server's clock,
	// so an estimate of the time left does not depend on the browser's clock.
	Elapsed time.Duration
}

// Running reports whether details are still being filled in.
func (b ImportBatch) Running() bool { return b.FinishedAt == "" }

const timeStamp = "2006-01-02T15:04:05.000Z"

func stamp(t time.Time) string { return t.UTC().Format(timeStamp) }

func titleTable(kind string) string {
	if kind == "movie" {
		return "movies"
	}
	return "series"
}

// RegisterImport adds the entries to the library in one transaction and
// returns what happened to each, in order. Nothing here talks to the
// network. A title that cannot be added is reported as failed and does not
// stop the others.
func (r *Repo) RegisterImport(opts ImportOptions, entries []ImportEntry) (int64, []ImportOutcome, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, nil, fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO import_batches (kind, root, monitor, no_upgrade, monitor_missing) VALUES (?, ?, ?, ?, ?)`,
		opts.Kind, opts.Root, opts.Monitor, opts.NoUpgrade, opts.MonitorMissing)
	if err != nil {
		return 0, nil, fmt.Errorf("insert import batch: %w", err)
	}
	batchID, err := res.LastInsertId()
	if err != nil {
		return 0, nil, fmt.Errorf("get import batch id: %w", err)
	}

	now := time.Now()
	outcomes := make([]ImportOutcome, len(entries))
	for i, e := range entries {
		if _, err := tx.Exec(`SAVEPOINT entry`); err != nil {
			return 0, nil, fmt.Errorf("savepoint: %w", err)
		}
		var out ImportOutcome
		if e.Kind == "movie" {
			out, err = registerMovie(tx, batchID, opts, e, now)
		} else {
			out, err = registerSeries(tx, batchID, opts, e, now)
		}
		if err != nil {
			// Undo this title only and record it as a problem.
			if _, rerr := tx.Exec(`ROLLBACK TO entry`); rerr != nil {
				return 0, nil, fmt.Errorf("roll back entry: %w", rerr)
			}
			out = ImportOutcome{Outcome: ImportFailed, Note: "Couldn't add this title. Try importing it again."}
			if _, ierr := tx.Exec(`INSERT INTO import_items (batch_id, kind, tmdb_id, title, outcome, state, note) VALUES (?, ?, ?, ?, 'failed', 'problem', ?)`,
				batchID, e.Kind, e.TMDBID, e.Title, out.Note); ierr != nil {
				return 0, nil, fmt.Errorf("record failed entry: %w", ierr)
			}
		}
		if _, err := tx.Exec(`RELEASE entry`); err != nil {
			return 0, nil, fmt.Errorf("release savepoint: %w", err)
		}
		outcomes[i] = out
	}
	if err := finishBatchIfDone(tx, batchID); err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("commit import: %w", err)
	}
	return batchID, outcomes, nil
}

func addedStamp(t, now time.Time) string {
	if t.IsZero() || t.After(now) {
		return stamp(now)
	}
	return stamp(t)
}

func registerMovie(tx *sql.Tx, batchID int64, opts ImportOptions, e ImportEntry, now time.Time) (ImportOutcome, error) {
	var (
		id     int64
		status string
	)
	err := tx.QueryRow(`SELECT id, status FROM movies WHERE tmdb_id = ?`, e.TMDBID).Scan(&id, &status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ImportOutcome{}, fmt.Errorf("look up movie: %w", err)
	}
	known := err == nil
	if known && Status(status) == StatusDownloaded {
		return recordImportItem(tx, batchID, e, id, ImportAlready, ImportDone, "Already in your library.", nil, false)
	}

	state := ImportPending
	if known {
		// Known but not downloaded: the file on disk is what it was waiting
		// for. The row already has its details.
		state = ImportDone
		if _, err := tx.Exec(`UPDATE movies SET status = 'downloaded', quality = COALESCE(NULLIF(?, ''), quality), file_path = ?, no_upgrade = ?, updated_at = ? WHERE id = ?`,
			e.Quality, e.FilePath, opts.NoUpgrade, stamp(now), id); err != nil {
			return ImportOutcome{}, fmt.Errorf("update movie: %w", err)
		}
	} else {
		res, err := tx.Exec(`INSERT INTO movies (tmdb_id, title, year, overview, poster_path, status, quality, file_path, monitored, added_at, updated_at, no_upgrade, details_state)
			VALUES (?, ?, ?, '', ?, 'downloaded', ?, ?, ?, ?, ?, ?, 'pending')`,
			e.TMDBID, e.Title, e.Year, e.PosterPath, e.Quality, e.FilePath, opts.Monitor, addedStamp(e.AddedAt, now), stamp(now), opts.NoUpgrade)
		if err != nil {
			return ImportOutcome{}, fmt.Errorf("insert movie: %w", err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return ImportOutcome{}, fmt.Errorf("get movie id: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO activity (movie_id, event_type, message) VALUES (?, 'imported', ?)`,
		id, fmt.Sprintf("%s: registered existing file %s", e.Title, e.FilePath)); err != nil {
		return ImportOutcome{}, fmt.Errorf("log import: %w", err)
	}
	return recordImportItem(tx, batchID, e, id, ImportAdded, state, "", nil, !known)
}

func registerSeries(tx *sql.Tx, batchID int64, opts ImportOptions, e ImportEntry, now time.Time) (ImportOutcome, error) {
	var id int64
	created := false
	err := tx.QueryRow(`SELECT id FROM series WHERE tmdb_id = ?`, e.TMDBID).Scan(&id)
	switch {
	case err == nil:
		// A show that is already there can still gain episodes from the
		// files found; the worker adds them the same way.
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.Exec(`INSERT INTO series (tmdb_id, title, year, overview, poster_path, first_air_date, monitored, added_at, updated_at, no_upgrade, details_state)
			VALUES (?, ?, ?, '', ?, '', ?, ?, ?, ?, 'pending')`,
			e.TMDBID, e.Title, e.Year, e.PosterPath, opts.Monitor || opts.MonitorMissing, addedStamp(e.AddedAt, now), stamp(now), opts.NoUpgrade)
		if err != nil {
			return ImportOutcome{}, fmt.Errorf("insert series: %w", err)
		}
		created = true
		if id, err = res.LastInsertId(); err != nil {
			return ImportOutcome{}, fmt.Errorf("get series id: %w", err)
		}
	default:
		return ImportOutcome{}, fmt.Errorf("look up series: %w", err)
	}
	return recordImportItem(tx, batchID, e, id, ImportAdded, ImportPending, "", e.Files, created)
}

func recordImportItem(tx *sql.Tx, batchID int64, e ImportEntry, titleID int64, outcome, state, note string, files []ImportFile, created bool) (ImportOutcome, error) {
	encoded, err := json.Marshal(files)
	if err != nil {
		return ImportOutcome{}, fmt.Errorf("encode import files: %w", err)
	}
	if files == nil {
		encoded = []byte("[]")
	}
	res, err := tx.Exec(`INSERT INTO import_items (batch_id, kind, item_id, tmdb_id, title, outcome, state, note, files, created) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		batchID, e.Kind, titleID, e.TMDBID, e.Title, outcome, state, note, string(encoded), created)
	if err != nil {
		return ImportOutcome{}, fmt.Errorf("insert import item: %w", err)
	}
	itemID, err := res.LastInsertId()
	if err != nil {
		return ImportOutcome{}, fmt.Errorf("get import item id: %w", err)
	}
	return ImportOutcome{ItemID: itemID, TitleID: titleID, Outcome: outcome, Note: note}, nil
}

// finishBatchIfDone stamps the batch as finished once no title is waiting
// for its details.
func finishBatchIfDone(tx *sql.Tx, batchID int64) error {
	_, err := tx.Exec(`UPDATE import_batches SET finished_at = ?
		WHERE id = ? AND finished_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM import_items WHERE batch_id = ? AND outcome = 'added' AND state = 'pending')`,
		stamp(time.Now()), batchID, batchID)
	if err != nil {
		return fmt.Errorf("finish import batch: %w", err)
	}
	return nil
}

const importBatchSelect = `
	SELECT b.id, b.kind, b.root, b.created_at, COALESCE(b.finished_at, ''), b.dismissed, b.monitor, b.no_upgrade, b.monitor_missing,
	       COALESCE(SUM(i.outcome = 'added'), 0),
	       COALESCE(SUM(i.outcome = 'already'), 0),
	       COALESCE(SUM(i.outcome = 'failed'), 0),
	       COALESCE(SUM(i.outcome = 'added' AND i.state = 'pending'), 0),
	       COALESCE(SUM(i.outcome = 'failed' OR (i.outcome = 'added' AND i.state = 'problem')), 0)
	FROM import_batches b LEFT JOIN import_items i ON i.batch_id = b.id`

func scanBatch(scan func(dest ...any) error) (ImportBatch, error) {
	var b ImportBatch
	if err := scan(&b.ID, &b.Kind, &b.Root, &b.CreatedAt, &b.FinishedAt, &b.Dismissed, &b.Monitor, &b.NoUpgrade, &b.MonitorMissing,
		&b.Added, &b.Already, &b.Failed, &b.Pending, &b.Problems); err != nil {
		return ImportBatch{}, fmt.Errorf("scan import batch: %w", err)
	}
	end := time.Now()
	if b.FinishedAt != "" {
		end = parseStamp(b.FinishedAt)
	}
	if start := parseStamp(b.CreatedAt); !start.IsZero() && end.After(start) {
		b.Elapsed = end.Sub(start)
	}
	return b, nil
}

// ImportBatchByID returns one import; sql.ErrNoRows (wrapped) when there is none.
func (r *Repo) ImportBatchByID(id int64) (ImportBatch, error) {
	return scanBatch(r.db.QueryRow(importBatchSelect+` WHERE b.id = ? GROUP BY b.id`, id).Scan)
}

// ActiveImportBatches returns the imports still filling in details, and the
// ones that finished within the last week and were not dismissed, newest first.
func (r *Repo) ActiveImportBatches() ([]ImportBatch, error) {
	cutoff := stamp(time.Now().Add(-7 * 24 * time.Hour))
	rows, err := r.db.Query(importBatchSelect+`
		WHERE b.finished_at IS NULL OR (b.dismissed = 0 AND b.finished_at > ?)
		GROUP BY b.id ORDER BY b.id DESC`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list import batches: %w", err)
	}
	defer rows.Close()
	var out []ImportBatch
	for rows.Next() {
		b, err := scanBatch(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// RunningImportKinds returns the kinds ("movie" or "tv") that have an import
// still filling in details.
func (r *Repo) RunningImportKinds() (map[string]bool, error) {
	rows, err := r.db.Query(`SELECT DISTINCT kind FROM import_batches WHERE finished_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("list running imports: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan running import: %w", err)
		}
		out[k] = true
	}
	return out, rows.Err()
}

// ImportWatchResult counts the titles StartWatchingImport changed.
type ImportWatchResult struct {
	Movies int
	Shows  int
}

// StartWatchingImport switches monitoring on for the titles an import added
// itself, once it is done. Titles that were in the library before are left as
// they were, and so are titles removed since.
//
// watch starts watching the titles for new episodes and better versions.
// missing (shows only) makes the episodes a show does not have wanted, which
// also puts the show under watch for those. The batch remembers both, so its
// report and banner say what is switched on now.
func (r *Repo) StartWatchingImport(batchID int64, watch, missing bool) (ImportWatchResult, error) {
	var out ImportWatchResult
	tx, err := r.db.Begin()
	if err != nil {
		return out, fmt.Errorf("begin start watching: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT kind, item_id FROM import_items WHERE batch_id = ? AND outcome = 'added' AND created = 1 AND item_id > 0`, batchID)
	if err != nil {
		return out, fmt.Errorf("list imported titles: %w", err)
	}
	type title struct {
		kind string
		id   int64
	}
	var titles []title
	for rows.Next() {
		var t title
		if err := rows.Scan(&t.kind, &t.id); err != nil {
			rows.Close()
			return out, fmt.Errorf("scan imported title: %w", err)
		}
		titles = append(titles, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, fmt.Errorf("list imported titles: %w", err)
	}
	rows.Close()

	today := time.Now().UTC().Format("2006-01-02")
	for _, t := range titles {
		if t.kind == "movie" {
			if !watch {
				continue
			}
			res, err := tx.Exec(`UPDATE movies SET monitored = 1, no_upgrade = 0 WHERE id = ?`, t.id)
			if err != nil {
				return out, fmt.Errorf("watch movie %d: %w", t.id, err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				out.Movies++
			}
			continue
		}
		var changed int64
		if watch {
			res, err := tx.Exec(`UPDATE series SET monitored = 1, no_upgrade = 0 WHERE id = ?`, t.id)
			if err != nil {
				return out, fmt.Errorf("watch show %d: %w", t.id, err)
			}
			changed, _ = res.RowsAffected()
			if _, err := tx.Exec(`UPDATE episodes SET monitored = 1 WHERE series_id = ? AND season > 0 AND (status = 'downloaded' OR COALESCE(air_date, '') = '' OR air_date > ?)`, t.id, today); err != nil {
				return out, fmt.Errorf("watch episodes of show %d: %w", t.id, err)
			}
		}
		if missing {
			res, err := tx.Exec(`UPDATE series SET monitored = 1 WHERE id = ?`, t.id)
			if err != nil {
				return out, fmt.Errorf("look for missing episodes of show %d: %w", t.id, err)
			}
			if n, _ := res.RowsAffected(); n > changed {
				changed = n
			}
			if _, err := tx.Exec(`UPDATE episodes SET monitored = 1 WHERE series_id = ? AND season > 0 AND status != 'downloaded'`, t.id); err != nil {
				return out, fmt.Errorf("want missing episodes of show %d: %w", t.id, err)
			}
		}
		if changed > 0 {
			out.Shows++
		}
	}

	if watch {
		if _, err := tx.Exec(`UPDATE import_batches SET monitor = 1, no_upgrade = 0 WHERE id = ?`, batchID); err != nil {
			return out, fmt.Errorf("record watch on import %d: %w", batchID, err)
		}
	}
	if missing {
		if _, err := tx.Exec(`UPDATE import_batches SET monitor_missing = 1 WHERE id = ?`, batchID); err != nil {
			return out, fmt.Errorf("record missing episodes on import %d: %w", batchID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("commit start watching: %w", err)
	}
	return out, nil
}

// DismissImportBatch hides a finished import's banner.
func (r *Repo) DismissImportBatch(id int64) error {
	if _, err := r.db.Exec(`UPDATE import_batches SET dismissed = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("dismiss import batch %d: %w", id, err)
	}
	return nil
}

const importItemSelect = `SELECT id, batch_id, kind, item_id, tmdb_id, title, outcome, state, note, attempts, next_try_at, files, imported, skipped FROM import_items`

func scanImportItem(scan func(dest ...any) error) (ImportItem, error) {
	var (
		it    ImportItem
		files string
	)
	if err := scan(&it.ID, &it.BatchID, &it.Kind, &it.TitleID, &it.TMDBID, &it.Title, &it.Outcome, &it.State, &it.Note, &it.Attempts, &it.NextTry, &files, &it.Imported, &it.Skipped); err != nil {
		return ImportItem{}, fmt.Errorf("scan import item: %w", err)
	}
	if err := json.Unmarshal([]byte(files), &it.Files); err != nil {
		it.Files = nil // a file list that cannot be read only means no episodes get marked
	}
	return it, nil
}

// ImportBatchItems lists the titles of one import in the order they were confirmed.
func (r *Repo) ImportBatchItems(batchID int64) ([]ImportItem, error) {
	rows, err := r.db.Query(importItemSelect+` WHERE batch_id = ? ORDER BY id`, batchID)
	if err != nil {
		return nil, fmt.Errorf("list import items: %w", err)
	}
	defer rows.Close()
	var out []ImportItem
	for rows.Next() {
		it, err := scanImportItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DueImportItems returns up to limit titles whose details still need
// fetching and whose next try has come, oldest first. Problems come back
// too, once their wait is over: nothing is ever given up on.
func (r *Repo) DueImportItems(now time.Time, limit int) ([]ImportItem, error) {
	rows, err := r.db.Query(importItemSelect+` WHERE outcome = 'added' AND state != 'done' AND next_try_at <= ? ORDER BY id LIMIT ?`, stamp(now), limit)
	if err != nil {
		return nil, fmt.Errorf("list due import items: %w", err)
	}
	defer rows.Close()
	var out []ImportItem
	for rows.Next() {
		it, err := scanImportItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// MovieDetails is what the movie database says about a movie.
type MovieDetails struct {
	Title       string
	Year        int
	Overview    string
	PosterPath  string
	ReleaseDate string
	Genres      []string
}

// CompleteMovieImport stores a movie's details and marks its import item done.
func (r *Repo) CompleteMovieImport(item ImportItem, d MovieDetails) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin movie details: %w", err)
	}
	defer tx.Rollback()
	genres, err := encodeGenres(nonNil(d.Genres))
	if err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE movies SET title = ?, year = ?, overview = ?, poster_path = ?, release_date = ?, genres = ?, details_state = '', details_note = '' WHERE id = ?`,
		d.Title, d.Year, d.Overview, d.PosterPath, d.ReleaseDate, genres, item.TitleID)
	if err != nil {
		return fmt.Errorf("store movie details: %w", err)
	}
	note := ""
	if n, _ := res.RowsAffected(); n == 0 {
		note = "This title was removed from your library."
	}
	if err := markItemDone(tx, item, note, 0, 0); err != nil {
		return err
	}
	return tx.Commit()
}

func nonNil(g []string) []string {
	if g == nil {
		return []string{}
	}
	return g
}

func markItemDone(tx *sql.Tx, item ImportItem, note string, imported, skipped int) error {
	if _, err := tx.Exec(`UPDATE import_items SET state = 'done', note = ?, imported = ?, skipped = ? WHERE id = ?`, note, imported, skipped, item.ID); err != nil {
		return fmt.Errorf("finish import item: %w", err)
	}
	return finishBatchIfDone(tx, item.BatchID)
}

// SeriesDetails is what the movie database says about a show.
type SeriesDetails struct {
	Title        string
	Year         int
	Overview     string
	PosterPath   string
	FirstAirDate string
	Genres       []string
}

// SeriesImportResult counts what a show's import did with its files.
type SeriesImportResult struct {
	Imported int // episodes marked downloaded
	Skipped  int // episodes that already were
	Unknown  int // episodes on disk that the movie database does not list
}

// CompleteSeriesImport stores a show's details and full episode list and
// marks the episodes found on disk as downloaded, all in one transaction, so
// automation never sees the episodes as missing in between.
func (r *Repo) CompleteSeriesImport(item ImportItem, d SeriesDetails, episodes []Episode) (SeriesImportResult, error) {
	var out SeriesImportResult
	tx, err := r.db.Begin()
	if err != nil {
		return out, fmt.Errorf("begin show details: %w", err)
	}
	defer tx.Rollback()

	// A show this import just added starts with the choices made for the
	// import. The episodes it is missing are wanted only if asked for. The
	// ones found on disk, and the ones that have not aired yet, are watched
	// only when the person chose to watch the titles. A show that was
	// already in the library keeps its own monitoring. Read before the
	// details are stored, which clear the show's "pending" mark.
	var (
		monitored, wantMissing, watch bool
		state                         string
	)
	err = tx.QueryRow(`SELECT s.monitored, s.details_state, b.monitor_missing, b.monitor FROM series s, import_batches b WHERE s.id = ? AND b.id = ?`,
		item.TitleID, item.BatchID).Scan(&monitored, &state, &wantMissing, &watch)
	if errors.Is(err, sql.ErrNoRows) {
		if err := markItemDone(tx, item, "This title was removed from your library.", 0, 0); err != nil {
			return out, err
		}
		return out, tx.Commit()
	}
	if err != nil {
		return out, fmt.Errorf("read show monitoring: %w", err)
	}

	genres, err := encodeGenres(nonNil(d.Genres))
	if err != nil {
		return out, err
	}
	if _, err := tx.Exec(`UPDATE series SET title = ?, year = ?, overview = ?, poster_path = ?, first_air_date = ?, genres = ?, details_state = '', details_note = '' WHERE id = ?`,
		d.Title, d.Year, d.Overview, d.PosterPath, d.FirstAirDate, genres, item.TitleID); err != nil {
		return out, fmt.Errorf("store show details: %w", err)
	}

	newShow := state != ""
	monitorNew := monitored
	if newShow {
		monitorNew = wantMissing
	}
	if err := upsertEpisodes(tx, item.TitleID, episodes, monitorNew); err != nil {
		return out, err
	}
	if newShow && watch && !wantMissing {
		// Nothing missing is wanted, but what has yet to air is.
		if _, err := tx.Exec(`UPDATE episodes SET monitored = 1 WHERE series_id = ? AND season > 0 AND (COALESCE(air_date, '') = '' OR air_date > ?)`,
			item.TitleID, time.Now().UTC().Format("2006-01-02")); err != nil {
			return out, fmt.Errorf("watch episodes that have not aired: %w", err)
		}
	}

	type known struct {
		id     int64
		status string
	}
	byNumber := map[[2]int]known{}
	rows, err := tx.Query(`SELECT id, season, episode, status FROM episodes WHERE series_id = ?`, item.TitleID)
	if err != nil {
		return out, fmt.Errorf("list show episodes: %w", err)
	}
	for rows.Next() {
		var (
			k        known
			ssn, epn int
		)
		if err := rows.Scan(&k.id, &ssn, &epn, &k.status); err != nil {
			rows.Close()
			return out, fmt.Errorf("scan show episode: %w", err)
		}
		byNumber[[2]int{ssn, epn}] = k
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, fmt.Errorf("list show episodes: %w", err)
	}
	rows.Close()

	mark, err := tx.Prepare(`UPDATE episodes SET status = 'downloaded', quality = COALESCE(NULLIF(?, ''), quality), file_path = COALESCE(NULLIF(?, ''), file_path), monitored = CASE WHEN ? THEN ? ELSE monitored END WHERE id = ?`)
	if err != nil {
		return out, fmt.Errorf("prepare mark episode: %w", err)
	}
	defer mark.Close()
	for _, f := range item.Files {
		for _, n := range f.Episodes {
			k, ok := byNumber[[2]int{f.Season, n}]
			switch {
			case !ok:
				out.Unknown++ // specials, or numbering that differs from the movie database
			case Status(k.status) == StatusDownloaded:
				out.Skipped++
			default:
				if _, err := mark.Exec(f.Quality, f.Path, newShow, watch, k.id); err != nil {
					return out, fmt.Errorf("mark episode downloaded: %w", err)
				}
				k.status = string(StatusDownloaded)
				byNumber[[2]int{f.Season, n}] = k
				out.Imported++
			}
		}
	}

	note := ""
	if out.Unknown > 0 {
		note = fmt.Sprintf("Left out %s that the movie database doesn't list for this show.", countOf(out.Unknown, "episode"))
	}
	if _, err := tx.Exec(`INSERT INTO activity (series_id, event_type, message) VALUES (?, 'imported', ?)`,
		item.TitleID, fmt.Sprintf("%s: registered %s that were already on disk", d.Title, countOf(out.Imported, "episode"))); err != nil {
		return out, fmt.Errorf("log import: %w", err)
	}
	if err := markItemDone(tx, item, note, out.Imported, out.Skipped); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// SetImportItemState records that a title's details are pending or hit a
// problem, with the reason in plain words and when to try again.
func (r *Repo) SetImportItemState(item ImportItem, state, note string, attempts int, next time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin import state: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE import_items SET state = ?, note = ?, attempts = ?, next_try_at = ? WHERE id = ?`,
		state, note, attempts, stamp(next), item.ID); err != nil {
		return fmt.Errorf("set import item state: %w", err)
	}
	if _, err := tx.Exec(`UPDATE `+titleTable(item.Kind)+` SET details_state = ?, details_note = ? WHERE id = ?`, state, note, item.TitleID); err != nil {
		return fmt.Errorf("set title details state: %w", err)
	}
	if err := finishBatchIfDone(tx, item.BatchID); err != nil {
		return err
	}
	return tx.Commit()
}

// AddedDateCandidate is an imported title whose added date may still be the
// moment of the import instead of the age of its files.
type AddedDateCandidate struct {
	Kind    string // "movie" or "series"
	ID      int64
	AddedAt time.Time
	Paths   []string // files on disk
}

// ImportedForAddedDates lists the titles that an import registered before
// the import recorded file dates, found by the activity entries the old
// import wrote. Titles that came in any other way are never listed.
func (r *Repo) ImportedForAddedDates() ([]AddedDateCandidate, error) {
	var out []AddedDateCandidate

	rows, err := r.db.Query(`SELECT m.id, m.added_at, m.file_path FROM movies m
		WHERE COALESCE(m.file_path, '') != ''
		AND EXISTS (SELECT 1 FROM activity a WHERE a.movie_id = m.id AND a.event_type = 'imported' AND a.message LIKE '%: registered existing file %')`)
	if err != nil {
		return nil, fmt.Errorf("list imported movies: %w", err)
	}
	for rows.Next() {
		var (
			c    AddedDateCandidate
			at   string
			path string
		)
		if err := rows.Scan(&c.ID, &at, &path); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan imported movie: %w", err)
		}
		c.Kind, c.AddedAt, c.Paths = "movie", parseStamp(at), []string{path}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// The old import logged a show's entry without its id, so a show is
	// recognised by the title at the start of the message as well.
	rows, err = r.db.Query(`SELECT s.id, s.added_at FROM series s
		WHERE EXISTS (SELECT 1 FROM activity a WHERE a.event_type = 'imported' AND a.message LIKE '%: registered % that were already on disk'
			AND (a.series_id = s.id OR substr(a.message, 1, length(s.title) + 13) = s.title || ': registered '))`)
	if err != nil {
		return nil, fmt.Errorf("list imported shows: %w", err)
	}
	var shows []AddedDateCandidate
	for rows.Next() {
		var (
			c  AddedDateCandidate
			at string
		)
		if err := rows.Scan(&c.ID, &at); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan imported show: %w", err)
		}
		c.Kind, c.AddedAt = "series", parseStamp(at)
		shows = append(shows, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for _, c := range shows {
		pr, err := r.db.Query(`SELECT file_path FROM episodes WHERE series_id = ? AND status = 'downloaded' AND COALESCE(file_path, '') != ''`, c.ID)
		if err != nil {
			return nil, fmt.Errorf("list episode files: %w", err)
		}
		for pr.Next() {
			var p string
			if err := pr.Scan(&p); err != nil {
				pr.Close()
				return nil, fmt.Errorf("scan episode file: %w", err)
			}
			c.Paths = append(c.Paths, p)
		}
		pr.Close()
		out = append(out, c)
	}
	return out, nil
}

func parseStamp(s string) time.Time {
	for _, layout := range []string{timeStamp, time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// SetAddedAt changes when a movie ("movie") or show ("series") counts as added.
func (r *Repo) SetAddedAt(kind string, id int64, at time.Time) error {
	if _, err := r.db.Exec(`UPDATE `+titleTable(kind)+` SET added_at = ? WHERE id = ?`, stamp(at), id); err != nil {
		return fmt.Errorf("set %s %d added date: %w", kind, id, err)
	}
	return nil
}
