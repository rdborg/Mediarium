package api

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

// profileChain builds a profileSet with the 1080p preset (id 1) as the
// default, falling back to the given presets (ids 10, 11, ...) in order.
func profileChain(upgrades bool, fallbackKeys ...string) (profileSet, quality.Profile) {
	presets := quality.Presets()
	main := presets[quality.Preset1080p]
	main.ID, main.UpgradeAllowed = 1, upgrades
	ps := profileSet{byID: map[int64]quality.Profile{}}
	for i, k := range fallbackKeys {
		f := presets[k]
		f.ID = int64(10 + i)
		ps.byID[f.ID] = f
		main.Fallback = append(main.Fallback, f.ID)
	}
	ps.byID[main.ID] = main
	ps.def = main
	return ps, ps.resolve(main.ID)
}

func TestPickBestResultFallbackOrder(t *testing.T) {
	var (
		web1080 = indexers.Result{Title: "Film.2026.1080p.WEB-DL.x264-GRP", DownloadURL: "1080"}
		web720  = indexers.Result{Title: "Film.2026.720p.WEB-DL.x264-GRP", DownloadURL: "720"}
		ts      = indexers.Result{Title: "Film.2026.1080p.TeleSync.x264-GRP", DownloadURL: "ts"}
		cam     = indexers.Result{Title: "Film.2026.HDCAM.x264-GRP", DownloadURL: "cam"}
		uhd     = indexers.Result{Title: "Film.2026.2160p.WEB-DL.x264-GRP", DownloadURL: "2160"}
		oldYear = indexers.Result{Title: "Film.2019.720p.WEB-DL.x264-GRP", DownloadURL: "old"}
	)
	cases := []struct {
		name      string
		fallbacks []string
		results   []indexers.Result
		want      string // DownloadURL, "" = nothing
	}{
		{"own profile wins over every fallback", []string{quality.Preset720p, quality.PresetCinema}, []indexers.Result{ts, web720, web1080}, "1080"},
		{"no fallback: nothing", nil, []indexers.Result{ts, web720}, ""},
		{"first fallback that accepts anything", []string{quality.Preset720p, quality.PresetCinema}, []indexers.Result{ts, web720}, "720"},
		{"order matters", []string{quality.PresetCinema, quality.Preset720p}, []indexers.Result{ts, web720}, "ts"},
		{"skips a fallback that accepts nothing", []string{quality.Preset4K, quality.PresetCinema}, []indexers.Result{ts, cam}, "ts"},
		{"best within the fallback", []string{quality.PresetAny}, []indexers.Result{web720, uhd}, "2160"},
		{"year still checked in a fallback", []string{quality.Preset720p}, []indexers.Result{oldYear}, ""},
		{"nobody accepts", []string{quality.Preset720p, quality.PresetCinema}, []indexers.Result{uhd}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, profile := profileChain(true, tc.fallbacks...)
			best := pickBestResult(tc.results, profile, 2026)
			got := ""
			if best != nil {
				got = best.DownloadURL
			}
			if got != tc.want {
				t.Fatalf("picked %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPickTVResultFallback(t *testing.T) {
	series := library.Series{Title: "Show", Year: 2026}
	_, profile := profileChain(true, quality.Preset720p)
	results := []indexers.Result{
		{Title: "Show.S01E01.720p.WEB-DL.x264-GRP", DownloadURL: "ep-720"},
	}
	if best := pickTVResult(results, series, 1, 1, profile, nil); best == nil || best.DownloadURL != "ep-720" {
		t.Fatalf("missing episode: want the 720p fallback, got %+v", best)
	}
	// Upgrades never use a fallback.
	current := quality.TierHDTV1080p
	if best := pickTVResult(results, series, 1, 1, profile, &current); best != nil {
		t.Fatalf("an upgrade must not come from a fallback, got %+v", best)
	}
}

// After a fallback grab, the item keeps its own profile: the upgrade hunt
// wants to replace the fallback file, even with upgrades off, and picks the
// first release the own profile accepts.
func TestUpgradeAfterFallback(t *testing.T) {
	for _, upgrades := range []bool{true, false} {
		_, profile := profileChain(upgrades, quality.PresetCinema)
		current := string(quality.TierPreRelease)
		if !wantsUpgrade(profile, current) {
			t.Fatalf("upgrades=%v: a cinema recording on a 1080p profile must be searched for a replacement", upgrades)
		}
		results := []indexers.Result{
			{Title: "Film.2026.HDTS.x264-GRP", DownloadURL: "another-ts"},
			{Title: "Film.2026.720p.WEB-DL.x264-GRP", DownloadURL: "720"},
			{Title: "Film.2026.1080p.WEB-DL.x264-GRP", DownloadURL: "1080"},
		}
		best := pickUpgradeResult(results, profile, quality.Tier(current), 2026)
		if best == nil || best.DownloadURL != "1080" {
			t.Fatalf("upgrades=%v: want the 1080p release to replace the recording, got %+v", upgrades, best)
		}
		// Once replaced, the normal rules apply again.
		if upgrades == false && wantsUpgrade(profile, string(quality.TierWebDL1080p)) {
			t.Fatal("with upgrades off, a file the own profile allows is kept")
		}
	}
	// Without a fallback, a file outside the profile is left alone as before
	// (an imported 4K file under a 1080p profile is not "downgraded").
	_, plain := profileChain(true)
	if wantsUpgrade(plain, string(quality.TierWebDL2160p)) {
		t.Fatal("a 4K file on a 1080p profile without fallbacks must not be searched")
	}
}

func TestRejectionsMentionFallback(t *testing.T) {
	_, profile := profileChain(true, quality.PresetCinema)
	title := "Film.2026.1080p.TeleSync.x264-GRP"
	got := strings.Join(rejectionsFor(title, profile, false, ""), "; ")
	if !strings.Contains(got, `allowed only as a fallback ("Cinema recordings")`) {
		t.Fatalf("rejections = %q", got)
	}
	if by := acceptedBy(profile, title); by == nil || !by.Fallback || by.ProfileName != "Cinema recordings" {
		t.Fatalf("acceptedBy = %+v", by)
	}
	if by := acceptedBy(profile, "Film.2026.1080p.WEB-DL.x264-GRP"); by == nil || by.Fallback || by.ProfileID != 1 {
		t.Fatalf("acceptedBy own = %+v", by)
	}
	if by := acceptedBy(profile, "Film.2026.720p.WEB-DL.x264-GRP"); by != nil {
		t.Fatalf("acceptedBy none = %+v", by)
	}
	// A recording on disk under a 1080p profile: a 1080p release is not
	// rejected as "not an upgrade" or "at the cutoff".
	if r := rejectionsFor("Film.2026.1080p.WEB-DL.x264-GRP", profile, true, string(quality.TierPreRelease)); len(r) != 0 {
		t.Fatalf("a proper release over a fallback file should have no rejections, got %v", r)
	}
}
