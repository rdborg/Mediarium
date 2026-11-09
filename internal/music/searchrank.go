package music

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Ranking for the music search (#143): one list of artists, albums and songs
// found by the music database, best first. It is a pure function of the
// candidates (no network), so the order can be tested with real-looking data.
//
// A candidate's rank is how well its names match what was typed (exact, then
// prefix, then the same words in any order, then close spellings), plus the
// music database's own score, plus a push for studio albums and soundtracks
// and a pull-down for singles, compilations, live albums and bootlegs.

// SearchKind is what a search result is.
type SearchKind string

const (
	SearchArtist SearchKind = "artist"
	SearchAlbum  SearchKind = "album"
	SearchSong   SearchKind = "song"
)

// SearchCandidate is one thing the music database returned.
type SearchCandidate struct {
	Kind SearchKind
	// Name is the artist's name, the album's title or the song's title.
	Name string
	// Artist is the main artist of an album or song.
	Artist string
	// Album is the album a song was found on.
	Album string
	// Disambiguation is the database's note that tells two artists apart.
	Disambiguation string
	// Score is the database's own relevance, 0 to 100.
	Score int
	// PrimaryType and SecondaryTypes are the release group's types (Album,
	// EP, Single; Soundtrack, Compilation, Live...); for a song, those of the
	// album it was found on.
	PrimaryType    string
	SecondaryTypes []string
	// Releases is how many releases carry a song: a song on many albums is
	// a well known one.
	Releases int
	// Ref lets the caller find its own data again.
	Ref int
}

// Ranked is a candidate with the rank it earned (higher is better).
type Ranked struct {
	SearchCandidate
	Rank float64
}

// RankSearch orders the candidates for a query, best first. Candidates with
// the same rank keep the order the database gave them.
func RankSearch(query string, cands []SearchCandidate) []Ranked {
	q := searchTokens(query)
	wantsLive := false
	for _, t := range q {
		if t == "live" || t == "bootleg" || t == "unplugged" {
			wantsLive = true
		}
	}
	out := make([]Ranked, len(cands))
	for i, c := range cands {
		out[i] = Ranked{SearchCandidate: c, Rank: searchRank(q, c, wantsLive)}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank > out[j].Rank })
	return out
}

func searchRank(q []string, c SearchCandidate, wantsLive bool) float64 {
	rel := searchRelevance(q, c)
	rank := rel*70 + float64(clampInt(c.Score, 0, 100))*0.2
	switch c.Kind {
	case SearchArtist:
		rank += 6
		d := strings.ToLower(c.Disambiguation)
		if strings.Contains(d, "tribute") || strings.Contains(d, "cover") || strings.Contains(d, "karaoke") {
			rank -= 8
		}
	case SearchAlbum:
		rank += GroupPreference(c.PrimaryType, c.SecondaryTypes, c.Name, wantsLive)
		rank += soundtrackMatch(q, c)
	case SearchSong:
		rank += GroupPreference(c.PrimaryType, c.SecondaryTypes, c.Album, wantsLive) * 0.6
		rank += soundtrackMatch(q, c) * 0.5
		rank += float64(clampInt(c.Releases, 0, 10)) * 0.5
	}
	return rank
}

// soundtrackMatch is the extra push for a soundtrack whose title holds what
// was typed: a film or show name leads to its soundtrack first.
func soundtrackMatch(q []string, c SearchCandidate) float64 {
	if len(q) == 0 || !hasType(c.SecondaryTypes, "soundtrack") {
		return 0
	}
	title := c.Name
	if c.Kind == SearchSong {
		title = c.Album
	}
	have := map[string]bool{}
	for _, t := range searchTokens(title) {
		have[t] = true
	}
	for _, t := range q {
		if !have[t] {
			return 0
		}
	}
	return 14
}

// GroupPreference is how much an album, EP or single is worth as a result,
// from its types: studio albums and soundtracks up, singles down, and live
// albums, compilations, remixes and bootlegs further down (unless the query
// asked for live). The title is checked too: a bootleg often only says so there.
func GroupPreference(primary string, secondary []string, title string, wantsLive bool) float64 {
	var p float64
	switch strings.ToLower(primary) {
	case "album":
		p = 10
	case "ep":
		p = 5
	case "single":
		p = -2
	case "broadcast", "other":
		p = -6
	}
	liveish := false
	for _, s := range secondary {
		switch strings.ToLower(s) {
		case "soundtrack":
			p += 6
		case "compilation":
			p -= 8
		case "live":
			liveish = true
		case "remix", "demo", "interview":
			p -= 10
		case "dj-mix", "spokenword", "audiobook", "audio drama", "field recording":
			p -= 8
		case "mixtape/street":
			p -= 3
		}
	}
	lt := strings.ToLower(title)
	for _, w := range []string{"bootleg", "live at", "live in", "(live", "[live", "unplugged"} {
		if strings.Contains(lt, w) {
			liveish = true
		}
	}
	if liveish && !wantsLive {
		p -= 12
	}
	return p
}

func hasType(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

func clampInt(v, lo, hi int) int {
	return max(lo, min(hi, v))
}

// searchRelevance is 0 to 1: how well the names of a candidate fit the query.
func searchRelevance(q []string, c SearchCandidate) float64 {
	if len(q) == 0 {
		return 0
	}
	name := searchTokens(c.Name)
	rel := textRelevance(q, name)
	if c.Kind == SearchArtist {
		return rel
	}
	// An album title often carries an edition in brackets: it is the same
	// album as the name without it.
	if bare := searchTokens(reBrackets.ReplaceAllString(c.Name, " ")); len(bare) > 0 {
		rel = max(rel, textRelevance(q, bare))
	}
	// "Song Artist" or "Artist Song" typed together: the artist counts as
	// part of the name.
	artist := searchTokens(c.Artist)
	if len(artist) > 0 {
		joined := append(append([]string{}, name...), artist...)
		rel = max(rel, 0.97*textRelevance(q, joined))
		joined = append(append([]string{}, artist...), name...)
		rel = max(rel, 0.97*textRelevance(q, joined))
	}
	if c.Kind == SearchSong {
		// A song is also found by the album it is on (a soundtrack by its name), a little less.
		if album := searchTokens(c.Album); len(album) > 0 {
			rel = max(rel, 0.6*textRelevance(q, album))
		}
	}
	return rel
}

// textRelevance compares the typed words with the words of a name: 1 for the
// same words in the same order, up to 0.98 for a name that starts with them
// or holds them in any order, less for extra words in the name and for
// words that are only near a typed one (a misspelling).
func textRelevance(q, f []string) float64 {
	if len(q) == 0 || len(f) == 0 {
		return 0
	}
	if equalTokens(q, f) {
		return 1
	}
	used := make([]bool, len(f))
	sum := 0.0
	for _, qt := range q {
		best, at := 0.0, -1
		for i, ft := range f {
			if s := tokenSimilarity(qt, ft); s > best {
				best, at = s, i
			}
		}
		if at >= 0 {
			sum += best
			used[at] = true
		}
	}
	cov := sum / float64(len(q))
	extras := 0
	for _, u := range used {
		if !u {
			extras++
		}
	}
	rel := cov*0.9 - 0.04*float64(min(extras, 8))
	if len(f) >= len(q) && cov > 0.99 && equalTokens(q, f[:len(q)]) {
		rel += 0.06
	}
	return max(0, min(rel, 0.98))
}

func equalTokens(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// tokenSimilarity is 1 for the same word, 0.9 when the typed word begins the
// name's word (still typing), and a little less for a word one or two
// letters off (swapped, missing or wrong letters).
func tokenSimilarity(q, f string) float64 {
	if q == f {
		return 1
	}
	if len(q) >= 3 && strings.HasPrefix(f, q) {
		return 0.9
	}
	rq, rf := []rune(q), []rune(f)
	if len(rq) < 4 || len(rf) < 4 {
		return 0
	}
	d := editDistance(rq, rf)
	s := 1 - float64(d)/float64(max(len(rq), len(rf)))
	if s >= 0.74 {
		return s * 0.9
	}
	return 0
}

// editDistance is the optimal string alignment distance: insertions,
// deletions, substitutions and swapped neighbours count one each.
func editDistance(a, b []rune) int {
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}

// SearchTokens splits a name or a query into lowercase words with accents
// dropped and "&" read as "and", for comparing and for building searches.
func SearchTokens(s string) []string { return searchTokens(s) }

func searchTokens(s string) []string {
	s = accentFold.Replace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "&", " and ")
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
	words := strings.Fields(b.String())
	if len(words) > 1 && words[0] == "the" {
		words = words[1:] // "The Beatles" and "Beatles" are the same name
	}
	return words
}
