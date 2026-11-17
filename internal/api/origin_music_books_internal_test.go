package api

import (
	"fmt"
	"testing"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/origin"
	"github.com/rdborg/mediarium/internal/queue"
)

// An album's or a book's download says who or what started it, as a movie's
// does: the person who picked the release, whoever is behind the search run
// that grabbed it, or the scheduled search.
func TestAlbumAndBookDownloadOrigins(t *testing.T) {
	s := newAuditServer(t)
	s.dispatch.Stop() // nothing is downloaded: only what the download line records
	artist, albums, err := s.MusicRepo.AddArtist(music.Artist{MBID: "mb-1", Name: "Artist", Monitored: true}, []music.Album{{MBID: "rg-1", Title: "Album", Monitored: true}})
	if err != nil || len(albums) != 1 {
		t.Fatalf("add artist: %v", err)
	}
	album := albums[0]
	book, err := s.BookRepo.Add(books.Book{OLKey: "OL1W", Title: "A Book", WantEbook: true, WantAudiobook: true})
	if err != nil {
		t.Fatal(err)
	}
	anna := origin.Origin{Kind: origin.Manual, UserID: 3, User: "Anna"}
	annaSearch := origin.Origin{Kind: origin.Search, UserID: 3, User: "Anna"}
	list := origin.Origin{Kind: origin.List, Name: "Mixes"}
	scheduled := origin.Origin{Kind: origin.Auto, Name: origin.JobSearch}

	tests := []struct {
		name string
		grab func(n int) (int64, error)
		want origin.Origin
	}{
		{"an album release a person picked", func(n int) (int64, error) {
			return s.grabAlbum(artist, album, "Artist - Album [FLAC]", fmt.Sprintf("http://x.invalid/%d", n), 1, indexers.ProtocolUsenet, grabPicked, anna)
		}, anna},
		{"a person's search for the album", func(n int) (int64, error) {
			return s.grabAlbum(artist, album, "Artist - Album [FLAC]", fmt.Sprintf("http://x.invalid/%d", n), 1, indexers.ProtocolUsenet, grabSearchNow, annaSearch)
		}, annaSearch},
		{"a list's search for the album", func(n int) (int64, error) {
			return s.grabAlbum(artist, album, "Artist - Album [FLAC]", fmt.Sprintf("http://x.invalid/%d", n), 1, indexers.ProtocolUsenet, grabAutomatic, list)
		}, list},
		{"the scheduled album search", func(n int) (int64, error) {
			return s.grabAlbum(artist, album, "Artist - Album [FLAC]", fmt.Sprintf("http://x.invalid/%d", n), 1, indexers.ProtocolUsenet, grabAutomatic, origin.Origin{})
		}, scheduled},
		{"a book release a person picked", func(n int) (int64, error) {
			return s.grabBook(book, books.Ebook, "Writer - A Book [EPUB]", fmt.Sprintf("http://x.invalid/%d", n), 1, indexers.ProtocolUsenet, queue.PriorityManual, anna)
		}, anna},
		{"a search for the book", func(n int) (int64, error) {
			return s.grabBook(book, books.Audiobook, "Writer - A Book [M4B]", fmt.Sprintf("http://x.invalid/%d", n), 1, indexers.ProtocolUsenet, queue.PriorityAutomatic, annaSearch)
		}, annaSearch},
		{"the scheduled book search", func(n int) (int64, error) {
			return s.grabBook(book, books.Ebook, "Writer - A Book [EPUB]", fmt.Sprintf("http://x.invalid/%d", n), 1, indexers.ProtocolUsenet, queue.PriorityAutomatic, origin.Origin{})
		}, scheduled},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := tt.grab(i)
			if err != nil {
				t.Fatalf("grab: %v", err)
			}
			it, err := s.QueueRepo.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if it.Origin != tt.want {
				t.Fatalf("origin %+v, want %+v", it.Origin, tt.want)
			}
			// One download at a time per album or book format: clear it for the next case.
			if err := s.QueueRepo.Delete(id); err != nil {
				t.Fatal(err)
			}
			_ = s.MusicRepo.SetAlbumStatus(album.ID, music.StatusMissing)
			_ = s.BookRepo.SetState(book.ID, books.Ebook, books.StatusMissing, "", "")
			_ = s.BookRepo.SetState(book.ID, books.Audiobook, books.StatusMissing, "", "")
		})
	}
}
