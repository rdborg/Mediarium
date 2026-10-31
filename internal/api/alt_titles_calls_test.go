package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// newQueryLogIndexer is a Newznab fixture that counts its searches and offers
// the one release in answers whose key is in the search text.
func newQueryLogIndexer(t *testing.T, answers map[string]string) (srv *httptest.Server, queries func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/nzb/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		q := strings.ToLower(r.URL.Query().Get("q"))
		mu.Lock()
		seen = append(seen, q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
		for key, title := range answers {
			if strings.Contains(q, key) {
				fmt.Fprintf(&b, `<item><title>%s</title><guid>%s</guid><enclosure url="http://%s/nzb/x.nzb" length="8589934592" type="application/x-nzb"/><newznab:attr name="size" value="8589934592"/><newznab:attr name="category" value="2000"/></item>`, title, key, r.Host)
			}
		}
		b.WriteString(`</channel></rss>`)
		fmt.Fprint(w, b.String())
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// An automatic hunt tries a movie's other titles only while the main title
// found nothing it would take, and never more than two of them; a search the
// person starts with Search now tries every one.
func TestHuntOtherTitlesOnlyWhenMainFindsNothing(t *testing.T) {
	alts := []string{"Alpha Film", "Beta Film", "Gamma Film", "Delta Film"}
	tests := []struct {
		name    string
		answers map[string]string
		want    int
	}{
		{"found under the main title: one search", map[string]string{"fixture movie": "The.Fixture.Movie.1999.1080p.BluRay.x264-GRP"}, 1},
		{"found under the first other title: two searches", map[string]string{"alpha film": "Alpha.Film.1999.1080p.BluRay.x264-GRP"}, 2},
		{"found nowhere: the main title and two others", map[string]string{}, 3},
		{"found only under a third other title: not reached", map[string]string{"gamma film": "Gamma.Film.1999.1080p.BluRay.x264-GRP"}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, base, client := loginNewServer(t)
			putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguage": "German"}, http.StatusOK)
			indexer, queries := newQueryLogIndexer(t, tt.answers)
			postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
				"name": "Counting Indexer", "definitionId": "fixture", "baseUrl": indexer.URL, "apiKey": "k",
			}, http.StatusCreated)
			if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "The Fixture Movie", Year: 1999, Monitored: true}); err != nil {
				t.Fatal(err)
			}
			if err := server.MovieRepo.SaveTitleNames("movie", 605, alts, "German", time.Now()); err != nil {
				t.Fatal(err)
			}

			server.TestHunt(context.Background())
			waitBackground(t, server)
			if got := queries(); len(got) != tt.want {
				t.Errorf("the hunt searched %d times (%v), want %d", len(got), got, tt.want)
			}
		})
	}
}

// The other titles are all tried when the person searches by hand.
func TestInteractiveSearchTriesEveryOtherTitle(t *testing.T) {
	server, base, client := loginNewServer(t)
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguage": "German"}, http.StatusOK)
	indexer, queries := newQueryLogIndexer(t, map[string]string{"fixture movie": "The.Fixture.Movie.1999.1080p.BluRay.x264-GRP"})
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Counting Indexer", "definitionId": "fixture", "baseUrl": indexer.URL, "apiKey": "k",
	}, http.StatusCreated)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SaveTitleNames("movie", 605, []string{"Alpha Film", "Beta Film", "Gamma Film", "Delta Film"}, "German", time.Now()); err != nil {
		t.Fatal(err)
	}
	getJSON[[]map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/search", base, m.ID))
	if got := queries(); len(got) != 5 {
		t.Errorf("a search by hand made %d searches (%v), want 5", len(got), got)
	}
}

// Words typed into the search box are the only search made, and a release that
// does not look like the movie is still listed.
func TestInteractiveSearchWithOwnWords(t *testing.T) {
	server, base, client := loginNewServer(t)
	indexer, queries := newQueryLogIndexer(t, map[string]string{"some other name": "Some.Other.Name.1999.1080p.BluRay.x264-GRP"})
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Counting Indexer", "definitionId": "fixture", "baseUrl": indexer.URL, "apiKey": "k",
	}, http.StatusCreated)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	got := getJSON[[]map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/search?q=%s", base, m.ID, "some+other+name"))
	if len(got) != 1 {
		t.Fatalf("listed %d releases for the typed words, want 1", len(got))
	}
	if q := queries(); len(q) != 1 {
		t.Errorf("made %d searches (%v), want 1", len(q), q)
	}
}
