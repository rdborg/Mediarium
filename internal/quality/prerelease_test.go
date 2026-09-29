package quality_test

import (
	"testing"

	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/quality"
)

// Cinema recordings and screeners are their own tier whatever resolution
// they claim, and only the "Cinema recordings" preset accepts them.
func TestPreReleaseCopies(t *testing.T) {
	cases := []struct {
		name string
		want quality.Tier
	}{
		{"Resident.Evil.2026.1080p.TeleSync.AAC.AV1-LuCY", quality.TierPreRelease},
		{"Resident.Evil.2026.1080p.TeleSync.AAC2.0.AV1-LuCY", quality.TierPreRelease},
		{"Some.Movie.2026.HDTS.x264-GRP", quality.TierPreRelease},
		{"Some.Movie.2026.HD-TS.1080p.x264-GRP", quality.TierPreRelease},
		{"Some.Movie.2026.720p.HDCAM.x264-GRP", quality.TierPreRelease},
		{"Some.Movie.2026.CAMRip.x264-GRP", quality.TierPreRelease},
		{"Some.Movie.2026.TELECINE.x264-GRP", quality.TierPreRelease},
		{"Some.Movie.2026.DVDSCR.x264-GRP", quality.TierPreRelease},
		{"Some.Movie.2026.1080p.WEBSCR.x264-GRP", quality.TierPreRelease},
		{"Some.Movie.2026.PDVD.x264-GRP", quality.TierPreRelease},
		// Proper releases are unaffected.
		{"Some.Movie.2026.1080p.WEB-DL.DDP5.1.H.264-GRP", quality.TierWebDL1080p},
		{"Some.Movie.2026.1080p.BluRay.x264-GRP", quality.TierBluray1080p},
		{"Some.Movie.2026.2160p.WEB-DL.DV.HDR.H.265-GRP", quality.TierWebDL2160p},
		// A word that merely contains the letters is not a match.
		{"Tsunami.2026.1080p.WEB-DL.x264-GRP", quality.TierWebDL1080p},
		{"The.Cameraman.1928.1080p.BluRay.x264-GRP", quality.TierBluray1080p},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := quality.Classify(parser.Parse(tc.name)); got != tc.want {
				t.Fatalf("Classify(%s) = %s, want %s", tc.name, got, tc.want)
			}
		})
	}
	for key, p := range quality.Presets() {
		if key == quality.PresetCinema {
			if len(p.Allowed) != 1 || p.Allowed[0] != quality.TierPreRelease {
				t.Errorf("preset %q must accept only %s, got %v", key, quality.TierPreRelease, p.Allowed)
			}
			continue
		}
		for _, tier := range p.Allowed {
			if tier == quality.TierPreRelease {
				t.Errorf("preset %q must not accept %s", key, tier)
			}
		}
	}
}
