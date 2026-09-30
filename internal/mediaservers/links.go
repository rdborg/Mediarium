package mediaservers

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Link is one place a title (or the server itself) can be opened.
type Link struct {
	ServerID int64  `json:"serverId"`
	Name     string `json:"name"`
	Kind     Kind   `json:"kind"`
	URL      string `json:"url"`
	AppURL   string `json:"appUrl,omitempty"` // Plex: the same item on app.plex.tv
}

// HomeLinks lists "Open my media server" links for the enabled servers.
func HomeLinks(servers []Server) []Link {
	out := []Link{}
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		l := Link{ServerID: s.ID, Name: s.Name, Kind: s.Kind, URL: HomeURL(s)}
		if s.Kind == KindPlex && s.PublicURL != "" {
			l.AppURL = "https://app.plex.tv/desktop/"
		}
		out = append(out, l)
	}
	return out
}

// Finder finds titles on media servers by TMDB id, remembering answers for
// a few minutes so opening a title page doesn't query every server each
// time.
type Finder struct {
	client *Client
	// TTL is how long an answer is kept; ErrorTTL how long a failed lookup
	// is (so a server that is down isn't asked on every page view).
	TTL      time.Duration
	ErrorTTL time.Duration

	mu    sync.Mutex
	items map[findKey]findEntry
	plex  map[plexKey]plexIndexEntry
}

type findKey struct {
	server int64
	kind   MediaKind
	tmdbID int
}

type findEntry struct {
	item    Item
	found   bool
	failed  bool
	expires time.Time
}

type plexKey struct {
	server int64
	kind   MediaKind
}

type plexIndexEntry struct {
	index   map[int]string
	expires time.Time
}

// NewFinder returns a finder that keeps answers for five minutes.
func NewFinder(client *Client) *Finder {
	return &Finder{client: client, TTL: 5 * time.Minute, ErrorTTL: time.Minute}
}

// Invalidate forgets every cached answer (after an import, or when servers
// change).
func (f *Finder) Invalidate() {
	f.mu.Lock()
	f.items, f.plex = nil, nil
	f.mu.Unlock()
}

// Links looks the title up on every enabled server at once and returns the
// servers that have it, in server order. A server that can't be asked is
// left out (and logged), never an error for the page.
func (f *Finder) Links(ctx context.Context, servers []Server, kind MediaKind, tmdbID int) []Link {
	type result struct {
		order int
		link  Link
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		got []result
	)
	for i, s := range servers {
		if !s.Enabled {
			continue
		}
		wg.Add(1)
		go func(i int, s Server) {
			defer wg.Done()
			item, found := f.find(ctx, s, kind, tmdbID)
			if !found {
				return
			}
			mu.Lock()
			got = append(got, result{i, Link{ServerID: s.ID, Name: s.Name, Kind: s.Kind, URL: item.URL, AppURL: item.AppURL}})
			mu.Unlock()
		}(i, s)
	}
	wg.Wait()
	sort.Slice(got, func(a, b int) bool { return got[a].order < got[b].order })
	out := make([]Link, 0, len(got))
	for _, r := range got {
		out = append(out, r.link)
	}
	return out
}

func (f *Finder) find(ctx context.Context, s Server, kind MediaKind, tmdbID int) (Item, bool) {
	key := findKey{s.ID, kind, tmdbID}
	now := time.Now()
	f.mu.Lock()
	if e, ok := f.items[key]; ok && now.Before(e.expires) {
		f.mu.Unlock()
		return e.item, e.found
	}
	var index map[int]string
	if e, ok := f.plex[plexKey{s.ID, kind}]; ok && now.Before(e.expires) {
		index = e.index
	}
	f.mu.Unlock()

	var (
		item  Item
		found bool
		err   error
	)
	if s.Kind == KindPlex {
		if index == nil {
			index, err = f.client.plexIndex(ctx, s, kind)
			if err == nil {
				f.mu.Lock()
				if f.plex == nil {
					f.plex = map[plexKey]plexIndexEntry{}
				}
				f.plex[plexKey{s.ID, kind}] = plexIndexEntry{index: index, expires: time.Now().Add(f.TTL)}
				f.mu.Unlock()
			}
		}
		if err == nil {
			item, found, err = f.client.plexFind(ctx, s, kind, tmdbID, index)
		}
	} else {
		item, found, err = f.client.Find(ctx, s, kind, tmdbID)
	}

	entry := findEntry{item: item, found: found && err == nil, expires: time.Now().Add(f.TTL)}
	if err != nil {
		entry.failed, entry.expires = true, time.Now().Add(f.ErrorTTL)
		if ctx.Err() == nil { // a page closed early is not the server's fault
			slog.Warn("media server lookup failed", "server", s.Name, "type", string(s.Kind), "tmdbId", tmdbID, "err", err)
		}
	}
	if ctx.Err() == nil {
		f.mu.Lock()
		if f.items == nil {
			f.items = map[findKey]findEntry{}
		}
		if len(f.items) > 5000 { // keep the cache small: drop what has expired
			for k, e := range f.items {
				if now.After(e.expires) {
					delete(f.items, k)
				}
			}
		}
		f.items[key] = entry
		f.mu.Unlock()
	}
	return entry.item, entry.found
}
