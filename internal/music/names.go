package music

import (
	"regexp"
	"strings"
)

var (
	reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	// reEdition matches the edition words that follow an album's name in a
	// release or folder name and do not change which album it is.
	reEdition  = regexp.MustCompile(`(?i)\s+(?:deluxe|super deluxe|expanded|remaster(?:ed)?|\d+(?:st|nd|rd|th) anniversary|anniversary|special|limited|collector'?s|bonus tracks?|reissue)\b.*$`)
	reBrackets = regexp.MustCompile(`\s*[(\[{][^)\]}]*[)\]}]`)
	accentFold = strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a", "æ", "ae",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o", "ø", "o", "œ", "oe",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ñ", "n", "ç", "c", "ý", "y", "ÿ", "y", "ß", "ss",
	)
)

// NormalizeName reduces an artist, album or track name to lowercase letters
// and digits for comparing: accents folded, "&" read as "and", a leading
// "The" dropped and "Name, The" (a sort name) read as "Name".
func NormalizeName(s string) string {
	s = accentFold.Replace(strings.ToLower(strings.TrimSpace(s)))
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.TrimSuffix(s, ", the")
	s = strings.TrimSpace(reNonAlnum.ReplaceAllString(s, " "))
	s = strings.TrimPrefix(s, "the ")
	return strings.ReplaceAll(s, " ", "")
}

// baseAlbumName drops bracketed parts and trailing edition words:
// "OK Computer (Collector's Edition)" and "OK Computer Remastered" are both
// "OK Computer". A name that would become empty is kept as it is.
func baseAlbumName(s string) string {
	b := strings.TrimSpace(reBrackets.ReplaceAllString(s, ""))
	b = strings.TrimSpace(reEdition.ReplaceAllString(b, ""))
	if NormalizeName(b) == "" {
		return s
	}
	return b
}

// SameAlbum reports whether two album names are the same album: equal once
// normalized, or equal once editions ("Deluxe", "(2011 Remaster)") are left
// out.
func SameAlbum(a, b string) bool {
	na, nb := NormalizeName(a), NormalizeName(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb || NormalizeName(baseAlbumName(a)) == NormalizeName(baseAlbumName(b))
}

// SameArtist reports whether two artist names are the same artist.
func SameArtist(a, b string) bool {
	na := NormalizeName(a)
	return na != "" && na == NormalizeName(b)
}

// ReleaseMatch checks that a release found on an indexer is this album by
// this artist. The reason says why not, in words for the album's activity
// log and the interactive search list.
func ReleaseMatch(r Release, artist, album string, albumYear int) (ok bool, reason string) {
	switch {
	case r.Discography && !SameAlbum(r.Album, album):
		return false, "a discography or collection, not one album"
	case !SameArtist(r.Artist, artist):
		return false, "another artist"
	case !SameAlbum(r.Album, album):
		return false, "another album"
	case r.Year > 0 && albumYear > 0 && r.Year < albumYear:
		// A later year is fine (a reissue or remaster); an earlier one is
		// another album of the same name.
		return false, "older than the album"
	}
	return true, ""
}

// NameCandidate is an artist a name might be (a MusicBrainz search result).
type NameCandidate struct {
	Name     string
	SortName string
	Score    int
}

// BestArtist picks the candidate whose name (or sort name) is the given
// name, the highest search score first; -1 when none is.
func BestArtist(name string, candidates []NameCandidate) int {
	best, bestScore := -1, -1
	for i, c := range candidates {
		if !SameArtist(name, c.Name) && !SameArtist(name, c.SortName) {
			continue
		}
		if c.Score > bestScore {
			best, bestScore = i, c.Score
		}
	}
	return best
}

// AlbumCandidate is an album a folder might be (an artist's release group).
type AlbumCandidate struct {
	Title string
	Year  int
}

// BestAlbum picks the candidate that is the named album: the same name, and
// of those the one from the same year when the year is known; -1 when none
// has the name.
func BestAlbum(title string, year int, candidates []AlbumCandidate) int {
	best := -1
	for i, c := range candidates {
		if !SameAlbum(title, c.Title) {
			continue
		}
		if year > 0 && c.Year == year {
			return i
		}
		if best < 0 || (NormalizeName(c.Title) == NormalizeName(title) && NormalizeName(candidates[best].Title) != NormalizeName(title)) {
			best = i
		}
	}
	return best
}
