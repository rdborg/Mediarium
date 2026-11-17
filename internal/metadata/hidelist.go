package metadata

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TMDB's trending and popular lists cannot be filtered by streaming service,
// and a /discover query is a different list (another order, other titles). So
// with something to hide, these lists are read as they are and each title is
// checked against its watch providers in the region. The titles on a hidden
// service are dropped and the gaps are filled with the ones that follow, so
// the result is the same list, in the same order, minus what is hidden.
//
// Pages of the filtered list are not the same as pages of TMDB's list (a page
// that lost five titles takes five from the next one), so where each filtered
// page starts in the source list is remembered, together with the source
// pages it was worked out from. Pages are normally read one after the other,
// which is what makes this cheap: each page needs the source page it starts
// in, and at most a few more, and the providers of a title are looked up once
// and kept.
const (
	// hideSourcePagesPerPage is the most source pages one filtered page reads.
	// Past it the page is returned short instead of reading on.
	hideSourcePagesPerPage = 4
	// hideCatchUpPages is how many filtered pages ahead of the last known one
	// one request works out. A page further on than that comes back empty
	// but says the list goes on, and the next request carries on from there.
	hideCatchUpPages = 6
	// A filtered list's page starts are kept while the list is read: one
	// nobody has read for hideStateIdle is dropped, and one older than
	// hideStateMaxAge starts afresh the next time its first page is asked
	// for, so it follows TMDB's list as that changes. At most
	// hideStateEntries lists are kept.
	hideStateIdle    = 30 * time.Minute
	hideStateMaxAge  = 3 * time.Hour
	hideStateEntries = 32
	// hideLookupWorkers is how many watch provider lookups run at once.
	hideLookupWorkers = 6

	providerCacheTTL     = 6 * time.Hour
	providerCacheEntries = 6000
)

// region is the TMDB watch region the hiding applies to.
func (h Hiding) region() string {
	r := strings.ToUpper(h.Region)
	if len(r) != 2 {
		return "US"
	}
	return r
}

// hiddenProviders is the set of provider ids behind the chosen services.
func (h Hiding) hiddenProviders() map[int]bool {
	set := map[int]bool{}
	for _, k := range h.Services {
		for _, s := range streamingServices {
			if s.Key != k {
				continue
			}
			for _, id := range s.Providers {
				set[id] = true
			}
		}
	}
	return set
}

// hideCursor is a place in TMDB's list: a source page (from 1) and the index
// of a title on it.
type hideCursor struct{ page, idx int }

// hideState is what is remembered about one filtered list: where each of its
// pages starts, and the source pages those starts were worked out from. A
// start only fits the very source page it was found on (TMDB's lists move, and
// on a newer copy of that page the same place holds other titles, which would
// show some twice and skip others), so the two are kept together, apart from
// the page cache, for as long as the list is in use.
type hideState[T any] struct {
	mu     sync.Mutex
	start  []hideCursor // start[i] is where filtered page i+1 begins
	last   int          // the filtered page the list ends on, 0 while unknown
	source map[int]hideSourcePage[T]
}

// hideSourcePage is one page of TMDB's list as a filtered list first read it.
type hideSourcePage[T any] struct {
	items      []T
	totalPages int
}

// fetch is source page p as this list first read it.
func (st *hideState[T]) fetch(ctx context.Context, src hideSource[T], p int) ([]T, int, error) {
	if sp, ok := st.source[p]; ok {
		return sp.items, sp.totalPages, nil
	}
	items, totalPages, err := src.fetch(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	if st.source == nil {
		st.source = map[int]hideSourcePage[T]{}
	}
	st.source[p] = hideSourcePage[T]{items: items, totalPages: totalPages}
	return items, totalPages, nil
}

// hideStates keeps the state of each filtered list (see hideState and
// hideStateIdle). The zero value is ready to use.
type hideStates struct {
	mu    sync.Mutex
	lists map[string]*hideStateEntry
	now   func() time.Time
}

type hideStateEntry struct {
	state      any // a *hideState[T]
	born, used time.Time
}

func (h *hideStates) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// hideStateFor is the state of the list named key, for a request for page.
func hideStateFor[T any](h *hideStates, key string, page int) *hideState[T] {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.clock()
	if e, ok := h.lists[key]; ok {
		st, typed := e.state.(*hideState[T])
		stale := now.Sub(e.used) >= hideStateIdle || (page == 1 && now.Sub(e.born) >= hideStateMaxAge)
		if typed && !stale {
			e.used = now
			return st
		}
		delete(h.lists, key)
	}
	if h.lists == nil {
		h.lists = map[string]*hideStateEntry{}
	}
	if len(h.lists) >= hideStateEntries {
		// Drop the idle ones; if that is not enough, the one read longest ago.
		var oldestKey string
		var oldest time.Time
		for k, e := range h.lists {
			if now.Sub(e.used) >= hideStateIdle {
				delete(h.lists, k)
				continue
			}
			if oldestKey == "" || e.used.Before(oldest) {
				oldestKey, oldest = k, e.used
			}
		}
		if len(h.lists) >= hideStateEntries {
			delete(h.lists, oldestKey)
		}
	}
	st := &hideState[T]{start: []hideCursor{{1, 0}}}
	h.lists[key] = &hideStateEntry{state: st, born: now, used: now}
	return st
}

// hideSource is what the filter needs to know about one kind of list.
type hideSource[T any] struct {
	kind  string // "movie" or "tv", as in TMDB's paths
	key   string // names the list and the hiding, for remembering page starts
	fetch func(ctx context.Context, page int) (items []T, totalPages int, err error)
	id    func(T) int
}

// providerRoot is the client whose caches are shared: a per-language view
// reads the provider cache of the client it came from, since where a title
// can be watched does not depend on the language.
func (c *Client) providerRoot() *Client {
	if c.parent != nil {
		return c.parent
	}
	return c
}

// providerIDs lists the provider ids a title can be watched on in a region,
// whether streaming, free, with adverts, to rent or to buy (which is how
// TMDB's own without_watch_providers reads it).
func (c *Client) providerIDs(ctx context.Context, kind string, id int, region string) ([]int, error) {
	root := c.providerRoot()
	root.provsInit.Do(func() { root.provs.max = providerCacheEntries })
	key := kind + ":" + strconv.Itoa(id) + ":" + region
	if v, ok := root.provs.get(key); ok {
		return v.([]int), nil
	}
	type provider struct {
		ID int `json:"provider_id"`
	}
	var resp struct {
		Results map[string]struct {
			Flatrate []provider `json:"flatrate"`
			Free     []provider `json:"free"`
			Ads      []provider `json:"ads"`
			Rent     []provider `json:"rent"`
			Buy      []provider `json:"buy"`
		} `json:"results"`
	}
	if err := c.get(ctx, fmt.Sprintf("/%s/%d/watch/providers", kind, id), nil, &resp); err != nil {
		return nil, err
	}
	ids := []int{}
	if r, ok := resp.Results[region]; ok {
		for _, group := range [][]provider{r.Flatrate, r.Free, r.Ads, r.Rent, r.Buy} {
			for _, p := range group {
				ids = append(ids, p.ID)
			}
		}
	}
	root.provs.putFor(key, ids, providerCacheTTL)
	return ids, nil
}

// hiddenTitles looks up which of ids are on a hidden provider. A title whose
// providers cannot be read is kept (and tried again next time), so a TMDB
// hiccup shows a title that should have been hidden rather than losing one.
func (c *Client) hiddenTitles(ctx context.Context, kind string, ids []int, hide Hiding) (map[int]bool, error) {
	hidden := hide.hiddenProviders()
	region := hide.region()
	out := make(map[int]bool, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, hideLookupWorkers)
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()
			provs, err := c.providerIDs(ctx, kind, id, region)
			if err != nil {
				return
			}
			for _, p := range provs {
				if hidden[p] {
					mu.Lock()
					out[id] = true
					mu.Unlock()
					return
				}
			}
		}(id)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// hideBuild reads one filtered page starting at from, from the source pages
// st holds (reading and keeping the ones it lacks); st must be locked. next is
// where the page after it starts; done is true when the list has nothing left
// after this page.
func hideBuild[T any](ctx context.Context, c *Client, src hideSource[T], st *hideState[T], hide Hiding, from hideCursor) (out []T, next hideCursor, done bool, err error) {
	out = []T{}
	p, i := from.page, from.idx
	for read := 0; ; read++ {
		if len(out) >= tmdbPageSize {
			return out, hideCursor{p, i}, false, nil
		}
		if read >= hideSourcePagesPerPage {
			return out, hideCursor{p, 0}, false, nil
		}
		if p > maxDiscoverPages {
			return out, hideCursor{p, 0}, true, nil
		}
		items, totalPages, err := st.fetch(ctx, src, p)
		if err != nil {
			return nil, hideCursor{}, false, err
		}
		if len(items) == 0 || p > totalPages {
			return out, hideCursor{p, 0}, true, nil
		}
		rest := items
		if i < len(rest) {
			rest = rest[i:]
		} else {
			rest = nil
		}
		ids := make([]int, len(rest))
		for k, it := range rest {
			ids[k] = src.id(it)
		}
		hiddenSet, err := c.hiddenTitles(ctx, src.kind, ids, hide)
		if err != nil {
			return nil, hideCursor{}, false, err
		}
		for k, it := range rest {
			if hiddenSet[ids[k]] {
				continue
			}
			out = append(out, it)
			if len(out) == tmdbPageSize {
				i += k + 1
				if i >= len(items) {
					p, i = p+1, 0
				}
				return out, hideCursor{p, i}, i == 0 && p > totalPages, nil
			}
		}
		p, i = p+1, 0
		if p > totalPages {
			return out, hideCursor{p, 0}, true, nil
		}
	}
}

// hideList is page of the list in src with the hidden titles left out. It
// returns the titles and whether the list goes on after this page.
func hideList[T any](ctx context.Context, c *Client, src hideSource[T], hide Hiding, page int) ([]T, bool, error) {
	st := hideStateFor[T](&c.hides, src.key, page)
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.last > 0 && page > st.last {
		return []T{}, false, nil
	}
	// Learn where each page up to this one starts, a few pages per request. A
	// page further on than that comes back empty but says the list goes on;
	// asking again carries on from where this request stopped.
	for steps := 0; len(st.start) < page; steps++ {
		if steps == hideCatchUpPages {
			return []T{}, true, nil
		}
		k := len(st.start)
		_, next, done, err := hideBuild(ctx, c, src, st, hide, st.start[k-1])
		if err != nil {
			return nil, false, err
		}
		if done {
			st.last = k
			return []T{}, false, nil
		}
		st.start = append(st.start, next)
	}
	items, next, done, err := hideBuild(ctx, c, src, st, hide, st.start[page-1])
	if err != nil {
		return nil, false, err
	}
	if done {
		st.last = page
	} else if len(st.start) == page {
		st.start = append(st.start, next)
	}
	return items, !done, nil
}

// hideKey names a list and its hiding, for the remembered page starts.
func hideKey(kind, list string, hide Hiding) string {
	return kind + "|" + list + "|" + hide.region() + "|" + providerParam(hide.Services)
}

// hidePageMeta shapes a filtered page as a page of the list.
func hidePageMeta(page int, more bool) pageMeta {
	total := page
	if more {
		total = page + 1
	}
	return pageMeta{Page: page, TotalPages: total, TotalResults: total * tmdbPageSize}
}

// listPath is TMDB's path for an unfiltered trending or popular list.
func listPath(kind, list string) string {
	switch {
	case list == ListTrending:
		return "/trending/" + kind + "/week"
	case kind == "movie":
		return "/movie/popular"
	}
	return "/tv/popular"
}

func (c *Client) hiddenMovieList(ctx context.Context, list string, page int, hide Hiding) (*MoviePage, error) {
	page = ClampPage(page)
	src := hideSource[Movie]{
		kind: "movie", key: hideKey("movie", list, hide),
		fetch: func(ctx context.Context, p int) ([]Movie, int, error) {
			mp, err := c.moviePage(ctx, listPath("movie", list), url.Values{"page": {strconv.Itoa(p)}})
			if err != nil {
				return nil, 0, err
			}
			return mp.Results, mp.TotalPages, nil
		},
		id: func(m Movie) int { return m.TMDBID },
	}
	items, more, err := hideList(ctx, c, src, hide, page)
	if err != nil {
		return nil, err
	}
	m := hidePageMeta(page, more)
	return &MoviePage{Page: m.Page, TotalPages: m.TotalPages, TotalResults: m.TotalResults, Results: items}, nil
}

func (c *Client) hiddenTVList(ctx context.Context, list string, page int, hide Hiding) (*ShowPage, error) {
	page = ClampPage(page)
	src := hideSource[Show]{
		kind: "tv", key: hideKey("tv", list, hide),
		fetch: func(ctx context.Context, p int) ([]Show, int, error) {
			sp, err := c.showPage(ctx, listPath("tv", list), url.Values{"page": {strconv.Itoa(p)}})
			if err != nil {
				return nil, 0, err
			}
			return sp.Results, sp.TotalPages, nil
		},
		id: func(s Show) int { return s.TMDBID },
	}
	items, more, err := hideList(ctx, c, src, hide, page)
	if err != nil {
		return nil, err
	}
	m := hidePageMeta(page, more)
	return &ShowPage{Page: m.Page, TotalPages: m.TotalPages, TotalResults: m.TotalResults, Results: items}, nil
}
