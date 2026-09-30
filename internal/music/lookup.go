package music

import (
	"fmt"
	"strings"
)

// lookupChunk keeps every IN (...) list well under SQLite's variable limit.
const lookupChunk = 400

// AlbumsByMBID returns the library's albums whose release-group id is in
// mbids, by that id. When two artists share a release group (a split
// release), the album with the lowest id wins.
func (r *Repo) AlbumsByMBID(mbids []string) (map[string]Album, error) {
	out := map[string]Album{}
	for start := 0; start < len(mbids); start += lookupChunk {
		chunk := mbids[start:min(start+lookupChunk, len(mbids))]
		args := make([]any, len(chunk))
		for i, m := range chunk {
			args[i] = m
		}
		albums, err := r.queryAlbums(`WHERE mbid IN (`+placeholders(len(chunk))+`) ORDER BY id`, args...)
		if err != nil {
			return nil, fmt.Errorf("albums by release group id: %w", err)
		}
		for _, a := range albums {
			if _, ok := out[a.MBID]; !ok {
				out[a.MBID] = a
			}
		}
	}
	return out, nil
}

// ArtistsByMBID returns the library's artists whose id is in mbids, by that id.
func (r *Repo) ArtistsByMBID(mbids []string) (map[string]Artist, error) {
	out := map[string]Artist{}
	for start := 0; start < len(mbids); start += lookupChunk {
		chunk := mbids[start:min(start+lookupChunk, len(mbids))]
		args := make([]any, len(chunk))
		for i, m := range chunk {
			args[i] = m
		}
		rows, err := r.db.Query(`SELECT `+artistColumns+` FROM artists WHERE mbid IN (`+placeholders(len(chunk))+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("artists by id: %w", err)
		}
		for rows.Next() {
			a, err := scanArtist(rows)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan artist: %w", err)
			}
			out[a.MBID] = a
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("artists by id: %w", err)
		}
	}
	return out, nil
}

// Counts are the totals the dashboard shows for the music library.
type Counts struct {
	Artists    int
	Albums     int
	Downloaded int // albums with their files in the library
	Missing    int // albums not downloaded and not downloading now
}

// Counts totals the library.
func (r *Repo) Counts() (Counts, error) {
	var c Counts
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM artists`).Scan(&c.Artists); err != nil {
		return Counts{}, fmt.Errorf("count artists: %w", err)
	}
	err := r.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		FROM albums`, string(StatusDownloaded), string(StatusMissing)).Scan(&c.Albums, &c.Downloaded, &c.Missing)
	if err != nil {
		return Counts{}, fmt.Errorf("count albums: %w", err)
	}
	return c, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
