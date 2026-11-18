package books

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Matching releases to books, and finding the book's files in a finished
// download.

// Letters and digits of any script make words: a Russian or Japanese title is
// not an empty one.
var nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// words splits a title into lower-case words. Accented letters are folded to
// their plain form first, so a release named "Le Pere Goriot" still matches
// the book "Le Père Goriot" (and the other way round). Apostrophes join the
// letters around them, the curly ones Open Library often has too, so
// "L'Étranger" and "L’étranger" are the same word.
var apostrophes = strings.NewReplacer("'", "", "’", "", "‘", "", "ʼ", "", "`", "", "´", "")

func words(s string) []string {
	s = foldAccents(strings.ToLower(apostrophes.Replace(s)))
	return strings.Fields(nonWord.ReplaceAllString(s, " "))
}

// foldAccents strips diacritics (é becomes e, ö becomes o, and so on) so
// release names written without them still match.
func foldAccents(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) { // combining diacritical mark
			return -1
		}
		return r
	}, norm.NFD.String(s))
}

// seriesNote is a bracketed series entry, as Calibre and Goodreads write it:
// "(Foundation #1)", "[Dune Chronicles #2.5]".
var seriesNote = regexp.MustCompile(`[(\[][^()\[\]]*#\s*\d+(?:\.\d+)?[^()\[\]]*[)\]]`)

// foreignArticle are the leading words of titles in other languages that
// release names also leave out, like an English "The".
var foreignArticle = map[string]bool{"le": true, "la": true, "les": true, "el": true, "los": true, "las": true, "die": true, "der": true,
	"das": true, "il": true, "lo": true, "gli": true, "het": true, "een": true, "um": true, "uma": true}

var small = map[string]bool{"the": true, "a": true, "an": true, "of": true, "and": true, "to": true, "in": true, "on": true, "for": true}

// Matches reports whether a release name is about book b: the title's words
// appear together, not as part of a longer title ("It" is not "If It
// Bleeds"), and the author's surname is in it too (when known).
func Matches(release string, b Book) bool {
	// "(Dune Chronicles #1)": the series a catalogue lists a book under is not
	// part of its title.
	rel := words(seriesNote.ReplaceAllString(release, " "))
	have := map[string]bool{}
	for _, w := range rel {
		have[w] = true
	}
	if a := words(b.Author); len(a) > 0 && !have[a[len(a)-1]] {
		return false
	}
	title := words(b.Title)
	important := 0
	for _, w := range title {
		if !small[w] {
			important++
		}
	}
	if important == 0 {
		return false
	}
	author := map[string]bool{}
	for _, w := range words(b.Author) {
		author[w] = true
	}
	// A leading "The" or "A" is often left out of release names.
	for len(title) > 0 {
		if titleRunAt(rel, title, author) {
			return true
		}
		if !small[title[0]] && !(foreignArticle[title[0]] && len(title) > 1) {
			return false
		}
		title = title[1:]
	}
	return false
}

// titleRunAt reports whether title appears in rel as a run of words with only
// the author's name, a number (a year, a series number) or a tag such as the
// format next to it.
func titleRunAt(rel, title []string, author map[string]bool) bool {
	edge := func(w string) bool {
		return author[w] || noiseWord[w] || EbookRank(w) > 0 || AudioRank(w) > 0 || isNumber(w) || isOrdinal(w)
	}
	for i := 0; i+len(title) <= len(rel); i++ {
		run := true
		for k, w := range title {
			if rel[i+k] != w {
				run = false
				break
			}
		}
		if !run {
			continue
		}
		before := i == 0 || edge(rel[i-1])
		end := i + len(title)
		after := end == len(rel) || edge(rel[end])
		// "Dune 02 - Dune Messiah": a series number with another title after it
		// is the next book of the series, not this one.
		if after && end+1 < len(rel) && isNumber(rel[end]) && len(rel[end]) < 4 && !edge(rel[end+1]) {
			after = false
		}
		if before && after {
			return true
		}
	}
	return false
}

// Words that sit next to a title in release names without being part of it.
var noiseWord = map[string]bool{
	"by": true, "retail": true, "ebook": true, "ebooks": true, "audiobook": true, "audiobooks": true,
	"unabridged": true, "abridged": true, "read": true, "narrated": true, "web": true, "en": true,
	"eng": true, "english": true, "kbps": true, "illustrated": true, "edition": true, "repack": true,
	"proper": true, "fixed": true, "book": true, "novel": true, "audio": true, "aac": true, "x264": true,
	"anniversary": true, "deluxe": true, "special": true, "revised": true, "annotated": true, "collectors": true,
	// Formats that are not ranked as ebook or audio files, but name a release.
	"fb2": true, "azw": true, "kfx": true, "lit": true, "rtf": true, "txt": true, "doc": true, "docx": true,
	"djvu": true, "cbz": true, "cbr": true, "cb7": true, "zip": true, "rar": true, "digital": true, "ocr": true,
}

// isOrdinal reports whether w is a number with its ending: "50th", "1st".
func isOrdinal(w string) bool {
	for _, end := range []string{"st", "nd", "rd", "th"} {
		if strings.HasSuffix(w, end) && isNumber(strings.TrimSuffix(w, end)) {
			return true
		}
	}
	return false
}

func isNumber(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Ebook file formats, best first.
var ebookRank = map[string]int{"epub": 4, "azw3": 3, "mobi": 2, "pdf": 1}

// Audiobook file formats, best first.
var audioRank = map[string]int{"m4b": 4, "m4a": 3, "mp3": 2, "flac": 2, "ogg": 1, "opus": 1}

// EbookRank is how good an ebook file format is (0 = not an ebook).
func EbookRank(ext string) int { return ebookRank[strings.TrimPrefix(strings.ToLower(ext), ".")] }

// AudioRank is how good an audiobook file format is (0 = not audio).
func AudioRank(ext string) int { return audioRank[strings.TrimPrefix(strings.ToLower(ext), ".")] }

// ReleaseFormat guesses the file format from a release name ("" = can't tell).
func ReleaseFormat(release string, f Format) string {
	ws := words(release)
	best, bestRank := "", 0
	for _, w := range ws {
		r := EbookRank(w)
		if f == Audiobook {
			r = AudioRank(w)
		}
		if r > bestRank {
			best, bestRank = w, r
		}
	}
	return best
}

// ReleaseRank orders releases for a format: a known good file format first.
// It is 0 for a release that names only formats of the other kind (an
// audiobook release offered for an ebook), which is not picked.
func ReleaseRank(release string, f Format) int {
	ws := words(release)
	other := false
	for _, w := range ws {
		if f == Ebook && AudioRank(w) > 0 && w != "flac" {
			other = true
		}
		if f == Audiobook && EbookRank(w) > 0 {
			other = true
		}
	}
	if fmt := ReleaseFormat(release, f); fmt != "" {
		if f == Ebook {
			return 10 + EbookRank(fmt)
		}
		return 10 + AudioRank(fmt)
	}
	if other {
		return 0
	}
	return 5 // nothing said about the format: acceptable, after the ones that say
}

// FindOption changes what EbookFile and AudioFiles count.
type FindOption int

// FollowLinks counts a symbolic link as the file it leads to. Only for a
// download Mediarium laid out itself as links to an external download
// client's files; the import decides about each link.
const FollowLinks FindOption = 1

// fileInfo returns what a walked entry is as a file, or nil when it isn't a
// plain file (or a link to one, when links are followed).
func fileInfo(p string, d fs.DirEntry, opts []FindOption) fs.FileInfo {
	if d.IsDir() || strings.HasPrefix(d.Name(), ".") {
		return nil
	}
	if d.Type().IsRegular() {
		info, err := d.Info()
		if err != nil {
			return nil
		}
		return info
	}
	if d.Type()&fs.ModeSymlink == 0 || !slices.Contains(opts, FollowLinks) {
		return nil
	}
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	return info
}

// EbookFile finds the best ebook file in dir: the best format, then the
// largest. It returns "" when there is none.
func EbookFile(dir string, opts ...FindOption) (path, format string) {
	best, bestRank, bestSize := "", 0, int64(-1)
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info := fileInfo(p, d, opts)
		if info == nil {
			return nil
		}
		r := EbookRank(filepath.Ext(p))
		if r == 0 {
			return nil
		}
		if r > bestRank || (r == bestRank && info.Size() > bestSize) {
			best, bestRank, bestSize = p, r, info.Size()
		}
		return nil
	})
	if best == "" {
		return "", ""
	}
	return best, strings.TrimPrefix(strings.ToLower(filepath.Ext(best)), ".")
}

// AudioFiles lists the audiobook's files in dir: every file of the best audio
// format found, in name order (chapters stay in order). It returns nil when
// there are none.
func AudioFiles(dir string, opts ...FindOption) (files []string, format string) {
	byFormat := map[string][]string{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || fileInfo(p, d, opts) == nil {
			return nil
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
		if AudioRank(ext) > 0 {
			byFormat[ext] = append(byFormat[ext], p)
		}
		return nil
	})
	bestRank := 0
	for ext, list := range byFormat {
		if r := AudioRank(ext); r > bestRank || (r == bestRank && len(list) > len(files)) {
			files, format, bestRank = list, ext, r
		}
	}
	sort.Strings(files)
	return files, format
}

// FolderName is the book's folder: "Title (Year)".
func FolderName(b Book) string { return b.Name() }

// UniqueFolder is folder (where a book's files go), unless another book of the
// library already has its files there: two works of one title, author and year
// (an illustrated and a plain edition catalogued apart, two "Collected Poems").
// Their files would meet in one folder. The one that comes second gets its Open
// Library key after the name. A folder is only moved aside from when another
// book holds it, never the book that holds it.
func UniqueFolder(folder string, b Book, f Format, all []Book) string {
	held := func(p string) bool {
		for _, o := range all {
			if o.ID != b.ID && o.Path(f) != "" && strings.EqualFold(filepath.Clean(o.Path(f)), filepath.Clean(p)) {
				return true
			}
		}
		return false
	}
	if !held(folder) {
		return folder
	}
	key := strings.Map(func(r rune) rune {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return r
		}
		return -1
	}, b.OLKey)
	if key == "" {
		key = fmt.Sprint(b.ID)
	}
	return folder + " [" + key + "]"
}
