package indexers_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write([]byte(s))
	w.Close()
	return buf.Bytes()
}

const oneItem = `<item><title>%s</title><guid>g1</guid><enclosure url="https://x.test/g1.nzb" length="1000" type="application/x-nzb"/></item>`

func feedWith(items string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>` + items + `</channel></rss>`
}

func itemTitled(title string) string { return strings.Replace(oneItem, "%s", title, 1) }

// Indexers, and the proxies in front of them, answer in many odd ways. A feed
// that can be read is read; one that cannot says what is wrong in plain words.
func TestNewznabClientReadsOddRealWorldReplies(t *testing.T) {
	good := itemTitled("Show.S01E01.1080p-GRP")
	latin1 := func(enc, title string) []byte {
		return []byte(strings.Replace(feedWith(itemTitled(title)), `encoding="UTF-8"`, `encoding="`+enc+`"`, 1))
	}
	cut := feedWith(good + good)
	cut = cut[:len(cut)-len("</channel></rss>")] + `<item><title>Cut.Of`
	tests := []struct {
		name      string
		header    map[string]string
		body      []byte
		wantTitle string // the first result's title; "" when an error is expected
		wantErr   string // part of the error text
	}{
		{"plain", nil, []byte(feedWith(good)), "Show.S01E01.1080p-GRP", ""},
		{"gzip without saying so", nil, gz(t, feedWith(good)), "Show.S01E01.1080p-GRP", ""},
		{"gzip and saying so", map[string]string{"Content-Encoding": "gzip"}, gz(t, feedWith(good)), "Show.S01E01.1080p-GRP", ""},
		{"byte order mark", nil, append([]byte("\xef\xbb\xbf"), feedWith(good)...), "Show.S01E01.1080p-GRP", ""},
		{"blank lines before the declaration", nil, []byte("\n\n  " + feedWith(good)), "Show.S01E01.1080p-GRP", ""},
		{"a byte that is not UTF-8", nil, []byte(feedWith(itemTitled("Amelie.2001.1080p.Am\xe9lie-GRP"))), "Amelie", ""},
		{"declared Latin-1 with Latin-1 text", nil, latin1("ISO-8859-1", "Am\xe9lie.2001.1080p-GRP"), "Amélie.2001.1080p-GRP", ""},
		{"declared windows-1252", nil, latin1("windows-1252", "Caf\xe9.2001-GRP"), "Café.2001-GRP", ""},
		{"a control character in a title", nil, []byte(feedWith(itemTitled("Bad\x08Title.2001-GRP"))), "Bad", ""},
		{"a null byte in a title", nil, []byte(feedWith(itemTitled("Nul\x00l.2001-GRP"))), "Nul", ""},
		{"a bare ampersand", nil, []byte(feedWith(itemTitled("Tom & Jerry.2001-GRP"))), "Tom & Jerry", ""},
		{"cut off half way", nil, []byte(cut), "Show.S01E01.1080p-GRP", ""},
		{"nothing at all", nil, nil, "", "nothing"},
		{"only white space", nil, []byte("  \n "), "", "nothing"},
		{"a web page", nil, []byte("<!DOCTYPE html><html><body>Login</body></html>"), "", "web page"},
		{"a web page with a declaration first", nil, []byte(`<?xml version="1.0"?><html><body>Just a moment...</body></html>`), "", "web page"},
		{"a json error", nil, []byte(`{"error":"Invalid API key"}`), "", "Invalid API key"},
		{"an error element", nil, []byte(`<?xml version="1.0"?><error code="100" description="Incorrect user credentials"/>`), "", "Incorrect user credentials"},
		{"an error with a code but no words", nil, []byte(`<?xml version="1.0"?><error code="429"/>`), "", "429"},
		{"plain text", nil, []byte("Too many requests, slow down"), "", "Too many requests"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tt.header {
					w.Header().Set(k, v)
				}
				w.Write(tt.body)
			}))
			defer srv.Close()
			client := indexers.NewNewznabClient("Odd", srv.URL, "secretkey123")
			results, err := client.Search(context.Background(), "show", nil)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("no error, got %d results", len(results))
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not mention %q", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), "secretkey123") {
					t.Errorf("error holds the key: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if len(results) == 0 || !strings.HasPrefix(results[0].Title, tt.wantTitle) {
				t.Fatalf("results %+v, want first title starting %q", results, tt.wantTitle)
			}
		})
	}
}

// A reply too big to read is refused with a sentence, not kept in memory.
func TestNewznabClientRefusesAHugeReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><rss><channel>`))
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for i := 0; i < 20; i++ {
			w.Write(chunk)
		}
	}))
	defer srv.Close()
	_, err := indexers.NewNewznabClient("Huge", srv.URL, "k").Search(context.Background(), "a", nil)
	if err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("error = %v", err)
	}
}

// Torznab feeds spell their attributes under their own namespace, repeat the
// category, count grabs, and may carry nothing but a magnet link.
func TestTorznabItemsReadAsTheyAreWritten(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel><atom:link rel="self" type="application/rss+xml"/>
<torznab:response offset="0" total="3"/>
<item><title>  Movie.2020.1080p.WEB-DL-GRP  </title><guid>https://tracker.test/details/1</guid><link>https://tracker.test/dl/1.torrent</link>
<pubDate>Tue, 3 Nov 2026 10:00:00 +0000</pubDate>
<enclosure url="https://tracker.test/dl/1.torrent" length="0" type="application/x-bittorrent"/>
<torznab:attr name="category" value="2000"/><torznab:attr name="category" value="2040"/><torznab:attr name="category" value="100001"/>
<torznab:attr name="seeders" value="12"/><torznab:attr name="peers" value="15"/><torznab:attr name="grabs" value="99"/>
<torznab:attr name="infohash" value="ABCDEF0123456789ABCDEF0123456789ABCDEF01"/><torznab:attr name="size" value="2147483648"/>
<torznab:attr name="downloadvolumefactor" value="0"/><torznab:attr name="uploadvolumefactor" value="1"/></item>
<item><title>Magnet.Only.2020-GRP</title><guid>m1</guid>
<torznab:attr name="magneturl" value="MAGNET:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=Magnet.Only"/>
<torznab:attr name="seeders" value="0"/><torznab:attr name="size" value="1000"/></item>
<item><title>Link.Is.Magnet-GRP</title><guid>m2</guid><link>magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98</link></item>
</channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	got, err := indexers.NewNewznabClient("T", srv.URL, "k").Search(context.Background(), "movie", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d results", len(got))
	}
	a := got[0]
	if a.Title != "Movie.2020.1080p.WEB-DL-GRP" || !a.Torrent || a.Seeders != 12 || !a.SeedersKnown || a.Peers != 15 || a.SizeBytes != 2147483648 ||
		a.Discount != 100 || a.InfoHash != "abcdef0123456789abcdef0123456789abcdef01" || a.GUID != "https://tracker.test/details/1" || a.InfoURL != "https://tracker.test/details/1" {
		t.Errorf("first: %+v", a)
	}
	if want := []int{2000, 2040, 100001}; len(a.Categories) != len(want) || a.Categories[0] != 2000 || a.Categories[1] != 2040 || a.Categories[2] != 100001 {
		t.Errorf("categories = %v, want every one the feed names: %v", a.Categories, want)
	}
	m := got[1]
	if !strings.HasPrefix(m.DownloadURL, "magnet:?xt=urn:btih:0123") || !m.Torrent || !m.SeedersKnown || m.Seeders != 0 {
		t.Errorf("magnet only: %+v", m)
	}
	if got[2].DownloadURL == "" || !got[2].Torrent {
		t.Errorf("link is a magnet: %+v", got[2])
	}
}
