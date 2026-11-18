package api

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/quality"
)

// The automatic pick has to give the same answer however the indexers'
// answers happen to be lined up, as long as the releases differ in something
// the pick looks at. Two releases that are equal in everything it looks at
// (same key, same indexer priority, same protocol, no seeders, no dates) are
// settled by which one was found first, which is the documented last step.
func TestPickDoesNotDependOnTheOrderFound(t *testing.T) {
	profile := quality.Presets()[quality.Preset1080p]
	profile.Preferred = []quality.Preferred{{Term: "HYBRID", Score: 50}}
	day := func(n int) time.Time { return time.Date(2026, 11, n, 12, 0, 0, 0, time.UTC) }
	mk := func(title string, prio int, proto indexers.Protocol, seeders int, size int64) indexers.Result {
		r := indexers.Result{Title: title, Priority: prio, Protocol: proto, SizeBytes: size, DownloadURL: "http://x/" + title, IndexerName: fmt.Sprintf("idx%d", prio)}
		if proto == indexers.ProtocolTorrent {
			r.Seeders, r.SeedersKnown = seeders, true
		}
		return r
	}
	torrent, usenet := indexers.ProtocolTorrent, indexers.ProtocolUsenet
	sets := map[string][]indexers.Result{
		"quality ladder": {
			mk("Movie.2020.1080p.HDTV.x264-A", 25, usenet, 0, 3<<30),
			mk("Movie.2020.1080p.WEB-DL.x264-B", 25, usenet, 0, 4<<30),
			mk("Movie.2020.1080p.BluRay.x264-C", 25, usenet, 0, 8<<30),
		},
		"score": {
			mk("Movie.2020.1080p.WEB-DL.x264-A", 25, usenet, 0, 4<<30),
			mk("Movie.2020.HYBRID.1080p.WEB-DL.x264-B", 25, usenet, 0, 4<<30),
		},
		"indexer priority": {
			mk("Movie.2020.1080p.WEB-DL.x264-A", 40, usenet, 0, 4<<30),
			mk("Movie.2020.1080p.WEB-DL.x264-B", 10, usenet, 0, 4<<30),
			mk("Movie.2020.1080p.WEB-DL.x264-C", 25, usenet, 0, 4<<30),
		},
		"protocol": {
			mk("Movie.2020.1080p.WEB-DL.x264-A", 25, torrent, 20, 4<<30),
			mk("Movie.2020.1080p.WEB-DL.x264-B", 25, usenet, 0, 4<<30),
		},
		"seeders": {
			mk("Movie.2020.1080p.WEB-DL.x264-A", 25, torrent, 5, 4<<30),
			mk("Movie.2020.1080p.WEB-DL.x264-B", 25, torrent, 50, 4<<30),
			mk("Movie.2020.1080p.WEB-DL.x264-C", 25, torrent, 0, 4<<30),
		},
		"dead torrent": {
			mk("Movie.2020.1080p.BluRay.x264-A", 25, torrent, 0, 8<<30),
			mk("Movie.2020.1080p.WEB-DL.x264-B", 25, torrent, 3, 4<<30),
		},
	}
	// The same name posted twice, a day apart: the later post wins everywhere.
	older := mk("Movie.2020.1080p.WEB-DL.x264-A", 25, usenet, 0, 4<<30)
	older.PublishDate = day(1)
	newer := older
	newer.PublishDate = day(3)
	newer.SizeBytes = 4<<30 + 1
	newer.DownloadURL = "http://x/newer"
	sets["same name reposted"] = []indexers.Result{older, newer}
	// The same name on two indexers a few hours apart, and another name with
	// a priority in between.
	x := mk("Movie.2020.1080p.WEB-DL.x264-A", 5, usenet, 0, 4<<30)
	x.PublishDate = day(1)
	y := mk("Movie.2020.1080p.WEB-DL.x264-A", 25, usenet, 0, 4<<30)
	y.PublishDate = day(1).Add(5 * time.Hour)
	y.DownloadURL = "http://x/y"
	z := mk("Movie.2020.1080p.WEB-DL.x264-C", 10, usenet, 0, 4<<30)
	sets["same name on two indexers and another name"] = []indexers.Result{x, y, z}

	rng := rand.New(rand.NewSource(3))
	for name, set := range sets {
		first := pickBestFor(set, profile, 2020)
		if first == nil {
			t.Fatalf("%s: nothing picked", name)
		}
		want := first.DownloadURL + fmt.Sprint(first.SizeBytes)
		for n := 0; n < 40; n++ {
			shuffled := append([]indexers.Result(nil), set...)
			rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			got := pickBestFor(shuffled, profile, 2020)
			if got == nil || got.DownloadURL+fmt.Sprint(got.SizeBytes) != want {
				t.Errorf("%s: order %v picked %v, another order picked %s", name, resultTitles(shuffled), got, want)
				break
			}
		}
	}

	// Releases equal in everything the pick looks at: the first found stays.
	a := mk("Movie.2020.1080p.WEB-DL.x264-A", 25, usenet, 0, 4<<30)
	b := mk("Movie.2020.1080p.WEB-DL.x264-B", 25, usenet, 0, 4<<30)
	if got := pickBestFor([]indexers.Result{a, b}, profile, 2020); got.Title != a.Title {
		t.Errorf("a tie picked %q, want the first found", got.Title)
	}
	if got := pickBestFor([]indexers.Result{b, a}, profile, 2020); got.Title != b.Title {
		t.Errorf("a tie picked %q, want the first found", got.Title)
	}
}

func resultTitles(rs []indexers.Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Title
	}
	return out
}
