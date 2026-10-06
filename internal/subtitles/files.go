package subtitles

import (
	"database/sql"
	"fmt"
	"time"
)

// SavedFile is a subtitle Mediarium downloaded and saved next to a video.
type SavedFile struct {
	Kind         string // "movie" or "episode"
	MediaID      int64
	Language     string
	FileID       int  // OpenSubtitles file id
	HashMatch    bool // made for this exact video file
	Chosen       bool // picked by hand: never replaced
	VideoHash    string
	Path         string
	Size         int64 // the subtitle's size and modification time when saved,
	ModTime      int64 // to notice it was changed since (Unix seconds)
	DownloadedAt time.Time
}

// FileRepo remembers the subtitles Mediarium saved.
type FileRepo struct {
	db *sql.DB
}

func NewFileRepo(db *sql.DB) *FileRepo { return &FileRepo{db: db} }

// Record notes a saved subtitle, replacing what was known about that
// title's subtitle in that language.
func (r *FileRepo) Record(f SavedFile) error {
	if f.DownloadedAt.IsZero() {
		f.DownloadedAt = time.Now()
	}
	_, err := r.db.Exec(`INSERT INTO subtitle_files (kind, media_id, language, file_id, hash_match, chosen, video_hash, sub_path, sub_size, sub_mtime, downloaded_at, checked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (kind, media_id, language) DO UPDATE SET file_id = excluded.file_id, hash_match = excluded.hash_match,
			chosen = excluded.chosen, video_hash = excluded.video_hash, sub_path = excluded.sub_path, sub_size = excluded.sub_size,
			sub_mtime = excluded.sub_mtime, downloaded_at = excluded.downloaded_at, checked_at = excluded.checked_at`,
		f.Kind, f.MediaID, f.Language, f.FileID, f.HashMatch, f.Chosen, f.VideoHash, f.Path, f.Size, f.ModTime,
		f.DownloadedAt.UTC().Format(attemptTimeLayout), f.DownloadedAt.UTC().Format(attemptTimeLayout)) // just looked: it was picked with the video's hash
	if err != nil {
		return fmt.Errorf("record subtitle file: %w", err)
	}
	return nil
}

// UpgradeCandidates lists the subtitles that may still be swapped for one
// made for the exact video file: picked automatically, not a hash match,
// downloaded after since and not looked at again after checkedBefore.
// Oldest checks first.
func (r *FileRepo) UpgradeCandidates(since, checkedBefore time.Time, limit int) ([]SavedFile, error) {
	rows, err := r.db.Query(`SELECT kind, media_id, language, file_id, video_hash, sub_path, sub_size, sub_mtime, downloaded_at
		FROM subtitle_files
		WHERE chosen = 0 AND hash_match = 0 AND downloaded_at >= ? AND checked_at < ?
		ORDER BY checked_at, downloaded_at LIMIT ?`,
		since.UTC().Format(attemptTimeLayout), checkedBefore.UTC().Format(attemptTimeLayout), limit)
	if err != nil {
		return nil, fmt.Errorf("list subtitle upgrade candidates: %w", err)
	}
	defer rows.Close()
	var out []SavedFile
	for rows.Next() {
		var f SavedFile
		var at string
		if err := rows.Scan(&f.Kind, &f.MediaID, &f.Language, &f.FileID, &f.VideoHash, &f.Path, &f.Size, &f.ModTime, &at); err != nil {
			return nil, fmt.Errorf("scan subtitle file: %w", err)
		}
		f.DownloadedAt, _ = time.Parse(attemptTimeLayout, at)
		out = append(out, f)
	}
	return out, rows.Err()
}

// MarkChecked notes that a better subtitle was just looked for.
func (r *FileRepo) MarkChecked(kind string, mediaID int64, language string) error {
	_, err := r.db.Exec(`UPDATE subtitle_files SET checked_at = ? WHERE kind = ? AND media_id = ? AND language = ?`,
		time.Now().UTC().Format(attemptTimeLayout), kind, mediaID, language)
	if err != nil {
		return fmt.Errorf("mark subtitle checked: %w", err)
	}
	return nil
}

// Forget drops what is known about a subtitle (it was changed or removed
// outside Mediarium, so it is no longer Mediarium's to replace).
func (r *FileRepo) Forget(kind string, mediaID int64, language string) error {
	if _, err := r.db.Exec(`DELETE FROM subtitle_files WHERE kind = ? AND media_id = ? AND language = ?`, kind, mediaID, language); err != nil {
		return fmt.Errorf("forget subtitle file: %w", err)
	}
	return nil
}
