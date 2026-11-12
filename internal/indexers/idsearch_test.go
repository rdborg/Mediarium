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
<movie-search available="yes" supportedParams="q,imdbid"/></searching></caps>`

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
