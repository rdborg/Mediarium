package indexers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

// An indexer (or Prowlarr) that ignores the offset sends the same page for every
// page asked, and puts a fresh token in each download link. The list must show
// each release once (GitHub #188).
func TestNewznabSearchIgnoresRepeatedPages(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
		b.WriteString(`<newznab:response offset="0" total="300"/>`)
		for i := 0; i < 100; i++ {
			fmt.Fprintf(&b, `<item><title>Some.Show.S01E01.%d.1080p-GRP</title><guid>g%d</guid><enclosure url="https://fixture.test/dl/%d?token=%d" length="1000" type="application/x-bittorrent"/></item>`, i, i, i, n)
		}
		b.WriteString(`</channel></rss>`)
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()
	client := indexers.NewNewznabClient("Prowlarr one", srv.URL, "key")
	got, err := client.Search(indexers.AsManual(context.Background()), "some show", []int{5000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 100 {
		t.Errorf("got %d releases, want 100", len(got))
	}
	if hits.Load() != 2 {
		t.Errorf("%d requests, want 2: the repeated page ends the reading", hits.Load())
	}
}

func TestMergeResultsKeepsEachReleaseOncePerIndexer(t *testing.T) {
	res := func(indexer, title, guid, link, hash string) indexers.Result {
		return indexers.Result{IndexerName: indexer, Title: title, GUID: guid, DownloadURL: link, InfoHash: hash}
	}
	tests := []struct {
		name string
		in   [][]indexers.Result // one outcome each
		want int
	}{
		{"same link twice in one answer", [][]indexers.Result{{res("A", "T", "", "http://x/1", ""), res("A", "T", "", "http://x/1", "")}}, 1},
		{"same link, two queries", [][]indexers.Result{{res("A", "T", "", "http://x/1", "")}, {res("A", "T", "", "http://x/1", "")}}, 1},
		{"same guid, links differ", [][]indexers.Result{{res("A", "T", "g1", "http://x/1?t=1", "")}, {res("A", "T", "g1", "http://x/1?t=2", "")}}, 1},
		{"same hash, links and guids differ", [][]indexers.Result{{res("A", "T", "g1", "http://x/1", "ABCD")}, {res("A", "T", "g2", "magnet:?xt=2", "abcd")}}, 1},
		{"same link, title spelled another way", [][]indexers.Result{{res("A", "T one", "", "http://x/1", "")}, {res("A", "T.one", "", "http://x/1", "")}}, 1},
		{"same release on two indexers stays", [][]indexers.Result{{res("A", "T", "g1", "http://a/1", "")}, {res("B", "T", "g1", "http://b/1", "")}}, 2},
		{"same name, different guid and link stays", [][]indexers.Result{{res("A", "T", "g1", "http://x/1", "")}, {res("A", "T", "g2", "http://x/2", "")}}, 2},
		{"nothing to compare stays", [][]indexers.Result{{res("A", "T", "", "", ""), res("A", "T", "", "", "")}}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var outcomes []indexers.Outcome
			for _, rs := range tt.in {
				outcomes = append(outcomes, indexers.Outcome{IndexerName: rs[0].IndexerName, Results: rs})
			}
			if got := indexers.MergeResults(outcomes); len(got) != tt.want {
				t.Errorf("got %d releases, want %d", len(got), tt.want)
			}
		})
	}
}
