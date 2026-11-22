package queue

import (
	"database/sql"
	"errors"
	"fmt"
)

// Duplicate detection. Like SABnzbd, a download is a duplicate when it is the
// same NZB (the same article ids, whatever the file is called) or has the same
// release name and size as one already in the queue or in the history. It only
// counts when the earlier one is for the same movie, episode, album or book,
// and is not one that failed or was stopped: those never delivered anything,
// so trying the release again is not a second copy.

// SetNZBHash stores the fingerprint of the NZB item id was started from.
func (r *Repo) SetNZBHash(id int64, hash string) error {
	if _, err := r.db.Exec(`UPDATE download_queue SET nzb_hash = ? WHERE id = ?`, hash, id); err != nil {
		return fmt.Errorf("save the NZB fingerprint of queue item %d: %w", id, err)
	}
	return nil
}

// hasTarget reports whether the item says what it is for.
func (it Item) hasTarget() bool {
	return it.MovieID > 0 || it.SeriesID > 0 || it.AlbumID > 0 || it.BookID > 0 || it.OtherID > 0
}

// targetClause is the SQL that matches the rows made for the same thing as it,
// with its arguments.
func (it Item) targetClause() (string, []any) {
	switch {
	case it.MovieID > 0:
		return `movie_id = ?`, []any{it.MovieID}
	case it.SeriesID > 0:
		return `series_id = ? AND COALESCE(season, 0) = ? AND COALESCE(episode, 0) = ?`, []any{it.SeriesID, it.Season, it.Episode}
	case it.AlbumID > 0:
		return `album_id = ?`, []any{it.AlbumID}
	case it.OtherID > 0:
		return `other_id = ?`, []any{it.OtherID}
	default:
		return `book_id = ? AND book_format = ?`, []any{it.BookID, it.BookFormat}
	}
}

// FindDuplicate looks for an earlier download of the same thing that makes it a
// duplicate: the same fingerprint (when hash is not empty), or the same release
// name (ignoring case) and size. The item itself (its ID) is never its own
// duplicate, and only items with a lower ID count as earlier: of two identical
// downloads that start at the same moment, the later one is the duplicate and
// the earlier one is not, so both can never skip each other. It returns the
// earlier download, or false when there is none.
func (r *Repo) FindDuplicate(it Item, hash string) (Item, bool, error) {
	if !it.hasTarget() {
		return Item{}, false, nil
	}
	target, args := it.targetClause()
	query := `SELECT id FROM download_queue WHERE (? = 0 OR id < ?) AND status IN ('queued', 'downloading', 'importing', 'paused', 'conflict', 'completed')
		AND ` + target + ` AND ((release_title = ? COLLATE NOCASE AND size_bytes = ?)`
	all := append([]any{it.ID, it.ID}, args...)
	all = append(all, it.ReleaseTitle, it.SizeBytes)
	if hash != "" {
		query += ` OR nzb_hash = ?`
		all = append(all, hash)
	}
	// The oldest first: that is the download that got there before.
	query += `) ORDER BY id ASC LIMIT 1`
	var id int64
	err := r.db.QueryRow(query, all...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, false, nil
	}
	if err != nil {
		return Item{}, false, fmt.Errorf("look for a duplicate download: %w", err)
	}
	other, err := r.Get(id)
	if err != nil {
		return Item{}, false, err
	}
	return other, true, nil
}
