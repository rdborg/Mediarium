package books

import (
	"fmt"
	"path/filepath"
	"strings"
)

// How new books are filed (the books.folder_layout setting). Books already in
// the library are never moved when this changes.
const (
	// LayoutAuthorTitle is <Author>/<Title (Year)>/, the layout since the start.
	LayoutAuthorTitle = "author_title"
	// LayoutAuthorSeries groups books by series: <Author>/<Series>/. An ebook is
	// a file there ("01. Title (Year).epub"), an audiobook keeps a folder of
	// its own ("<Series>/<Title (Year)>/"). A book with no series goes straight
	// under its author.
	LayoutAuthorSeries = "author_series"
)

// NormalizeLayout is v when it names a layout, and the default otherwise.
func NormalizeLayout(v string) string {
	if strings.TrimSpace(v) == LayoutAuthorSeries {
		return LayoutAuthorSeries
	}
	return LayoutAuthorTitle
}

// SeriesPrefix is "01. " for a book that is whole book 1 of a series, and ""
// for anything else: no position, or a place that isn't a whole book ("2.5",
// "1-3").
func SeriesPrefix(b Book) string {
	n, ok := PositionNumber(b.SeriesPosition)
	if !ok || n != float64(int(n)) {
		return ""
	}
	return fmt.Sprintf("%02d. ", int(n))
}

// Place says where a book's files go under root: dir is the folder they are in
// (for an audiobook, the book's own folder), stem the file name of an ebook
// without its extension (empty for an audiobook, whose files keep their own
// names). clean makes one name safe for a path (the illegal-character
// setting).
func Place(root, author string, b Book, f Format, layout string, clean func(string) string) (dir, stem string) {
	author = strings.TrimSpace(author)
	if author == "" {
		author = "Unknown author"
	}
	dir = filepath.Join(root, clean(author))
	if NormalizeLayout(layout) == LayoutAuthorTitle {
		// Author/Title (Year)/Title.epub
		dir = filepath.Join(dir, clean(FolderName(b)))
		if f == Audiobook {
			return dir, ""
		}
		return dir, clean(b.Title)
	}
	series := ""
	if name := strings.TrimSpace(b.SeriesName); name != "" {
		series = strings.TrimSpace(clean(name))
	}
	if series != "" {
		dir = filepath.Join(dir, series)
	}
	if f == Audiobook {
		// Author/[Series/]Title (Year)/files
		return filepath.Join(dir, clean(FolderName(b))), ""
	}
	// Author/[Series/]NN. Title (Year).epub
	prefix := ""
	if series != "" {
		prefix = SeriesPrefix(b)
	}
	return dir, clean(prefix + FolderName(b))
}

// UniqueStem is base (the folder an audiobook goes in, or an ebook's path
// without its extension) unless another book of the library already has its
// files there; then the Open Library key goes after the name, as UniqueFolder
// does.
func UniqueStem(base string, b Book, f Format, all []Book) string {
	return uniquePath(base, b, f, all, f == Ebook)
}
