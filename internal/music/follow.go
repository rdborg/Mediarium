package music

import (
	"database/sql"
	"fmt"
)

// SetArtistFollow switches whether an artist is followed: whether albums
// found later (see AddAlbums) are picked up and monitored. It never touches
// the monitored flag of any album that is already listed.
func (r *Repo) SetArtistFollow(id int64, follow bool) error {
	res, err := r.db.Exec(`UPDATE artists SET monitored = ?, monitor_new = ? WHERE id = ?`, follow, follow, id)
	if err != nil {
		return fmt.Errorf("set follow of artist %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set follow of artist %d: %w", id, sql.ErrNoRows)
	}
	return nil
}

// AddAlbums adds albums to an artist that already has some, skipping the
// ones it has (same MusicBrainz id). It returns only the albums it added,
// with ids.
func (r *Repo) AddAlbums(artistID int64, albums []Album) ([]Album, error) {
	added := make([]Album, 0, len(albums))
	for _, al := range albums {
		al.ArtistID = artistID
		if al.Status == "" {
			al.Status = StatusMissing
		}
		if al.Type == "" {
			al.Type = TypeAlbum
		}
		res, err := r.db.Exec(`INSERT INTO albums (artist_id, mbid, release_mbid, title, type, release_date, monitored, status, quality, path) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (artist_id, mbid) DO NOTHING`,
			al.ArtistID, al.MBID, al.ReleaseMBID, al.Title, al.Type, al.ReleaseDate, al.Monitored, string(al.Status), al.Quality, al.Path)
		if err != nil {
			return added, fmt.Errorf("add album %s: %w", al.Title, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if al.ID, err = res.LastInsertId(); err != nil {
			return added, fmt.Errorf("add album %s: %w", al.Title, err)
		}
		added = append(added, al)
	}
	return added, nil
}

// SetAlbumReleaseDate records a release date MusicBrainz now gives for an
// album (an announced album gets its day).
func (r *Repo) SetAlbumReleaseDate(id int64, date string) error {
	return r.execAlbum("set release date of", id, `UPDATE albums SET release_date = ? WHERE id = ?`, date, id)
}

// BulkSetArtists follows or unfollows (follow) and/or gives a quality
// profile (profileID, 0 = the default) to every artist in ids in one
// transaction. Either may be nil to leave that setting alone. It returns the
// ids that are not in the library (any more); the rest are still changed. A
// database error undoes the whole change.
func (r *Repo) BulkSetArtists(ids []int64, follow *bool, profileID *int64) (missing []int64, err error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin bulk change of artists: %w", err)
	}
	defer tx.Rollback()
	for _, id := range ids {
		var found int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM artists WHERE id = ?`, id).Scan(&found); err != nil {
			return nil, fmt.Errorf("look up artist %d: %w", id, err)
		}
		if found == 0 {
			missing = append(missing, id)
			continue
		}
		if follow != nil {
			if _, err := tx.Exec(`UPDATE artists SET monitored = ?, monitor_new = ? WHERE id = ?`, *follow, *follow, id); err != nil {
				return nil, fmt.Errorf("set follow of artist %d: %w", id, err)
			}
		}
		if profileID != nil {
			if _, err := tx.Exec(`UPDATE artists SET profile_id = ? WHERE id = ?`, nullID(*profileID), id); err != nil {
				return nil, fmt.Errorf("set profile of artist %d: %w", id, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bulk change of artists: %w", err)
	}
	return missing, nil
}
