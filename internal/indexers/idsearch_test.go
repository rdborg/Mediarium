package indexers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const idCapsXML = `<?xml version="1.0"?><caps><searching>
<search available="yes" supportedParams="q"/>
<tv-search available="yes" supportedParams="q,imdbid,tvdbid,season,ep"/>
<movie-search available="yes" supportedParams="q,imdbid,tmdbid"/></searching></caps>`

func itemXML(title, link string) string {
	return `<item><title>` + title + `</title><link>` + link + `</link><enclosure url="` + link + `" length="1000" type="application/x-nzb"/></item>`
}

func TestSearchByID(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		seen = append(seen, q.Get("t")+"|"+q.Get("imdbid")+"|"+q.Get("season")+"|"+q.Get("ep"))
		mu.Unlock()
		switch q.Get("t") {
		case "caps":
			w.Write([]byte(idCapsXML))
		case "tvsearch":
			w.Write([]byte(`<rss><channel>` + itemXML("The.Girlfriend.S01.2160p", "http://x/1") + itemXML("Shared.S01", "http://x/2") + `</channel></rss>`))
		default:
			w.Write([]byte(`<rss><channel>` + itemXML("Kanojo.mo.Kanojo", "http://x/3") + itemXML("Shared.S01", "http://x/2") + `</channel></rss>`))
		}
	}))
	defer srv.Close()
	idCapsMu.Lock()
	delete(idCapsCache, srv.URL)
	idCapsMu.Unlock()

	c := NewNewznabClient("t", srv.URL, "k")
	ctx := WithIDs(context.Background(), IDs{IMDB: "tt32267726", Season: 1})
	text, err := c.Search(ctx, "The Girlfriend S01", []int{5000})
	if err != nil {
		t.Fatal(err)
	}
	byID, n := c.SearchByID(ctx, []int{5000})
	if n != 2 { // caps, then the search by id
		t.Errorf("requests = %d, want 2 (caps and the id search)", n)
	}
	got := mergeByLink(text, byID)
	var titles []string
	for _, r := range got {
		titles = append(titles, r.Title)
	}
	if len(got) != 3 || !strings.Contains(strings.Join(titles, ","), "The.Girlfriend.S01.2160p") {
		t.Fatalf("merged = %v, want the id result, the text result and no duplicate", titles)
	}
	if !strings.Contains(strings.Join(seen, " "), "tvsearch|tt32267726|1|") {
		t.Errorf("requests seen: %v", seen)
	}
	// Caps are remembered: a second search by id costs one request.
	if _, n := c.SearchByID(ctx, []int{5000}); n != 1 {
		t.Errorf("second search requests = %d, want 1", n)
	}
	// No ids in the context: nothing is asked.
	if r, n := c.SearchByID(context.Background(), nil); r != nil || n != 0 {
		t.Errorf("without ids: %v, %d", r, n)
	}
}

func TestIDQuery(t *testing.T) {
	tv, movie := parseIDCaps([]byte(idCapsXML))
	caps := idCaps{tv: tv, movie: movie, ok: true}
	q, ok := idQuery(caps, IDs{IMDB: "tt1", Season: 2, Episode: 3}, []int{5000})
	if !ok || q.Get("t") != "tvsearch" || q.Get("season") != "2" || q.Get("ep") != "3" || q.Get("cat") != "5000" {
		t.Errorf("tv query = %v %v", q, ok)
	}
	if q, ok := idQuery(caps, IDs{TVDB: 99, Season: 1}, nil); !ok || q.Get("tvdbid") != "99" {
		t.Errorf("tvdb query = %v %v", q, ok)
	}
	if _, ok := idQuery(caps, IDs{TVDB: 99, Movie: true}, nil); ok {
		t.Error("a film search must not use the tvdb id")
	}
	if _, ok := idQuery(idCaps{tv: map[string]bool{"q": true}}, IDs{IMDB: "tt1"}, nil); ok {
		t.Error("an indexer that does not list imdbid must not be asked by id")
	}
	if _, ok := idQuery(idCaps{}, IDs{IMDB: "tt1"}, nil); ok {
		t.Error("an indexer with no tv search must not be asked by id")
	}
}

func TestSearchByIDMovie(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		seen = append(seen, q.Get("t")+"|"+q.Get("imdbid")+"|"+q.Get("tmdbid")+"|"+q.Get("season"))
		mu.Unlock()
		switch q.Get("t") {
		case "caps":
			w.Write([]byte(idCapsXML))
		case "movie":
			w.Write([]byte(`<rss><channel>` + itemXML("Wrong.Title.2024.1080p", "http://x/1") + itemXML("Shared.2024", "http://x/2") + `</channel></rss>`))
		default:
			w.Write([]byte(`<rss><channel>` + itemXML("Shared.2024", "http://x/2") + `</channel></rss>`))
		}
	}))
	defer srv.Close()
	idCapsMu.Lock()
	delete(idCapsCache, srv.URL)
	idCapsMu.Unlock()

	c := NewNewznabClient("t", srv.URL, "k")
	ctx := WithIDs(context.Background(), IDs{IMDB: "tt1234567", TMDB: 55, Movie: true})
	text, err := c.Search(ctx, "Some Film", []int{2000})
	if err != nil {
		t.Fatal(err)
	}
	byID, n := c.SearchByID(ctx, []int{2000})
	if n != 2 {
		t.Errorf("requests = %d, want 2 (caps and the id search)", n)
	}
	if got := mergeByLink(text, byID); len(got) != 2 {
		t.Errorf("merged %d releases, want 2 without a duplicate", len(got))
	}
	if !strings.Contains(strings.Join(seen, " "), "movie|tt1234567||") {
		t.Errorf("requests seen: %v", seen)
	}
}

func TestSearchByIDMovieLookup(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		asked = append(asked, q.Get("t")+"|"+q.Get("imdbid"))
		if q.Get("t") == "caps" {
			if strings.Contains(r.URL.Path, "plain") {
				w.Write([]byte(`<caps><searching><movie-search available="yes" supportedParams="q"/></searching></caps>`))
				return
			}
			w.Write([]byte(idCapsXML))
			return
		}
		w.Write([]byte(`<rss><channel>` + itemXML("Film.2024", "http://x/9") + `</channel></rss>`))
	}))
	defer srv.Close()
	calls := 0
	lookup := func(context.Context) string { calls++; return "tt777" }
	ctx := WithIDs(context.Background(), IDs{TMDB: 9, Movie: true, LookupIMDB: lookup})

	// An indexer that takes no ids is never the reason for a lookup.
	plain := NewNewznabClient("p", srv.URL+"/plain", "k")
	if r, _ := plain.SearchByID(ctx, nil); r != nil || calls != 0 {
		t.Errorf("plain indexer: results %v, lookups %d", r, calls)
	}
	c := NewNewznabClient("t", srv.URL, "k")
	idCapsMu.Lock()
	delete(idCapsCache, srv.URL)
	idCapsMu.Unlock()
	if r, _ := c.SearchByID(ctx, nil); len(r) != 1 || calls != 1 {
		t.Errorf("results %v, lookups %d", r, calls)
	}
	if !strings.Contains(strings.Join(asked, " "), "movie|tt777") {
		t.Errorf("requests: %v", asked)
	}
}

func TestIDQueryMovie(t *testing.T) {
	tv, movie := parseIDCaps([]byte(idCapsXML))
	caps := idCaps{tv: tv, movie: movie, ok: true}
	q, ok := idQuery(caps, IDs{IMDB: "tt1", TMDB: 5, Movie: true, Season: 3}, []int{2000})
	if !ok || q.Get("t") != "movie" || q.Get("imdbid") != "tt1" || q.Get("season") != "" || q.Get("cat") != "2000" {
		t.Errorf("movie query = %v %v", q, ok)
	}
	// Without an IMDb id the TMDB id is used, when the indexer lists it.
	if q, ok := idQuery(caps, IDs{TMDB: 5, Movie: true}, nil); !ok || q.Get("tmdbid") != "5" || q.Get("imdbid") != "" {
		t.Errorf("tmdb query = %v %v", q, ok)
	}
	only := idCaps{movie: map[string]bool{"q": true, "imdbid": true}}
	if _, ok := idQuery(only, IDs{TMDB: 5, Movie: true}, nil); ok {
		t.Error("an indexer that does not list tmdbid must not be asked by it")
	}
	if _, ok := idQuery(idCaps{tv: tv}, IDs{IMDB: "tt1", Movie: true}, nil); ok {
		t.Error("an indexer with no movie search must not be asked by id")
	}
	if got := WithIDs(context.Background(), IDs{TMDB: 5}); got != context.Background() {
		t.Error("a TMDB id alone must only count for a film")
	}
}
