package books

import (
	"path/filepath"
	"strings"
	"testing"
)

// clean stands in for the organizer's sanitiser: it drops what a path can't hold.
func clean(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, s))
}

func TestPlace(t *testing.T) {
	root := "lib"
	hp := Book{ID: 1, Title: "Harry Potter and the Chamber of Secrets", Year: 1998, Author: "J. K. Rowling", SeriesName: "Harry Potter", SeriesPosition: "2"}
	tests := []struct {
		name   string
		b      Book
		f      Format
		layout string
		dir    string
		stem   string
	}{
		{"default ebook", hp, Ebook, LayoutAuthorTitle, "lib/J. K. Rowling/Harry Potter and the Chamber of Secrets (1998)", "Harry Potter and the Chamber of Secrets"},
		{"unset layout is the default", hp, Ebook, "", "lib/J. K. Rowling/Harry Potter and the Chamber of Secrets (1998)", "Harry Potter and the Chamber of Secrets"},
		{"unknown layout is the default", hp, Audiobook, "nonsense", "lib/J. K. Rowling/Harry Potter and the Chamber of Secrets (1998)", ""},
		{"series ebook", hp, Ebook, LayoutAuthorSeries, "lib/J. K. Rowling/Harry Potter", "02. Harry Potter and the Chamber of Secrets (1998)"},
		{"series audiobook", hp, Audiobook, LayoutAuthorSeries, "lib/J. K. Rowling/Harry Potter/Harry Potter and the Chamber of Secrets (1998)", ""},
		{"position with a leading zero and a point", withPos(hp, "03.0"), Ebook, LayoutAuthorSeries, "lib/J. K. Rowling/Harry Potter", "03. Harry Potter and the Chamber of Secrets (1998)"},
		{"two digit position", withPos(hp, "12"), Ebook, LayoutAuthorSeries, "lib/J. K. Rowling/Harry Potter", "12. Harry Potter and the Chamber of Secrets (1998)"},
		{"fractional position has no prefix", withPos(hp, "2.5"), Ebook, LayoutAuthorSeries, "lib/J. K. Rowling/Harry Potter", "Harry Potter and the Chamber of Secrets (1998)"},
		{"a range has no prefix", withPos(hp, "1-3"), Ebook, LayoutAuthorSeries, "lib/J. K. Rowling/Harry Potter", "Harry Potter and the Chamber of Secrets (1998)"},
		{"no position has no prefix", withPos(hp, ""), Ebook, LayoutAuthorSeries, "lib/J. K. Rowling/Harry Potter", "Harry Potter and the Chamber of Secrets (1998)"},
		{"no series goes under the author", Book{Title: "Dune", Year: 1965, Author: "Frank Herbert"}, Ebook, LayoutAuthorSeries, "lib/Frank Herbert", "Dune (1965)"},
		{"no series, a stray position is no prefix", Book{Title: "Dune", Author: "Frank Herbert", SeriesPosition: "1"}, Ebook, LayoutAuthorSeries, "lib/Frank Herbert", "Dune"},
		{"no series audiobook", Book{Title: "Dune", Year: 1965, Author: "Frank Herbert"}, Audiobook, LayoutAuthorSeries, "lib/Frank Herbert/Dune (1965)", ""},
		{"no year", Book{Title: "Dune", Author: "Frank Herbert", SeriesName: "Dune", SeriesPosition: "1"}, Ebook, LayoutAuthorSeries, "lib/Frank Herbert/Dune", "01. Dune"},
		{"odd characters", Book{Title: "Who? What: Why", Year: 2001, Author: "A/B", SeriesName: `The "Why": Saga?`, SeriesPosition: "4"}, Ebook, LayoutAuthorSeries, "lib/AB/The Why Saga", "04. Who What Why (2001)"},
		{"a series that is all odd characters is no series", Book{Title: "Dune", Author: "Frank Herbert", SeriesName: "???", SeriesPosition: "1"}, Ebook, LayoutAuthorSeries, "lib/Frank Herbert", "Dune"},
		{"no author", Book{Title: "Dune", SeriesName: "Dune", SeriesPosition: "1"}, Audiobook, LayoutAuthorSeries, "lib/Unknown author/Dune/Dune", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, stem := Place(root, tt.b.Author, tt.b, tt.f, tt.layout, clean)
			if want := filepath.FromSlash(tt.dir); dir != want || stem != tt.stem {
				t.Errorf("got %q, %q; want %q, %q", dir, stem, want, tt.stem)
			}
		})
	}
}

func withPos(b Book, pos string) Book {
	b.SeriesPosition = pos
	return b
}

func TestUniqueStem(t *testing.T) {
	base := filepath.Join("lib", "A", "Series", "01. Title (2000)")
	held := Book{ID: 2, EbookPath: base + ".epub", AudioPath: filepath.Join("lib", "A", "Series", "Title (2000)")}
	me := Book{ID: 1, OLKey: "OL1W"}
	tests := []struct {
		name string
		b    Book
		f    Format
		base string
		all  []Book
		want string
	}{
		{"free", me, Ebook, base, nil, base},
		{"held by a file of another book", me, Ebook, base, []Book{held}, base + " [OL1W]"},
		{"held in other letters", me, Ebook, base, []Book{{ID: 2, EbookPath: strings.ToUpper(base) + ".EPUB"}}, base + " [OL1W]"},
		{"the book's own file is not a clash", Book{ID: 2, OLKey: "OL2W"}, Ebook, base, []Book{held}, base},
		{"an audiobook folder is compared whole", me, Audiobook, filepath.Join("lib", "A", "Series", "Title (2000)"), []Book{held}, filepath.Join("lib", "A", "Series", "Title (2000)") + " [OL1W]"},
		{"no key", Book{ID: 9}, Ebook, base, []Book{held}, base + " [9]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UniqueStem(tt.base, tt.b, tt.f, tt.all); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSeriesPrefix(t *testing.T) {
	for pos, want := range map[string]string{"1": "01. ", "10": "10. ", "7.0": "07. ", "#3": "03. ", "Book 4": "04. ", "0": "00. ", "2.5": "", "1-7": "", "": "", "x": "", "-1": ""} {
		if got := SeriesPrefix(Book{SeriesPosition: pos}); got != want {
			t.Errorf("SeriesPrefix(%q) = %q, want %q", pos, got, want)
		}
	}
}

func TestNormalizeLayout(t *testing.T) {
	for in, want := range map[string]string{"": LayoutAuthorTitle, "author_title": LayoutAuthorTitle, "author_series": LayoutAuthorSeries, " author_series ": LayoutAuthorSeries, "x": LayoutAuthorTitle} {
		if got := NormalizeLayout(in); got != want {
			t.Errorf("NormalizeLayout(%q) = %q, want %q", in, got, want)
		}
	}
}

// The files the series layout writes are read back as the right books.
func TestScanSeriesLayout(t *testing.T) {
	eb := t.TempDir()
	write(t, filepath.Join(eb, "J. K. Rowling", "Harry Potter", "02. Harry Potter and the Chamber of Secrets (1998).epub"), 10)
	write(t, filepath.Join(eb, "J. K. Rowling", "Harry Potter", "02. Harry Potter and the Chamber of Secrets (1998).pdf"), 10)
	write(t, filepath.Join(eb, "J. K. Rowling", "Harry Potter", "03.5. Odd Novella.epub"), 10)
	write(t, filepath.Join(eb, "J. K. Rowling", "Harry Potter", "Companion Guide (2001).epub"), 10)
	write(t, filepath.Join(eb, "Frank Herbert", "Dune (1965).epub"), 10)
	write(t, filepath.Join(eb, "Frank Herbert", "Dune (12)", "Dune - Frank Herbert.epub"), 10)
	list, err := ScanEbooks(eb)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Author, Title string
		Year          int
		Series, Pos   string
		Format        string
	}
	got := map[string]row{}
	for _, b := range list {
		got[b.Path] = row{b.Author, b.Title, b.Year, b.Series, b.Position, b.Format}
	}
	want := map[string]row{
		filepath.Join(eb, "J. K. Rowling", "Harry Potter", "02. Harry Potter and the Chamber of Secrets (1998).epub"): {"J. K. Rowling", "Harry Potter and the Chamber of Secrets", 1998, "Harry Potter", "2", "epub"},
		filepath.Join(eb, "J. K. Rowling", "Harry Potter", "03.5. Odd Novella.epub"):                                  {"J. K. Rowling", "Odd Novella", 0, "Harry Potter", "", "epub"},
		filepath.Join(eb, "J. K. Rowling", "Harry Potter", "Companion Guide (2001).epub"):                             {"J. K. Rowling", "Companion Guide", 2001, "Harry Potter", "", "epub"},
		filepath.Join(eb, "Frank Herbert", "Dune (1965).epub"):                                                        {"Frank Herbert", "Dune", 1965, "", "", "epub"},
		filepath.Join(eb, "Frank Herbert", "Dune (12)", "Dune - Frank Herbert.epub"):                                  {"Frank Herbert", "Dune", 0, "", "", "epub"},
	}
	for p, w := range want {
		if g, ok := got[p]; !ok || g != w {
			t.Errorf("%s: got %+v (found %v), want %+v", p, g, ok, w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("found %d books, want %d: %+v", len(got), len(want), got)
	}

	// Audiobooks: Author/Series/Title (Year)/files, and with no series Author/Title (Year)/files.
	ab := t.TempDir()
	write(t, filepath.Join(ab, "J. K. Rowling", "Harry Potter", "Harry Potter and the Chamber of Secrets (1998)", "01.mp3"), 10)
	write(t, filepath.Join(ab, "Frank Herbert", "Dune (1965)", "Dune.m4b"), 10)
	alist, err := ScanAudiobooks(ab)
	if err != nil || len(alist) != 2 {
		t.Fatalf("audiobooks: %+v %v", alist, err)
	}
	for _, b := range alist {
		switch b.Title {
		case "Harry Potter and the Chamber of Secrets":
			if b.Author != "J. K. Rowling" || b.Year != 1998 {
				t.Errorf("series audiobook: %+v", b)
			}
		case "Dune":
			if b.Author != "Frank Herbert" || b.Year != 1965 {
				t.Errorf("plain audiobook: %+v", b)
			}
		default:
			t.Errorf("unexpected book %+v", b)
		}
	}
}
