package musicbrainz

import (
	"context"
	"regexp"
	"strings"
	"unicode"
)

// The searches behind the one music search box (#143). A typed line such as
// "Gul Anuv Jain" holds words of a song and of its artist, so every word has
// to be found in the title or in the artist, not all of them in the title. When
// that finds almost nothing (a misspelt word), a looser search with near
// spellings runs too and the answers are merged.

// minStrictHits is how many answers a strict search must give before the
// looser one is skipped.
const minStrictHits = 5

// dottedLetters is an acronym written with dots, like S.T.A.Y. or R.E.M.
var dottedLetters = regexp.MustCompile(`(?:\p{L}\.){2,}\p{L}?`)

// queryWords are the words of a typed line that go into a search query:
// letters and digits only, so nothing needs escaping. The music database
// reads S.T.A.Y. as one word, so the dots of an acronym are dropped first
// instead of leaving four one-letter words that match almost anything.
func queryWords(s string) []string {
	s = dottedLetters.ReplaceAllStringFunc(s, func(m string) string { return strings.ReplaceAll(m, ".", "") })
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// strictQuery asks for every word to be in the title field or in the artist.
func strictQuery(field, text string) string {
	words := queryWords(text)
	if len(words) == 0 {
		return field + ":(" + luceneEscape(text) + ")"
	}
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = "(" + field + ":" + w + " OR artist:" + w + ")"
	}
	return strings.Join(parts, " AND ")
}

// looseQuery asks for any word, or one close to it, in the title or artist.
func looseQuery(field, text string) string {
	words := queryWords(text)
	if len(words) == 0 {
		return field + ":(" + luceneEscape(text) + ")"
	}
	parts := make([]string, 0, 2*len(words))
	for _, w := range words {
		if len([]rune(w)) >= 4 {
			w += "~"
		}
		parts = append(parts, field+":"+w, "artist:"+w)
	}
	return strings.Join(parts, " OR ")
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 25
	}
	return limit
}

// SearchAlbumsAny finds albums, EPs and singles whose title or artist holds
// the typed words.
func (c *Client) SearchAlbumsAny(ctx context.Context, text string, limit int) ([]FoundAlbum, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []FoundAlbum{}, nil
	}
	limit = clampLimit(limit)
	found, err := c.searchReleaseGroups(ctx, strictQuery("releasegroup", text), limit)
	if err != nil || len(found) >= minStrictHits {
		return found, err
	}
	more, lerr := c.searchReleaseGroups(ctx, looseQuery("releasegroup", text), limit)
	if lerr != nil {
		if len(found) > 0 {
			return found, nil
		}
		return nil, lerr
	}
	return mergeFound(found, more, limit), nil
}

// SearchSongsAny finds songs whose title or artist holds the typed words,
// with the albums they are on.
func (c *Client) SearchSongsAny(ctx context.Context, text string, limit int) ([]FoundAlbum, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []FoundAlbum{}, nil
	}
	limit = clampLimit(limit)
	found, err := c.searchRecordings(ctx, strictQuery("recording", text), limit)
	if err != nil || len(found) >= minStrictHits {
		return found, err
	}
	more, lerr := c.searchRecordings(ctx, looseQuery("recording", text), limit)
	if lerr != nil {
		if len(found) > 0 {
			return found, nil
		}
		return nil, lerr
	}
	return mergeFound(found, more, limit*2), nil
}

// SearchArtistsAny is SearchArtists with a looser second search when the first
// finds little (a misspelt name).
func (c *Client) SearchArtistsAny(ctx context.Context, text string, limit int) ([]Artist, error) {
	found, err := c.SearchArtists(ctx, text, limit)
	if err != nil || len(found) >= minStrictHits {
		return found, err
	}
	words := queryWords(text)
	if len(words) == 0 {
		return found, nil
	}
	parts := make([]string, len(words))
	for i, w := range words {
		if len([]rune(w)) >= 4 {
			w += "~"
		}
		parts[i] = "artist:" + w
	}
	more, lerr := c.searchArtistsQuery(ctx, strings.Join(parts, " OR "), clampLimit(limit))
	if lerr != nil {
		return found, nil // what the first search found is still good
	}
	seen := map[string]bool{}
	for _, a := range found {
		seen[a.ID] = true
	}
	for _, a := range more {
		if !seen[a.ID] {
			seen[a.ID] = true
			found = append(found, a)
		}
	}
	return found, nil
}

// mergeFound appends what the second list has that the first lacks. A song is
// the same entry when its album and title are; an album when its id is.
func mergeFound(first, second []FoundAlbum, limit int) []FoundAlbum {
	seen := map[string]bool{}
	for _, f := range first {
		seen[f.ID+"|"+f.Song] = true
	}
	for _, f := range second {
		k := f.ID + "|" + f.Song
		if seen[k] || len(first) >= limit {
			continue
		}
		seen[k] = true
		first = append(first, f)
	}
	return first
}
