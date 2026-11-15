package indexers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

func TestSafeInfoURL(t *testing.T) {
	tests := []struct {
		name, raw, download, want string
	}{
		{"plain details page", "https://tracker.example/details/42", "", "https://tracker.example/details/42"},
		{"torrent page with an id", "https://tracker.example/torrent.php?id=42&hit=1", "", "https://tracker.example/torrent.php?id=42&hit=1"},
		{"fragment is dropped", "https://nzb.example/details/abc123#comments", "", "https://nzb.example/details/abc123"},
		{"http is fine", "http://tracker.example:8080/t/7", "", "http://tracker.example:8080/t/7"},
		{"spaces around", "  https://tracker.example/t/7 \n", "", "https://tracker.example/t/7"},
		{"32 hex id in the path is a guid, not a key", "https://nzb.example/details/0123456789abcdef0123456789abcdef", "", "https://nzb.example/details/0123456789abcdef0123456789abcdef"},

		{"empty", "", "", ""},
		{"a bare guid", "abc123", "", ""},
		{"magnet", "magnet:?xt=urn:btih:abc", "", ""},
		{"javascript", "javascript:alert(1)", "", ""},
		{"data", "data:text/html,hi", "", ""},
		{"ftp", "ftp://tracker.example/x", "", ""},
		{"file", "file:///etc/passwd", "", ""},
		{"no host", "https:///details/1", "", ""},
		{"name and password", "https://user:pw@tracker.example/t/1", "", ""},
		{"apikey", "https://nzb.example/details/1?apikey=SECRET", "", ""},
		{"apikey upper case", "https://nzb.example/details/1?ApiKey=SECRET", "", ""},
		{"api_key", "https://nzb.example/details/1?api_key=SECRET", "", ""},
		{"passkey", "https://tracker.example/details.php?id=1&passkey=SECRET", "", ""},
		{"rsskey", "https://tracker.example/details.php?id=1&rsskey=SECRET", "", ""},
		{"torrent_pass", "https://tracker.example/details.php?id=1&torrent_pass=SECRET", "", ""},
		{"authkey", "https://tracker.example/torrents.php?id=1&authkey=A&torrent_pass=B", "", ""},
		{"token", "https://tracker.example/t/1?token=SECRET", "", ""},
		{"a made up name with apikey in it", "https://tracker.example/t/1?myapikey=SECRET", "", ""},
		{"newznab download key", "https://nzb.example/getnzb/abc.nzb&i=1&r=SECRET", "", ""},
		{"newznab r= key", "https://nzb.example/details/abc?r=SECRET", "", ""},
		{"t=get on the api", "https://nzb.example/api?t=get&id=1", "", ""},
		{"t=getnzb", "https://nzb.example/x?t=getnzb&id=1", "", ""},
		{"api path", "https://nzb.example/api/details/1", "", ""},
		{"download path", "https://tracker.example/download.php/12/file.torrent", "", ""},
		{"torrent file", "https://tracker.example/files/a.torrent", "", ""},
		{"nzb file", "https://nzb.example/files/a.nzb", "", ""},
		{"the download link itself", "https://tracker.example/t/1", "https://tracker.example/t/1", ""},
		{"the download link itself, slash differs", "https://tracker.example/t/1/", "https://tracker.example/t/1", ""},
		{"control character", "https://tracker.example/t/1\x00", "", ""},
		{"inner space", "https://tracker.example/t /1", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := indexers.SafeInfoURL(tc.raw, tc.download); got != tc.want {
				t.Errorf("SafeInfoURL(%q, %q) = %q, want %q", tc.raw, tc.download, got, tc.want)
			}
		})
	}
}

const torznabInfoFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel>
<item>
<title>Film.2024.1080p.WEB-DL.x264-GRP</title>
<guid>https://tracker.example/details/1</guid>
<comments>https://tracker.example/details/1#comments</comments>
<enclosure url="https://tracker.example/download/1.torrent?passkey=SECRET" length="1000" type="application/x-bittorrent" />
<torznab:attr name="seeders" value="5"/>
</item>
<item>
<title>Film.2024.720p.WEB-DL.x264-GRP</title>
<guid>https://tracker.example/details/2</guid>
<enclosure url="https://tracker.example/download/2.torrent?passkey=SECRET" length="1000" type="application/x-bittorrent" />
<torznab:attr name="seeders" value="5"/>
</item>
<item>
<title>Film.2024.480p.WEB-DL.x264-GRP</title>
<guid>https://tracker.example/api?t=get&amp;id=3&amp;apikey=SECRET</guid>
<enclosure url="https://tracker.example/download/3.torrent?passkey=SECRET" length="1000" type="application/x-bittorrent" />
</item>
</channel>
</rss>`

// A Torznab feed gives the page in <comments> or, without it, in <guid>; what the
// UI then gets goes through SafeInfoURL, so the key in the guid of the third item
// and in every download link never leaves the server.
func TestTorznabInfoURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(torznabInfoFeed))
	}))
	defer srv.Close()
	client := indexers.NewNewznabClient("Tracker", srv.URL, "fixture-key")
	results, err := client.Search(context.Background(), "film", []int{2000})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	want := []string{"https://tracker.example/details/1", "https://tracker.example/details/2", ""}
	for i, r := range results {
		got := indexers.SafeInfoURL(r.InfoURL, r.DownloadURL)
		if got != want[i] {
			t.Errorf("result %d: info url %q (from %q), want %q", i, got, r.InfoURL, want[i])
		}
	}
}
