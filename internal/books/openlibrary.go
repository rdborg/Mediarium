package books

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Open Library (openlibrary.org) is a free, open catalogue of books run by
// the Internet Archive. It needs no account or key. Mediarium uses it to
// find books, their authors, first publication year, cover and description.

const openLibraryBase = "https://openlibrary.org"

// CoverURL is the address of a book cover by its Open Library cover id
// (size "S", "M" or "L"), or "" for none.
func CoverURL(coverID int, size string) string {
	if coverID <= 0 {
		return ""
	}
	return fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-%s.jpg", coverID, size)
}

// Client talks to Open Library.
type Client struct {
	Base      string // https://openlibrary.org; tests point it elsewhere
	UserAgent string
	HTTP      *http.Client

	mu     sync.Mutex
	lists  map[string]cachedList // Discover lists, kept for an hour
	works  map[string]cachedWork // book info pages, kept for an hour
	series seriesCache           // series answers, kept for an hour
}

type cachedList struct {
	until time.Time
	found []Found
}

// listTTL is how long a Discover list is kept before Open Library is asked again.
const listTTL = time.Hour

// cachedFound answers key from the cache, or calls fetch and keeps its answer.
func (c *Client) cachedFound(key string, fetch func() ([]Found, error)) ([]Found, error) {
	c.mu.Lock()
	if l, ok := c.lists[key]; ok && time.Now().Before(l.until) {
		c.mu.Unlock()
		return l.found, nil
	}
	c.mu.Unlock()
	found, err := fetch()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lists == nil || len(c.lists) > 500 {
		c.lists = map[string]cachedList{}
	}
	c.lists[key] = cachedList{until: time.Now().Add(listTTL), found: found}
	return found, nil
}

// NewClient returns a client for the real Open Library.
func NewClient(userAgent string) *Client {
	return &Client{Base: openLibraryBase, UserAgent: userAgent, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// Found is one search result.
type Found struct {
	Key       string `json:"key"` // work key, "OL45883W"
	Title     string `json:"title"`
	Author    string `json:"author"`
	AuthorKey string `json:"authorKey"`
	Year      int    `json:"year"`
	CoverID   int    `json:"coverId"`
	// HasEbook and HasAudio say whether an ebook or an audiobook edition of
	// the book has been published (from Open Library's edition formats).
	HasEbook bool `json:"hasEbook"`
	HasAudio bool `json:"hasAudio"`
}

func (c *Client) get(ctx context.Context, path string, query url.Values, into any) error {
	u := strings.TrimRight(c.Base, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("open library: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("open library answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("open library: read the answer: %w", err)
	}
	return nil
}

// ErrNotFound is returned for a book Open Library does not have.
var ErrNotFound = fmt.Errorf("open library has no such book")

// Search finds books by title, author or both.
func (c *Client) Search(ctx context.Context, q string, limit int) ([]Found, error) {
	if limit <= 0 || limit > 40 {
		limit = 20
	}
	var res struct {
		Docs []olDoc `json:"docs"`
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return []Found{}, nil
	}
	if err := c.get(ctx, "/search.json", url.Values{
		"q":      {q},
		"fields": {docFields},
		"limit":  {fmt.Sprint(limit)},
	}, &res); err != nil {
		return nil, err
	}
	return founds(res.Docs, false), nil
}

// Work is a book's details.
type Work struct {
	Key         string
	Title       string
	Description string
	CoverID     int
	AuthorKey   string
}

// GetWork returns a book's details by its work key.
func (c *Client) GetWork(ctx context.Context, key string) (Work, error) {
	key = strings.TrimPrefix(strings.TrimSpace(key), "/works/")
	if key == "" || strings.ContainsAny(key, "/?#") {
		return Work{}, ErrNotFound
	}
	var res struct {
		Title       string          `json:"title"`
		Description json.RawMessage `json:"description"`
		Covers      []int           `json:"covers"`
		Authors     []struct {
			Author struct {
				Key string `json:"key"`
			} `json:"author"`
		} `json:"authors"`
	}
	if err := c.get(ctx, "/works/"+url.PathEscape(key)+".json", nil, &res); err != nil {
		return Work{}, err
	}
	w := Work{Key: key, Title: res.Title, Description: textOf(res.Description)}
	for _, cv := range res.Covers {
		if cv > 0 {
			w.CoverID = cv
			break
		}
	}
	if len(res.Authors) > 0 {
		w.AuthorKey = strings.TrimPrefix(res.Authors[0].Author.Key, "/authors/")
	}
	return w, nil
}

// textOf reads a description that is either a string or {"value": "..."}.
func textOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return CleanDescription(s)
	}
	var v struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &v) == nil {
		return CleanDescription(v.Value)
	}
	return ""
}

var (
	refLink   = regexp.MustCompile(`\[([^\]]+)\]\[\d+\]`)              // [text][1]
	refTarget = regexp.MustCompile(`(?m)^\s*\[\d+\]:.*$`)              // [1]: https://...
	mdLink    = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)]+)\)`) // [text](https://...)
	blankRuns = regexp.MustCompile(`\n{3,}`)
)

// CleanDescription turns an Open Library description into plain text: the
// "----------" footer (lists of other editions, sources) is cut, Markdown
// links keep only their text, and "(Source)" style notes at the end go.
func CleanDescription(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if i := strings.Index(s, "\n----------"); i >= 0 {
		s = s[:i]
	}
	s = refTarget.ReplaceAllString(s, "")
	s = refLink.ReplaceAllString(s, "$1")
	s = mdLink.ReplaceAllString(s, "$1")
	s = strings.TrimSpace(s)
	for _, tail := range []string{"([source])", "(source)", "([Source])", "(Source)"} {
		s = strings.TrimSpace(strings.TrimSuffix(s, tail))
	}
	return blankRuns.ReplaceAllString(s, "\n\n")
}
