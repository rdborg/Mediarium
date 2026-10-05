package books

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// WorkInfo is everything the book's info page shows for a book that may not
// be in the library yet.
type WorkInfo struct {
	Found
	Description string   `json:"description,omitempty"`
	Subjects    []string `json:"subjects"`
}

type cachedWork struct {
	until time.Time
	info  WorkInfo
}

// WorkInfo looks one book up: its details and description, its author, and
// which editions exist. Kept for an hour.
func (c *Client) WorkInfo(ctx context.Context, key string) (WorkInfo, error) {
	key = strings.TrimPrefix(strings.TrimSpace(key), "/works/")
	if key == "" || strings.ContainsAny(key, "/?#") {
		return WorkInfo{}, ErrNotFound
	}
	c.mu.Lock()
	if w, ok := c.works[key]; ok && time.Now().Before(w.until) {
		c.mu.Unlock()
		return w.info, nil
	}
	c.mu.Unlock()

	var res struct {
		Title       string          `json:"title"`
		Description json.RawMessage `json:"description"`
		Covers      []int           `json:"covers"`
		Subjects    []string        `json:"subjects"`
		Authors     []struct {
			Author struct {
				Key string `json:"key"`
			} `json:"author"`
		} `json:"authors"`
	}
	if err := c.get(ctx, "/works/"+url.PathEscape(key)+".json", nil, &res); err != nil {
		return WorkInfo{}, err
	}
	info := WorkInfo{Found: Found{Key: key, Title: res.Title}, Description: textOf(res.Description), Subjects: []string{}}
	for _, cv := range res.Covers {
		if cv > 0 {
			info.CoverID = cv
			break
		}
	}
	if len(res.Authors) > 0 {
		info.AuthorKey = strings.TrimPrefix(res.Authors[0].Author.Key, "/authors/")
	}
	for _, s := range res.Subjects {
		if len(info.Subjects) == 8 {
			break
		}
		if s = strings.TrimSpace(s); s != "" && len(s) <= 40 && !strings.Contains(strings.ToLower(s), "nyt:") {
			info.Subjects = append(info.Subjects, s)
		}
	}
	// The search index knows the author's name, the first year and the editions.
	var search struct {
		Docs []olDoc `json:"docs"`
	}
	if err := c.get(ctx, "/search.json", url.Values{"q": {"key:/works/" + key}, "fields": {docFields}, "limit": {"1"}}, &search); err == nil {
		if f := founds(search.Docs, false); len(f) > 0 {
			info.Author, info.Year, info.HasEbook, info.HasAudio = f[0].Author, f[0].Year, f[0].HasEbook, f[0].HasAudio
			if info.AuthorKey == "" {
				info.AuthorKey = f[0].AuthorKey
			}
			if info.CoverID == 0 {
				info.CoverID = f[0].CoverID
			}
		}
	}
	if info.Author == "" && info.AuthorKey != "" {
		var a struct {
			Name string `json:"name"`
		}
		if c.get(ctx, "/authors/"+url.PathEscape(info.AuthorKey)+".json", nil, &a) == nil {
			info.Author = a.Name
		}
	}
	if info.Title == "" {
		return WorkInfo{}, fmt.Errorf("open library: %w", ErrNotFound)
	}
	c.mu.Lock()
	if c.works == nil || len(c.works) > 500 {
		c.works = map[string]cachedWork{}
	}
	c.works[key] = cachedWork{until: time.Now().Add(listTTL), info: info}
	c.mu.Unlock()
	return info, nil
}
