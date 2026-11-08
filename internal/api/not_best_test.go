package api

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/quality"
)

func TestNotBestSaysWhichStepDecided(t *testing.T) {
	usenet := func(title string) indexers.Result {
		return indexers.Result{Title: title, DownloadURL: "http://x/" + title, Protocol: indexers.ProtocolUsenet, IndexerName: "n"}
	}
	torrent := func(title string, seeders int) indexers.Result {
		return indexers.Result{Title: title, DownloadURL: "http://x/" + title, Protocol: indexers.ProtocolTorrent, Seeders: seeders, SeedersKnown: true, IndexerName: "t"}
	}
	withPriority := func(r indexers.Result, p int) indexers.Result { r.Priority = p; return r }
	cached := func(r indexers.Result) indexers.Result { r.Cached = true; return r }

	tests := []struct {
		name     string
		language string
		results  []indexers.Result // the first is the best match
		want     string            // a piece of the sentence the second one gets
	}{
		{"score", "", []indexers.Result{usenet(i91Fun), usenet(i91Bdrip)}, "higher score: 150 against 50. Best match: x265-FuN +100, x265 +50. This release: x265 +50."},
		{"score with nothing matched", "", []indexers.Result{usenet(i91Fun), usenet("Some.Movie.2020.1080p.BluRay.x264-GRP")}, "This release: 0."},
		{"tier", "", []indexers.Result{usenet("Some.Movie.2020.1080p.BluRay.x264-GRP"), usenet("Some.Movie.2020.1080p.WEB-DL.x264-GRP")}, "Bluray-1080p against WEBDL-1080p"},
		{"language", "English", []indexers.Result{usenet(i91Bdrip), usenet(i91FunDE)}, "fits your preferred language better"},
		{"language, several wanted", "German,English", []indexers.Result{usenet(i91FunDE), usenet("Some.Movie.2020.German.1080p.BluRay.x265-FuN")}, "The best match names your languages better."},
		{"dead torrent", "", []indexers.Result{torrent(i91Bdrip, 5), torrent(i91Fun, 0)}, "no seeders"},
		{"indexer priority", "", []indexers.Result{withPriority(usenet(i91Fun), 1), withPriority(usenet(i91Fun), 40)}, "indexer with a higher priority"},
		{"protocol", "", []indexers.Result{usenet(i91Fun), torrent(i91Fun, 9)}, "protocol of the best match first"},
		{"cached", "", []indexers.Result{cached(torrent(i91Fun, 9)), torrent(i91Fun, 9)}, "cached at your debrid service"},
		{"seeders", "", []indexers.Result{torrent(i91Fun, 50), torrent(i91Fun, 9)}, "more seeders: 50 against 9"},
		{"exact tie", "", []indexers.Result{usenet(i91Fun), usenet(i91Fun)}, "found after the best match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := quality.Presets()[quality.PresetAny]
			p.Language = tt.language
			p.Preferred = i91Orders()["fun first"]
			// the second release must not be the pick, whichever protocol is preferred
			payloads := make([]searchResultPayload, len(tt.results))
			markBestMatch(payloads, tt.results, p)
			if payloads[0].Best == payloads[1].Best {
				t.Fatalf("exactly one should be the best match: %+v / %+v", payloads[0].Best, payloads[1].Best)
			}
			loser := 1
			if payloads[1].Best {
				loser = 0
			}
			if tt.name != "protocol" && payloads[0].Best != true {
				t.Fatalf("the first release should be the best match")
			}
			var got []releaseReason
			for _, r := range payloads[loser].Reasons {
				if r.Kind == "notBest" {
					got = append(got, r)
				}
			}
			if len(got) != 1 || !strings.Contains(got[0].Text, tt.want) {
				t.Fatalf("not-best reasons = %+v, want one containing %q", got, tt.want)
			}
			if len(payloads[loser].Rejections) != 0 {
				t.Fatalf("a not-best reason is no rejection: %v", payloads[loser].Rejections)
			}
			for _, r := range payloads[1-loser].Reasons {
				if r.Kind == "notBest" {
					t.Fatalf("the best match must not explain itself: %+v", r)
				}
			}
		})
	}
}

func TestNotBestSkipsReleasesAutomationRejects(t *testing.T) {
	p := quality.Presets()[quality.Preset1080p]
	results := []indexers.Result{
		{Title: "Some.Movie.2020.1080p.BluRay.x264-GRP", DownloadURL: "a", Protocol: indexers.ProtocolUsenet},
		{Title: "Some.Movie.2020.1080p.BluRay.x264-OTHER", DownloadURL: "b", Protocol: indexers.ProtocolUsenet},
		{Title: "Some.Movie.2020.1080p.BluRay.x264-BLOCKED", DownloadURL: "c", Protocol: indexers.ProtocolUsenet},
	}
	payloads := make([]searchResultPayload, 3)
	payloads[2].Blocklisted = true
	markBestMatch(payloads, results, p)
	if !payloads[0].Best {
		t.Fatalf("first should be the best match")
	}
	// An accepted release says why it was not the best match and where it ranks.
	if len(payloads[1].Reasons) != 2 || payloads[1].Reasons[0].Kind != "rank" || payloads[1].Reasons[1].Kind != "notBest" {
		t.Fatalf("an accepted release gets the reasons: %+v", payloads[1].Reasons)
	}
	if len(payloads[2].Reasons) != 0 {
		t.Fatalf("a blocklisted release gets none: %+v", payloads[2].Reasons)
	}
}

func TestPreferredHitsOnThePayload(t *testing.T) {
	p := quality.Presets()[quality.PresetAny]
	p.Preferred = i91Orders()["x265 first"]
	results := []indexers.Result{{Title: i91Fun, DownloadURL: "a", Protocol: indexers.ProtocolUsenet}}
	payloads := make([]searchResultPayload, 1)
	markBestMatch(payloads, results, p)
	if len(payloads[0].Terms) != 2 || payloads[0].Terms[0] != (preferredHit{"x265", 50}) || payloads[0].Terms[1] != (preferredHit{"x265-FuN", 100}) {
		t.Fatalf("terms %+v", payloads[0].Terms)
	}
}
