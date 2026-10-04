// Package queue tracks releases moving through grab -> download -> import
// (the download_queue table) and the activity feed ("one live
// view of everything currently downloading/importing/post-processing").
package queue

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
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
	// StatusPaused is a download whose transfer is stopped but whose partly
	// downloaded files and claim on the title are kept. It survives a
	// restart and is never resumed by itself: a person presses Resume.
	StatusPaused Status = "paused"
	// StatusStopped is a download a person cancelled. The title is free to
	// be searched again; the item stays in the list until it is retried or
	// removed.
	StatusStopped Status = "stopped"
)

// Protocol mirrors internal/indexers.Protocol — duplicated as its own type
// rather than imported so this package (which owns persistence only)
// doesn't need to depend on the indexers package for a two-value enum.
type Protocol string

const (
	ProtocolUsenet  Protocol = "usenet"
	ProtocolTorrent Protocol = "torrent"
)

// Priority orders the download line. Something a person asked for goes before
// anything the automatic searches added.
type Priority int

const (
	PriorityAutomatic Priority = 0
	PriorityManual    Priority = 1
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
	AlbumID      int64 // music grabs: the album; 0 otherwise
	ReleaseTitle string
	NZBURL       string
	SizeBytes    int64
	Protocol     Protocol
	Status       Status
	ProgressPct  float64
	Error        string
	// Interrupted marks a paused item that nobody paused: Mediarium was
	// stopped or restarted while it was downloading (see RecoverInterrupted).
	Interrupted bool
	// Priority and LineSeq say where a waiting item stands in the download
	// line: a higher Priority goes first, and within a priority the lower
	// LineSeq. Enqueue sets both; List and Get fill them in.
	Priority Priority
	LineSeq  int64
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
	db  *sql.DB
	box *crypto.Box // encrypts the download address; nil keeps it as written (tests)
}

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// SetBox makes the repo keep download addresses encrypted. A download address
// from an indexer usually carries the source's API key or a tracker
// passkey (`...&apikey=KEY`), so the database file alone must not reveal it.
func (r *Repo) SetBox(b *crypto.Box) { r.box = b }

// encURLPrefix marks an encrypted download address. An address that does not
// start with it is plain text, as older versions stored it.
const encURLPrefix = "enc1:"

// sealURL is the form of a download address that is stored.
func (r *Repo) sealURL(u string) (string, error) {
	if r.box == nil || u == "" {
		return u, nil
	}
	enc, err := r.box.Encrypt(u)
	if err != nil {
		return "", fmt.Errorf("encrypt download address: %w", err)
	}
	return encURLPrefix + enc, nil
}

// openURL turns a stored download address back into the real one. An address
// that cannot be decrypted (the key changed) comes back empty: the item is
// listed, but cannot be retried.
func (r *Repo) openURL(stored string) string {
	rest, ok := strings.CutPrefix(stored, encURLPrefix)
	if !ok {
		return stored
	}
	if r.box == nil {
		return ""
	}
	u, err := r.box.Decrypt(rest)
	if err != nil {
		return ""
	}
	return u
}

// EncryptStoredURLs encrypts the download addresses that older versions kept in
// plain text. It returns how many it changed and does nothing once they are all
// encrypted.
func (r *Repo) EncryptStoredURLs() (int, error) {
	if r.box == nil {
		return 0, nil
	}
	rows, err := r.db.Query(`SELECT id, nzb_url FROM download_queue WHERE nzb_url != '' AND nzb_url NOT LIKE 'enc1:%'`)
	if err != nil {
		return 0, fmt.Errorf("look for plain-text download addresses: %w", err)
	}
	type row struct {
		id  int64
		url string
	}
	var todo []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.url); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan download address: %w", err)
		}
		todo = append(todo, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, x := range todo {
		sealed, err := r.sealURL(x.url)
		if err != nil {
			return 0, err
		}
		if _, err := r.db.Exec(`UPDATE download_queue SET nzb_url = ? WHERE id = ? AND nzb_url = ?`, sealed, x.id, x.url); err != nil {
			return 0, fmt.Errorf("encrypt download address of item %d: %w", x.id, err)
		}
	}
	return len(todo), nil
}

func (r *Repo) Enqueue(item Item) (int64, error) {
	if item.Protocol == "" {
		item.Protocol = ProtocolUsenet
	}
	storedURL, err := r.sealURL(item.NZBURL)
	if err != nil {
		return 0, err
	}
	// The new item goes to the back of the line: one number past the highest
	// place anyone has taken.
	res, err := r.db.Exec(
		`INSERT INTO download_queue (movie_id, series_id, season, episode, album_id, indexer_id, release_title, nzb_url, size_bytes, protocol, status, priority, line_seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(line_seq), 0) + 1 FROM download_queue))`,
		nullIfZero(item.MovieID), nullIfZero(item.SeriesID), nullIfZero(int64(item.Season)), nullIfZero(int64(item.Episode)), nullIfZero(item.AlbumID),
		item.IndexerID, item.ReleaseTitle, storedURL, item.SizeBytes, string(item.Protocol), string(StatusQueued), int(item.Priority),
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
	if status == StatusCompleted || status == StatusFailed || status == StatusStopped {
		completedAt = nowRFC3339()
	}
	// A download that has finished its transfer (now being processed) or has
	// completed is at 100%. The transfer itself can end a little short of it:
	// the sizes in an NZB are the encoded article sizes, larger than the files.
	finished := status == StatusImporting || status == StatusCompleted
	_, err := r.db.Exec(
		`UPDATE download_queue SET status = ?, error = NULLIF(?, ''), completed_at = COALESCE(?, completed_at), interrupted = 0,
		   progress_pct = CASE WHEN ? THEN 100 ELSE progress_pct END WHERE id = ?`,
		string(status), errMsg, completedAt, finished, id,
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
		`SELECT id, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(season, 0), COALESCE(episode, 0), COALESCE(album_id, 0), indexer_id, release_title, nzb_url, size_bytes, protocol, status, progress_pct, COALESCE(error, ''), source_path, dest_path, interrupted, priority, line_seq FROM download_queue WHERE id = ?`, id,
	).Scan(&it.ID, &it.MovieID, &it.SeriesID, &it.Season, &it.Episode, &it.AlbumID, &it.IndexerID, &it.ReleaseTitle, &it.NZBURL, &it.SizeBytes, &protocol, &status, &it.ProgressPct, &it.Error, &sourcePath, &destPath, &it.Interrupted, &it.Priority, &it.LineSeq)
	if err != nil {
		return Item{}, fmt.Errorf("get queue item %d: %w", id, err)
	}
	it.Protocol = Protocol(protocol)
	it.Status = Status(status)
	it.NZBURL = r.openURL(it.NZBURL)
	it.SourcePath = sourcePath.String
	it.DestPath = destPath.String
	return it, nil
}

// Pause parks item id as paused. interrupted says nobody paused it: Mediarium
// was stopped or restarted while it was downloading. Its progress is left as
// it is and its error text is cleared.
func (r *Repo) Pause(id int64, interrupted bool) error {
	res, err := r.db.Exec(`UPDATE download_queue SET status = ?, interrupted = ?, error = NULL WHERE id = ?`, string(StatusPaused), interrupted, id)
	if err != nil {
		return fmt.Errorf("pause queue item %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	r.statusEvent(id, StatusPaused)
	return nil
}

// RecoverInterrupted pauses every item that was downloading or importing when
// Mediarium last stopped: nothing is running it any more, so it would sit
// there for ever. They come back paused and marked interrupted, and stay that
// way until a person resumes them. Items that were only waiting in line stay
// waiting: nothing was started for them, and the line carries on by itself.
// Call it once at startup, before anything can start a download. It returns
// how many it paused.
func (r *Repo) RecoverInterrupted() (int64, error) {
	res, err := r.db.Exec(`UPDATE download_queue SET status = ?, interrupted = 1 WHERE status IN ('downloading', 'importing')`, string(StatusPaused))
	if err != nil {
		return 0, fmt.Errorf("pause interrupted downloads: %w", err)
	}
	return res.RowsAffected()
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
		`SELECT id, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(season, 0), COALESCE(episode, 0), COALESCE(album_id, 0), indexer_id, release_title, nzb_url, size_bytes, protocol, status, progress_pct, COALESCE(error, ''), dest_path, added_at, COALESCE(completed_at, ''), interrupted, priority, line_seq FROM download_queue ORDER BY added_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list queue: %w", err)
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		var it Item
		var status, protocol string
		var destPath sql.NullString
		if err := rows.Scan(&it.ID, &it.MovieID, &it.SeriesID, &it.Season, &it.Episode, &it.AlbumID, &it.IndexerID, &it.ReleaseTitle, &it.NZBURL, &it.SizeBytes, &protocol, &status, &it.ProgressPct, &it.Error, &destPath, &it.AddedAt, &it.CompletedAt, &it.Interrupted, &it.Priority, &it.LineSeq); err != nil {
			return nil, fmt.Errorf("scan queue item: %w", err)
		}
		it.Protocol = Protocol(protocol)
		it.Status = Status(status)
		it.NZBURL = r.openURL(it.NZBURL)
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
		`SELECT id, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(season, 0), COALESCE(episode, 0), COALESCE(album_id, 0), release_title, COALESCE(size_bytes, 0), protocol, COALESCE(completed_at, '')
		 FROM download_queue WHERE status = 'completed' ORDER BY completed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent completed downloads: %w", err)
	}
	defer rows.Close()
	var out []CompletedItem
	for rows.Next() {
		var c CompletedItem
		var protocol string
		if err := rows.Scan(&c.ID, &c.MovieID, &c.SeriesID, &c.Season, &c.Episode, &c.AlbumID, &c.ReleaseTitle, &c.SizeBytes, &protocol, &c.CompletedAt); err != nil {
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
// finished yet (queued, downloading, importing, paused, or parked on a conflict).
func (r *Repo) HasActiveForMovie(movieID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM download_queue WHERE movie_id = ? AND status IN ('queued', 'downloading', 'importing', 'paused', 'conflict')`, movieID,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check active downloads for movie %d: %w", movieID, err)
	}
	return n > 0, nil
}

// CountRunning counts the downloads that are being worked on: downloading or
// importing. Ones waiting in line are counted by CountWaiting.
func (r *Repo) CountRunning() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM download_queue WHERE status IN ('downloading', 'importing')`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count running downloads: %w", err)
	}
	return n, nil
}

// HasActiveForAlbum is HasActiveForMovie for an album.
func (r *Repo) HasActiveForAlbum(albumID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM download_queue WHERE album_id = ? AND status IN ('queued', 'downloading', 'importing', 'paused', 'conflict')`, albumID,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check active downloads for album %d: %w", albumID, err)
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

// ClearFinished removes every completed, failed and stopped entry and returns
// how many.
func (r *Repo) ClearFinished() (int64, error) {
	res, err := r.db.Exec(`DELETE FROM download_queue WHERE status IN ('completed', 'failed', 'stopped')`)
	if err != nil {
		return 0, fmt.Errorf("clear finished queue items: %w", err)
	}
	return res.RowsAffected()
}

// PruneFinished deletes completed, failed and stopped entries that finished
// before cutoff and returns how many. Entries still running, paused or parked
// on a conflict are never pruned.
func (r *Repo) PruneFinished(cutoff time.Time) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM download_queue WHERE status IN ('completed', 'failed', 'stopped') AND completed_at IS NOT NULL AND completed_at < ?`,
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
	// Filled in by ListActivityFor, for a link to the title's page: the
	// movie's TMDB id, the show's id or the album's artist id (0 = none).
	MovieTMDBID int64
	SeriesID    int64
	ArtistID    int64
}

func (r *Repo) ListActivity(limit int) ([]ActivityEntry, error) {
	return r.ListActivityFor(limit, "")
}

// ListActivityFor is ListActivity narrowed to lines whose text contains
// query (case-insensitive; "" = all), with what is needed to link each line
// to its title.
func (r *Repo) ListActivityFor(limit int, query string) ([]ActivityEntry, error) {
	rows, err := r.db.Query(`SELECT a.id, a.movie_id, a.event_type, a.message, a.created_at,
			COALESCE(m.tmdb_id, 0), COALESCE(a.series_id, 0), COALESCE(al.artist_id, 0)
		FROM activity a
		LEFT JOIN movies m ON m.id = a.movie_id
		LEFT JOIN albums al ON al.id = a.album_id
		WHERE a.item_only = 0 AND (? = '' OR a.message LIKE '%' || ? || '%')
		ORDER BY a.created_at DESC LIMIT ?`, query, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	defer rows.Close()

	var out []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		if err := rows.Scan(&e.ID, &e.MovieID, &e.EventType, &e.Message, &e.CreatedAt, &e.MovieTMDBID, &e.SeriesID, &e.ArtistID); err != nil {
			return nil, fmt.Errorf("scan activity entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
