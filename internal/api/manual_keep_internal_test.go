package api

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/books"
)

// A book added by hand keeps what was typed when a series look-up finds the
// same book in a catalogue, and an ordinary book still takes it.
func TestSeriesLookupLeavesABookAddedByHandAlone(t *testing.T) {
	db, err := OpenTestDB(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Server{BookRepo: books.NewRepo(db)}
	hand, err := s.BookRepo.Add(books.Book{OLKey: "manual:1", Title: "Home Brew", Author: "A. Writer", WantEbook: true})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.BookRepo.Add(books.Book{OLKey: "OL1W", Title: "Store Bought", Author: "B. Writer", WantEbook: true})
	if err != nil {
		t.Fatal(err)
	}
	series := &books.Series{Name: "The Series", Entries: []books.SeriesEntry{
		{Key: "manual:1", Title: "Home Brew", Author: "A. Writer", Position: "3", ReleaseDate: "2031-01-01"},
		{Key: "OL1W", Title: "Store Bought", Author: "B. Writer", Position: "2", ReleaseDate: "2031-02-02"},
	}}
	s.rememberSeries(hand, series, "3")
	s.rememberSeries(plain, series, "2")

	if got, _ := s.BookRepo.Get(hand.ID); got.SeriesName != "" || got.SeriesPosition != "" || got.ReleaseDate != "" || got.Title != "Home Brew" {
		t.Errorf("the book added by hand was changed: %+v", got)
	}
	if got, _ := s.BookRepo.Get(plain.ID); got.SeriesName != "The Series" || got.SeriesPosition != "2" || got.ReleaseDate != "2031-02-02" {
		t.Errorf("an ordinary book should still take the series: %+v", got)
	}
}
