package api

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/quality"
)

func TestWhyAboveNextNamesTheFirstDifference(t *testing.T) {
	usenet := func(title string) indexers.Result {
		return indexers.Result{Title: title, DownloadURL: "http://x/" + title, Protocol: indexers.ProtocolUsenet, IndexerName: "n"}
	}
	torrent := func(title string, seeders int) indexers.Result {
		return indexers.Result{Title: title, DownloadURL: "http://x/" + title, Protocol: indexers.ProtocolTorrent, Seeders: seeders, SeedersKnown: true, IndexerName: "t"}
	}
	withPriority := func(r indexers.Result, p int) indexers.Result { r.Priority = p; return r }

	tests := []struct {
		name     string
		language string
		hi, lo   indexers.Result
		want     string
	}{
		{"score", "", usenet(i91Fun), usenet(i91Bdrip), "its score is higher: 150 against 50"},
		{"tier", "", usenet("Some.Movie.2020.1080p.BluRay.x264-GRP"), usenet("Some.Movie.2020.1080p.WEB-DL.x264-GRP"), "its quality is higher: Bluray-1080p against WEBDL-1080p"},
		{"language", "English", usenet(i91Bdrip), usenet(i91FunDE), "fits your preferred language better"},
		{"dead torrent", "", torrent(i91Bdrip, 5), torrent(i91Fun, 0), "has seeders and the next one has none"},
		{"indexer priority", "", withPriority(usenet(i91Fun), 1), withPriority(usenet(i91Fun), 40), "indexer with a higher priority"},
		{"seeders", "", torrent(i91Fun, 50), torrent(i91Fun, 9), "more seeders: 50 against 9"},
		{"exact tie", "", usenet(i91Fun), usenet(i91Fun), "equal and it was found first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := quality.Presets()[quality.PresetAny]
			p.Language = tt.language
			p.Preferred = i91Orders()["fun first"]
			got := whyAboveNext(p, &tt.hi, &tt.lo)
			if got.Kind != "rank" || !strings.Contains(got.Text, tt.want) {
				t.Errorf("got %q (%s), want it to contain %q", got.Text, got.Kind, tt.want)
			}
		})
	}
}

// The ranking notes follow the pick: the best match comes first, and each
// release says why it stands above the next one.
func TestRankNotesFollowThePick(t *testing.T) {
	results := []indexers.Result{
		{Title: i91Bdrip, DownloadURL: "http://x/1", Protocol: indexers.ProtocolUsenet, IndexerName: "n"},
		{Title: i91Fun, DownloadURL: "http://x/2", Protocol: indexers.ProtocolUsenet, IndexerName: "n"},
		{Title: "Some.Movie.2020.1080p.WEB-DL.x264-GRP", DownloadURL: "http://x/3", Protocol: indexers.ProtocolUsenet, IndexerName: "n"},
		{Title: "Some.Movie.2020.CAM.x264-GRP", DownloadURL: "http://x/4", Protocol: indexers.ProtocolUsenet, IndexerName: "n"},
	}
	p := quality.Presets()[quality.Preset1080p]
	p.Preferred = i91Orders()["fun first"]
	payloads := make([]searchResultPayload, len(results))
	for i := range payloads {
		payloads[i].addReasonList(rejectionReasonsFor(results[i].Title, p, false, "", "", keepsNothing))
	}
	markBestMatch(payloads, results, p)
	rank := func(i int) string {
		for _, r := range payloads[i].Reasons {
			if r.Kind == "rank" {
				return r.Text
			}
		}
		return ""
	}
	if !payloads[1].Best {
		t.Fatalf("the x265-FuN release should be the best match: %+v", payloads[1])
	}
	if got := rank(1); !strings.HasPrefix(got, "It ranks above the next release because") {
		t.Errorf("the best match says %q", got)
	}
	if got := rank(2); !strings.Contains(got, "last release an automatic search would take") {
		t.Errorf("the lowest candidate says %q", got)
	}
	if got := rank(3); got != "" {
		t.Errorf("a release the profile rejects has no rank note, got %q", got)
	}
}

func TestExplainChecksAgreeWithTheReasons(t *testing.T) {
	p := quality.Presets()[quality.Preset1080p]
	tests := []struct {
		name  string
		title string
		size  int64
		kinds map[string]string // check kind -> state it must have
	}{
		{"allowed quality and size", "Some.Movie.2020.1080p.WEB-DL.x264-GRP", 5_000_000_000, map[string]string{"quality": "ok", "size": "ok"}},
		{"quality the profile does not take", "Some.Movie.2020.480p.DVDRip.x264-GRP", 1_000_000_000, map[string]string{"quality": "bad"}},
		{"size not stated", "Some.Movie.2020.1080p.WEB-DL.x264-GRP", 0, map[string]string{"quality": "ok", "size": "info"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := indexers.Result{Title: tt.title, Protocol: indexers.ProtocolUsenet, SizeBytes: tt.size}
			pl := searchResultPayload{Title: tt.title, SizeBytes: tt.size}
			pl.addReasonList(rejectionReasonsFor(tt.title, p, false, "", "", keepsNothing))
			got := map[string]string{}
			for _, c := range explainChecks(pl, res, p) {
				if got[c.Kind] == "" || c.State == "bad" {
					got[c.Kind] = c.State
				}
				if c.Text == "" {
					t.Errorf("a %s check has no text", c.Kind)
				}
			}
			for kind, state := range tt.kinds {
				if got[kind] != state {
					t.Errorf("check %q is %q, want %q (all: %+v)", kind, got[kind], state, got)
				}
			}
			// A check is bad exactly when a reason of its kind exists.
			for kind, state := range got {
				_, has := firstReason(pl.Reasons, kind)
				if (state == "bad") != has {
					t.Errorf("check %q is %q but reasons say %v", kind, state, has)
				}
			}
		})
	}
}
