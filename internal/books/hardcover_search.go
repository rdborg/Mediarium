package books

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Book search on Hardcover. Open Library stays the catalogue a book is filed
// under. When a token is saved, search also asks Hardcover and keeps its
// picture for a book Open Library has no cover for. A book only Hardcover
// knows is filed under a key "hc-<id>" until it can be matched to Open Library.

const hardcoverKeyPrefix = "hc-"

var descriptionTags = regexp.MustCompile(`<[^>]*>`)

// HardcoverKey is the library key of a book known by its Hardcover id.
func HardcoverKey(id int) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("%s%d", hardcoverKeyPrefix, id)
}

// ParseHardcoverKey reports the id in a key from HardcoverKey.
func ParseHardcoverKey(key string) (int, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(key), hardcoverKeyPrefix)
	if !ok || rest == "" {
		return 0, false
	}
	id, err := strconv.Atoi(rest)
	if err != nil || id <= 0 || strconv.Itoa(id) != rest {
		return 0, false
	}
	return id, true
}

// HardcoverCoverURL is the address of a Hardcover cover kept on this server.
func HardcoverCoverURL(id int) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("/api/book-covers/hc-%d.jpg", id)
}

// ParseHardcoverCover reports the id in a cover file name from HardcoverCoverURL.
func ParseHardcoverCover(file string) (int, bool) {
	stem, ok := strings.CutSuffix(file, ".jpg")
	if !ok {
		return 0, false
	}
	return ParseHardcoverKey(stem)
}

// CoverAddress is the picture to show for a search result. An Open Library
// cover wins when one exists; otherwise a Hardcover picture is served locally.
func (f Found) CoverAddress(size string) string {
	if f.CoverID > 0 {
		return LocalCoverURL(f.CoverID, size)
	}
	if f.HardcoverID > 0 && f.CoverImage != "" {
		return HardcoverCoverURL(f.HardcoverID)
	}
	if f.GoogleCover != "" {
		return GoogleCoverURL(f.GoogleID)
	}
	return ""
}

// yearsTooFar reports two publication years that are both known and more than
// a year apart. A shared title and surname is not enough to call them one book.
func yearsTooFar(a, b int) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	if a < b {
		a, b = b, a
	}
	return a-b > 1
}

// MergeHardcover adds Hardcover hits to an Open Library search. A hit that
// names the same book is folded in, and its picture is kept when Open Library
// has none. The Open Library key stays, so a book already in the library is
// still recognised. A hit Open Library doesn't list is appended at the end.
func MergeHardcover(openLib, hard []Found) []Found {
	if len(hard) == 0 {
		return openLib
	}
	out := append([]Found(nil), openLib...)
	for _, h := range hard {
		if h.HardcoverID <= 0 || strings.TrimSpace(h.Title) == "" {
			continue
		}
		folded := false
		for i := range out {
			if !SameTitle(out[i].Title, h.Title, true) || !SameAuthor(out[i].Author, h.Author) || yearsTooFar(out[i].Year, h.Year) {
				continue
			}
			folded = true
			if out[i].HardcoverID == 0 {
				out[i].HardcoverID = h.HardcoverID
			}
			if out[i].CoverID == 0 && h.CoverImage != "" {
				out[i].CoverImage = h.CoverImage
			}
			if out[i].Slug == "" {
				out[i].Slug = h.Slug
			}
			if !out[i].HasEbook {
				out[i].HasEbook = h.HasEbook
			}
			if !out[i].HasAudio {
				out[i].HasAudio = h.HasAudio
			}
			break
		}
		if folded {
			continue
		}
		if h.Key == "" {
			h.Key = HardcoverKey(h.HardcoverID)
		}
		out = append(out, h)
	}
	return out
}

// Search finds books by title, author or both. The order is Hardcover's own.
func (h *Hardcover) Search(ctx context.Context, q string, limit int) ([]Found, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []Found{}, nil
	}
	if limit <= 0 || limit > 25 {
		limit = 24
	}
	var res struct {
		Search struct {
			IDs     []int           `json:"ids"`
			Results json.RawMessage `json:"results"`
		} `json:"search"`
	}
	err := h.query(ctx, `query SearchBooks($q: String!, $n: Int!) {
  search(query: $q, query_type: "Book", per_page: $n, page: 1) {
    ids
    results
  }
}`, map[string]any{"q": q, "n": limit}, &res)
	if err != nil {
		return nil, err
	}
	docs := searchDocs(res.Search.Results)
	ids := res.Search.IDs
	if len(ids) == 0 {
		for _, d := range docs {
			if d.id() > 0 {
				ids = append(ids, d.id())
			}
		}
	}
	if len(ids) == 0 {
		return []Found{}, nil
	}
	cards, err := h.booksByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := map[int]hcCard{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	flags := map[int]hcDoc{}
	for _, d := range docs {
		if d.id() > 0 {
			flags[d.id()] = d
		}
	}
	out := make([]Found, 0, len(ids))
	seen := map[int]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		card, ok := byID[id]
		if !ok {
			continue
		}
		f := card.found()
		if d, ok := flags[id]; ok {
			f.HasEbook, f.HasAudio = d.HasEbook, d.HasAudiobook
			if f.Author == "" && len(d.AuthorNames) > 0 {
				f.Author = d.AuthorNames[0]
			}
			if f.CoverImage == "" {
				f.CoverImage = imageURLFrom(d.Image)
			}
		}
		if f.Title == "" {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// Book is one Hardcover book's details for its info page, or ErrNotFound.
func (h *Hardcover) Book(ctx context.Context, id int) (WorkInfo, error) {
	if id <= 0 {
		return WorkInfo{}, ErrNotFound
	}
	cards, err := h.booksByID(ctx, []int{id})
	if err != nil {
		return WorkInfo{}, err
	}
	for _, c := range cards {
		if c.ID != id || strings.TrimSpace(c.Title) == "" {
			continue
		}
		return WorkInfo{Found: c.found(), Description: plainDescription(c.Description), Subjects: []string{}}, nil
	}
	return WorkInfo{}, ErrNotFound
}

// ImageURL is the picture address Hardcover has for a book, or ErrNotFound.
func (h *Hardcover) ImageURL(ctx context.Context, id int) (string, error) {
	info, err := h.Book(ctx, id)
	if err != nil {
		return "", err
	}
	if info.CoverImage == "" {
		return "", ErrNotFound
	}
	return info.CoverImage, nil
}

type hcCard struct {
	ID            int             `json:"id"`
	Title         string          `json:"title"`
	ReleaseYear   int             `json:"release_year"`
	ReleaseDate   string          `json:"release_date"`
	Description   string          `json:"description"`
	Slug          string          `json:"slug"`
	Image         json.RawMessage `json:"image"`
	CachedImage   json.RawMessage `json:"cached_image"`
	Contributions []struct {
		Author *struct {
			Name string `json:"name"`
		} `json:"author"`
	} `json:"contributions"`
}

func (c hcCard) found() Found {
	year := c.ReleaseYear
	if year == 0 && len(c.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(c.ReleaseDate[:4])
	}
	f := Found{Key: HardcoverKey(c.ID), Title: strings.TrimSpace(c.Title), Year: year, HardcoverID: c.ID, Slug: c.Slug}
	f.CoverImage = imageURLFrom(c.Image)
	if f.CoverImage == "" {
		f.CoverImage = imageURLFrom(c.CachedImage)
	}
	for _, contrib := range c.Contributions {
		if contrib.Author != nil && strings.TrimSpace(contrib.Author.Name) != "" {
			f.Author = strings.TrimSpace(contrib.Author.Name)
			break
		}
	}
	return f
}

func (h *Hardcover) booksByID(ctx context.Context, ids []int) ([]hcCard, error) {
	var res struct {
		Books []hcCard `json:"books"`
	}
	err := h.query(ctx, `query BookCards($ids: [Int!]!) {
  books(where: {id: {_in: $ids}}) {
    id
    title
    release_year
    release_date
    description
    slug
    image { url }
    cached_image
    contributions { author { name } }
  }
}`, map[string]any{"ids": ids}, &res)
	if err != nil {
		return nil, err
	}
	return res.Books, nil
}

type hcDoc struct {
	ID           json.RawMessage `json:"id"`
	Title        string          `json:"title"`
	AuthorNames  []string        `json:"author_names"`
	HasEbook     bool            `json:"has_ebook"`
	HasAudiobook bool            `json:"has_audiobook"`
	Image        json.RawMessage `json:"image"`
}

func (d hcDoc) id() int { return intFrom(d.ID) }

func searchDocs(raw json.RawMessage) []hcDoc {
	raw = unwrapJSON(raw)
	if len(raw) == 0 {
		return nil
	}
	var wrap struct {
		Hits []struct {
			Document hcDoc `json:"document"`
		} `json:"hits"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return nil
	}
	out := make([]hcDoc, 0, len(wrap.Hits))
	for _, hit := range wrap.Hits {
		out = append(out, hit.Document)
	}
	return out
}

// unwrapJSON accepts a JSON value or a JSON string that itself holds JSON.
// Hardcover's search field has been both.
func unwrapJSON(raw json.RawMessage) json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return json.RawMessage(s)
	}
	return raw
}

func imageURLFrom(raw json.RawMessage) string {
	raw = unwrapJSON(raw)
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return strings.TrimSpace(obj.URL)
	}
	return ""
}

func intFrom(raw json.RawMessage) int {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		n, _ = strconv.Atoi(s)
		return n
	}
	return 0
}

func plainDescription(s string) string {
	s = descriptionTags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}
