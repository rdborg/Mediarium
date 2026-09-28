package quality_test

import (
	"testing"

	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/quality"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		release string
		want    quality.Tier
	}{
		{"Movie.2024.2160p.BluRay.REMUX.HDR10.DTS-HD-GROUP", quality.TierRemux2160p},
		{"Movie.2024.2160p.WEB-DL.x265-GROUP", quality.TierWebDL2160p},
		{"Movie.2024.1080p.BluRay.x264-GROUP", quality.TierBluray1080p},
		{"Movie.2024.1080p.WEB-DL.x264-GROUP", quality.TierWebDL1080p},
		{"Movie.2024.1080p.HDTV.x264-GROUP", quality.TierHDTV1080p},
		{"Movie.2024.720p.WEBRip.x264-GROUP", quality.TierWebDL720p},
		{"Movie.2024.DVDRip.XviD-GROUP", quality.TierDVD},
		{"Movie.2024.HDTV.x264-GROUP", quality.TierSDTV},
		{"Movie.2024.CAM-GROUP", quality.TierUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.release, func(t *testing.T) {
			got := quality.Classify(parser.Parse(tc.release))
			if got != tc.want {
				t.Errorf("Classify(%q) = %s, want %s", tc.release, got, tc.want)
			}
		})
	}
}

func TestRankOrdering(t *testing.T) {
	if quality.Rank(quality.TierRemux2160p) <= quality.Rank(quality.TierBluray2160p) {
		t.Error("expected Remux 2160p to outrank Bluray 2160p")
	}
	if quality.Rank(quality.TierBluray1080p) <= quality.Rank(quality.TierWebDL1080p) {
		t.Error("expected Bluray 1080p to outrank WEB-DL 1080p")
	}
	if quality.Rank(quality.TierWebDL1080p) <= quality.Rank(quality.TierHDTV720p) {
		t.Error("expected 1080p WEB-DL to outrank 720p HDTV")
	}
}

func TestProfileAcceptsAndUpgrade(t *testing.T) {
	profile := quality.Presets()["any-1080p"]

	webdl1080 := parser.Parse("Movie.2024.1080p.WEB-DL.x264-GROUP")
	bluray1080 := parser.Parse("Movie.2024.1080p.BluRay.x264-GROUP")
	remux2160 := parser.Parse("Movie.2024.2160p.BluRay.REMUX-GROUP")
	cam := parser.Parse("Movie.2024.CAM-GROUP")

	if !profile.Accepts(webdl1080) {
		t.Error("expected any-1080p profile to accept WEB-DL 1080p")
	}
	if profile.Accepts(remux2160) {
		t.Error("expected any-1080p profile to reject a 2160p remux (out of its allow-list)")
	}
	if profile.Accepts(cam) {
		t.Error("expected any-1080p profile to reject an unrecognized CAM release")
	}

	if !profile.IsUpgrade(webdl1080, bluray1080) {
		t.Error("expected Bluray 1080p to be an upgrade over WEB-DL 1080p")
	}
	if profile.IsUpgrade(bluray1080, webdl1080) {
		t.Error("did not expect WEB-DL 1080p to be an upgrade over Bluray 1080p")
	}
	if profile.IsUpgrade(webdl1080, remux2160) {
		t.Error("did not expect a 2160p remux (outside allow-list) to register as an upgrade")
	}

	// Bluray1080p is this profile's cutoff — no more upgrades once reached.
	if !profile.ReachedCutoff(bluray1080) {
		t.Error("expected Bluray 1080p to have reached the any-1080p cutoff")
	}
	if profile.IsUpgrade(bluray1080, bluray1080) {
		t.Error("did not expect an upgrade once cutoff is reached, even to an equal release")
	}
}

// TestIsUpgradeOverTier covers the tier-only entry point the automation
// hunt loop uses (a stored movie only has a previously-classified Tier on
// hand, not a fresh parser.Release for "current").
func TestIsUpgradeOverTier(t *testing.T) {
	profile := quality.Presets()["any-1080p"]
	bluray1080 := parser.Parse("Movie.2024.1080p.BluRay.x264-GROUP")

	if !profile.IsUpgradeOverTier(quality.TierWebDL1080p, bluray1080) {
		t.Error("expected Bluray 1080p to be an upgrade over a stored WEBDL-1080p tier")
	}
	if profile.IsUpgradeOverTier(quality.TierBluray1080p, bluray1080) {
		t.Error("did not expect an equal-tier release to register as an upgrade")
	}
	if !profile.ReachedCutoffTier(quality.TierBluray1080p) {
		t.Error("expected the stored Bluray-1080p tier to have reached the any-1080p cutoff")
	}
	if profile.ReachedCutoffTier(quality.TierWebDL1080p) {
		t.Error("did not expect WEBDL-1080p to have reached the any-1080p cutoff")
	}
	// A pre-migration movie whose stored quality is just "1080p" (the old
	// resolution-only format, not a recognized Tier) should be treated as
	// worst-rank rather than erroring — Rank falls back to 0 for an
	// unrecognized tier, same as TierUnknown.
	if profile.ReachedCutoffTier(quality.Tier("1080p")) {
		t.Error("expected an unrecognized legacy quality string to be treated as not having reached cutoff")
	}
}
