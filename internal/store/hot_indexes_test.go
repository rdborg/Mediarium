package store_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library/librarytest"
	"github.com/rdborg/mediarium/internal/store"
)

// The album lookup by release-group id and the queue lookups per title have
// their own indexes, so they no longer read the whole table.
func TestHotLookupsUseTheirIndexes(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	for _, c := range []struct{ name, query, want string }{
		{"albums by release group", `SELECT id FROM albums WHERE mbid IN (?, ?, ?) ORDER BY id`, "idx_albums_mbid"},
		{"queue of a movie", `SELECT COUNT(*) FROM download_queue WHERE movie_id = ? AND status IN ('queued', 'downloading')`, "idx_download_queue_movie"},
		{"queue clean-up of a movie", `DELETE FROM download_queue WHERE movie_id = ? AND id <> ? AND status = 'failed'`, "idx_download_queue_movie"},
		{"queue of an episode", `DELETE FROM download_queue WHERE series_id = ? AND season = ? AND episode = ? AND id <> ? AND status = 'failed'`, "idx_download_queue_episode"},
		{"queue of a show", `SELECT id FROM download_queue WHERE series_id = ?`, "idx_download_queue_episode"},
		{"queue of an album", `SELECT COUNT(*) FROM download_queue WHERE album_id = ? AND status IN ('queued', 'downloading')`, "idx_download_queue_album"},
		{"queue of a book", `SELECT COUNT(*) FROM download_queue WHERE book_id = ? AND book_format = ? AND status IN ('queued')`, "idx_download_queue_book"},
		{"blocklist of a movie", `DELETE FROM blocklist WHERE movie_id = ?`, "idx_blocklist_movie"},
		{"blocklist of a show", `DELETE FROM blocklist WHERE series_id = ?`, "idx_blocklist_series"},
		{"blocklist of an album", `DELETE FROM blocklist WHERE album_id = ?`, "idx_blocklist_album"},
		{"blocklist of a book", `DELETE FROM blocklist WHERE book_id = ?`, "idx_blocklist_book"},
		{"playlists holding a track", `DELETE FROM music_playlist_tracks WHERE track_id = ?`, "idx_music_playlist_tracks_track"},
		{"reading progress of a book", `DELETE FROM book_progress WHERE book_id = ?`, "idx_book_progress_book"},
	} {
		args := make([]any, strings.Count(c.query, "?"))
		for i := range args {
			args[i] = 1
		}
		plan, err := librarytest.Plan(db, c.query, args...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(plan, c.want) {
			t.Errorf("%s: plan\n%s\nwant it to use %s", c.name, plan, c.want)
		}
	}
}

// TestAlbumLookupTiming logs the release-group lookup on 60,000 albums with
// and without the index, and fails only if the index does not help.
func TestAlbumLookupTiming(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO artists (mbid, name) VALUES ('a', 'A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 60000)
		INSERT INTO albums (artist_id, mbid, title) SELECT 1, 'mbid-' || i, 'Album ' || i FROM n`); err != nil {
		t.Fatal(err)
	}
	timeIt := func() time.Duration {
		best := time.Hour
		for i := 0; i < 5; i++ {
			start := time.Now()
			for j := 0; j < 20; j++ { // a Discover page asks about dozens of covers
				rows, err := db.Query(`SELECT id, title FROM albums WHERE mbid IN (?, ?, ?, ?) ORDER BY id`, "mbid-7", "mbid-31000", "mbid-59999", "none")
				if err != nil {
					t.Fatal(err)
				}
				for rows.Next() {
				}
				rows.Close()
			}
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	with := timeIt()
	if _, err := db.Exec(`DROP INDEX idx_albums_mbid`); err != nil {
		t.Fatal(err)
	}
	without := timeIt()
	t.Logf("20 lookups on 60000 albums: with the index %v, without %v", with, without)
	if with >= without {
		t.Errorf("the index does not help: %v with, %v without", with, without)
	}
}
