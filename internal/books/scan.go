package books

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Reading a books folder that already has books in it, so they can be added
// to the library where they are. Nothing is moved or renamed. Author and title
// come from the folder layout most tools use:
//
//	<root>/<Author>/<Title>/<file or audio files>   (Mediarium, Calibre, Audiobookshelf)
//	<root>/<Author>/<Title>.epub
//	<root>/<Author> - <Title>.epub
//	<root>/<Author>/<Series>/<Title>/<audio files>

// LocalBook is one book found on disk.
type LocalBook struct {
	Path   string `json:"path"` // the ebook file, or the audiobook's folder
	Author string `json:"author"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
	Format string `json:"format"` // the file format: epub, m4b, mp3...
	Files  int    `json:"files"`
}

var (
	yearInParens = regexp.MustCompile(`\s*[\(\[]((?:1[5-9]|20)\d\d)[\)\]]`)
	leadingYear  = regexp.MustCompile(`^((?:1[5-9]|20)\d\d)\s*[-–.]\s*`)
	calibreID    = regexp.MustCompile(`\s*\(\d+\)$`)
	bracketed    = regexp.MustCompile(`\s*[\[\(][^\]\)]*[\]\)]`)
	discFolder   = regexp.MustCompile(`(?i)^(cd|disc|disk|part|pt)[\s._-]*\d+$`)
	spaces       = regexp.MustCompile(`\s+`)
	seriesIndex  = regexp.MustCompile(`^(?i:book\s*)?\d{1,3}(?:\.\d)?\s*[-–.]\s+`)
)

// cleanName takes the year out of a name and drops bracketed notes such as
// "[Unabridged]" or Calibre's "(123)".
func cleanName(s string) (string, int) {
	s = strings.ReplaceAll(s, "_", " ")
	year := 0
	if m := yearInParens.FindStringSubmatch(s); m != nil {
		year, _ = strconv.Atoi(m[1])
		s = yearInParens.ReplaceAllString(s, "")
	} else if m := leadingYear.FindStringSubmatch(s); m != nil {
		year, _ = strconv.Atoi(m[1])
		s = leadingYear.ReplaceAllString(s, "")
	}
	s = seriesIndex.ReplaceAllString(s, "")
	s = calibreID.ReplaceAllString(s, "")
	s = bracketed.ReplaceAllString(s, "")
	return strings.TrimSpace(spaces.ReplaceAllString(s, " ")), year
}

// splitAuthorTitle reads "Author - Title".
func splitAuthorTitle(s string) (author, title string) {
	if i := strings.Index(s, " - "); i > 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+3:])
	}
	return "", s
}

// Guess works out author, title and year from where a book is: parts are the
// folders (and, when file is true, the file name without its extension) under
// the library root.
func Guess(parts []string, file bool) (author, title string, year int) {
	switch len(parts) {
	case 0:
		return "", "", 0
	case 1:
		name, y := cleanName(parts[0])
		a, t := splitAuthorTitle(name)
		return a, t, y
	}
	author, _ = cleanName(parts[0])
	title, year = cleanName(parts[len(parts)-1])
	if file && len(parts) >= 3 {
		// <Author>/<Title>/<file>: the folder names the book; the file may
		// be "Title - Author" (Calibre) or anything else.
		if t, y := cleanName(parts[len(parts)-2]); t != "" {
			title = t
			if y > 0 {
				year = y
			}
		}
	}
	// "Title - Author" or "Author - Title" in the last part.
	if a, t := splitAuthorTitle(title); a != "" {
		switch {
		case strings.EqualFold(t, author):
			title = a
		case strings.EqualFold(a, author):
			title = t
		}
	}
	return author, title, year
}

func relParts(root, path string) []string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range strings.Split(filepath.ToSlash(rel), "/") {
		if p != "" && p != "." {
			out = append(out, p)
		}
	}
	return out
}

func hidden(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || name == "#recycle"
}

// ScanEbooks lists the ebooks under root: one per folder and title, in the
// best format there is.
func ScanEbooks(root string) ([]LocalBook, error) {
	best := map[string]LocalBook{} // folder + title
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable folder is skipped
		}
		if d.IsDir() {
			if path != root && hidden(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if EbookRank(ext) == 0 || hidden(d.Name()) {
			return nil
		}
		parts := relParts(root, strings.TrimSuffix(path, filepath.Ext(path)))
		author, title, year := Guess(parts, true)
		if title == "" {
			return nil
		}
		key := filepath.Dir(path) + "|" + strings.ToLower(title)
		if cur, ok := best[key]; ok {
			cur.Files++
			if EbookRank(ext) > EbookRank(cur.Format) {
				cur.Path, cur.Format = path, ext
			}
			best[key] = cur
			return nil
		}
		best[key] = LocalBook{Path: path, Author: author, Title: title, Year: year, Format: ext, Files: 1}
		return nil
	})
	return sorted(best), err
}

// ScanAudiobooks lists the audiobooks under root: every folder that holds
// audio files is one book, with "CD 1", "Disc 2" and "Part 3" folders counted
// as part of the folder above them.
func ScanAudiobooks(root string) ([]LocalBook, error) {
	found := map[string]LocalBook{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && hidden(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if AudioRank(ext) == 0 || hidden(d.Name()) {
			return nil
		}
		dir := filepath.Dir(path)
		for discFolder.MatchString(filepath.Base(dir)) && filepath.Dir(dir) != dir && dir != root {
			dir = filepath.Dir(dir)
		}
		loose := dir == root
		key, at := dir, dir
		if loose {
			// A loose file right in the root is a book of its own, at that file.
			key, at = strings.TrimSuffix(path, filepath.Ext(path)), path
		}
		cur, ok := found[key]
		if !ok {
			author, title, year := Guess(relParts(root, key), loose)
			if title == "" {
				return nil
			}
			cur = LocalBook{Path: at, Author: author, Title: title, Year: year, Format: ext}
		}
		cur.Files++
		if AudioRank(ext) > AudioRank(cur.Format) {
			cur.Format = ext
		}
		found[key] = cur
		return nil
	})
	return sorted(found), err
}

func sorted(m map[string]LocalBook) []LocalBook {
	out := make([]LocalBook, 0, len(m))
	for _, b := range m {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// SameAuthor reports whether two spellings name the same author: the surname
// of either is among the words of the other ("Tolkien" and "J.R.R. Tolkien",
// "Weir, Andy" and "Andy Weir").
func SameAuthor(a, b string) bool {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	has := func(ws []string, w string) bool {
		for _, x := range ws {
			if x == w {
				return true
			}
		}
		return false
	}
	return has(wb, wa[len(wa)-1]) || has(wa, wb[len(wb)-1])
}

// SameTitle reports whether a title found on disk names the same book as an
// Open Library title. exact asks for the same words; otherwise every word of
// the Open Library title has to be on disk (the name on disk may carry extra
// words, like a subtitle or "Unabridged").
func SameTitle(local, catalogue string, exact bool) bool {
	sig := func(s string) map[string]bool {
		out := map[string]bool{}
		for _, w := range words(s) {
			if !small[w] && len(w) > 1 {
				out[w] = true
			}
		}
		return out
	}
	l, c := sig(local), sig(catalogue)
	if len(c) == 0 || len(l) == 0 {
		return false
	}
	for w := range c {
		if !l[w] {
			return false
		}
	}
	return !exact || len(l) == len(c)
}
