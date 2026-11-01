package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// A title with more releases than a search reads ("Dune") is searched again
// with its year, so the right film's releases are seen; a title with a short
// list is searched once.
func TestPopularMovieTitleIsSearchedAgainWithItsYear(t *testing.T) {
	for _, tt := range []struct {
		name     string
		total    int
		wantYear bool
	}{
		{"full pages", 500, true},
		{"short list", 40, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var seen []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := strings.ToLower(r.URL.Query().Get("q"))
				offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				mu.Lock()
				seen = append(seen, fmt.Sprintf("%s@%d", q, offset))
				mu.Unlock()
				var b strings.Builder
				b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
				total := tt.total
				if strings.Contains(q, "2021") {
					total = 1
				}
				fmt.Fprintf(&b, `<newznab:response offset="%d" total="%d"/>`, offset, total)
				for i := offset; i < offset+100 && i < total; i++ {
					fmt.Fprintf(&b, `<item><title>Dune.%d.1080p.BluRay.x264-GRP%d</title><guid>g%s%d</guid><enclosure url="http://%s/nzb/%d.nzb" length="8589934592" type="application/x-nzb"/><newznab:attr name="category" value="2000"/></item>`, 1984-i%3, i, q, i, r.Host, i)
				}
				b.WriteString(`</channel></rss>`)
				fmt.Fprint(w, b.String())
			}))
			t.Cleanup(srv.Close)

			server, base, client := loginNewServer(t)
			postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
				"name": "Busy Indexer", "definitionId": "fixture", "baseUrl": srv.URL, "apiKey": "k",
			}, http.StatusCreated)
			if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 438631, Title: "Dune", Year: 2021, Monitored: true}); err != nil {
				t.Fatal(err)
			}
			server.TestHunt(context.Background())
			waitBackground(t, server)

			mu.Lock()
			defer mu.Unlock()
			withYear := false
			for _, q := range seen {
				if strings.HasPrefix(q, "dune 2021@") {
					withYear = true
				}
			}
			if withYear != tt.wantYear {
				t.Errorf("searched with the year = %v, want %v (searches: %v)", withYear, tt.wantYear, seen)
			}
		})
	}
}
