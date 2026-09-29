// Package queue tracks releases moving through grab -> download -> import
// (the download_queue table) and the activity feed ("one live
// view of everything currently downloading/importing/post-processing").
package queue

import (
	"database/sql"
	"fmt"
	"time"
)

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type Status string

const (
	StatusQueued      Status = "queued"
	StatusDownloading Status = "downloading"
	StatusImporting   Status = "importing"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	// StatusConflict means the download finished but landed on a naming
	// collision under the "always ask" import conflict policy —
	// parked here with SourcePath/DestPath populated so a person can
	// resolve it (skip or overwrite) rather than it being silently skipped.
	StatusConflict Status = "conflict"
)

// Protocol mirrors internal/indexers.Protocol — duplicated as its own type
// rather than imported so this package (which owns persistence only)
// doesn't need to depend on the indexers package for a two-value enum.
type Protocol string

const (
	ProtocolUsenet  Protocol = "usenet"
	ProtocolTorrent Protocol = "torrent"
)

type Item struct {
	ID        int64
	MovieID   int64 // 0 for a TV grab
	IndexerID sql.NullInt64
	// TV grabs: SeriesID > 0 and Season > 0; Episode == 0 means a whole
	// season pack. Movie grabs leave all three zero.
	SeriesID     int64
	Season       int
	Episode      int
	ReleaseTitle string
	NZBURL       string
	SizeBytes    int64
	Protocol     Protocol
	Status       Status
	ProgressPct  float64
	Error        string
	// SourcePath/DestPath are only set for StatusConflict items (see
	// SetConflict) — the file still sitting under /downloads, and where it
	// would go once resolved.
	SourcePath string
	DestPath   string
	// AddedAt/CompletedAt are set by List (RFC 3339 text; CompletedAt is
	// empty until the item finishes).
	AddedAt     string
	CompletedAt string
}

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Enqueue(item Item) (int64, error) {
	if item.Protocol == "" {
		item.Protocol = ProtocolUsenet
	}
	res, err := r.db.Exec(
		`INSERT INTO download_queue (movie_id, series_id, season, episode, indexer_id, release_title, nzb_url, size_bytes, protocol, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullIfZero(item.MovieID), nullIfZero(item.SeriesID), nullIfZero(int64(item.Season)), nullIfZero(int64(item.Episode)),
		item.IndexerID, item.ReleaseTitle, item.NZBURL, item.SizeBytes, string(item.Protocol), string(StatusQueued),
	)
	if err != nil {
		return 0, fmt.Errorf("enqueue download: %w", err)
	}
	return res.LastInsertId()
}

func nullIfZero(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func (r *Repo) SetStatus(id int64, status Status, errMsg string) error {
	var completedAt any
	if status == StatusCompleted || status == StatusFailed {
		completedAt = nowRFC3339()
	}
	_, err := r.db.Exec(
		`UPDATE download_queue SET status = ?, error = NULLIF(?, ''), completed_at = COALESCE(?, completed_at) WHERE id = ?`,
		string(status), errMsg, completedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update queue item %d status: %w", id, err)
	}
	r.statusEvent(id, status)
	return nil
}

// SetConflict parks item id in StatusConflict with the source file's
// current location and its proposed (but not-yet-taken) destination, for
// a person to resolve later via Get/List + a caller-driven overwrite-or-
// skip decision (this package only persists the state, same separation as
// the rest of the pipeline — internal/api owns the actual file move).
func (r *Repo) SetConflict(id int64, sourcePath, destPath string) error {
	_, err := r.db.Exec(
		`UPDATE download_queue SET status = ?, source_path = ?, dest_path = ?, error = NULL WHERE id = ?`,
		string(StatusConflict), sourcePath, destPath, id,
	)
	if err != nil {
		return fmt.Errorf("set queue item %d conflict: %w", id, err)
	}
	return nil
}

// Get returns one queue item by id, including SourcePath — List includes
// DestPath (the UI needs it to show a conflict item's proposed
// destination) but omits SourcePath, which only resolveConflict's actual
// file move needs.
func (r *Repo) Get(id int64) (Item, error) {
	var it Item
	var status, protocol string
	var sourcePath, destPath sql.NullString
	err := r.db.QueryRow(
		`SELECT id, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(season, 0), COALESCE(episode, 0), indexer_id, release_title, nzb_url, size_bytes, protocol, status, progress_pct, COALESCE(error, ''), source_path, dest_path FROM download_queue WHERE id = ?`, id,
	).Scan(&it.ID, &it.MovieID, &it.SeriesID, &it.Season, &it.Episode, &it.IndexerID, &it.ReleaseTitle, &it.NZBURL, &it.SizeBytes, &protocol, &status, &it.ProgressPct, &it.Error, &sourcePath, &destPath)
	if err != nil {
		return Item{}, fmt.Errorf("get queue item %d: %w", id, err)
	}
	it.Protocol = Protocol(protocol)
	it.Status = Status(status)
	it.SourcePath = sourcePath.String
	it.DestPath = destPath.String
	return it, nil
}

func (r *Repo) SetProgress(id int64, pct float64) error {
	_, err := r.db.Exec(`UPDATE download_queue SET progress_pct = ? WHERE id = ?`, pct, id)
	if err != nil {
		return fmt.Errorf("update queue item %d progress: %w", id, err)
	}
	return nil
}

func (r *Repo) List() ([]Item, error) {
	rows, err := r.db.Query(
		`SELECT id, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(season, 0), COALESCE(episode, 0), indexer_id, release_title, nzb_url, size_bytes, protocol, status, progress_pct, COALESCE(error, ''), dest_path, added_at, COALESCE(completed_at, '') FROM download_queue ORDER BY added_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list queue: %w", err)
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		var it Item
		var status, protocol string
		var destPath sql.NullString
		if err := rows.Scan(&it.ID, &it.MovieID, &it.SeriesID, &it.Season, &it.Episode, &it.IndexerID, &it.ReleaseTitle, &it.NZBURL, &it.SizeBytes, &protocol, &status, &it.ProgressPct, &it.Error, &destPath, &it.AddedAt, &it.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan queue item: %w", err)
		}
		it.Protocol = Protocol(protocol)
		it.Status = Status(status)
		it.DestPath = destPath.String
		out = append(out, it)
	}
	return out, rows.Err()
}

// CompletedItem is a finished download with the time it finished.
type CompletedItem struct {
	Item
	CompletedAt string
}

// RecentCompleted returns the most recently completed downloads, newest first.
func (r *Repo) RecentCompleted(limit int) ([]CompletedItem, error) {
	rows, err := r.db.Query(
		`SELECT id, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(season, 0), COALESCE(episode, 0), release_title, COALESCE(size_bytes, 0), protocol, COALESCE(completed_at, '')
		 FROM download_queue WHERE status = 'completed' ORDER BY completed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent completed downloads: %w", err)
	}
	defer rows.Close()
	var out []CompletedItem
	for rows.Next() {
		var c CompletedItem
		var protocol string
		if err := rows.Scan(&c.ID, &c.MovieID, &c.SeriesID, &c.Season, &c.Episode, &c.ReleaseTitle, &c.SizeBytes, &protocol, &c.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan completed download: %w", err)
		}
		c.Protocol = Protocol(protocol)
		c.Status = StatusCompleted
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecentFailed counts downloads that failed since the given time.
func (r *Repo) RecentFailed(since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM download_queue WHERE status = 'failed' AND completed_at >= ?`, since.UTC().Format(time.RFC3339Nano)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count recent failures: %w", err)
	}
	return n, nil
}

// HasActiveForMovie reports whether movieID has a download that has not
// finished yet (queued, downloading, importing, or parked on a conflict).
func (r *Repo) HasActiveForMovie(movieID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM download_queue WHERE movie_id = ? AND status IN ('queued', 'downloading', 'importing', 'conflict')`, movieID,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check active downloads for movie %d: %w", movieID, err)
	}
	return n > 0, nil
}

// Delete removes one queue entry (its history), not any file. Callers must
// not delete an item whose pipeline is still running.
func (r *Repo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM download_queue WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete queue item %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ClearFinished removes every completed and failed entry and returns how many.
func (r *Repo) ClearFinished() (int64, error) {
	res, err := r.db.Exec(`DELETE FROM download_queue WHERE status IN ('completed', 'failed')`)
	if err != nil {
		return 0, fmt.Errorf("clear finished queue items: %w", err)
	}
	return res.RowsAffected()
}

// PruneFinished deletes completed and failed entries that finished before
// cutoff and returns how many. Entries still running or parked on a
// conflict are never pruned.
func (r *Repo) PruneFinished(cutoff time.Time) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM download_queue WHERE status IN ('completed', 'failed') AND completed_at IS NOT NULL AND completed_at < ?`,
		cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("prune finished queue items: %w", err)
	}
	return res.RowsAffected()
}

// PruneActivity deletes activity entries older than cutoff and returns how
// many.
func (r *Repo) PruneActivity(cutoff time.Time) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM activity WHERE created_at < ?`, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("prune activity: %w", err)
	}
	return res.RowsAffected()
}

// LogActivity records one event on the activity feed.
//
// movieID <= 0 is stored as NULL (activity.movie_id is nullable and
// SET NULL on delete) — used by TV events, which aren't tied to a movie.
func (r *Repo) LogActivity(movieID int64, eventType, message string) error {
	var movie any
	if movieID > 0 {
		movie = movieID
	}
	_, err := r.db.Exec(`INSERT INTO activity (movie_id, event_type, message, level) VALUES (?, ?, ?, ?)`, movie, eventType, message, string(levelFor(eventType)))
	if err != nil {
		return fmt.Errorf("log activity: %w", err)
	}
	return nil
}

type ActivityEntry struct {
	ID        int64
	MovieID   sql.NullInt64
	EventType string
	Message   string
	CreatedAt string
}

func (r *Repo) ListActivity(limit int) ([]ActivityEntry, error) {
	rows, err := r.db.Query(`SELECT id, movie_id, event_type, message, created_at FROM activity WHERE item_only = 0 ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	defer rows.Close()

	var out []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		if err := rows.Scan(&e.ID, &e.MovieID, &e.EventType, &e.Message, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan activity entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
