package indexers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

// pagedFeed answers like an indexer with total releases that hands out at most
// 100 of them at a time, and counts the requests.
func pagedFeed(t *testing.T, total int, requests *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		*requests = append(*requests, fmt.Sprintf("limit=%d offset=%d", limit, offset))
		if limit > 100 || limit == 0 {
			limit = 100
		}
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
		fmt.Fprintf(&b, `<newznab:response offset="%d" total="%d"/>`, offset, total)
		for i := offset; i < offset+limit && i < total; i++ {
			fmt.Fprintf(&b, `<item><title>The.Batman.2022.%d.1080p.WEB-DL-GRP</title><guid>g%d</guid><enclosure url="https://fixture.test/nzb/%d" length="1000" type="application/x-nzb"/></item>`, i, i, i)
		}
		b.WriteString(`</channel></rss>`)
		_, _ = w.Write([]byte(b.String()))
	}))
}

// An indexer that has more releases than one page gets asked again, so a title
// with many releases is not cut short at the indexer's own page size.
func TestNewznabSearchReadsFurtherPages(t *testing.T) {
	var requests []string
	srv := pagedFeed(t, 250, &requests)
	defer srv.Close()
	client := indexers.NewNewznabClient("Paged", srv.URL, "key")
	got, err := client.Search(context.Background(), "the batman", []int{2000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 250 {
		t.Errorf("got %d releases, want 250 (requests: %v)", len(got), requests)
	}
	if client.ExtraRequests != 2 {
		t.Errorf("ExtraRequests = %d, want 2", client.ExtraRequests)
	}
	if len(requests) != 3 || requests[0] != "limit=100 offset=0" || requests[1] != "limit=100 offset=100" || requests[2] != "limit=100 offset=200" {
		t.Errorf("requests = %v", requests)
	}
}

// The reading stops at three pages, and an indexer that does not say how many
// there are is asked once.
func TestNewznabSearchStopsAtThreePagesAndNeedsATotal(t *testing.T) {
	var requests []string
	srv := pagedFeed(t, 900, &requests)
	defer srv.Close()
	client := indexers.NewNewznabClient("Paged", srv.URL, "key")
	got, err := client.Search(context.Background(), "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 300 || len(requests) != 3 {
		t.Errorf("got %d releases in %d requests, want 300 in 3", len(got), len(requests))
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(fixtureRSS))
	}))
	defer srv2.Close()
	one := indexers.NewNewznabClient("Plain", srv2.URL, "key")
	if got, err := one.Search(context.Background(), "x", nil); err != nil || len(got) != 1 || one.ExtraRequests != 0 {
		t.Errorf("plain feed: %d releases, err %v, extra %d", len(got), err, one.ExtraRequests)
	}
}

// A search a person started reads up to ten pages, so the list in Choose a
// release is not cut at 300.
func TestNewznabManualSearchReadsTenPages(t *testing.T) {
	var requests []string
	srv := pagedFeed(t, 1500, &requests)
	defer srv.Close()
	client := indexers.NewNewznabClient("Paged", srv.URL, "key")
	got, err := client.Search(indexers.AsManual(context.Background()), "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1000 || len(requests) != 10 {
		t.Errorf("got %d releases in %d requests, want 1000 in 10", len(got), len(requests))
	}
}
