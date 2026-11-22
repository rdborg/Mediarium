package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/other"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/store"
)

// A 4.1.0 database (schema 0280) gets the "kept short releases" table and the
// Other library on upgrade, and nothing already in it is touched.
func TestUpgradeFrom410AddsKeptShortAndOther(t *testing.T) {
	names := migrationNames(t)
	upTo := -1
	for i, n := range names {
		if strings.HasPrefix(n, "0281_") {
			upTo = i
		}
	}
	if upTo < 0 {
		t.Fatal("no 0281 migration")
	}
	path := oldInstall(t, upTo, func(db *sql.DB) {
		mustExec(t, db, `INSERT INTO movies (id, tmdb_id, title, year) VALUES (1, 100, 'Movie', 2001)`)
		mustExec(t, db, `INSERT INTO books (id, ol_key, title) VALUES (1, 'OL1W', 'Book')`)
		mustExec(t, db, `INSERT INTO download_queue (movie_id, release_title, nzb_url, size_bytes, protocol, status) VALUES (1, 'Movie.2001.1080p-GRP', 'http://x/m.nzb', 0, 'usenet', 'completed')`)
		mustExec(t, db, `INSERT INTO download_queue (book_id, release_title, nzb_url, size_bytes, protocol, status) VALUES (1, 'Book.EPUB', 'http://x/b.nzb', 0, 'usenet', 'failed')`)
		mustExec(t, db, `INSERT INTO blocklist (release_title, title_key, reason) VALUES ('Dub.S01E01-GRP', ?, 'incomplete file')`, blocklist.Key("Dub.S01E01-GRP"))
	})
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_queue WHERE other_id IS NULL`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("old downloads should have no Other item: %d %v", n, err)
	}
	q := queue.NewRepo(db)
	old, err := q.Get(1)
	if err != nil || old.MovieID != 1 || old.OtherID != 0 {
		t.Fatalf("old download after the upgrade: %+v %v", old, err)
	}

	// Keep anyway works on the upgraded database.
	bl := blocklist.NewRepo(db)
	if got, err := bl.KeepShort("DUB.S01E01-grp", "incomplete file"); err != nil || got != 1 {
		t.Fatalf("keep: %d %v", got, err)
	}
	if !bl.KeptShort("dub.s01e01-GRP") {
		t.Fatal("the release should be remembered")
	}

	// Other items: made, downloaded for, and removed with their downloads.
	repo := other.NewRepo(db)
	it, err := repo.Add("Lectures", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	id, err := q.Enqueue(queue.Item{OtherID: it.ID, ReleaseTitle: "Lectures.Series", NZBURL: "http://x/a.nzb", Protocol: queue.ProtocolUsenet})
	if err != nil {
		t.Fatal(err)
	}
	if busy, _ := q.HasActiveForOther(it.ID); !busy {
		t.Fatal("the download should be active")
	}
	if err := repo.Delete(it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Get(id); err == nil {
		t.Fatal("the download of a removed item should go with it")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_queue`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("the other downloads stay: %d %v", n, err)
	}
	violations := 0
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		violations++
	}
	rows.Close()
	if violations != 0 {
		t.Fatalf("%d foreign key problems after the upgrade", violations)
	}
}
