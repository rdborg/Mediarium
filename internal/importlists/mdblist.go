package importlists

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/netguard"
)

// MDBList (mdblist.com) keeps public lists of movies and shows. Reading one
// needs the person's own API key (made on mdblist.com/preferences). The call
// is GET https://api.mdblist.com/lists/{user}/{list}/items?apikey=..., which
// answers {"movies": [...], "shows": [...]}; each entry has id, title,
// imdb_id, mediatype and release_year. Larger lists are read in pages with
// limit and offset.

const mdblistBase = "https://api.mdblist.com"

// ErrMDBListKey is returned when MDBList doesn't accept the key.
var ErrMDBListKey = errors.New("MDBList doesn't accept that API key. Copy it again from mdblist.com/preferences")

var mdbName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// MDBListClient reads lists from MDBList.
type MDBListClient struct {
	base   string
	client *http.Client
}

// NewMDBListClient returns a client for the real service.
func NewMDBListClient() *MDBListClient {
	return &MDBListClient{base: mdblistBase, client: newClient()}
}

// NewMDBListClientAt points a client at another server (tests use a fixture).
func NewMDBListClientAt(base string) *MDBListClient {
	return &MDBListClient{base: strings.TrimRight(base, "/"), client: newClient()}
}

// ParseMDBListAddress reads the user and list name from an address like
// https://mdblist.com/lists/someone/top-movies (or "someone/top-movies").
func ParseMDBListAddress(raw string) (user, list string, err error) {
	raw = strings.TrimSpace(raw)
	bad := errors.New("That isn't an MDBList address. It looks like https://mdblist.com/lists/name/list-name")
	if raw == "" {
		return "", "", bad
	}
	var parts []string
	if strings.Contains(raw, "://") || strings.HasPrefix(strings.ToLower(raw), "mdblist.com") || strings.HasPrefix(strings.ToLower(raw), "www.mdblist.com") {
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		u, perr := url.Parse(raw)
		if perr != nil || (strings.ToLower(u.Hostname()) != "mdblist.com" && strings.ToLower(u.Hostname()) != "www.mdblist.com") {
			return "", "", bad
		}
		p := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(p) < 3 || p[0] != "lists" {
			return "", "", bad
		}
		parts = p[1:3]
	} else {
		parts = strings.Split(strings.Trim(raw, "/"), "/")
		if len(parts) != 2 {
			return "", "", bad
		}
	}
	if !mdbName.MatchString(parts[0]) || !mdbName.MatchString(parts[1]) || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", "", bad
	}
	return parts[0], parts[1], nil
}

type mdbEntry struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	IMDBID      string `json:"imdb_id"`
	MediaType   string `json:"mediatype"`
	ReleaseYear int    `json:"release_year"`
}

const (
	mdbPageSize = 200
	mdbMaxItems = 5000
)

// Items reads every title on a public list. Each title is named by its IMDb id
// where MDBList has one (it maps to exactly one TMDB title); the entry's own id
// is used only when there is no IMDb id.
func (c *MDBListClient) Items(ctx context.Context, apiKey, user, list string) ([]Item, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("Add your MDBList API key to read this list")
	}
	var out []Item
	for offset := 0; offset < mdbMaxItems; offset += mdbPageSize {
		q := url.Values{"apikey": {apiKey}, "limit": {strconv.Itoa(mdbPageSize)}, "offset": {strconv.Itoa(offset)}}
		target := fmt.Sprintf("%s/lists/%s/%s/items?%s", c.base, url.PathEscape(user), url.PathEscape(list), q.Encode())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, fmt.Errorf("build mdblist request: %w", err)
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "application/json")
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("mdblist request: %w", cleanKey(err, apiKey))
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if rerr != nil {
			return nil, fmt.Errorf("read mdblist answer: %w", rerr)
		}
		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, ErrMDBListKey
		case http.StatusNotFound:
			return nil, errors.New("MDBList has no public list at that address")
		case http.StatusTooManyRequests:
			return nil, &RateLimitError{Service: "MDBList", RetryAfter: retryAfter(resp.Header)}
		default:
			return nil, fmt.Errorf("mdblist request: unexpected status %d", resp.StatusCode)
		}
		var page struct {
			Movies []mdbEntry `json:"movies"`
			Shows  []mdbEntry `json:"shows"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode mdblist answer: %w", err)
		}
		for _, e := range page.Movies {
			out = appendMDB(out, e, "movie")
		}
		for _, e := range page.Shows {
			out = appendMDB(out, e, "tv")
		}
		// The limit may count movies and shows together (a page of 120 movies and
		// 80 shows is a full page), so the page counts as full by what it holds in all.
		if len(page.Movies)+len(page.Shows) < mdbPageSize {
			break
		}
	}
	return out, nil
}

func appendMDB(out []Item, e mdbEntry, kind string) []Item {
	it := Item{Kind: kind, Title: e.Title, Year: e.ReleaseYear}
	if id := strings.ToLower(strings.TrimSpace(e.IMDBID)); strings.HasPrefix(id, "tt") {
		it.IMDBID = id
	} else if e.ID > 0 {
		it.TMDBID = e.ID
	}
	if it.IMDBID == "" && it.TMDBID == 0 && it.Title == "" {
		return out
	}
	return append(out, it)
}

// cleanKey removes the API key from a network error: net/http repeats the
// request address, and this key travels in it, URL-encoded when it has
// characters that need it.
func cleanKey(err error, key string) error {
	if err == nil {
		return nil
	}
	err = netguard.CleanError(err)
	if key == "" {
		return err
	}
	msg := err.Error()
	for _, k := range []string{key, url.QueryEscape(key), url.PathEscape(key)} {
		msg = strings.ReplaceAll(msg, k, "***")
	}
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}
