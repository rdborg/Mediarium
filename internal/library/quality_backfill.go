package library

import "fmt"

// UnratedEpisode is a downloaded episode that has a file but no quality.
type UnratedEpisode struct {
	ID       int64
	FilePath string
}

// EpisodesWithoutQuality lists the downloaded episodes that have a file on
// record but whose quality is empty or Unknown.
func (r *Repo) EpisodesWithoutQuality() ([]UnratedEpisode, error) {
	rows, err := r.db.Query(`SELECT id, file_path FROM episodes
		WHERE status = 'downloaded' AND COALESCE(file_path, '') != '' AND COALESCE(quality, '') IN ('', 'Unknown')
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list episodes without quality: %w", err)
	}
	defer rows.Close()
	var out []UnratedEpisode
	for rows.Next() {
		var e UnratedEpisode
		if err := rows.Scan(&e.ID, &e.FilePath); err != nil {
			return nil, fmt.Errorf("scan episode without quality: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetEpisodeQualities stores a quality for each episode id in one
// transaction. An episode that has gained a quality in the meantime is left
// as it is.
func (r *Repo) SetEpisodeQualities(byID map[int64]string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin episode qualities: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`UPDATE episodes SET quality = ? WHERE id = ? AND COALESCE(quality, '') IN ('', 'Unknown')`)
	if err != nil {
		return fmt.Errorf("prepare episode qualities: %w", err)
	}
	defer stmt.Close()
	for id, q := range byID {
		if _, err := stmt.Exec(q, id); err != nil {
			return fmt.Errorf("set quality of episode %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit episode qualities: %w", err)
	}
	return nil
}
