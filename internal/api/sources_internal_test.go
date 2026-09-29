package api

import (
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/quality"
)

func TestFilterSources(t *testing.T) {
	results := []indexers.Result{
		{Title: "a", Protocol: indexers.ProtocolUsenet},
		{Title: "b", Protocol: indexers.ProtocolTorrent},
		{Title: "c", Protocol: indexers.ProtocolUsenet},
	}
	cases := []struct {
		sources string
		want    int
	}{{"both", 3}, {"", 3}, {"usenet", 2}, {"torrent", 1}}
	for _, tc := range cases {
		if got := filterSources(results, tc.sources); len(got) != tc.want {
			t.Errorf("filterSources(%q) kept %d, want %d", tc.sources, len(got), tc.want)
		}
	}
}

// With identical quality, a Usenet release wins whichever order they arrive in.
func TestEqualReleasesPreferUsenet(t *testing.T) {
	profile := quality.Presets()[quality.Preset1080p]
	torrent := indexers.Result{Title: "Movie.2024.1080p.BluRay.x264-T", DownloadURL: "t", Protocol: indexers.ProtocolTorrent}
	usenet := indexers.Result{Title: "Movie.2024.1080p.BluRay.x264-U", DownloadURL: "u", Protocol: indexers.ProtocolUsenet}

	for name, order := range map[string][]indexers.Result{"torrent first": {torrent, usenet}, "usenet first": {usenet, torrent}} {
		best := pickBestResult(order, profile, 2024)
		if best == nil || best.DownloadURL != "u" {
			t.Fatalf("%s: expected the Usenet release, got %+v", name, best)
		}
	}

	// But quality still beats the protocol preference.
	worse := indexers.Result{Title: "Movie.2024.1080p.WEB-DL.x264-U", DownloadURL: "u2", Protocol: indexers.ProtocolUsenet}
	better := indexers.Result{Title: "Movie.2024.1080p.BluRay.x264-T", DownloadURL: "t2", Protocol: indexers.ProtocolTorrent}
	if best := pickBestResult([]indexers.Result{worse, better}, profile, 2024); best == nil || best.DownloadURL != "t2" {
		t.Fatalf("a better-quality torrent should still win, got %+v", best)
	}
}

func TestUnreleased(t *testing.T) {
	future := time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02")
	past := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
	if !unreleased(future) || unreleased(past) || unreleased("") {
		t.Fatalf("unreleased: future=%v past=%v empty=%v", unreleased(future), unreleased(past), unreleased(""))
	}
}
