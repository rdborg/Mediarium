package music

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	// reEditionBracket is what, inside brackets, says the name is the same
	// album in another edition. Other brackets ("(Instrumental)", "(Live)",
	// "(Remixes)") make it another album.
	reEditionBracket = regexp.MustCompile(`(?i)deluxe|remaster|expanded|anniversary|bonus|edition|reissue|special|limited|collector|digipak|explicit|clean|mono|stereo|(?:19|20)\d\d`)
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
		"ł", "l", "đ", "d", "ð", "d", "þ", "th", "ı", "i",
	)
	// umlautSpelled is how scene names write umlauts out: "Die Aerzte".
	umlautSpelled = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue")
)

// NormalizeName reduces an artist, album or track name to lowercase letters
// and digits for comparing: accents folded, "&" read as "and", a leading
// "The" dropped and "Name, The" (a sort name) read as "Name".
func NormalizeName(s string) string { return normalizeWith(s, accentFold) }

// normalizeUmlauts is NormalizeName with umlauts written out ("ä" as "ae").
func normalizeUmlauts(s string) string {
	return normalizeWith(strings.ToLower(s), umlautSpelled)
}

// normalizeWith keeps the letters and digits of any alphabet (a name in
// Cyrillic or Japanese must not shrink to nothing), with accents dropped.
// first is applied to the lowercase name before the fold.
func normalizeWith(s string, first *strings.Replacer) string {
	s = first.Replace(strings.ToLower(strings.TrimSpace(s)))
	s = accentFold.Replace(s)
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.TrimSuffix(s, ", the")
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimPrefix(out, "the ")
	return strings.ReplaceAll(out, " ", "")
}

// sameName compares two names the way the scene writes them: as they are, or
// with umlauts written out.
func sameName(a, b string) bool {
	if na := NormalizeName(a); na != "" && na == NormalizeName(b) {
		return true
	}
	na := normalizeUmlauts(a)
	return na != "" && na == normalizeUmlauts(b)
}

// baseAlbumName drops bracketed parts and trailing edition words:
// "OK Computer (Collector's Edition)" and "OK Computer Remastered" are both
// "OK Computer". A name that would become empty is kept as it is.
func baseAlbumName(s string) string {
	b := strings.TrimSpace(reBrackets.ReplaceAllStringFunc(s, func(m string) string {
		if reEditionBracket.MatchString(m) {
			return ""
		}
		return m
	}))
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
	return na == nb || sameName(a, b) || sameName(baseAlbumName(a), baseAlbumName(b))
}

// SameArtist reports whether two artist names are the same artist.
func SameArtist(a, b string) bool {
	return sameName(a, b)
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

// maxSameNameLookups bounds how many artists with the same name have their
// albums looked at; each look can be a request to the music database.
const maxSameNameLookups = 4

// BestArtistByAlbums is BestArtist for a name that more than one artist has
// (two bands called Blue Ruin): the artist that has the albums found in the
// folder wins, and the highest search score decides between equals. hints are
// the album names found on disk; albumsOf lists the album names of the
// candidate at that index.
func BestArtistByAlbums(name string, candidates []NameCandidate, hints []string, albumsOf func(i int) []string) int {
	best := BestArtist(name, candidates)
	var same []int
	for i, c := range candidates {
		if SameArtist(name, c.Name) || SameArtist(name, c.SortName) {
			same = append(same, i)
		}
	}
	if len(same) < 2 || len(hints) == 0 {
		return best
	}
	if len(same) > maxSameNameLookups {
		same = same[:maxSameNameLookups]
	}
	pick, bestHits := best, 0
	for _, i := range same {
		titles := albumsOf(i)
		hits := 0
		for _, h := range hints {
			for _, t := range titles {
				if SameAlbum(h, t) {
					hits++
					break
				}
			}
		}
		if hits > bestHits {
			pick, bestHits = i, hits
		}
	}
	return pick
}

// AlbumCandidate is an album a folder might be (an artist's release group).
type AlbumCandidate struct {
	Title string
	Year  int
	Type  string // album, ep or single; "" when unknown
}

// BestAlbum picks the candidate that is the named album: the same name, and
// of those the one from the same year when the year is known; -1 when none
// has the name. Where an album and a single share a name and a year, the
// album wins.
func BestAlbum(title string, year int, candidates []AlbumCandidate) int {
	return BestAlbumForFiles(title, year, 0, candidates)
}

// BestAlbumForFiles is BestAlbum for a folder that holds files audio files:
// when an album, an EP and a single have the same name and year, the number of
// files says which one the folder is (a folder of ten tracks is the album, a
// folder of one or two is the single). files of 0 means not known, and an
// album is preferred.
func BestAlbumForFiles(title string, year, files int, candidates []AlbumCandidate) int {
	best, bestScore := -1, -1
	for i, c := range candidates {
		if !SameAlbum(title, c.Title) {
			continue
		}
		score := releaseTypeRank(c.Type, files)
		if NormalizeName(c.Title) == NormalizeName(title) {
			score += 10
		}
		if year > 0 && c.Year == year {
			score += 100
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	return best
}

// ReleaseTypeRank says how much an album, EP or single weighs against the
// others of the same name: higher is more of a full album.
func ReleaseTypeRank(kind string) int {
	switch strings.ToLower(kind) {
	case "album":
		return 3
	case "ep":
		return 2
	case "single":
		return 1
	}
	return 0
}

// releaseTypeRank is how well a release type fits a folder of files audio
// files, 0 to 3.
func releaseTypeRank(kind string, files int) int {
	r := ReleaseTypeRank(kind)
	switch {
	case files <= 0 || r == 0:
		return r
	case files >= 6:
		return r
	case files >= 4:
		return [...]int{0, 1, 3, 2}[r]
	}
	return 4 - r
}
