package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// shiftingList is a TMDB with a long list of numbered titles, 20 to a page,
// where every third title is on Netflix in the US. shift moves the whole list
// down: with shift 5, five new titles (ids from 100000) sit at the top, the way
// a trending list moves from one hour to the next.
type shiftingList struct {
	srv   *httptest.Server
	shift atomic.Int32
	lists atomic.Int32
}

func newShiftingList(t *testing.T) *shiftingList {
	f := &shiftingList{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 4 && parts[2] == "watch" {
			id, _ := strconv.Atoi(parts[1])
			us := "[]"
			if id%3 == 0 {
				us = `[{"provider_id":8}]`
			}
			fmt.Fprintf(w, `{"id":%d,"results":{"US":{"flatrate":%s}}}`, id, us)
			return
		}
		f.lists.Add(1)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		shift := int(f.shift.Load())
		var items []string
		for i := 1; i <= 20; i++ {
			pos := (page-1)*20 + i
			id := pos - shift
			if id < 1 {
				id = 100000 + pos
			}
			items = append(items, fmt.Sprintf(`{"id":%d,"title":"M%d"}`, id, id))
		}
		fmt.Fprintf(w, `{"page":%d,"total_pages":100,"total_results":2000,"results":[%s]}`, page, strings.Join(items, ","))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func pageIDs(p *MoviePage) []int {
	ids := make([]int, 0, len(p.Results))
	for _, m := range p.Results {
		ids = append(ids, m.TMDBID)
	}
	return ids
}

func sameIDs(a, b []int) bool {
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

// hiddenUSPage is filtered page n of the unshifted list with every third title left out.
func hiddenUSPage(n int) []int {
	var out []int
	seen := 0
	for id := 1; len(out) < 20; id++ {
		if id%3 == 0 {
			continue
		}
		seen++
		if seen > (n-1)*20 {
			out = append(out, id)
		}
	}
	return out
}

// testClock is a clock the page cache and the filtered lists both read.
func testClock(c *Client) func(time.Duration) {
	now := time.Now()
	c.pages.now = func() time.Time { return now }
	c.hides.now = func() time.Time { return now }
	return func(d time.Duration) { now = now.Add(d) }
}

// Someone reading a filtered list page by page, slower than the page cache
// keeps pages and while other lists fill it, still gets every page in order
// and the list does not end early.
func TestHiddenListPageStartsOutliveThePageCache(t *testing.T) {
	f := newShiftingList(t)
	c := NewWithBaseURL("k", f.srv.URL)
	advance := testClock(c)
	ctx := context.Background()
	hide := Hiding{Services: []string{"netflix"}, Region: "US"}

	for n := 1; n <= 12; n++ {
		p, err := c.MovieListHiding(ctx, ListTrending, n, hide)
		if err != nil {
			t.Fatalf("page %d: %v", n, err)
		}
		if got, want := pageIDs(p), hiddenUSPage(n); !sameIDs(got, want) {
			t.Fatalf("page %d = %v, want %v", n, got, want)
		}
		if p.TotalPages <= n {
			t.Fatalf("page %d says the list ends (total pages %d)", n, p.TotalPages)
		}
		advance(70 * time.Second) // a person looking at the page
		// Other lists and pages push everything out of the page cache.
		for k := 0; k < pageCacheEntries+10; k++ {
			c.pages.put(fmt.Sprintf("/other?page=%d-%d", n, k), &MoviePage{})
		}
	}
}

// A page asked for again shows the same titles even after TMDB's list moved
// on: the page starts point into the source pages they were worked out from,
// not into newer copies of those pages.
func TestHiddenListPageStartsStayWithTheirSourcePages(t *testing.T) {
	f := newShiftingList(t)
	c := NewWithBaseURL("k", f.srv.URL)
	advance := testClock(c)
	ctx := context.Background()
	hide := Hiding{Services: []string{"netflix"}, Region: "US"}

	var first [][]int
	for n := 1; n <= 3; n++ {
		p, err := c.MovieListHiding(ctx, ListTrending, n, hide)
		if err != nil {
			t.Fatal(err)
		}
		first = append(first, pageIDs(p))
	}
	f.shift.Store(7)
	advance(pageCacheTTL + time.Minute)
	for n := 1; n <= 3; n++ {
		p, err := c.MovieListHiding(ctx, ListTrending, n, hide)
		if err != nil {
			t.Fatal(err)
		}
		if got := pageIDs(p); !sameIDs(got, first[n-1]) {
			t.Errorf("page %d after the list moved = %v, want %v as before", n, got, first[n-1])
		}
	}
}

// When the page starts of a list have been dropped (nobody read it for a long
// time), a deep page is worked out again a few pages at a time; it never says
// the list has ended.
func TestHiddenListOutOfReachPageDoesNotEndTheList(t *testing.T) {
	f := newShiftingList(t)
	c := NewWithBaseURL("k", f.srv.URL)
	advance := testClock(c)
	ctx := context.Background()
	hide := Hiding{Services: []string{"netflix"}, Region: "US"}

	for n := 1; n <= 9; n++ {
		if _, err := c.MovieListHiding(ctx, ListTrending, n, hide); err != nil {
			t.Fatal(err)
		}
	}
	advance(hideStateIdle + time.Minute)

	before := f.lists.Load()
	p, err := c.MovieListHiding(ctx, ListTrending, 10, hide)
	if err != nil {
		t.Fatal(err)
	}
	if p.TotalPages <= 10 {
		t.Fatalf("page 10 after the page starts were dropped: %d titles, total pages %d (the list ends early)", len(p.Results), p.TotalPages)
	}
	if got := f.lists.Load() - before; got > hideCatchUpPages*hideSourcePagesPerPage+hideSourcePagesPerPage {
		t.Errorf("working page 10 out again read %d source pages", got)
	}
	for tries := 0; len(p.Results) == 0 && tries < 3; tries++ {
		if p, err = c.MovieListHiding(ctx, ListTrending, 10, hide); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := pageIDs(p), hiddenUSPage(10); !sameIDs(got, want) {
		t.Errorf("page 10 = %v, want %v", got, want)
	}
}
