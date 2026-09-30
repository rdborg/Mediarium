package queue

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// The download line: items with status "queued" are waiting for a free place.
// A person's downloads go first (PriorityManual before PriorityAutomatic);
// within a priority the lower LineSeq goes first, which is the order they
// were added, except for a resumed item, which goes back to the front of its
// priority.

// lineOrder is the order the line is served in.
const lineOrder = `ORDER BY priority DESC, line_seq ASC, id ASC`

// NextWaiting returns the item that should start next, or false when nothing
// is waiting. With manualOnly it only looks at what a person asked for.
func (r *Repo) NextWaiting(manualOnly bool) (Item, bool, error) {
	query := `SELECT id FROM download_queue WHERE status = 'queued' `
	if manualOnly {
		query += `AND priority >= 1 `
	}
	var id int64
	err := r.db.QueryRow(query + lineOrder + ` LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, false, nil
	}
	if err != nil {
		return Item{}, false, fmt.Errorf("find the next waiting download: %w", err)
	}
	it, err := r.Get(id)
	if err != nil {
		return Item{}, false, err
	}
	return it, true, nil
}

// Claim takes a waiting item out of the line and marks it as downloading, in
// one step: it reports false when the item is no longer waiting (a person
// paused, stopped or removed it a moment ago). The "download started" note is
// left to the pipeline, which sets the same status when it begins.
func (r *Repo) Claim(id int64) (bool, error) {
	res, err := r.db.Exec(`UPDATE download_queue SET status = ?, error = NULL WHERE id = ? AND status = ?`, string(StatusDownloading), id, string(StatusQueued))
	if err != nil {
		return false, fmt.Errorf("start queue item %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Requeue puts a paused item back in the line, at the front of its priority
// (it was already started once, so it goes before the ones that never were).
// It reports false when the item is not paused.
func (r *Repo) Requeue(id int64) (bool, error) {
	res, err := r.db.Exec(
		`UPDATE download_queue SET status = ?, interrupted = 0, error = NULL,
		   line_seq = (SELECT COALESCE(MIN(line_seq), 1) - 1 FROM download_queue WHERE status = ? AND priority = (SELECT priority FROM download_queue WHERE id = ?))
		 WHERE id = ? AND status = ?`,
		string(StatusQueued), string(StatusQueued), id, id, string(StatusPaused))
	if err != nil {
		return false, fmt.Errorf("put queue item %d back in line: %w", id, err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CountWaiting counts the downloads waiting in line.
func (r *Repo) CountWaiting() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM download_queue WHERE status = 'queued'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count waiting downloads: %w", err)
	}
	return n, nil
}

// CountAutomaticAddedSince counts the downloads the automatic searches added
// to the line at or after since, however they finished. Downloads a person
// asked for are not counted.
func (r *Repo) CountAutomaticAddedSince(since time.Time) (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM download_queue WHERE priority = 0 AND added_at >= ?`, since.UTC().Format("2006-01-02T15:04:05.000Z")).Scan(&n); err != nil {
		return 0, fmt.Errorf("count recent automatic downloads: %w", err)
	}
	return n, nil
}

// LineOrder sorts waiting items into the order they will start in.
func LineOrder(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.LineSeq != b.LineSeq {
		return a.LineSeq < b.LineSeq
	}
	return a.ID < b.ID
}

// PauseWaiting parks every download waiting in line as paused and marked
// interrupted, so a person has to press Resume. Safe mode uses it at start-up.
// It returns how many it paused.
func (r *Repo) PauseWaiting() (int64, error) {
	res, err := r.db.Exec(`UPDATE download_queue SET status = ?, interrupted = 1 WHERE status = ?`, string(StatusPaused), string(StatusQueued))
	if err != nil {
		return 0, fmt.Errorf("pause waiting downloads: %w", err)
	}
	return res.RowsAffected()
}

// CountAutomaticOpen counts the downloads the automatic searches added that
// are still waiting or running.
func (r *Repo) CountAutomaticOpen() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM download_queue WHERE priority = 0 AND status IN ('queued', 'downloading', 'importing')`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count automatic downloads in line: %w", err)
	}
	return n, nil
}
