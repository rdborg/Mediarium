package subtitles

import (
	"database/sql"
	"fmt"
	"time"
)

const attemptTimeLayout = "2006-01-02T15:04:05.000Z"

// AttemptRepo remembers failed subtitle lookups.
type AttemptRepo struct {
	db *sql.DB
}

func NewAttemptRepo(db *sql.DB) *AttemptRepo { return &AttemptRepo{db: db} }

// Record notes that we just looked for (kind, mediaID, language).
func (r *AttemptRepo) Record(kind string, mediaID int64, language string) error {
	_, err := r.db.Exec(
		`INSERT INTO subtitle_attempts (kind, media_id, language, attempted_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (kind, media_id, language) DO UPDATE SET attempted_at = excluded.attempted_at`,
		kind, mediaID, language, time.Now().UTC().Format(attemptTimeLayout),
	)
	if err != nil {
		return fmt.Errorf("record subtitle attempt: %w", err)
	}
	return nil
}

// Recent reports whether we looked for this within the given window.
func (r *AttemptRepo) Recent(kind string, mediaID int64, language string, within time.Duration) (bool, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM subtitle_attempts WHERE kind = ? AND media_id = ? AND language = ? AND attempted_at >= ?`,
		kind, mediaID, language, time.Now().Add(-within).UTC().Format(attemptTimeLayout),
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check subtitle attempt: %w", err)
	}
	return n > 0, nil
}
