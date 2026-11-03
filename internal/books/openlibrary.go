package books

import (
	"context"
	"encoding/json"
	"errors"
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
	// HardcoverID is the Hardcover book this result is, or the one matched to
	// it when Open Library lists the same book. 0 when Hardcover isn't used.
	HardcoverID int `json:"hardcoverId,omitempty"`
	// GoogleID is the Google Books volume this result is, or the one matched
	// to it. Empty when Google Books isn't used.
	GoogleID string `json:"googleId,omitempty"`
	// CoverImage is Hardcover's picture for the book. The page never sees it:
	// the API serves a copy from this server, and only when CoverID is 0.
	CoverImage string `json:"-"`
	// GoogleCover is Google Books' picture for the book. Served the same way
	// as CoverImage, and only when neither Open Library nor Hardcover has one.
	GoogleCover string `json:"-"`
	// GoogleBlurb is Google Books' description, used when the other catalogues
	// have none. A book only Google Books lists keeps this as its description.
	GoogleBlurb string `json:"-"`
	// Slug is Hardcover's name for the book in its web address.
	Slug string `json:"slug,omitempty"`
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
	if resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusBadRequest {
		return fmt.Errorf("open library answered %d: %w", resp.StatusCode, ErrBadQuery)
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

// ErrBadQuery is returned when Open Library refuses a search as malformed
// (it answers 422 for text its search syntax can't read, such as an
// unbalanced bracket or quote, or a lone colon).
var ErrBadQuery = fmt.Errorf("open library could not read the search")

// maxQueryRunes caps what is sent as a search; longer text is cut at a word.
const maxQueryRunes = 200

// PlainQuery turns whatever was typed into plain words for Open Library's
// search: characters that mean something in its query syntax (quotes,
// brackets, colons, + - ! ^ ~ * ? / \ & |) become spaces, a dangling AND, OR
// or NOT at either end is dropped and the text is cut to maxQueryRunes.
func PlainQuery(q string) string {
	words := strings.Fields(strings.Map(func(r rune) rune {
		if strings.ContainsRune("+-!(){}[]^\"~*?:\\/&|<>=;", r) {
			return ' '
		}
		return r
	}, q))
	isOp := func(w string) bool { return w == "AND" || w == "OR" || w == "NOT" }
	for len(words) > 0 && isOp(words[0]) {
		words = words[1:]
	}
	for len(words) > 0 && isOp(words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	out := ""
	for _, w := range words {
		if out != "" {
			w = " " + w
		}
		if len([]rune(out+w)) > maxQueryRunes {
			break
		}
		out += w
	}
	return out
}

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
	search := func(q string) error {
		return c.get(ctx, "/search.json", url.Values{
			"q":      {q},
			"fields": {docFields},
			"limit":  {fmt.Sprint(limit)},
		}, &res)
	}
	err := search(q)
	if errors.Is(err, ErrBadQuery) {
		// What was typed isn't valid in Open Library's search syntax:
		// look for the plain words in it instead.
		plain := PlainQuery(q)
		if plain == "" {
			return []Found{}, nil
		}
		if plain != q {
			err = search(plain)
		}
	}
	if err != nil {
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
