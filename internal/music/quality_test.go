package music

import (
	"strings"
	"testing"
)

func presetsByName() (lossless, lossy Profile) {
	ps := Presets()
	lossy, lossless = ps[0], ps[1]
	lossless.ID, lossy.ID = 1, 2
	lossless.UpgradeAllowed, lossy.UpgradeAllowed = true, true // the presets start with upgrades off; these cases are about the upgrade rules
	lossless.Fallback = []int64{2}
	lossless.FallbackProfiles = []Profile{lossy}
	return lossless, lossy
}

func TestPresetsStartWithUpgradesOff(t *testing.T) {
	for _, p := range Presets() {
		if p.UpgradeAllowed {
			t.Errorf("%q should start with upgrades off", p.Name)
		}
	}
}

// With upgrades off, an album that is only there because of the fallback
// profile is still searched for a replacement; one at the cutoff is left alone.
func TestFallbackFileIsReplacedEvenWithUpgradesOff(t *testing.T) {
	lossless, lossy := presetsByName()
	lossless.UpgradeAllowed, lossy.UpgradeAllowed = false, false
	lossless.FallbackProfiles = []Profile{lossy}
	if !lossless.WantsUpgrade(TierMP3320) {
		t.Error("a file that is only there because of the fallback profile should still be replaced")
	}
	if lossless.WantsUpgrade(TierFLAC) || lossy.WantsUpgrade(TierAAC256) {
		t.Error("with upgrades off nothing else is searched again")
	}
}

func TestLadderOrder(t *testing.T) {
	want := []Tier{TierUnknown, TierMP3192, TierMP3256, TierAAC256, TierMP3320, TierFLAC, TierFLAC24}
	for i, tier := range want {
		if Rank(tier) != i {
			t.Errorf("Rank(%s) = %d, want %d", tier, Rank(tier), i)
		}
	}
	if Rank("nonsense") != 0 || !ValidTier("FLAC") || ValidTier("FLAC-16") {
		t.Fatal("unknown tiers rank as Unknown and are not valid")
	}
}

func TestTierForExtension(t *testing.T) {
	for ext, want := range map[string]Tier{".flac": TierFLAC, "FLAC": TierFLAC, ".mp3": TierMP3192, ".m4a": TierAAC256, ".ogg": TierUnknown, ".opus": TierUnknown} {
		if got := TierForExtension(ext); got != want {
			t.Errorf("TierForExtension(%q) = %s, want %s", ext, got, want)
		}
	}
}

func TestProfileDecisions(t *testing.T) {
	lossless, lossy := presetsByName()
	cases := []struct {
		name    string
		profile Profile
		current Tier
		cand    Tier
		upgrade bool
	}{
		{"lossless replaces a fallback MP3 with FLAC", lossless, TierMP3320, TierFLAC, true},
		{"lossless keeps FLAC (at the cutoff)", lossless, TierFLAC, TierFLAC24, false},
		{"lossless never takes MP3 as an upgrade", lossless, TierMP3320, TierMP3320, false},
		{"lossy upgrades 256 AAC to 320", lossy, TierAAC256, TierMP3320, true},
		{"lossy does not take FLAC", lossy, TierAAC256, TierFLAC, false},
		{"lossy at the cutoff stays", lossy, TierMP3320, TierMP3320, false},
	}
	for _, tc := range cases {
		if got := tc.profile.IsUpgrade(tc.current, tc.cand); got != tc.upgrade {
			t.Errorf("%s: IsUpgrade = %v", tc.name, got)
		}
	}

	if !lossless.WantsUpgrade(TierMP3320) || lossless.WantsUpgrade(TierFLAC) || lossless.WantsUpgrade(TierUnknown) || lossless.WantsUpgrade("") {
		t.Fatal("lossless: hunt only a fallback file; never an unknown one")
	}
	if !lossy.WantsUpgrade(TierAAC256) || lossy.WantsUpgrade(TierMP3320) {
		t.Fatal("lossy: hunt below the cutoff only")
	}
	if by, fallback, ok := lossless.AcceptedBy(TierMP3320); !ok || !fallback || by.Name != PresetLossy {
		t.Fatalf("MP3 320 should be accepted by the lossy fallback: %v %v %v", by.Name, fallback, ok)
	}
	if _, _, ok := lossless.AcceptedBy(TierMP3192); ok {
		t.Fatal("MP3 192 is accepted by neither")
	}
}

func TestRejections(t *testing.T) {
	lossless, _ := presetsByName()
	got := strings.Join(lossless.Rejections(TierMP3320, ""), " | ")
	if !strings.Contains(got, `MP3-320/V0 is not allowed by the "Lossless (FLAC)" profile`) || !strings.Contains(got, "allowed only as a fallback") {
		t.Fatalf("missing album, MP3 release: %s", got)
	}
	if r := lossless.Rejections(TierFLAC, ""); len(r) != 0 {
		t.Fatalf("FLAC for a missing album is fine: %v", r)
	}
	if r := strings.Join(lossless.Rejections(TierFLAC24, TierFLAC), " | "); !strings.Contains(r, "already at the profile cutoff (FLAC)") {
		t.Fatalf("downloaded FLAC album: %s", r)
	}
	if r := lossless.Rejections(TierFLAC, TierMP3320); len(r) != 0 {
		t.Fatalf("FLAC replacing a fallback MP3 is fine: %v", r)
	}
}
