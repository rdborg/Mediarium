package indexers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

// A Torznab feed handed out by Prowlarr looks like a Newznab one. Each result
// has to say for itself that it is a torrent, because the indexer may have
// been added as a Usenet indexer.
func TestNewznabResultsKnowWhenTheyAreTorrents(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel>
<item>
<title>Heat.1995.1080p.BluRay-GROUP</title>
<link>http://prowlarr.test/7/download?file=heat</link>
<enclosure url="http://prowlarr.test/7/download?file=heat" length="1000" type="application/x-bittorrent" />
<torznab:attr name="seeders" value="12"/>
</item>
<item>
<title>Ronin.1998.720p-GROUP</title>
<enclosure url="http://prowlarr.test/7/download?file=ronin" length="900" type="application/octet-stream" />
<torznab:attr name="infohash" value="ABCDEF0123456789"/>
<torznab:attr name="seeders" value="0"/>
</item>
<item>
<title>Collateral.2004-GROUP</title>
<link>http://prowlarr.test/7/details</link>
<torznab:attr name="magneturl" value="magnet:?xt=urn:btih:abcdef0123456789"/>
</item>
<item>
<title>Plain.Usenet.Release-GROUP</title>
<enclosure url="http://fixture.test/getnzb/abc.nzb" length="1234" type="application/x-nzb" />
</item>
</channel>
</rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()

	client := indexers.NewNewznabClient("Prowlarr", srv.URL, "key")
	results, err := client.Search(context.Background(), "heat", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4", len(results))
	}
	want := []struct {
		torrent bool
		url     string
	}{
		{true, "http://prowlarr.test/7/download?file=heat"},
		{true, "http://prowlarr.test/7/download?file=ronin"},
		{true, "magnet:?xt=urn:btih:abcdef0123456789"},
		{false, "http://fixture.test/getnzb/abc.nzb"},
	}
	for i, w := range want {
		if results[i].Torrent != w.torrent {
			t.Errorf("%s: Torrent = %v, want %v", results[i].Title, results[i].Torrent, w.torrent)
		}
		if results[i].DownloadURL != w.url {
			t.Errorf("%s: DownloadURL = %q, want %q", results[i].Title, results[i].DownloadURL, w.url)
		}
	}
}
