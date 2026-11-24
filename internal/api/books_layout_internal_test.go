package api

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/settings"
)

// The folder layout setting decides where a new book goes; the default is the
// layout every library already has.
func TestBookFolderLayouts(t *testing.T) {
	db, err := OpenTestDB(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	root := t.TempDir()
	s := &Server{Settings: settings.New(db, nil), BookRepo: books.NewRepo(db)}
	if err := s.Settings.Set(settings.KeyEbooksPath, root, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Settings.Set(settings.KeyAudiobooksPath, root, false); err != nil {
		t.Fatal(err)
	}
	b, err := s.BookRepo.Add(books.Book{OLKey: "OL1W", Title: "Dune: Messiah", Year: 1969, Author: "Frank Herbert", SeriesName: "Dune", SeriesPosition: "2", WantEbook: true, WantAudiobook: true})
	if err != nil {
		t.Fatal(err)
	}
	loose, err := s.BookRepo.Add(books.Book{OLKey: "OL2W", Title: "Hyperion", Year: 1989, Author: "Dan Simmons", WantEbook: true, WantAudiobook: true})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, layout string
		b            books.Book
		f            books.Format
		folder, stem string
	}{
		{"default ebook", "", b, books.Ebook, filepath.Join(root, "Frank Herbert", "Dune Messiah (1969)"), "Dune Messiah"},
		{"explicit default audiobook", books.LayoutAuthorTitle, b, books.Audiobook, filepath.Join(root, "Frank Herbert", "Dune Messiah (1969)"), ""},
		{"series ebook", books.LayoutAuthorSeries, b, books.Ebook, filepath.Join(root, "Frank Herbert", "Dune"), "02. Dune Messiah (1969)"},
		{"series audiobook", books.LayoutAuthorSeries, b, books.Audiobook, filepath.Join(root, "Frank Herbert", "Dune", "Dune Messiah (1969)"), ""},
		{"no series ebook", books.LayoutAuthorSeries, loose, books.Ebook, filepath.Join(root, "Dan Simmons"), "Hyperion (1989)"},
		{"no series audiobook", books.LayoutAuthorSeries, loose, books.Audiobook, filepath.Join(root, "Dan Simmons", "Hyperion (1989)"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.layout != "" {
				if err := s.Settings.Set(settings.KeyBooksFolderLayout, tt.layout, false); err != nil {
					t.Fatal(err)
				}
			}
			folder, stem, err := s.bookFolder(tt.b, tt.f)
			if err != nil || folder != tt.folder || stem != tt.stem {
				t.Errorf("got %q, %q, %v; want %q, %q", folder, stem, err, tt.folder, tt.stem)
			}
		})
	}

	// A second book that lands on the same file gets its key after the name.
	if err := s.Settings.Set(settings.KeyBooksFolderLayout, books.LayoutAuthorSeries, false); err != nil {
		t.Fatal(err)
	}
	_ = s.BookRepo.SetState(b.ID, books.Ebook, books.StatusDownloaded, "epub", filepath.Join(root, "Frank Herbert", "Dune", "02. Dune Messiah (1969).epub"))
	twin, err := s.BookRepo.Add(books.Book{OLKey: "OL9W", Title: "Dune: Messiah", Year: 1969, Author: "Frank Herbert", SeriesName: "Dune", SeriesPosition: "2", WantEbook: true})
	if err != nil {
		t.Fatal(err)
	}
	_, stem, err := s.bookFolder(twin, books.Ebook)
	if err != nil || stem != "02. Dune Messiah (1969) [OL9W]" {
		t.Errorf("twin: %q, %v", stem, err)
	}
	// ...and the book that holds it keeps it.
	if _, stem, err = s.bookFolder(b, books.Ebook); err != nil || stem != "02. Dune Messiah (1969)" {
		t.Errorf("holder: %q, %v", stem, err)
	}
}
