package music

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Status is where an album stands, the same lifecycle as movies and
// episodes.
type Status string

const (
	StatusMissing     Status = "missing"
	StatusDownloading Status = "downloading"
	StatusDownloaded  Status = "downloaded"
)

// Album types.
const (
	TypeAlbum  = "album"
	TypeEP     = "ep"
	TypeSingle = "single"
)

// ErrArtistExists is returned when adding an artist already in the library.
var ErrArtistExists = errors.New("artist is already in the library")

// Artist is an artist in the library.
type Artist struct {
	ID             int64
	MBID           string
	Name           string
	SortName       string
	Disambiguation string
	Monitored      bool
	MonitorNew     bool  // albums found later start monitored
	ProfileID      int64 // 0 = the default profile
	AddedAt        string
	AddedBy        int64 // account that added it; 0 = unknown
}

// ArtistSummary is an artist with counts of its albums.
type ArtistSummary struct {
	Artist
	Albums     int
	Monitored  int // monitored albums
	Downloaded int // downloaded albums
}

// Album is one album, EP or single of an artist (a MusicBrainz release
// group).
type Album struct {
	ID          int64
	ArtistID    int64
	MBID        string // release-group id
	ReleaseMBID string // the release whose tracklist is used; "" until fetched
	Title       string
	Type        string // album, ep or single
	ReleaseDate string // "YYYY[-MM[-DD]]", "" if unknown
	Monitored   bool
	Status      Status
	Quality     string // a Tier once downloaded
	Path        string // the album's folder once imported
}

// Year is the album's release year, 0 if unknown.
func (a Album) Year() int {
	if len(a.ReleaseDate) < 4 {
		return 0
	}
	y, err := strconv.Atoi(a.ReleaseDate[:4])
	if err != nil {
		return 0
	}
	return y
}

// Track is one track of an album's tracklist.
type Track struct {
	ID       int64
	AlbumID  int64
	Disc     int
	Position int
	Title    string
	LengthMs int
	FilePath string // "" until imported
}

// Repo stores artists, albums, tracks and music profiles.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a repo on db (migrated by internal/store).
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

const artistColumns = `id, mbid, name, sort_name, disambiguation, monitored, monitor_new, COALESCE(profile_id, 0), added_at, COALESCE(added_by, 0)`

func scanArtist(row interface{ Scan(...any) error }) (Artist, error) {
	var a Artist
	err := row.Scan(&a.ID, &a.MBID, &a.Name, &a.SortName, &a.Disambiguation, &a.Monitored, &a.MonitorNew, &a.ProfileID, &a.AddedAt, &a.AddedBy)
	return a, err
}

// AddArtist adds an artist with its albums in one step. Albums get their
// ids and ArtistID filled in.
func (r *Repo) AddArtist(a Artist, albums []Album) (Artist, []Album, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return Artist{}, nil, fmt.Errorf("add artist: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM artists WHERE mbid = ?`, a.MBID).Scan(&exists); err != nil {
		return Artist{}, nil, fmt.Errorf("add artist: %w", err)
	}
	if exists > 0 {
		return Artist{}, nil, ErrArtistExists
	}
	res, err := tx.Exec(`INSERT INTO artists (mbid, name, sort_name, disambiguation, monitored, monitor_new, profile_id, added_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.MBID, a.Name, a.SortName, a.Disambiguation, a.Monitored, a.MonitorNew, nullID(a.ProfileID), nullID(a.AddedBy))
	if err != nil {
		return Artist{}, nil, fmt.Errorf("insert artist %s: %w", a.Name, err)
	}
	if a.ID, err = res.LastInsertId(); err != nil {
		return Artist{}, nil, fmt.Errorf("insert artist %s: %w", a.Name, err)
	}
	out := make([]Album, 0, len(albums))
	for _, al := range albums {
		al.ArtistID = a.ID
		if al.Status == "" {
			al.Status = StatusMissing
		}
		if al.Type == "" {
			al.Type = TypeAlbum
		}
		res, err := tx.Exec(`INSERT INTO albums (artist_id, mbid, release_mbid, title, type, release_date, monitored, status, quality, path) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (artist_id, mbid) DO NOTHING`,
			al.ArtistID, al.MBID, al.ReleaseMBID, al.Title, al.Type, al.ReleaseDate, al.Monitored, string(al.Status), al.Quality, al.Path)
		if err != nil {
			return Artist{}, nil, fmt.Errorf("insert album %s: %w", al.Title, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // listed twice by the source
		}
		if al.ID, err = res.LastInsertId(); err != nil {
			return Artist{}, nil, fmt.Errorf("insert album %s: %w", al.Title, err)
		}
		out = append(out, al)
	}
	if err := tx.Commit(); err != nil {
		return Artist{}, nil, fmt.Errorf("add artist: %w", err)
	}
	a2, err := r.GetArtist(a.ID)
	if err != nil {
		return Artist{}, nil, err
	}
	return a2, out, nil
}

// GetArtist returns one artist; the error wraps sql.ErrNoRows when there is
// no such artist.
func (r *Repo) GetArtist(id int64) (Artist, error) {
	a, err := scanArtist(r.db.QueryRow(`SELECT `+artistColumns+` FROM artists WHERE id = ?`, id))
	if err != nil {
		return Artist{}, fmt.Errorf("get artist %d: %w", id, err)
	}
	return a, nil
}

// GetArtistByMBID looks an artist up by MusicBrainz id.
func (r *Repo) GetArtistByMBID(mbid string) (Artist, bool, error) {
	a, err := scanArtist(r.db.QueryRow(`SELECT `+artistColumns+` FROM artists WHERE mbid = ?`, mbid))
	if errors.Is(err, sql.ErrNoRows) {
		return Artist{}, false, nil
	}
	if err != nil {
		return Artist{}, false, fmt.Errorf("get artist %s: %w", mbid, err)
	}
	return a, true, nil
}

// ListArtists lists every artist with album counts, by sort name.
func (r *Repo) ListArtists() ([]ArtistSummary, error) {
	rows, err := r.db.Query(`SELECT a.id, a.mbid, a.name, a.sort_name, a.disambiguation, a.monitored, a.monitor_new, COALESCE(a.profile_id, 0), a.added_at, COALESCE(a.added_by, 0),
		(SELECT COUNT(*) FROM albums WHERE artist_id = a.id),
		(SELECT COUNT(*) FROM albums WHERE artist_id = a.id AND monitored = 1),
		(SELECT COUNT(*) FROM albums WHERE artist_id = a.id AND status = 'downloaded')
		FROM artists a ORDER BY LOWER(CASE WHEN a.sort_name != '' THEN a.sort_name ELSE a.name END), a.id`)
	if err != nil {
		return nil, fmt.Errorf("list artists: %w", err)
	}
	defer rows.Close()
	out := []ArtistSummary{}
	for rows.Next() {
		var s ArtistSummary
		a := &s.Artist
		if err := rows.Scan(&a.ID, &a.MBID, &a.Name, &a.SortName, &a.Disambiguation, &a.Monitored, &a.MonitorNew, &a.ProfileID, &a.AddedAt, &a.AddedBy,
			&s.Albums, &s.Monitored, &s.Downloaded); err != nil {
			return nil, fmt.Errorf("scan artist: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetArtistMonitored switches an artist's monitoring.
func (r *Repo) SetArtistMonitored(id int64, monitored bool) error {
	if _, err := r.db.Exec(`UPDATE artists SET monitored = ? WHERE id = ?`, monitored, id); err != nil {
		return fmt.Errorf("set artist %d monitored: %w", id, err)
	}
	return nil
}

// DeleteArtist removes an artist with its albums and tracks (not files).
func (r *Repo) DeleteArtist(id int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("delete artist %d: %w", id, err)
	}
	defer tx.Rollback()
	// The detailed events shown only on an album's own page go with it.
	if _, err := tx.Exec(`DELETE FROM activity WHERE item_only = 1 AND album_id IN (SELECT id FROM albums WHERE artist_id = ?)`, id); err != nil {
		return fmt.Errorf("delete artist %d: remove album events: %w", id, err)
	}
	res, err := tx.Exec(`DELETE FROM artists WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete artist %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete artist %d: %w", id, sql.ErrNoRows)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete artist %d: %w", id, err)
	}
	return nil
}

const albumColumns = `id, artist_id, mbid, release_mbid, title, type, release_date, monitored, status, quality, path`

func scanAlbum(row interface{ Scan(...any) error }) (Album, error) {
	var a Album
	var status string
	err := row.Scan(&a.ID, &a.ArtistID, &a.MBID, &a.ReleaseMBID, &a.Title, &a.Type, &a.ReleaseDate, &a.Monitored, &status, &a.Quality, &a.Path)
	a.Status = Status(status)
	return a, err
}

func (r *Repo) queryAlbums(where string, args ...any) ([]Album, error) {
	rows, err := r.db.Query(`SELECT `+albumColumns+` FROM albums `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list albums: %w", err)
	}
	defer rows.Close()
	out := []Album{}
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			return nil, fmt.Errorf("scan album: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAlbums lists an artist's albums, oldest first (unknown dates last).
func (r *Repo) ListAlbums(artistID int64) ([]Album, error) {
	return r.queryAlbums(`WHERE artist_id = ? ORDER BY release_date = '', release_date, title`, artistID)
}

// ListAllAlbums lists every album of every artist.
func (r *Repo) ListAllAlbums() ([]Album, error) {
	return r.queryAlbums(`ORDER BY artist_id, release_date = '', release_date, title`)
}

// GetAlbum returns one album; the error wraps sql.ErrNoRows when there is
// no such album.
func (r *Repo) GetAlbum(id int64) (Album, error) {
	a, err := scanAlbum(r.db.QueryRow(`SELECT `+albumColumns+` FROM albums WHERE id = ?`, id))
	if err != nil {
		return Album{}, fmt.Errorf("get album %d: %w", id, err)
	}
	return a, nil
}

func (r *Repo) execAlbum(what string, id int64, query string, args ...any) error {
	res, err := r.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("%s album %d: %w", what, id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%s album %d: %w", what, id, sql.ErrNoRows)
	}
	return nil
}

// SetAlbumMonitored switches one album's monitoring.
func (r *Repo) SetAlbumMonitored(id int64, monitored bool) error {
	return r.execAlbum("set monitored of", id, `UPDATE albums SET monitored = ? WHERE id = ?`, monitored, id)
}

// SetAlbumStatus changes only an album's status (a grab starting, or one
// that failed going back).
func (r *Repo) SetAlbumStatus(id int64, status Status) error {
	return r.execAlbum("set status of", id, `UPDATE albums SET status = ? WHERE id = ?`, string(status), id)
}

// SetAlbumImported marks an album downloaded, with its quality and folder.
func (r *Repo) SetAlbumImported(id int64, quality Tier, path string) error {
	return r.execAlbum("mark imported", id, `UPDATE albums SET status = 'downloaded', quality = ?, path = ? WHERE id = ?`, string(quality), path, id)
}

// SetAlbumRelease records which release's tracklist the album uses.
func (r *Repo) SetAlbumRelease(id int64, releaseMBID string) error {
	return r.execAlbum("set release of", id, `UPDATE albums SET release_mbid = ? WHERE id = ?`, releaseMBID, id)
}

// ReplaceTracks sets an album's tracklist. Tracks already there (same disc
// and position) keep their file; tracks no longer listed are removed.
func (r *Repo) ReplaceTracks(albumID int64, tracks []Track) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("replace tracks of album %d: %w", albumID, err)
	}
	defer tx.Rollback()
	keep := make([]string, 0, len(tracks))
	for _, t := range tracks {
		if _, err := tx.Exec(`INSERT INTO tracks (album_id, disc, position, title, length_ms) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (album_id, disc, position) DO UPDATE SET title = excluded.title, length_ms = excluded.length_ms`,
			albumID, t.Disc, t.Position, t.Title, t.LengthMs); err != nil {
			return fmt.Errorf("save track %d-%d of album %d: %w", t.Disc, t.Position, albumID, err)
		}
		keep = append(keep, fmt.Sprintf("%d:%d", t.Disc, t.Position))
	}
	rows, err := tx.Query(`SELECT id, disc, position FROM tracks WHERE album_id = ?`, albumID)
	if err != nil {
		return fmt.Errorf("replace tracks of album %d: %w", albumID, err)
	}
	var drop []int64
	want := map[string]bool{}
	for _, k := range keep {
		want[k] = true
	}
	for rows.Next() {
		var id int64
		var disc, pos int
		if err := rows.Scan(&id, &disc, &pos); err != nil {
			rows.Close()
			return fmt.Errorf("replace tracks of album %d: %w", albumID, err)
		}
		if !want[fmt.Sprintf("%d:%d", disc, pos)] {
			drop = append(drop, id)
		}
	}
	rows.Close()
	for _, id := range drop {
		if _, err := tx.Exec(`DELETE FROM tracks WHERE id = ?`, id); err != nil {
			return fmt.Errorf("remove track %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("replace tracks of album %d: %w", albumID, err)
	}
	return nil
}

// ListTracks lists an album's tracks in order.
func (r *Repo) ListTracks(albumID int64) ([]Track, error) {
	rows, err := r.db.Query(`SELECT id, album_id, disc, position, title, length_ms, file_path FROM tracks WHERE album_id = ? ORDER BY disc, position`, albumID)
	if err != nil {
		return nil, fmt.Errorf("list tracks of album %d: %w", albumID, err)
	}
	defer rows.Close()
	out := []Track{}
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Disc, &t.Position, &t.Title, &t.LengthMs, &t.FilePath); err != nil {
			return nil, fmt.Errorf("scan track: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetTrackFile records the file a track was imported to ("" clears it).
func (r *Repo) SetTrackFile(trackID int64, path string) error {
	if _, err := r.db.Exec(`UPDATE tracks SET file_path = ? WHERE id = ?`, path, trackID); err != nil {
		return fmt.Errorf("set file of track %d: %w", trackID, err)
	}
	return nil
}

// ---- Profiles ----

func joinTiers(ts []Tier) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = string(t)
	}
	return strings.Join(parts, ",")
}

func splitTiers(s string) []Tier {
	var out []Tier
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, Tier(p))
		}
	}
	return out
}

// SeedPresets creates the built-in profiles on first use (an empty table):
// Lossy (the default), then Lossless with Lossy as its fallback. Profiles
// already there are left alone, so a saved choice never changes.
func (r *Repo) SeedPresets() error {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM music_profiles`).Scan(&n); err != nil {
		return fmt.Errorf("count music profiles: %w", err)
	}
	if n > 0 {
		return nil
	}
	presets := Presets()
	ids := make([]int64, len(presets))
	for i, p := range presets {
		res, err := r.db.Exec(`INSERT INTO music_profiles (name, allowed_tiers, cutoff, upgrade_allowed) VALUES (?, ?, ?, ?)`,
			p.Name, joinTiers(p.Allowed), string(p.Cutoff), p.UpgradeAllowed)
		if err != nil {
			return fmt.Errorf("seed music profile %s: %w", p.Name, err)
		}
		if ids[i], err = res.LastInsertId(); err != nil {
			return fmt.Errorf("seed music profile %s: %w", p.Name, err)
		}
	}
	fallback, _ := json.Marshal([]int64{ids[0]})
	if _, err := r.db.Exec(`UPDATE music_profiles SET fallback = ? WHERE id = ?`, string(fallback), ids[1]); err != nil {
		return fmt.Errorf("seed music profile fallback: %w", err)
	}
	return nil
}

// ListProfiles lists every music profile by id (the first is the default),
// each with its fallbacks resolved.
func (r *Repo) ListProfiles() ([]Profile, error) {
	rows, err := r.db.Query(`SELECT id, name, allowed_tiers, cutoff, upgrade_allowed, fallback FROM music_profiles ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list music profiles: %w", err)
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		var p Profile
		var allowed, cutoff, fallback string
		if err := rows.Scan(&p.ID, &p.Name, &allowed, &cutoff, &p.UpgradeAllowed, &fallback); err != nil {
			return nil, fmt.Errorf("scan music profile: %w", err)
		}
		p.Allowed, p.Cutoff = splitTiers(allowed), Tier(cutoff)
		if err := json.Unmarshal([]byte(fallback), &p.Fallback); err != nil {
			return nil, fmt.Errorf("music profile %d fallback: %w", p.ID, err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byID := map[int64]Profile{}
	for _, p := range out {
		byID[p.ID] = p
	}
	for i := range out {
		for _, id := range out[i].Fallback {
			if f, ok := byID[id]; ok && id != out[i].ID {
				out[i].FallbackProfiles = append(out[i].FallbackProfiles, f)
			}
		}
	}
	return out, nil
}

// ResolveProfile picks the profile with id from profiles, or the default
// (the first) when id is 0 or unknown. ok is false only when there are no
// profiles at all.
func ResolveProfile(profiles []Profile, id int64) (Profile, bool) {
	if len(profiles) == 0 {
		return Profile{}, false
	}
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return profiles[0], true
}
