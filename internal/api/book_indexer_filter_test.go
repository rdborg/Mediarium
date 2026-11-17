package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

// fakeBookSite is a small public site a definition drives. delay is how long
// it takes to answer (0 for quick); hits counts the searches it was asked.
type fakeBookSite struct {
	*httptest.Server
	hits atomic.Int32
	fail atomic.Bool // answer with a server error
}

func newFakeBookSite(t *testing.T, title string, delay time.Duration) *fakeBookSite {
	t.Helper()
	site := &fakeBookSite{}
	site.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/") {
			http.NotFound(w, r)
			return
		}
		site.hits.Add(1)
		if site.fail.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		fmt.Fprintf(w, `<table class="results"><tbody><tr><td class="name"><a href="/t/1">%s</a></td><td class="dl"><a href="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567">m</a></td><td class="size">5 MB</td><td class="seeds">12</td><td class="leeches">1</td></tr></tbody></table>`, title)
	}))
	t.Cleanup(site.Close)
	return site
}

// bookSiteDefinition is a definition for a public site that lists one category.
func bookSiteDefinition(id, category, siteURL string) []byte {
	return []byte(fmt.Sprintf(`id: %s
name: %s
description: test site
language: en-US
type: public
encoding: UTF-8
links:
  - %s/
caps:
  categorymappings:
    - {id: 7, cat: %s, desc: "Category"}
  modes:
    search: [q]
search:
  paths:
    - path: "search/{{ .Keywords }}"
  rows:
    selector: table.results > tbody > tr
  fields:
    title: {selector: td.name a}
    category: {text: "7"}
    details: {selector: td.name a, attribute: href}
    download: {selector: td.dl a, attribute: href}
    size: {selector: td.size}
    seeders: {selector: td.seeds}
    leechers: {selector: td.leeches}
    downloadvolumefactor: {text: 0}
    uploadvolumefactor: {text: 1}
`, id, id, siteURL, category))
}

type bookAnswer struct {
	Results []struct {
		Title       string `json:"title"`
		IndexerName string `json:"indexerName"`
	} `json:"results"`
	Sources  int      `json:"sources"`
	Skipped  []string `json:"skipped"`
	Searched []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Code   string `json:"code"`
		Count  int    `json:"count"`
	} `json:"searched"`
	Unanswered []struct {
		Name string `json:"name"`
	} `json:"unanswered"`
}

// addFakeBookSite registers the site's definition and adds it as an indexer.
func addFakeBookSite(t *testing.T, srv *api.Server, base string, client *http.Client, id, category string, site *fakeBookSite) {
	t.Helper()
	if _, err := srv.Definitions.AddCustom(bookSiteDefinition(id, category, site.URL)); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"definitionId": id, "name": id, "baseUrl": site.URL + "/", "protocol": "torrent", "settings": map[string]any{},
	}, http.StatusCreated)
}

// newBookForSearch sets up ebooks (and audiobooks when both is true) with
// The Hobbit in the library. add registers a fake site as an indexer.
func newBookForSearch(t *testing.T, both bool) (client *http.Client, bookURL string, setWait func(time.Duration), add func(id, category string, site *fakeBookSite)) {
	t.Helper()
	server, base, c := loginNewServer(t)
	server.Cardigann.DefaultDelay = 0
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	root := t.TempDir()
	for _, d := range []string{"ebooks", "audiobooks"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	postJSONMethod[map[string]any](t, c, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true, "audiobooks": both}, http.StatusOK)
	postJSONMethod[map[string]any](t, c, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": filepath.Join(root, "ebooks"), "audiobooksPath": filepath.Join(root, "audiobooks")}, http.StatusOK)
	book := postJSON[map[string]any](t, c, base+"/api/books", map[string]any{"olKey": "OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "year": 1937, "ebook": true, "audiobook": both}, http.StatusCreated)
	return c, fmt.Sprintf("%s/api/books/%v", base, book["id"]), server.TestSetBookWait, func(id, category string, site *fakeBookSite) {
		addFakeBookSite(t, server, base, c, id, category, site)
	}
}

// A search for a book asks the sites that list books and leaves the movie
// sites alone, so it is not held up by them.
func TestBookReleasesSkipSitesThatDoNotDoBooks(t *testing.T) {
	client, bookURL, _, add := newBookForSearch(t, false)
	books := newFakeBookSite(t, "J.R.R. Tolkien - The Hobbit (1937) [EPUB]", 0)
	movies1 := newFakeBookSite(t, "Some.Movie.2020.1080p", 20*time.Second)
	movies2 := newFakeBookSite(t, "Other.Movie.2021.1080p", 20*time.Second)
	add("booksite", "Books/EBook", books)
	add("movies-one", "Movies/HD", movies1)
	add("movies-two", "TV/HD", movies2)

	start := time.Now()
	got := getJSON[bookAnswer](t, client, bookURL+"/releases?format=ebook&detail=1")
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("the search took %v: it waited for the movie sites", took)
	}
	if movies1.hits.Load() != 0 || movies2.hits.Load() != 0 {
		t.Errorf("movie sites were asked: %d and %d times", movies1.hits.Load(), movies2.hits.Load())
	}
	if books.hits.Load() == 0 {
		t.Error("the book site was not asked")
	}
	if len(got.Results) != 1 || got.Results[0].IndexerName != "booksite" {
		t.Errorf("results: %+v", got.Results)
	}
	if got.Sources != 1 || len(got.Skipped) != 2 || len(got.Unanswered) != 0 {
		t.Errorf("sources %d, skipped %v, unanswered %v; want 1, two skipped, none unanswered", got.Sources, got.Skipped, got.Unanswered)
	}
	// Every indexer is listed: the one that answered, and the two left out.
	lines := map[string]string{}
	for _, l := range got.Searched {
		lines[l.Name] = l.Status + "/" + l.Code
	}
	if len(lines) != 3 || lines["booksite"] != "found/" || lines["movies-one"] != "skipped/no_category" || lines["movies-two"] != "skipped/no_category" {
		t.Errorf("searched: %v", lines)
	}
	// The plain list still works for scripts.
	plain := getJSON[[]map[string]any](t, client, bookURL+"/releases?format=ebook")
	if len(plain) != 1 {
		t.Errorf("plain list: %v", plain)
	}
}

// Audiobooks ask for the audiobook category: a book site that has no audio is skipped.
func TestBookReleasesAudiobooksAskAudioSites(t *testing.T) {
	client, bookURL, _, add := newBookForSearch(t, true)
	ebookSite := newFakeBookSite(t, "J.R.R. Tolkien - The Hobbit (1937) [EPUB]", 0)
	audioSite := newFakeBookSite(t, "J.R.R. Tolkien - The Hobbit (1937) [M4B]", 0)
	add("ebooksite", "Books/EBook", ebookSite)
	add("audiosite", "Audio/Audiobook", audioSite)
	got := getJSON[bookAnswer](t, client, bookURL+"/releases?format=audiobook&detail=1")
	if ebookSite.hits.Load() != 0 || audioSite.hits.Load() == 0 {
		t.Errorf("ebook site asked %d times, audio site %d times", ebookSite.hits.Load(), audioSite.hits.Load())
	}
	if len(got.Skipped) != 1 || got.Skipped[0] != "ebooksite" {
		t.Errorf("skipped: %v", got.Skipped)
	}
}

// When a site that does list books is slow, the search stops at its deadline
// and shows what the quick ones found, instead of failing.
func TestBookReleasesReturnWhatIsThereAtTheDeadline(t *testing.T) {
	client, bookURL, setWait, add := newBookForSearch(t, false)
	setWait(1500 * time.Millisecond)
	quick := newFakeBookSite(t, "J.R.R. Tolkien - The Hobbit (1937) [EPUB]", 0)
	slow := newFakeBookSite(t, "J.R.R. Tolkien - The Hobbit (1937) [EPUB] slow", 20*time.Second)
	add("quick", "Books/EBook", quick)
	add("slow", "Books", slow)

	start := time.Now()
	got := getJSON[bookAnswer](t, client, bookURL+"/releases?format=ebook&detail=1")
	if took := time.Since(start); took > 8*time.Second {
		t.Fatalf("the search took %v with a deadline of 1.5 seconds", took)
	}
	if len(got.Results) != 1 || got.Results[0].IndexerName != "quick" {
		t.Errorf("results: %+v", got.Results)
	}
	if len(got.Unanswered) != 1 || got.Unanswered[0].Name != "slow" {
		t.Errorf("unanswered: %+v", got.Unanswered)
	}
	if slow.hits.Load() != 1 {
		t.Errorf("the slow site was asked %d times; the second pass must not ask it again", slow.hits.Load())
	}
}

// When nobody has the author in the name, the second pass (title alone) only
// asks the sites that answered the first time.
func TestBookReleasesSecondPassSkipsSitesThatFailed(t *testing.T) {
	client, bookURL, _, add := newBookForSearch(t, false)
	empty := newFakeBookSite(t, "Unrelated Title (2001) [EPUB]", 0)
	broken := newFakeBookSite(t, "Unrelated Title (2001) [EPUB]", 0)
	broken.fail.Store(true)
	add("empty", "Books/EBook", empty)
	add("broken", "Books/EBook", broken)

	got := getJSON[bookAnswer](t, client, bookURL+"/releases?format=ebook&detail=1")
	if len(got.Results) != 0 {
		t.Errorf("results: %+v", got.Results)
	}
	if empty.hits.Load() != 2 {
		t.Errorf("the working site was asked %d times, want both passes", empty.hits.Load())
	}
	if broken.hits.Load() != 1 {
		t.Errorf("the broken site was asked %d times, want once: the second pass must leave it out", broken.hits.Load())
	}
	if len(got.Unanswered) != 1 || got.Unanswered[0].Name != "broken" {
		t.Errorf("unanswered: %+v", got.Unanswered)
	}
}

// The "Searches used" part of a book search says how many releases were left
// out for being another book or only the other format.
func TestBookReleasesSayWhatWasLeftOut(t *testing.T) {
	client, bookURL, _, add := newBookForSearch(t, false)
	add("right", "Books/EBook", newFakeBookSite(t, "J.R.R. Tolkien - The Hobbit (1937) [EPUB]", 0))
	add("other-book", "Books/EBook", newFakeBookSite(t, "Some Writer - A Different Story (2001) [EPUB]", 0))
	add("audio", "Books/EBook", newFakeBookSite(t, "J.R.R. Tolkien - The Hobbit (1937) [M4B]", 0))
	got := getJSON[struct {
		Results  []map[string]any `json:"results"`
		Ignored  int              `json:"ignored"`
		Filtered []struct {
			Reason string `json:"reason"`
			Count  int    `json:"count"`
		} `json:"filtered"`
	}](t, client, bookURL+"/releases?format=ebook&detail=1")
	if len(got.Results) != 1 {
		t.Fatalf("results: %v", got.Results)
	}
	counts := map[string]int{}
	for _, f := range got.Filtered {
		counts[f.Reason] = f.Count
	}
	if got.Ignored != 1 || counts["not this title"] != 1 || counts["Only in the other format (an audiobook for an ebook, or the other way round)."] != 1 {
		t.Fatalf("ignored %d, filtered %+v", got.Ignored, got.Filtered)
	}
}
