package importlists_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/importlists"
)

const newznabFeed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
<channel><title>Indexer</title>
<item>
  <title>Dune.Part.Two.2024.2160p.WEB-DL.DDP5.1.HDR.H.265-GRP</title>
  <guid>abc123</guid>
  <link>https://indexer.example/getnzb/abc123.nzb&amp;i=1&amp;r=KEY</link>
  <enclosure url="https://indexer.example/getnzb/abc123.nzb&amp;i=1&amp;r=KEY" length="12345678901" type="application/x-nzb"/>
  <newznab:attr name="imdb" value="15239678"/>
</item>
<item>
  <title>Fargo.S05E03.1080p.WEB.h264-GRP</title>
  <guid>def</guid>
  <enclosure url="https://indexer.example/getnzb/def.nzb" length="2000" type="application/x-nzb"/>
  <newznab:attr name="tvdbid" value="269613"/>
</item>
</channel></rss>`

const torznabFeed = `<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item>
  <title>The Holdovers 2023 1080p BluRay x264</title>
  <link>magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567</link>
  <comments>https://www.themoviedb.org/movie/840430</comments>
  <torznab:attr name="seeders" value="12"/>
</item>
</channel></rss>`

const atomFeed = `<feed xmlns="http://www.w3.org/2005/Atom"><title>Lists</title>
<entry><title>Past Lives (2023)</title><id>urn:1</id><link href="https://www.imdb.com/title/tt13238346/"/></entry>
</feed>`

func TestParseFeed(t *testing.T) {
	cases := []struct {
		name  string
		feed  string
		check func(t *testing.T, got []importlists.FeedEntry)
	}{
		{"newznab with ids and NZB links", newznabFeed, func(t *testing.T, got []importlists.FeedEntry) {
			if len(got) != 2 {
				t.Fatalf("got %d entries", len(got))
			}
			if e := got[0]; e.IMDBID != "tt15239678" || e.Size != 12345678901 || e.Torrent || e.Link == "" {
				t.Errorf("first entry: %+v", e)
			}
			if e := got[1]; e.TVDBID != 269613 || e.Link == "" {
				t.Errorf("second entry: %+v", e)
			}
		}},
		{"torznab with a magnet link and a TMDB page", torznabFeed, func(t *testing.T, got []importlists.FeedEntry) {
			if len(got) != 1 || !got[0].Torrent || got[0].TMDBID != 840430 {
				t.Fatalf("got %+v", got)
			}
		}},
		{"atom with an IMDb link and no download", atomFeed, func(t *testing.T, got []importlists.FeedEntry) {
			if len(got) != 1 || got[0].IMDBID != "tt13238346" || got[0].Link != "" {
				t.Fatalf("got %+v", got)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := importlists.ParseFeed([]byte(c.feed))
			if err != nil {
				t.Fatal(err)
			}
			c.check(t, got)
		})
	}
	if _, err := importlists.ParseFeed([]byte("<html><body>Sign in</body></html>")); err == nil {
		t.Error("a web page was read as a feed")
	}
}

func TestFeedItemsReadTheNames(t *testing.T) {
	entries, err := importlists.ParseFeed([]byte(newznabFeed))
	if err != nil {
		t.Fatal(err)
	}
	items := importlists.FeedItems(entries)
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
	if it := items[0]; it.Kind != "movie" || it.Title != "Dune Part Two" || it.Year != 2024 || it.IMDBID != "tt15239678" {
		t.Errorf("movie: %+v", it)
	}
	if it := items[1]; it.Kind != "tv" || it.Title != "Fargo" || it.TVDBID != 269613 {
		t.Errorf("show: %+v", it)
	}
}

func TestFeedAddress(t *testing.T) {
	for in, ok := range map[string]bool{"https://indexer.example/rss?t=2000&apikey=K": true, "indexer.example/rss": true, "ftp://x/y": false, "": false} {
		if _, err := importlists.FeedAddress(in); (err == nil) != ok {
			t.Errorf("FeedAddress(%q) err = %v", in, err)
		}
	}
}
