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
//	<root>/<Author>/<Series>/<NN. Title>.epub       (Mediarium's series layout)
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
	// A comic's own words about itself (ComicInfo.xml), when it has them.
	Series   string `json:"series,omitempty"`
	Position string `json:"position,omitempty"`
}

var (
	yearInParens = regexp.MustCompile(`\s*[\(\[]((?:1[5-9]|20)\d\d)[\)\]]`)
	leadingYear  = regexp.MustCompile(`^((?:1[5-9]|20)\d\d)\s*[-–.]\s*`)
	calibreID    = regexp.MustCompile(`\s*\(\d+\)$`)
	bracketed    = regexp.MustCompile(`\s*[\[\(][^\]\)]*[\]\)]`)
	discFolder   = regexp.MustCompile(`(?i)^(cd|disc|disk|part|pt)[\s._-]*\d+$`)
	spaces       = regexp.MustCompile(`\s+`)
	seriesNumber = regexp.MustCompile(`^(?i:book\s*)?(\d{1,3}(?:\.\d)?)\s*[-–.]\s+`)
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

// inSeriesFolder reports whether an ebook at parts (folders, then the file name
// without its extension) is filed the way Mediarium's series layout does:
// <Author>/<Series>/<NN. Title (Year)>. The book is the file then, not the
// folder above it, which is what <Author>/<Title>/<file> would say. The file
// is taken for a series book when its name starts with its place in the series
// ("02. ") or has a year in brackets while the folder has none (the old layout
// is the other way round). A book with neither is read the old way. position
// is "" when the place isn't a whole number.
func inSeriesFolder(parts []string) (series, position string, ok bool) {
	n := len(parts)
	if n < 3 {
		return "", "", false
	}
	folder, folderYear := cleanName(parts[n-2])
	if folder == "" {
		return "", "", false
	}
	if m := seriesNumber.FindStringSubmatch(parts[n-1]); m != nil {
		if num, err := strconv.ParseFloat(m[1], 64); err == nil && num == float64(int(num)) {
			position = strconv.Itoa(int(num))
		}
		return folder, position, true
	}
	if _, fileYear := cleanName(parts[n-1]); fileYear > 0 && folderYear == 0 {
		return folder, "", true
	}
	return "", "", false
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
	if _, _, inSeries := inSeriesFolder(parts); file && len(parts) >= 3 && !inSeries {
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

var (
	// Issue numbers in a comic's file name: "Batman #12", "Batman Issue 4",
	// "Batman 001", "Saga v3".
	comicHash   = regexp.MustCompile(`^(.*?)\s*#\s*(\d+(?:\.\d+)?)\b`)
	comicIssue  = regexp.MustCompile(`(?i)^(.*?)\s*\b(?:issue|no\.?)\s*(\d+(?:\.\d+)?)\b`)
	comicNumber = regexp.MustCompile(`^(.*?)\s*\b(\d{1,3}(?:\.\d+)?|0\d{3})$`)
	comicVolume = regexp.MustCompile(`(?i)^(.*?)\s*\bv(?:ol(?:ume)?)?\.?\s*(\d{1,3})$`)
)

// GuessComicIssue reads a series name and an issue (or volume) number from a
// comic's file name, without its extension. Either may be "".
func GuessComicIssue(name string) (series, position string) {
	s := strings.ReplaceAll(name, "_", " ")
	s = strings.TrimSpace(spaces.ReplaceAllString(bracketed.ReplaceAllString(s, ""), " "))
	for _, re := range []*regexp.Regexp{comicHash, comicIssue, comicVolume, comicNumber} {
		if m := re.FindStringSubmatch(s); m != nil {
			whole, frac, _ := strings.Cut(m[2], ".")
			if whole = strings.TrimLeft(whole, "0"); whole == "" {
				whole = "0"
			}
			position = whole
			if frac != "" {
				position += "." + frac
			}
			return strings.TrimSpace(strings.TrimRight(m[1], " -–_.,:")), position
		}
	}
	return "", ""
}

type scannedFile struct {
	path, ext string
}

// ScanEbooks lists the ebooks under root: one per folder and title, in the
// best format there is. Comics are the exception: in a folder of several
// comic files (one per issue, the usual layout) each file is its own book.
func ScanEbooks(root string) ([]LocalBook, error) {
	var files []scannedFile
	comics := map[string]int{} // comic files per folder
	err := walkFollowing(root, func(path string, d fs.DirEntry, err error) error {
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
		if ScanRank(ext) == 0 || hidden(d.Name()) {
			return nil
		}
		files = append(files, scannedFile{path, ext})
		if ComicFormat(ext) {
			comics[filepath.Dir(path)]++
		}
		return nil
	})
	best := map[string]LocalBook{} // folder + title
	for _, f := range files {
		path, ext := f.path, f.ext
		parts := relParts(root, strings.TrimSuffix(path, filepath.Ext(path)))
		author, title, year := Guess(parts, true)
		var series, position string
		if ComicFormat(ext) && comics[filepath.Dir(path)] > 1 && len(parts) >= 2 {
			// One issue of many: the file names the book, the folder the series.
			if _, t, y := Guess([]string{parts[0], parts[len(parts)-1]}, false); t != "" {
				series, position = GuessComicIssue(parts[len(parts)-1])
				if folder, _ := cleanName(parts[len(parts)-2]); series == "" && position != "" && folder != "" {
					// Just a number ("001.cbr"): the folder is the series.
					series, t = folder, folder+" #"+position
				}
				title = t
				if y > 0 {
					year = y
				}
			}
		}
		if s, p, ok := inSeriesFolder(parts); ok && series == "" && !ComicFormat(ext) {
			series, position = s, p
		}
		if ext == "cbz" {
			// A comic says what it is: the series, the number, who wrote it.
			if ci, ok := ReadComicInfo(path); ok {
				if name := ci.DisplayTitle(); name != "" {
					title = name
				}
				if ci.Writer != "" {
					author = strings.TrimSpace(strings.Split(ci.Writer, ",")[0])
				}
				if ci.Year > 0 {
					year = ci.Year
				}
				if ci.Series != "" || ci.Number != "" {
					series, position = ci.Series, ci.Number
				}
			}
		}
		if title == "" {
			continue
		}
		key := filepath.Dir(path) + "|" + strings.ToLower(title)
		if cur, ok := best[key]; ok {
			cur.Files++
			if ScanRank(ext) > ScanRank(cur.Format) {
				cur.Path, cur.Format = path, ext
			}
			best[key] = cur
			continue
		}
		best[key] = LocalBook{Path: path, Author: author, Title: title, Year: year, Format: ext, Files: 1, Series: series, Position: position}
	}
	return sorted(best), err
}

// ScanAudiobooks lists the audiobooks under root: every folder that holds
// audio files is one book, with "CD 1", "Disc 2" and "Part 3" folders counted
// as part of the folder above them.
func ScanAudiobooks(root string) ([]LocalBook, error) {
	found := map[string]LocalBook{}
	err := walkFollowing(root, func(path string, d fs.DirEntry, err error) error {
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
//
// A number counts the same as the number written out, so "1984" is the same
// title as "Nineteen Eighty-Four".
func SameTitle(local, catalogue string, exact bool) bool {
	lw, cw := words(local), words(catalogue)
	if sameTitleWords(lw, cw, exact) {
		return true
	}
	ls, lok := spelledNumbers(lw)
	cs, cok := spelledNumbers(cw)
	return (lok || cok) && sameTitleWords(ls, cs, exact)
}

func sameTitleWords(local, catalogue []string, exact bool) bool {
	sig := func(ws []string) map[string]bool {
		out := map[string]bool{}
		for _, w := range ws {
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
