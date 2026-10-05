package mediaservers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Audiobookshelf and Kavita: the apps people read and listen to their books
// with. Mediarium tests the connection, asks them to scan the library a new
// book landed in, and finds a book on them so its page can link to it
// ("Listen in Audiobookshelf", "Read in Kavita").
//
// Audiobookshelf takes an API token as a bearer token. Kavita takes an API
// key, which is swapped for a short-lived bearer token first.

const (
	KindAudiobookshelf Kind = "audiobookshelf"
	KindKavita         Kind = "kavita"
)

// BookServer reports whether a kind of server is for books.
func (k Kind) BookServer() bool { return k == KindAudiobookshelf || k == KindKavita }

// ---- Audiobookshelf ------------------------------------------------------

type absLibrary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"` // book or podcast
	Folders   []struct {
		FullPath string `json:"fullPath"`
	} `json:"folders"`
}

func (c *Client) absLibraries(ctx context.Context, s Server) ([]Library, error) {
	var res struct {
		Libraries []absLibrary `json:"libraries"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/api/libraries", nil, nil, &res); err != nil {
		return nil, explain(s, err)
	}
	out := make([]Library, 0, len(res.Libraries))
	for _, l := range res.Libraries {
		lib := Library{ID: l.ID, Title: l.Name, Type: l.MediaType}
		for _, f := range l.Folders {
			lib.Locations = append(lib.Locations, f.FullPath)
		}
		out = append(out, lib)
	}
	return out, nil
}

func (c *Client) absTest(ctx context.Context, s Server) (TestResult, error) {
	libs, err := c.absLibraries(ctx, s)
	if err != nil {
		return TestResult{}, err
	}
	res := TestResult{ServerName: "Audiobookshelf", Libraries: libs}
	var status struct {
		App           string `json:"app"`
		ServerVersion string `json:"serverVersion"`
	}
	if c.do(ctx, s, http.MethodGet, "/status", nil, nil, &status) == nil {
		res.Version = status.ServerVersion
	}
	return res, nil
}

func (c *Client) absScan(ctx context.Context, s Server, lib Library) error {
	return explain(s, c.do(ctx, s, http.MethodPost, "/api/libraries/"+url.PathEscape(lib.ID)+"/scan", nil, nil, nil))
}

func (c *Client) absFind(ctx context.Context, s Server, title, author string) (Item, bool, error) {
	libs, err := c.absLibraries(ctx, s)
	if err != nil {
		return Item{}, false, err
	}
	for _, l := range libs {
		if l.Type != "book" {
			continue
		}
		var res struct {
			Book []struct {
				LibraryItem struct {
					ID    string `json:"id"`
					Media struct {
						Metadata struct {
							Title      string `json:"title"`
							AuthorName string `json:"authorName"`
						} `json:"metadata"`
					} `json:"media"`
				} `json:"libraryItem"`
			} `json:"book"`
		}
		q := url.Values{"q": {title}, "limit": {"10"}}
		if err := c.do(ctx, s, http.MethodGet, "/api/libraries/"+url.PathEscape(l.ID)+"/search", q, nil, &res); err != nil {
			return Item{}, false, explain(s, err)
		}
		for _, b := range res.Book {
			md := b.LibraryItem.Media.Metadata
			if sameWords(md.Title, title) && (author == "" || md.AuthorName == "" || shareSurname(md.AuthorName, author)) {
				return Item{ID: b.LibraryItem.ID, URL: s.WebURL() + "/item/" + url.PathEscape(b.LibraryItem.ID)}, true, nil
			}
		}
	}
	return Item{}, false, nil
}

// ---- Kavita --------------------------------------------------------------

type kavitaToken struct {
	token string
	until time.Time
}

var (
	kavitaMu     sync.Mutex
	kavitaTokens = map[string]kavitaToken{} // base URL + API key -> bearer token
)

// kavitaAuth swaps the API key for a bearer token, kept for 30 minutes.
func (c *Client) kavitaAuth(ctx context.Context, s Server) (Server, error) {
	if strings.TrimSpace(s.Token) == "" {
		return s, explain(s, errUnauthorized)
	}
	key := s.BaseURL + "\x00" + s.Token
	kavitaMu.Lock()
	t, ok := kavitaTokens[key]
	kavitaMu.Unlock()
	if !ok || time.Now().After(t.until) {
		anon := s
		anon.Token = ""
		var res struct {
			Token string `json:"token"`
		}
		q := url.Values{"apiKey": {s.Token}, "pluginName": {"Mediarium"}}
		if err := c.do(ctx, anon, http.MethodPost, "/api/Plugin/authenticate", q, nil, &res); err != nil {
			return s, explain(s, err)
		}
		if res.Token == "" {
			return s, explain(s, errUnauthorized)
		}
		t = kavitaToken{token: res.Token, until: time.Now().Add(30 * time.Minute)}
		kavitaMu.Lock()
		if len(kavitaTokens) > 50 {
			kavitaTokens = map[string]kavitaToken{}
		}
		kavitaTokens[key] = t
		kavitaMu.Unlock()
	}
	authed := s
	authed.Token = t.token
	return authed, nil
}

func (c *Client) kavitaLibraries(ctx context.Context, s Server) ([]Library, Server, error) {
	authed, err := c.kavitaAuth(ctx, s)
	if err != nil {
		return nil, s, err
	}
	var libs []struct {
		ID      int      `json:"id"`
		Name    string   `json:"name"`
		Type    int      `json:"type"`
		Folders []string `json:"folders"`
	}
	err = c.do(ctx, authed, http.MethodGet, "/api/Library/libraries", nil, nil, &libs)
	if errors.Is(err, errNotFound) {
		err = c.do(ctx, authed, http.MethodGet, "/api/Library", nil, nil, &libs) // older versions
	}
	if err != nil {
		return nil, s, explain(s, err)
	}
	out := make([]Library, 0, len(libs))
	for _, l := range libs {
		out = append(out, Library{ID: fmt.Sprint(l.ID), Title: l.Name, Type: "books", Locations: l.Folders})
	}
	return out, authed, nil
}

func (c *Client) kavitaTest(ctx context.Context, s Server) (TestResult, error) {
	libs, _, err := c.kavitaLibraries(ctx, s)
	if err != nil {
		return TestResult{}, err
	}
	return TestResult{ServerName: "Kavita", Libraries: libs}, nil
}

func (c *Client) kavitaScan(ctx context.Context, authed Server, lib Library) error {
	return explain(authed, c.do(ctx, authed, http.MethodPost, "/api/Library/scan", url.Values{"libraryId": {lib.ID}, "force": {"false"}}, nil, nil))
}

func (c *Client) kavitaFind(ctx context.Context, s Server, title string) (Item, bool, error) {
	authed, err := c.kavitaAuth(ctx, s)
	if err != nil {
		return Item{}, false, err
	}
	var res struct {
		Series []struct {
			SeriesID  int    `json:"seriesId"`
			LibraryID int    `json:"libraryId"`
			Name      string `json:"name"`
		} `json:"series"`
	}
	if err := c.do(ctx, authed, http.MethodGet, "/api/Search/search", url.Values{"queryString": {title}}, nil, &res); err != nil {
		return Item{}, false, explain(s, err)
	}
	for _, r := range res.Series {
		if sameWords(r.Name, title) {
			return Item{ID: fmt.Sprint(r.SeriesID), URL: fmt.Sprintf("%s/library/%d/series/%d", s.WebURL(), r.LibraryID, r.SeriesID)}, true, nil
		}
	}
	return Item{}, false, nil
}

// ---- shared --------------------------------------------------------------

// bookRefresh scans the libraries of a book server that hold the new
// folders. A folder outside every library is left alone: that server does
// not keep that kind of book (Audiobookshelf libraries are often audiobooks
// only, Kavita ebooks only).
func (c *Client) bookRefresh(ctx context.Context, s Server, folders []string) ([]string, error) {
	var (
		libs   []Library
		authed = s
		err    error
	)
	if s.Kind == KindKavita {
		libs, authed, err = c.kavitaLibraries(ctx, s)
	} else {
		libs, err = c.absLibraries(ctx, s)
	}
	if err != nil {
		return nil, err
	}
	var done []string
	scanned := map[string]bool{}
	for _, f := range folders {
		mapped := MapPath(f, s.PathMap)
		for _, l := range libs {
			if scanned[l.ID] || (s.Kind == KindAudiobookshelf && l.Type == "podcast") {
				continue
			}
			for _, loc := range l.Locations {
				if _, ok := within(mapped, loc); !ok {
					continue
				}
				scanned[l.ID] = true
				if s.Kind == KindKavita {
					err = c.kavitaScan(ctx, authed, l)
				} else {
					err = c.absScan(ctx, s, l)
				}
				if err != nil {
					return done, err
				}
				done = append(done, "library "+l.Title)
				break
			}
		}
	}
	return done, nil
}

// FindBook looks a book up on a book server by title (and author, where the
// server says it). found is false when the server doesn't have it.
func (c *Client) FindBook(ctx context.Context, s Server, title, author string) (Item, bool, error) {
	switch s.Kind {
	case KindAudiobookshelf:
		return c.absFind(ctx, s, title, author)
	case KindKavita:
		return c.kavitaFind(ctx, s, title)
	}
	return Item{}, false, nil
}

func titleWords(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		switch w {
		case "the", "a", "an", "of", "and":
			continue
		}
		out = append(out, w)
	}
	return out
}

// sameWords reports whether two titles have the same words, ignoring case,
// punctuation and small words.
func sameWords(a, b string) bool {
	wa, wb := titleWords(a), titleWords(b)
	if len(wa) == 0 || len(wa) != len(wb) {
		return false
	}
	for i := range wa {
		if wa[i] != wb[i] {
			return false
		}
	}
	return true
}

// shareSurname reports whether the last word of either name is in the other.
func shareSurname(a, b string) bool {
	wa, wb := titleWords(a), titleWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	in := func(ws []string, w string) bool {
		for _, x := range ws {
			if x == w {
				return true
			}
		}
		return false
	}
	return in(wb, wa[len(wa)-1]) || in(wa, wb[len(wb)-1])
}
