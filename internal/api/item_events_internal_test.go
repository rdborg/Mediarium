package api

import (
	"errors"
	"testing"

	"github.com/ryanborg/mediarium/internal/blocklist"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/quality"
	"github.com/ryanborg/mediarium/internal/queue"
)

func TestSearchVerdictMessages(t *testing.T) {
	year := func(want int) func(indexers.Result) (bool, string) {
		return func(res indexers.Result) (bool, string) {
			if rel := parser.Parse(res.Title); rel.Year != 0 && rel.Year != want {
				return false, "wrong year"
			}
			return true, ""
		}
	}
	usenet := func(title string) indexers.Result {
		return indexers.Result{Title: title, Protocol: indexers.ProtocolUsenet}
	}
	torrent := func(title string) indexers.Result {
		return indexers.Result{Title: title, Protocol: indexers.ProtocolTorrent}
	}
	repeat := func(r indexers.Result, n int) []indexers.Result {
		out := make([]indexers.Result, n)
		for i := range out {
			out[i] = r
		}
		return out
	}
	join := func(lists ...[]indexers.Result) []indexers.Result {
		var out []indexers.Result
		for _, l := range lists {
			out = append(out, l...)
		}
		return out
	}
	_, plain := profileChain(true)
	_, withCinema := profileChain(true, quality.PresetCinema)
	blocked := map[string]bool{blocklist.Key("Film.2026.1080p.WEB-DL.x264-BAD"): true}

	cases := []struct {
		name        string
		results     []indexers.Result
		sources     string
		profile     quality.Profile
		upgradeFrom quality.Tier
		want        string
		level       queue.Level
	}{
		{"nothing found", nil, sourcesBoth, plain, "", "Searched: no releases found", queue.LevelInfo},
		{"owner's example", join(repeat(usenet("Film.2026.1080p.TeleSync.x264-GRP"), 8), repeat(usenet("Film.2019.1080p.WEB-DL.x264-GRP"), 4)), sourcesBoth, plain, "",
			"Searched: 12 releases, none acceptable: 8 CAM/TeleSync, 4 wrong year", queue.LevelWarn},
		{"blocklist, downloader and terms", join(
			[]indexers.Result{usenet("Film.2026.1080p.WEB-DL.x264-BAD")},
			repeat(torrent("Film.2026.1080p.WEB-DL.x264-GRP"), 2),
			[]indexers.Result{usenet("Film.2026.x264-GRP")},
		), sourcesUsenet, plain, "",
			"Searched: 4 releases, none acceptable: 2 from torrent sites (this title uses Usenet only), 1 blocklisted, 1 unknown quality", queue.LevelWarn},
		{"acceptable", []indexers.Result{usenet("Film.2026.1080p.WEB-DL.x264-GRP"), usenet("Film.2026.720p.WEB-DL.x264-GRP")}, sourcesBoth, plain, "",
			"Searched: 2 releases, 1 acceptable", queue.LevelInfo},
		{"fallback only", []indexers.Result{usenet("Film.2026.HDTS.x264-GRP")}, sourcesBoth, withCinema, "",
			"Searched: 1 release, 1 acceptable (1 only as a fallback)", queue.LevelInfo},
		{"upgrade", []indexers.Result{usenet("Film.2026.1080p.WEB-DL.x264-GRP"), usenet("Film.2026.1080p.BluRay.x264-GRP")}, sourcesBoth, plain, quality.TierWebDL1080p,
			"Searched: 2 releases, 1 acceptable", queue.LevelInfo},
		{"no upgrade", []indexers.Result{usenet("Film.2026.1080p.HDTV.x264-GRP")}, sourcesBoth, plain, quality.TierWebDL1080p,
			"Searched: 1 release, none acceptable: 1 not an upgrade over WEBDL-1080p", queue.LevelWarn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := judgeSearch(tc.results, blocked, tc.sources, tc.profile, year(2026), tc.upgradeFrom)
			got, level := v.message("Searched", "")
			if got != tc.want || level != tc.level {
				t.Fatalf("got %q (%s)\nwant %q (%s)", got, level, tc.want, tc.level)
			}
		})
	}

	v := searchVerdict{total: 2, acceptable: 1, failedIndexers: []string{"Idx"}}
	if got, level := v.message("Searched", `picked "X" from Idx with the "1080p" profile`); got != `Searched: 2 releases, 1 acceptable; picked "X" from Idx with the "1080p" profile (1 indexer did not answer: Idx)` || level != queue.LevelWarn {
		t.Fatalf("picked/failed indexers: %q %s", got, level)
	}
	best := &indexers.Result{Title: "Film.2026.HDTS.x264-GRP", IndexerName: "Idx"}
	if got := pickedText(withCinema, best); got != `picked "Film.2026.HDTS.x264-GRP" from Idx with the fallback profile "Cinema recordings"` {
		t.Fatalf("pickedText = %q", got)
	}
}

func TestEpisodesOverlap(t *testing.T) {
	cases := []struct {
		a, b []int
		want bool
	}{
		{nil, []int{3}, true}, // a pack covers every episode
		{[]int{3}, nil, true}, // and the other way round
		{nil, nil, true},      // two packs of the same season
		{[]int{1, 2}, []int{2}, true},
		{[]int{1}, []int{2}, false},
	}
	for _, tc := range cases {
		if got := episodesOverlap(tc.a, tc.b); got != tc.want {
			t.Errorf("episodesOverlap(%v, %v) = %v", tc.a, tc.b, got)
		}
	}
	err := error(alreadyDownloadingError{release: "Film.2026.1080p-GRP"})
	if !isAlreadyDownloading(err) || err.Error() != "Already downloading: Film.2026.1080p-GRP. Cancel it first to pick another." {
		t.Fatalf("error: %v", err)
	}
	if isAlreadyDownloading(errors.New("x")) {
		t.Fatal("any other error is not 'already downloading'")
	}
}
