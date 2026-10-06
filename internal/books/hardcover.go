package books

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Hardcover (hardcover.app) is a book catalogue with good series lists and
// release dates. Its API needs a personal token, so it is only used when
// someone pastes their own under Settings; without one Mediarium uses Open
// Library alone. Tokens belong to an account and are never shared or built in.

const hardcoverAPI = "https://api.hardcover.app/v1/graphql"

// ErrHardcoverToken is returned when Hardcover refuses the token.
var ErrHardcoverToken = errors.New("hardcover refused the token: check it under Settings > Info, lists and subtitles")

// Hardcover talks to the Hardcover API with one person's token.
type Hardcover struct {
	Base      string // the GraphQL address; tests point it elsewhere
	Token     string
	UserAgent string
	HTTP      *http.Client

	cache seriesCache
}

// NewHardcover returns a client for token ("" means none: every call fails).
func NewHardcover(token, userAgent string) *Hardcover {
	return &Hardcover{Base: hardcoverAPI, Token: CleanHardcoverToken(token), UserAgent: userAgent, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// CleanHardcoverToken trims a pasted token, and the "Bearer " that
// Hardcover's settings page shows in front of it.
func CleanHardcoverToken(t string) string {
	t = strings.TrimSpace(t)
	if len(t) > 7 && strings.EqualFold(t[:7], "bearer ") {
		t = strings.TrimSpace(t[7:])
	}
	return t
}

func (h *Hardcover) query(ctx context.Context, q string, vars map[string]any, into any) error {
	if h.Token == "" {
		return ErrHardcoverToken
	}
	body, err := json.Marshal(map[string]any{"query": q, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.Base, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.Token)
	if h.UserAgent != "" {
		req.Header.Set("User-Agent", h.UserAgent)
	}
	resp, err := h.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("hardcover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrHardcoverToken
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return errors.New("hardcover: too many requests for now, try again in a minute")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hardcover answered %d", resp.StatusCode)
	}
	var out struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return fmt.Errorf("hardcover: read the answer: %w", err)
	}
	if len(out.Errors) > 0 {
		msg := out.Errors[0].Message
		if strings.Contains(strings.ToLower(msg), "jwt") || strings.Contains(strings.ToLower(msg), "unauthorized") {
			return ErrHardcoverToken
		}
		return fmt.Errorf("hardcover: %s", msg)
	}
	if err := json.Unmarshal(out.Data, into); err != nil {
		return fmt.Errorf("hardcover: read the answer: %w", err)
	}
	return nil
}

// Me checks the token and answers the account's user name.
func (h *Hardcover) Me(ctx context.Context) (string, error) {
	var res struct {
		Me json.RawMessage `json:"me"`
	}
	if err := h.query(ctx, `query { me { username } }`, nil, &res); err != nil {
		return "", err
	}
	type user struct {
		Username string `json:"username"`
	}
	var list []user
	if json.Unmarshal(res.Me, &list) == nil && len(list) > 0 {
		return list[0].Username, nil
	}
	var one user
	if json.Unmarshal(res.Me, &one) == nil && one.Username != "" {
		return one.Username, nil
	}
	return "", ErrHardcoverToken
}

type hcBook struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	ReleaseDate   *string `json:"release_date"`
	Contributions []struct {
		Author *struct {
			Name string `json:"name"`
		} `json:"author"`
	} `json:"contributions"`
	BookSeries []struct {
		Position *float64 `json:"position"`
		Series   *struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"series"`
	} `json:"book_series"`
}

func (b hcBook) author() string {
	for _, c := range b.Contributions {
		if c.Author != nil && c.Author.Name != "" {
			return c.Author.Name
		}
	}
	return ""
}

func (b hcBook) release() string {
	if b.ReleaseDate == nil {
		return ""
	}
	if d := strings.TrimSpace(*b.ReleaseDate); len(d) >= 10 {
		return d[:10]
	}
	return ""
}

func formatPosition(p float64) string { return strconv.FormatFloat(p, 'f', -1, 64) }

// SeriesFor finds the series of the book with this title by this author.
// It answers nil when Hardcover knows no series for it.
func (h *Hardcover) SeriesFor(ctx context.Context, title, author string) (*Series, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil
	}
	cacheKey := "for|" + strings.ToLower(title) + "|" + strings.ToLower(author)
	if s, ok := h.cache.get(cacheKey); ok {
		return s, nil
	}
	var res struct {
		Books []hcBook `json:"books"`
	}
	err := h.query(ctx, `query FindBook($title: String!) {
  books(where: {title: {_eq: $title}}, limit: 15) {
    id title release_date
    contributions { author { name } }
    book_series { position series { id name } }
  }
}`, map[string]any{"title": title}, &res)
	if err != nil {
		return nil, err
	}
	var out *Series
	for _, b := range res.Books {
		if author != "" && !SameAuthor(b.author(), author) {
			continue
		}
		for _, bs := range b.BookSeries {
			if bs.Series == nil || bs.Position == nil || bs.Series.ID <= 0 {
				continue
			}
			s, err := h.SeriesBooks(ctx, bs.Series.ID)
			if err != nil {
				return nil, err
			}
			out = s
			break
		}
		if out != nil {
			break
		}
	}
	h.cache.put(cacheKey, out)
	return out, nil
}

// SeriesBooks lists a Hardcover series' books in reading order, with their
// release dates.
func (h *Hardcover) SeriesBooks(ctx context.Context, id int) (*Series, error) {
	cacheKey := "books|" + strconv.Itoa(id)
	if s, ok := h.cache.get(cacheKey); ok {
		return s, nil
	}
	var res struct {
		Series []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			BookSeries []struct {
				Position *float64 `json:"position"`
				Book     *hcBook  `json:"book"`
			} `json:"book_series"`
		} `json:"series"`
	}
	err := h.query(ctx, `query SeriesBooks($id: Int!) {
  series(where: {id: {_eq: $id}}, limit: 1) {
    id name
    book_series(where: {position: {_is_null: false}}, order_by: {position: asc}, limit: 100) {
      position
      book { id title release_date contributions { author { name } } }
    }
  }
}`, map[string]any{"id": id}, &res)
	if err != nil {
		return nil, err
	}
	if len(res.Series) == 0 {
		return nil, ErrNotFound
	}
	src := res.Series[0]
	s := &Series{Source: SourceHardcover, Key: strconv.Itoa(src.ID), Name: src.Name}
	var entries []SeriesEntry
	for _, bs := range src.BookSeries {
		if bs.Book == nil || bs.Position == nil {
			continue
		}
		e := SeriesEntry{Title: bs.Book.Title, Author: bs.Book.author(), Position: formatPosition(*bs.Position), ReleaseDate: bs.Book.release()}
		if len(e.ReleaseDate) >= 4 {
			e.Year, _ = strconv.Atoi(e.ReleaseDate[:4])
		}
		entries = append(entries, e)
	}
	s.Entries = tidyEntries(entries)
	if s.Name == "" {
		s.Name = "Series"
	}
	h.cache.put(cacheKey, s)
	return s, nil
}

// LinkToOpenLibrary fills in the Open Library side (work key, cover, author
// key, editions) of entries that only Hardcover knows, a few at a time.
// Entries Open Library doesn't have keep an empty key.
func (c *Client) LinkToOpenLibrary(ctx context.Context, s *Series) {
	if s == nil {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.Key != "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			found, err := c.cachedFound("find|"+strings.ToLower(e.Title)+"|"+strings.ToLower(e.Author), func() ([]Found, error) {
				f, err := c.FindOnOpenLibrary(ctx, e.Title, e.Author)
				if errors.Is(err, ErrNotFound) {
					return []Found{}, nil
				}
				if err != nil {
					return nil, err
				}
				return []Found{f}, nil
			})
			if err != nil || len(found) == 0 {
				return
			}
			f := found[0]
			e.Key, e.CoverID, e.AuthorKey, e.HasEbook, e.HasAudio = f.Key, f.CoverID, f.AuthorKey, f.HasEbook, f.HasAudio
			if e.Year == 0 {
				e.Year = f.Year
			}
		}()
	}
	wg.Wait()
}
