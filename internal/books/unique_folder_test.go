package books

import (
	"path/filepath"
	"testing"
)

func TestUniqueFolder(t *testing.T) {
	folder := filepath.Join("lib", "Neil Gaiman", "Neverwhere (1996)")
	mine := Book{ID: 2, OLKey: "OL2W", EbookPath: folder}
	tests := []struct {
		name string
		b    Book
		f    Format
		all  []Book
		want string
	}{
		{"nothing there", Book{ID: 1, OLKey: "OL1W"}, Ebook, nil, folder},
		{"the book itself holds it", mine, Ebook, []Book{mine}, folder},
		{"another book holds it", Book{ID: 1, OLKey: "OL1W"}, Ebook, []Book{mine}, folder + " [OL1W]"},
		{"held in other letters", Book{ID: 1, OLKey: "OL1W"}, Ebook, []Book{{ID: 2, EbookPath: filepath.Join("LIB", "neil gaiman", "NEVERWHERE (1996)")}}, folder + " [OL1W]"},
		{"the other book's audiobook is another root", Book{ID: 1, OLKey: "OL1W"}, Audiobook, []Book{mine}, folder},
		{"a key with odd characters", Book{ID: 7, OLKey: "../x y"}, Ebook, []Book{mine}, folder + " [xy]"},
		{"no key at all", Book{ID: 7}, Ebook, []Book{mine}, folder + " [7]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UniqueFolder(folder, tt.b, tt.f, tt.all); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
