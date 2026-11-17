package metadata_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
)

// fakeLists is a TMDB with one list of numbered titles, 20 to a page, and a
// watch provider answer for each: every third title is on Netflix in Germany,
// every fifth only on Netflix in the US, every tenth can be bought on Prime
// Video in Germany. It counts what it is asked.
type fakeLists struct {
	srv        *httptest.Server
	totalPages int
	lists      atomic.Int32
	providers  atomic.Int32
	failID     int // a title whose provider lookup answers 500
	allNetflix bool

	mu        sync.Mutex
	listPaths []string
}

func (f *fakeLists) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.listPaths...)
}

func newFakeLists(t *testing.T, totalPages int) *fakeLists {
	f := &fakeLists{totalPages: totalPages}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 4 && parts[2] == "watch" {
			f.providers.Add(1)
			id, _ := strconv.Atoi(parts[1])
			if id == f.failID {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			de, us, buy := "[]", "[]", "[]"
			if id%3 == 0 || f.allNetflix {
				de = `[{"provider_id":8}]`
			}
			if id%5 == 0 {
				us = `[{"provider_id":8}]`
			}
			if id%10 == 0 {
				buy = `[{"provider_id":9}]`
			}
			fmt.Fprintf(w, `{"id":%d,"results":{"DE":{"flatrate":%s,"buy":%s},"US":{"flatrate":%s}}}`, id, de, buy, us)
			return
		}
		f.lists.Add(1)
		f.mu.Lock()
		f.listPaths = append(f.listPaths, r.URL.Path)
		f.mu.Unlock()
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 || page > f.totalPages {
			http.Error(w, "bad page", http.StatusUnprocessableEntity)
			return
		}
		var items []string
		for i := 1; i <= 20; i++ {
			id := (page-1)*20 + i
			items = append(items, fmt.Sprintf(`{"id":%d,"title":"M%d","name":"S%d"}`, id, id, id))
		}
		fmt.Fprintf(w, `{"page":%d,"total_pages":%d,"total_results":%d,"results":[%s]}`, page, f.totalPages, f.totalPages*20, strings.Join(items, ","))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func movieIDs(p *metadata.MoviePage) []int {
	var ids []int
	for _, m := range p.Results {
		ids = append(ids, m.TMDBID)
	}
	return ids
}

// wantPages is the list 1..n in order without the titles hidden rejects, cut into pages of 20.
func wantPages(n int, hidden func(id int) bool) [][]int {
	var pages [][]int
	var cur []int
	for id := 1; id <= n; id++ {
		if hidden(id) {
			continue
		}
		cur = append(cur, id)
		if len(cur) == 20 {
			pages = append(pages, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		pages = append(pages, cur)
	}
	return pages
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The filtered list is TMDB's own list in its own order with the hidden titles
// taken out, the gaps filled from what follows, and no title twice.
func TestHiddenListKeepsOrderAndFillsGaps(t *testing.T) {
	f := newFakeLists(t, 10)
	c := metadata.NewWithBaseURL("k", f.srv.URL)
	ctx := context.Background()
	hide := metadata.Hiding{Services: []string{"netflix"}, Region: "de"}

	want := wantPages(200, func(id int) bool { return id%3 == 0 })
	seen := map[int]bool{}
	for n := 1; n <= 4; n++ {
		p, err := c.MovieListHiding(ctx, metadata.ListTrending, n, hide)
		if err != nil {
			t.Fatalf("page %d: %v", n, err)
		}
		got := movieIDs(p)
		if !equalInts(got, want[n-1]) {
			t.Fatalf("page %d = %v, want %v", n, got, want[n-1])
		}
		if len(got) != 20 || p.Page != n || p.TotalPages != n+1 {
			t.Errorf("page %d: %d titles, page %d of %d", n, len(got), p.Page, p.TotalPages)
		}
		for _, id := range got {
			if seen[id] {
				t.Errorf("title %d is on two pages", id)
			}
			seen[id] = true
		}
	}
	for _, path := range f.paths() {
		if path != "/trending/movie/week" {
			t.Errorf("asked %s; the list should be TMDB's own trending list", path)
		}
	}
}

// A page costs the source pages it reads plus one provider lookup per title it
// looks at, once; asking again, or asking another list with the same titles,
// costs nothing more.
func TestHiddenListRequestsAreBoundedAndCached(t *testing.T) {
	f := newFakeLists(t, 10)
	c := metadata.NewWithBaseURL("k", f.srv.URL)
	ctx := context.Background()
	hide := metadata.Hiding{Services: []string{"netflix"}, Region: "DE"}

	if _, err := c.MovieListHiding(ctx, metadata.ListTrending, 1, hide); err != nil {
		t.Fatal(err)
	}
	// 20 survivors take the first 30 titles: two source pages, and the 40
	// titles on them are looked up once each.
	if got := f.lists.Load(); got != 2 {
		t.Errorf("list requests for page 1 = %d, want 2", got)
	}
	if got := f.providers.Load(); got != 40 {
		t.Errorf("provider requests for page 1 = %d, want 40", got)
	}

	lists, provs := f.lists.Load(), f.providers.Load()
	if _, err := c.MovieListHiding(ctx, metadata.ListTrending, 1, hide); err != nil {
		t.Fatal(err)
	}
	if f.lists.Load() != lists || f.providers.Load() != provs {
		t.Errorf("asking for page 1 again made %d list and %d provider requests", f.lists.Load()-lists, f.providers.Load()-provs)
	}

	// Page 2 starts part way through source page 2 and reads on from there.
	if _, err := c.MovieListHiding(ctx, metadata.ListTrending, 2, hide); err != nil {
		t.Fatal(err)
	}
	if got := f.lists.Load() - lists; got > 2 {
		t.Errorf("page 2 read %d new source pages, want at most 2", got)
	}

	// The popular list holds the same titles here: their providers are known.
	provs = f.providers.Load()
	if _, err := c.MovieListHiding(ctx, metadata.ListPopular, 1, hide); err != nil {
		t.Fatal(err)
	}
	if got := f.providers.Load() - provs; got != 0 {
		t.Errorf("a second list with the same titles looked up %d providers again", got)
	}
	paths := f.paths()
	if last := paths[len(paths)-1]; last != "/movie/popular" {
		t.Errorf("popular was read from %s", last)
	}
}

// Only the region's services and only the chosen ones hide a title; renting
// or buying on a chosen service counts like streaming, as it does in TMDB's
// own filter.
func TestHiddenListUsesRegionAndChosenServices(t *testing.T) {
	f := newFakeLists(t, 10)
	c := metadata.NewWithBaseURL("k", f.srv.URL)
	ctx := context.Background()

	us, err := c.MovieListHiding(ctx, metadata.ListTrending, 1, metadata.Hiding{Services: []string{"netflix"}, Region: "US"})
	if err != nil {
		t.Fatal(err)
	}
	if want := wantPages(200, func(id int) bool { return id%5 == 0 })[0]; !equalInts(movieIDs(us), want) {
		t.Errorf("US = %v, want %v", movieIDs(us), want)
	}
	both, err := c.MovieListHiding(ctx, metadata.ListTrending, 1, metadata.Hiding{Services: []string{"netflix", "prime"}, Region: "DE"})
	if err != nil {
		t.Fatal(err)
	}
	if want := wantPages(200, func(id int) bool { return id%3 == 0 || id%10 == 0 })[0]; !equalInts(movieIDs(both), want) {
		t.Errorf("Netflix and Prime = %v, want %v", movieIDs(both), want)
	}
}

// The end of the list: the last page is short and says there is no more, and a
// page after it is empty. TMDB is never asked for a page it does not have.
func TestHiddenListEnds(t *testing.T) {
	f := newFakeLists(t, 2)
	c := metadata.NewWithBaseURL("k", f.srv.URL)
	ctx := context.Background()
	hide := metadata.Hiding{Services: []string{"netflix"}, Region: "DE"}

	want := wantPages(40, func(id int) bool { return id%3 == 0 })
	p1, err := c.MovieListHiding(ctx, metadata.ListTrending, 1, hide)
	if err != nil || len(p1.Results) != 20 || p1.TotalPages != 2 {
		t.Fatalf("page 1: %v, %d titles, %d pages", err, len(p1.Results), p1.TotalPages)
	}
	p2, err := c.MovieListHiding(ctx, metadata.ListTrending, 2, hide)
	if err != nil {
		t.Fatal(err)
	}
	if !equalInts(movieIDs(p2), want[1]) || p2.TotalPages != 2 {
		t.Errorf("page 2 = %v of %d, want %v of 2", movieIDs(p2), p2.TotalPages, want[1])
	}
	p3, err := c.MovieListHiding(ctx, metadata.ListTrending, 3, hide)
	if err != nil || len(p3.Results) != 0 {
		t.Errorf("page 3: %v, %d titles, want none", err, len(p3.Results))
	}
	if got := f.lists.Load(); got != 2 {
		t.Errorf("TMDB was asked for %d list pages, want 2", got)
	}
}

// When almost everything is hidden a page is returned short after a few source
// pages instead of reading the whole list, and a page far beyond what has been
// read is only worked out a few pages at a time, without ending the list.
func TestHiddenListStaysBounded(t *testing.T) {
	f := newFakeLists(t, 500)
	f.allNetflix = true
	c := metadata.NewWithBaseURL("k", f.srv.URL)
	ctx := context.Background()
	hide := metadata.Hiding{Services: []string{"netflix"}, Region: "DE"}

	p, err := c.MovieListHiding(ctx, metadata.ListTrending, 1, hide)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Results) != 0 || p.TotalPages != 2 {
		t.Errorf("got %d titles and %d pages", len(p.Results), p.TotalPages)
	}
	if got := f.lists.Load(); got != 4 {
		t.Errorf("read %d source pages, want 4", got)
	}

	before := f.lists.Load()
	far, err := c.MovieListHiding(ctx, metadata.ListTrending, 300, hide)
	if err != nil || len(far.Results) != 0 || far.TotalPages <= 300 {
		t.Errorf("page 300: %v, %d titles, %d pages", err, len(far.Results), far.TotalPages)
	}
	// Six filtered pages of four source pages each, at most.
	if got := f.lists.Load() - before; got > 24 {
		t.Errorf("a page far beyond the known ones read %d source pages", got)
	}
}

// A title whose providers cannot be read stays in the list rather than
// leaving a hole.
func TestHiddenListKeepsTitlesItCannotCheck(t *testing.T) {
	f := newFakeLists(t, 10)
	f.failID = 6 // would be hidden (6 is a multiple of 3)
	c := metadata.NewWithBaseURL("k", f.srv.URL)
	hide := metadata.Hiding{Services: []string{"netflix"}, Region: "DE"}

	p, err := c.MovieListHiding(context.Background(), metadata.ListTrending, 1, hide)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range movieIDs(p) {
		if id == 6 {
			found = true
		}
		if id == 3 || id == 9 {
			t.Errorf("title %d should be hidden", id)
		}
	}
	if !found {
		t.Error("title 6 was dropped though its providers could not be read")
	}
}

func TestHiddenShowList(t *testing.T) {
	f := newFakeLists(t, 10)
	c := metadata.NewWithBaseURL("k", f.srv.URL)
	hide := metadata.Hiding{Services: []string{"netflix"}, Region: "DE"}

	p, err := c.TVListHiding(context.Background(), metadata.ListTrending, 1, hide)
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, s := range p.Results {
		got = append(got, s.TMDBID)
	}
	if want := wantPages(200, func(id int) bool { return id%3 == 0 })[0]; !equalInts(got, want) {
		t.Errorf("shows = %v, want %v", got, want)
	}
	if paths := f.paths(); paths[0] != "/trending/tv/week" {
		t.Errorf("asked %s", paths[0])
	}
}
