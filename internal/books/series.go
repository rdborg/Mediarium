package books

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Book series: the series a book belongs to and the other books in it, from
// Open Library's series fields (no account needed) or from Hardcover when a
// token is set (see hardcover.go), and the series a library follows.

// Where a series came from.
const (
	SourceOpenLibrary = "openlibrary"
	SourceHardcover   = "hardcover"
)

// SeriesEntry is one book in a series.
type SeriesEntry struct {
	Key         string `json:"key,omitempty"` // Open Library work key; "" when only Hardcover knows the book
	Title       string `json:"title"`
	Author      string `json:"author"`
	AuthorKey   string `json:"authorKey,omitempty"`
	Year        int    `json:"year,omitempty"`
	CoverID     int    `json:"-"`
	Position    string `json:"position"`              // "1", "2"
	ReleaseDate string `json:"releaseDate,omitempty"` // "2026-11-04" when known
	HasEbook    bool   `json:"hasEbook"`
	HasAudio    bool   `json:"hasAudio"`

	popularity int // Open Library edition count: picks between two books at the same place
}

// Series is a series and its books, in reading order.
type Series struct {
	Source  string        `json:"source"`
	Key     string        `json:"key"` // Open Library "OL326110L" or Hardcover's id
	Name    string        `json:"name"`
	Entries []SeriesEntry `json:"entries"`
}

var seriesKeyPattern = regexp.MustCompile(`^OL\d+L$`)

// ValidSeriesKey reports whether key can be a series of source.
func ValidSeriesKey(source, key string) bool {
	switch source {
	case SourceOpenLibrary:
		return seriesKeyPattern.MatchString(key)
	case SourceHardcover:
		n, err := strconv.Atoi(key)
		return err == nil && n > 0
	}
	return false
}

// PositionNumber reads a place in a series: "2", "2.5", "#3", "Book 4".
// Ranges ("1-7") and words are not a place.
func PositionNumber(p string) (float64, bool) {
	p = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p), "#"))
	p = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(p), "book"))
	if p == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(p, 64)
	if err != nil || n < 0 || n > 10000 {
		return 0, false
	}
	return n, true
}

// MainPosition reports whether p is a whole place in the series ("3"),
// rather than a novella in between ("2.5"), a range ("1-7") or nothing.
func MainPosition(p string) bool {
	n, ok := PositionNumber(p)
	return ok && n == float64(int(n))
}

// tidyEntries puts entries in reading order and keeps one book per place:
// the most popular, so a translation or a reissue under another title doesn't
// push out the book everyone knows. Box sets and entries without a place
// are left out.
func tidyEntries(in []SeriesEntry) []SeriesEntry {
	best := map[string]SeriesEntry{}
	for _, e := range in {
		n, ok := PositionNumber(e.Position)
		if !ok || LooksLikeSet(e.Title) || e.Title == "" {
			continue
		}
		place := strconv.FormatFloat(n, 'f', -1, 64)
		e.Position = place
		if cur, ok := best[place]; !ok || e.popularity > cur.popularity || (e.popularity == cur.popularity && cur.CoverID == 0 && e.CoverID != 0) {
			best[place] = e
		}
	}
	out := make([]SeriesEntry, 0, len(best))
	for _, e := range best {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := PositionNumber(out[i].Position)
		b, _ := PositionNumber(out[j].Position)
		return a < b
	})
	return out
}

// ---- Open Library

type olSeriesDoc struct {
	olDoc
	Editions       int      `json:"edition_count"`
	SeriesKey      []string `json:"series_key"`
	SeriesName     []string `json:"series_name"`
	SeriesPosition []string `json:"series_position"`
}

const seriesFields = docFields + ",edition_count,series_key,series_name,series_position"

type cachedSeries struct {
	until  time.Time
	series *Series
}

// seriesCache keeps series answers for an hour, like the Discover lists.
type seriesCache struct {
	mu sync.Mutex
	m  map[string]cachedSeries
}

func (c *seriesCache) get(key string) (*Series, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	if !ok || time.Now().After(v.until) {
		return nil, false
	}
	return v.series, true
}

func (c *seriesCache) put(key string, s *Series) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) > 500 {
		c.m = map[string]cachedSeries{}
	}
	c.m[key] = cachedSeries{until: time.Now().Add(listTTL), series: s}
}

// SeriesOf finds the series a book (by its work key) belongs to on Open
// Library, with all its books. It answers nil when the book is in no series.
func (c *Client) SeriesOf(ctx context.Context, workKey string) (*Series, error) {
	workKey = strings.TrimPrefix(strings.TrimSpace(workKey), "/works/")
	if workKey == "" || strings.ContainsAny(workKey, "/?#: ") {
		return nil, ErrNotFound
	}
	cacheKey := "of|" + workKey
	if s, ok := c.series.get(cacheKey); ok {
		return s, nil
	}
	var res struct {
		Docs []olSeriesDoc `json:"docs"`
	}
	if err := c.get(ctx, "/search.json", url.Values{"q": {"key:/works/" + workKey}, "fields": {seriesFields}, "limit": {"1"}}, &res); err != nil {
		return nil, err
	}
	var out *Series
	if len(res.Docs) > 0 && len(res.Docs[0].SeriesKey) > 0 {
		d := res.Docs[0]
		name := ""
		if len(d.SeriesName) > 0 {
			name = d.SeriesName[0]
		}
		s, err := c.SeriesBooks(ctx, d.SeriesKey[0], name)
		if err != nil {
			return nil, err
		}
		out = s
	}
	c.series.put(cacheKey, out)
	return out, nil
}

// SeriesBooks lists the books of an Open Library series, in reading order.
func (c *Client) SeriesBooks(ctx context.Context, seriesKey, name string) (*Series, error) {
	if !seriesKeyPattern.MatchString(seriesKey) {
		return nil, ErrNotFound
	}
	cacheKey := "books|" + seriesKey
	if s, ok := c.series.get(cacheKey); ok {
		return s, nil
	}
	var res struct {
		Docs []olSeriesDoc `json:"docs"`
	}
	if err := c.get(ctx, "/search.json", url.Values{"q": {"series_key:" + seriesKey}, "fields": {seriesFields}, "limit": {"100"}}, &res); err != nil {
		return nil, err
	}
	s := &Series{Source: SourceOpenLibrary, Key: seriesKey, Name: name}
	var entries []SeriesEntry
	for _, d := range res.Docs {
		idx := -1
		for i, k := range d.SeriesKey {
			if k == seriesKey {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		if s.Name == "" && idx < len(d.SeriesName) {
			s.Name = d.SeriesName[idx]
		}
		pos := ""
		if idx < len(d.SeriesPosition) {
			pos = d.SeriesPosition[idx]
		}
		f := founds([]olDoc{d.olDoc}, false)
		if len(f) == 0 {
			continue
		}
		entries = append(entries, SeriesEntry{Key: f[0].Key, Title: f[0].Title, Author: f[0].Author, AuthorKey: f[0].AuthorKey, Year: f[0].Year,
			CoverID: f[0].CoverID, Position: pos, HasEbook: f[0].HasEbook, HasAudio: f[0].HasAudio, popularity: d.Editions})
	}
	s.Entries = tidyEntries(entries)
	if s.Name == "" {
		s.Name = "Series"
	}
	c.series.put(cacheKey, s)
	return s, nil
}

// FindOnOpenLibrary finds the Open Library book for a title and author (from
// another source), or ErrNotFound.
func (c *Client) FindOnOpenLibrary(ctx context.Context, title, author string) (Found, error) {
	found, err := c.Search(ctx, strings.TrimSpace(title+" "+author), 8)
	if err != nil {
		return Found{}, err
	}
	for _, f := range found {
		if SameTitle(f.Title, title, true) && (author == "" || SameAuthor(f.Author, author)) {
			return f, nil
		}
	}
	return Found{}, ErrNotFound
}

// ---- Followed series

// FollowedSeries is a series whose missing and new books are added by
// themselves.
type FollowedSeries struct {
	Source        string `json:"source"`
	Key           string `json:"key"`
	Name          string `json:"name"`
	WantEbook     bool   `json:"ebook"`
	WantAudiobook bool   `json:"audiobook"`
	FollowedAt    string `json:"followedAt"`
}

// FollowSeries follows a series, or changes the formats wanted.
func (r *Repo) FollowSeries(f FollowedSeries) error {
	_, err := r.db.Exec(`INSERT INTO book_series (source, series_key, name, want_ebook, want_audiobook) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (source, series_key) DO UPDATE SET name = excluded.name, want_ebook = excluded.want_ebook, want_audiobook = excluded.want_audiobook`,
		f.Source, f.Key, f.Name, f.WantEbook, f.WantAudiobook)
	if err != nil {
		return fmt.Errorf("follow series: %w", err)
	}
	return nil
}

// UnfollowSeries stops following a series. Its books stay.
func (r *Repo) UnfollowSeries(source, key string) error {
	if _, err := r.db.Exec(`DELETE FROM book_series WHERE source = ? AND series_key = ?`, source, key); err != nil {
		return fmt.Errorf("unfollow series: %w", err)
	}
	return nil
}

// ListFollowedSeries lists the series followed, by name.
func (r *Repo) ListFollowedSeries() ([]FollowedSeries, error) {
	rows, err := r.db.Query(`SELECT source, series_key, name, want_ebook, want_audiobook, followed_at FROM book_series ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list followed series: %w", err)
	}
	defer rows.Close()
	out := []FollowedSeries{}
	for rows.Next() {
		var f FollowedSeries
		if err := rows.Scan(&f.Source, &f.Key, &f.Name, &f.WantEbook, &f.WantAudiobook, &f.FollowedAt); err != nil {
			return nil, fmt.Errorf("scan followed series: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetSeries records a book's series and place in it.
func (r *Repo) SetSeries(id int64, name, position string) error {
	if _, err := r.db.Exec(`UPDATE books SET series_name = ?, series_position = ? WHERE id = ?`, name, position, id); err != nil {
		return fmt.Errorf("set book series: %w", err)
	}
	return nil
}

// SetReleaseDate records when a book comes out ("" = out already or unknown).
func (r *Repo) SetReleaseDate(id int64, date string) error {
	if _, err := r.db.Exec(`UPDATE books SET release_date = ? WHERE id = ?`, date, id); err != nil {
		return fmt.Errorf("set book release date: %w", err)
	}
	return nil
}
