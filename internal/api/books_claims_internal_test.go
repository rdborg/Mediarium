package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/store"
)

// A book download that will not finish hands the book back: missing, or
// downloaded when its file is still there.
func TestBookClaimIsReleased(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Server{BookRepo: books.NewRepo(db), QueueRepo: queue.NewRepo(db)}
	b, err := s.BookRepo.Add(books.Book{OLKey: "OL1W", Title: "The Hobbit", WantEbook: true, WantAudiobook: true})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "The Hobbit.epub")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = s.BookRepo.SetState(b.ID, books.Ebook, books.StatusDownloading, "epub", file)
	_ = s.BookRepo.SetState(b.ID, books.Audiobook, books.StatusDownloading, "", "")

	id, err := s.QueueRepo.Enqueue(queue.Item{BookID: b.ID, BookFormat: "audiobook", ReleaseTitle: "r", NZBURL: "http://x"})
	if err != nil {
		t.Fatal(err)
	}
	item, _ := s.QueueRepo.Get(id)
	// Still waiting in line: the claim stays.
	s.releaseTitleClaims(item)
	if got, _ := s.BookRepo.Get(b.ID); got.AudioStatus != books.StatusDownloading {
		t.Fatalf("a waiting download keeps its claim: %s", got.AudioStatus)
	}
	_ = s.QueueRepo.SetStatus(id, queue.StatusFailed, "gone")
	s.releaseTitleClaims(item)
	item.BookFormat = "ebook"
	s.releaseTitleClaims(item)
	got, _ := s.BookRepo.Get(b.ID)
	if got.AudioStatus != books.StatusMissing || got.EbookStatus != books.StatusDownloaded {
		t.Fatalf("after the download ended: audio %s, ebook %s", got.AudioStatus, got.EbookStatus)
	}
}
