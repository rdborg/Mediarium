package books

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Matching releases to books, and finding the book's files in a finished
// download.

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// words splits a title into lower-case words.
func words(s string) []string {
	s = strings.ToLower(strings.ReplaceAll(s, "'", ""))
	return strings.Fields(nonWord.ReplaceAllString(s, " "))
}

var small = map[string]bool{"the": true, "a": true, "an": true, "of": true, "and": true, "to": true, "in": true, "on": true, "for": true}

// Matches reports whether a release name is about book b: the title's words
// appear together, not as part of a longer title ("It" is not "If It
// Bleeds"), and the author's surname is in it too (when known).
func Matches(release string, b Book) bool {
	rel := words(release)
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
		if !small[title[0]] {
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
		return author[w] || noiseWord[w] || EbookRank(w) > 0 || AudioRank(w) > 0 || isNumber(w)
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
		after := i+len(title) == len(rel) || edge(rel[i+len(title)])
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

// EbookFile finds the best ebook file in dir: the best format, then the
// largest. It returns "" when there is none.
func EbookFile(dir string) (path, format string) {
	best, bestRank, bestSize := "", 0, int64(-1)
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		r := EbookRank(filepath.Ext(p))
		if r == 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
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
func AudioFiles(dir string) (files []string, format string) {
	byFormat := map[string][]string{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") {
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
