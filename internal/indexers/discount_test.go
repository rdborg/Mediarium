package indexers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscountFromFactor(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"0", 100},
		{"0.0", 100},
		{"0.25", 75},
		{"0.5", 50},
		{"0.75", 25},
		{" 0.5 ", 50},
		{"1", 0},
		{"1.0", 0},
		{"2", 0},
		{"", 0},
		{"free", 0},
		{"-1", 0},
		{"NaN", 0},
		{"0.999", 0},
	}
	for _, c := range cases {
		if got := discountFromFactor(c.in); got != c.want {
			t.Errorf("discountFromFactor(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

const torznabDiscountFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel>
<item><title>Free.Movie.2020.1080p-GRP</title><enclosure url="https://t.test/1.torrent" length="100" type="application/x-bittorrent"/>
<torznab:attr name="seeders" value="5"/><torznab:attr name="downloadvolumefactor" value="0"/><torznab:attr name="uploadvolumefactor" value="1"/></item>
<item><title>Half.Movie.2020.1080p-GRP</title><enclosure url="https://t.test/2.torrent" length="100" type="application/x-bittorrent"/>
<torznab:attr name="seeders" value="5"/><torznab:attr name="downloadvolumefactor" value="0.5"/></item>
<item><title>Normal.Movie.2020.1080p-GRP</title><enclosure url="https://t.test/3.torrent" length="100" type="application/x-bittorrent"/>
<torznab:attr name="seeders" value="5"/><torznab:attr name="downloadvolumefactor" value="1"/></item>
<item><title>Silent.Movie.2020.1080p-GRP</title><enclosure url="https://t.test/4.torrent" length="100" type="application/x-bittorrent"/>
<torznab:attr name="seeders" value="5"/></item>
<item><title>Odd.Movie.2020.1080p-GRP</title><enclosure url="https://t.test/5.torrent" length="100" type="application/x-bittorrent"/>
<torznab:attr name="seeders" value="5"/><torznab:attr name="downloadvolumefactor" value="lots"/></item>
</channel>
</rss>`

func TestTorznabReadsDownloadVolumeFactor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(torznabDiscountFeed))
	}))
	defer srv.Close()
	client := NewNewznabClient("Fixture", srv.URL, "key")
	results, err := client.Search(context.Background(), "movie", []int{2000})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	want := map[string]int{"Free": 100, "Half": 50, "Normal": 0, "Silent": 0, "Odd": 0}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d", len(results), len(want))
	}
	for _, r := range results {
		name, _, _ := strings.Cut(r.Title, ".")
		if r.Discount != want[name] {
			t.Errorf("%s: discount %d, want %d", name, r.Discount, want[name])
		}
	}
}
