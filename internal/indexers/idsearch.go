package indexers

import (
	"context"
	"encoding/xml"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Searching by id. A title search for "The Girlfriend S01" finds unrelated
// shows with a similar name, while an indexer that understands ids answers
// exactly for the show. A caller that knows a show's or film's ids puts them
// in the context with WithIDs; a Newznab or Torznab indexer whose capabilities
// say it takes that id is asked a second question by id, and the releases of
// both questions are merged. An indexer that does not list the id is only
// asked the usual way.

// IDs are the ids of what is searched for.
type IDs struct {
	IMDB    string // "tt1234567"
	TVDB    int
	Season  int
	Episode int
	Movie   bool // a film (t=movie) rather than a show (t=tvsearch)
}

type idsKey struct{}

// WithIDs adds the ids of the search to the context.
func WithIDs(ctx context.Context, ids IDs) context.Context {
	if ids.IMDB == "" && ids.TVDB == 0 {
		return ctx
	}
	return context.WithValue(ctx, idsKey{}, ids)
}

func idsFrom(ctx context.Context) (IDs, bool) {
	v, ok := ctx.Value(idsKey{}).(IDs)
	return v, ok
}

// idCaps is what an indexer says it takes for shows and films.
type idCaps struct {
	tv, movie map[string]bool
	at        time.Time
	ok        bool
}

var (
	idCapsMu    sync.Mutex
	idCapsCache = map[string]idCaps{}
)

const (
	idCapsTTL       = 12 * time.Hour
	idCapsFailedTTL = 15 * time.Minute
)

// parseIDCaps reads the search types of a t=caps answer.
func parseIDCaps(body []byte) (tv, movie map[string]bool) {
	var root struct {
		TV struct {
			Available string `xml:"available,attr"`
			Params    string `xml:"supportedParams,attr"`
		} `xml:"searching>tv-search"`
		Movie struct {
			Available string `xml:"available,attr"`
			Params    string `xml:"supportedParams,attr"`
		} `xml:"searching>movie-search"`
	}
	if xml.Unmarshal(body, &root) != nil {
		return nil, nil
	}
	set := func(avail, params string) map[string]bool {
		if !strings.EqualFold(strings.TrimSpace(avail), "yes") {
			return nil
		}
		m := map[string]bool{}
		for _, p := range strings.Split(params, ",") {
			if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
				m[p] = true
			}
		}
		return m
	}
	return set(root.TV.Available, root.TV.Params), set(root.Movie.Available, root.Movie.Params)
}

// idCapsFor returns what the indexer takes, asking t=caps at most once in
// twelve hours (once in fifteen minutes after a failed answer). fetched says
// whether this call made a request, so the caller can count it.
func (c *NewznabClient) idCapsFor(ctx context.Context) (caps idCaps, fetched bool) {
	idCapsMu.Lock()
	cached, have := idCapsCache[c.BaseURL]
	idCapsMu.Unlock()
	if have {
		ttl := idCapsTTL
		if !cached.ok {
			ttl = idCapsFailedTTL
		}
		if time.Since(cached.at) < ttl {
			return cached, false
		}
	}
	q := url.Values{}
	q.Set("t", "caps")
	q.Set("apikey", c.APIKey)
	q.Set("o", "xml")
	out := idCaps{at: time.Now()}
	if body, err := c.get(ctx, q); err == nil {
		out.tv, out.movie = parseIDCaps(body)
		out.ok = true
	}
	idCapsMu.Lock()
	idCapsCache[c.BaseURL] = out
	idCapsMu.Unlock()
	return out, true
}

// idQuery builds the by-id question, or reports false when the indexer takes
// none of the ids it was given.
func idQuery(caps idCaps, ids IDs, categories []int) (url.Values, bool) {
	params, kind := caps.tv, "tvsearch"
	if ids.Movie {
		params, kind = caps.movie, "movie"
	}
	if params == nil {
		return nil, false
	}
	q := url.Values{}
	switch {
	case ids.IMDB != "" && params["imdbid"]:
		q.Set("imdbid", ids.IMDB)
	case ids.TVDB != 0 && !ids.Movie && params["tvdbid"]:
		q.Set("tvdbid", strconv.Itoa(ids.TVDB))
	default:
		return nil, false
	}
	q.Set("t", kind)
	if !ids.Movie {
		if ids.Season > 0 && params["season"] {
			q.Set("season", strconv.Itoa(ids.Season))
			if ids.Episode > 0 && params["ep"] {
				q.Set("ep", strconv.Itoa(ids.Episode))
			}
		}
	}
	if len(categories) > 0 {
		cats := make([]string, len(categories))
		for i, cat := range categories {
			cats[i] = strconv.Itoa(cat)
		}
		q.Set("cat", strings.Join(cats, ","))
	}
	return q, true
}

// SearchByID asks the indexer by id when the context carries ids and the
// indexer lists them. requests is how many requests it made (for the daily
// limit); results is empty when nothing was asked or it failed, since the
// title search has already given its answer.
func (c *NewznabClient) SearchByID(ctx context.Context, categories []int) (results []Result, requests int) {
	ids, ok := idsFrom(ctx)
	if !ok {
		return nil, 0
	}
	caps, fetched := c.idCapsFor(ctx)
	if fetched {
		requests++
	}
	q, ok := idQuery(caps, ids, categories)
	if !ok {
		return nil, requests
	}
	q.Set("apikey", c.APIKey)
	q.Set("o", "xml")
	q.Set("limit", strconv.Itoa(newznabPageSize))
	requests++
	body, err := c.get(ctx, q)
	if err != nil {
		return nil, requests
	}
	results, _, err = parseNewznabPage(body, c.Name, c.APIKey)
	if err != nil {
		return nil, requests
	}
	return results, requests
}

// mergeByLink adds the releases of extra that main does not have yet.
func mergeByLink(main, extra []Result) []Result {
	if len(extra) == 0 {
		return main
	}
	seen := make(map[string]bool, len(main))
	for _, r := range main {
		seen[r.DownloadURL] = true
	}
	for _, r := range extra {
		if !seen[r.DownloadURL] {
			seen[r.DownloadURL] = true
			main = append(main, r)
		}
	}
	return main
}
