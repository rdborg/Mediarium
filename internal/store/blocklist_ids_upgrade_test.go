package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/store"
)

// Album and book releases blocklisted on import before they were saved with
// their album or book are tied to it on upgrade, from the download or the
// album's event, so they can be limited to that title; an entry a person made
// for a download of no title, or one that fits two titles, stays everywhere.
func TestUpgradeTiesOldAlbumAndBookBlocklistEntries(t *testing.T) {
	names := migrationNames(t)
	upTo := -1
	for i, n := range names {
		if strings.HasPrefix(n, "0275_") {
			upTo = i
		}
	}
	if upTo < 0 {
		t.Fatal("no 0275 migration")
	}
	path := oldInstall(t, upTo, func(db *sql.DB) {
		mustExec(t, db, `INSERT INTO artists (id, mbid, name) VALUES (1, 'mb-1', 'Artist')`)
		mustExec(t, db, `INSERT INTO albums (id, artist_id, mbid, title) VALUES (1, 1, 'rg-1', 'One'), (2, 1, 'rg-2', 'Two'), (3, 1, 'rg-3', 'Three')`)
		mustExec(t, db, `INSERT INTO books (id, ol_key, title) VALUES (1, 'OL1W', 'Book')`)
		mustExec(t, db, `INSERT INTO download_queue (album_id, release_title, status) VALUES (1, 'Artist - One [FLAC]', 'failed')`)
		mustExec(t, db, `INSERT INTO download_queue (book_id, release_title, status) VALUES (1, 'Book.Author.EPUB', 'failed')`)
		mustExec(t, db, `INSERT INTO download_queue (release_title, status) VALUES ('Loose.Release', 'failed')`)
		mustExec(t, db, `INSERT INTO download_queue (album_id, release_title, status) VALUES (2, 'Shared.Release', 'failed'), (3, 'Shared.Release', 'failed')`)
		// The download of album two is gone; its event is still there.
		mustExec(t, db, `INSERT INTO activity (album_id, event_type, message) VALUES (2, 'blocklisted', 'Blocklisted "Artist - Two_100% [MP3]": bad archive')`)
		for _, rel := range []string{"Artist - One [FLAC]", "Book.Author.EPUB", "Loose.Release", "Shared.Release", "Artist - Two_100% [MP3]", "Artist - Tw"} {
			mustExec(t, db, `INSERT INTO blocklist (release_title, title_key, reason) VALUES (?, ?, 'bad')`, rel, blocklist.Key(rel))
		}
	})
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer db.Close()

	tests := []struct {
		release       string
		album, book   int64
		canBeNarrowed bool
	}{
		{"Artist - One [FLAC]", 1, 0, true},
		{"Book.Author.EPUB", 0, 1, true},
		{"Artist - Two_100% [MP3]", 2, 0, true},
		{"Loose.Release", 0, 0, false},  // a download for no title: blocked everywhere on purpose
		{"Shared.Release", 0, 0, false}, // two albums had it: which one is unknown
		{"Artist - Tw", 0, 0, false},    // only the start of another release's event
	}
	repo := blocklist.NewRepo(db)
	for _, tt := range tests {
		t.Run(tt.release, func(t *testing.T) {
			var id int64
			var album, book sql.NullInt64
			if err := db.QueryRow(`SELECT id, album_id, book_id FROM blocklist WHERE title_key = ?`, blocklist.Key(tt.release)).Scan(&id, &album, &book); err != nil {
				t.Fatal(err)
			}
			if album.Int64 != tt.album || book.Int64 != tt.book {
				t.Fatalf("album %d, book %d; want %d, %d", album.Int64, book.Int64, tt.album, tt.book)
			}
			err := repo.SetScope(id, true)
			if tt.canBeNarrowed && err != nil {
				t.Fatalf("narrow: %v", err)
			}
			if !tt.canBeNarrowed && err == nil {
				t.Fatal("narrowed an entry that is tied to no title")
			}
		})
	}
}
