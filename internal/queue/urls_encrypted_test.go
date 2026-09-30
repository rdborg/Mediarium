package queue_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

const keyedURL = "https://indexer.example/api?t=get&id=abc&apikey=SUPERSECRETKEY123"

func rawURL(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	// The repo returns the address as it was given, so read the column directly.
	var s string
	if err := db.QueryRow(`SELECT nzb_url FROM download_queue WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// The address of a download usually holds the indexer's API key, so the
// database must not.
func TestDownloadAddressIsStoredEncrypted(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	repo.SetBox(box)
	m, _ := library.NewRepo(db).Add(library.Movie{TMDBID: 1, Title: "Film"})

	id, err := repo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Film.2026.1080p", NZBURL: keyedURL})
	if err != nil {
		t.Fatal(err)
	}
	if stored := rawURL(t, db, id); strings.Contains(stored, "SUPERSECRETKEY123") || strings.Contains(stored, "indexer.example") || !strings.HasPrefix(stored, "enc1:") {
		t.Fatalf("the address is stored as %q", stored)
	}
	got, err := repo.Get(id)
	if err != nil || got.NZBURL != keyedURL {
		t.Fatalf("Get = %q, %v; want the original address", got.NZBURL, err)
	}
	list, err := repo.List()
	if err != nil || len(list) != 1 || list[0].NZBURL != keyedURL {
		t.Fatalf("List = %+v, %v", list, err)
	}
}

// Addresses stored in plain text by older versions still work, and are
// encrypted once at start.
func TestPlainTextDownloadAddressesAreEncryptedAtStart(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	m, _ := library.NewRepo(db).Add(library.Movie{TMDBID: 1, Title: "Film"})
	old, err := repo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Film.2026.1080p", NZBURL: keyedURL}) // no box yet: plain text, like an older version
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := repo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Film.2026.720p"})

	box, _ := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	repo.SetBox(box)
	if got, _ := repo.Get(old); got.NZBURL != keyedURL {
		t.Fatalf("a plain-text address must still be readable, got %q", got.NZBURL)
	}
	n, err := repo.EncryptStoredURLs()
	if err != nil || n != 1 {
		t.Fatalf("EncryptStoredURLs = %d, %v; want 1, nil", n, err)
	}
	if stored := rawURL(t, db, old); strings.Contains(stored, "SUPERSECRETKEY123") {
		t.Fatalf("still stored in plain text: %q", stored)
	}
	if got, _ := repo.Get(old); got.NZBURL != keyedURL {
		t.Fatalf("after encrypting: %q", got.NZBURL)
	}
	if got, _ := repo.Get(empty); got.NZBURL != "" {
		t.Fatalf("an empty address stays empty, got %q", got.NZBURL)
	}
	if n, err := repo.EncryptStoredURLs(); err != nil || n != 0 {
		t.Fatalf("second run = %d, %v; want 0, nil", n, err)
	}
}

// With another key the address cannot be read, but the list still loads.
func TestDownloadAddressWithTheWrongKeyIsEmptyNotAnError(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	m, _ := library.NewRepo(db).Add(library.Movie{TMDBID: 1, Title: "Film"})
	box1, _ := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	repo.SetBox(box1)
	id, _ := repo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Film", NZBURL: keyedURL})

	box2, _ := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	repo.SetBox(box2)
	got, err := repo.Get(id)
	if err != nil || got.NZBURL != "" {
		t.Fatalf("Get = %q, %v; want an empty address and no error", got.NZBURL, err)
	}
	if list, err := repo.List(); err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
}
