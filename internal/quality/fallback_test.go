package quality_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// withFallbacks is the 1080p preset with the given fallback profiles resolved.
func withFallbacks(upgrades bool, fallbacks ...quality.Profile) quality.Profile {
	p := quality.Presets()[quality.Preset1080p]
	p.ID = 1
	p.UpgradeAllowed = upgrades
	for i, f := range fallbacks {
		f.ID = int64(10 + i)
		p.Fallback = append(p.Fallback, f.ID)
		p.FallbackProfiles = append(p.FallbackProfiles, f)
	}
	return p
}

func TestAcceptedByFollowsTheChainInOrder(t *testing.T) {
	presets := quality.Presets()
	p := withFallbacks(true, presets[quality.Preset720p], presets[quality.PresetCinema], presets[quality.PresetAny])
	cases := []struct {
		title        string
		wantName     string
		wantFallback bool
		wantOK       bool
	}{
		{"Film.2026.1080p.WEB-DL.x264-GRP", "1080p", false, true},
		{"Film.2026.720p.WEB-DL.x264-GRP", "720p", true, true}, // first fallback
		{"Film.2026.1080p.TeleSync.x264-GRP", "Cinema recordings", true, true},
		{"Film.2026.2160p.WEB-DL.x264-GRP", "Any", true, true}, // third fallback
		{"Film.2026.x264-GRP", "", false, false},               // Unknown: nobody
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			by, fallback, ok := p.AcceptedBy(tc.title)
			if ok != tc.wantOK || by.Name != tc.wantName || fallback != tc.wantFallback {
				t.Fatalf("AcceptedBy = %q, fallback %v, ok %v; want %q, %v, %v", by.Name, fallback, ok, tc.wantName, tc.wantFallback, tc.wantOK)
			}
		})
	}
	if got := len(p.Chain()); got != 4 {
		t.Fatalf("chain length = %d, want 4", got)
	}
}

// A file grabbed through a fallback is replaced by any release the item's own
// profile accepts: it is never "at the cutoff", even when it ranks above it
// or upgrades are off.
func TestUpgradeOverAFallbackFile(t *testing.T) {
	presets := quality.Presets()
	cases := []struct {
		name      string
		profile   quality.Profile
		current   quality.Tier
		candidate string
		want      bool
	}{
		{"cinema file, 1080p appears", withFallbacks(true, presets[quality.PresetCinema]), quality.TierPreRelease, "Film.2026.1080p.WEB-DL.x264-GRP", true},
		{"cinema file, upgrades off", withFallbacks(false, presets[quality.PresetCinema]), quality.TierPreRelease, "Film.2026.1080p.WEB-DL.x264-GRP", true},
		{"cinema file, another recording", withFallbacks(true, presets[quality.PresetCinema]), quality.TierPreRelease, "Film.2026.HDTS.x264-GRP", false},
		{"cinema file, 720p not in the profile", withFallbacks(true, presets[quality.PresetCinema]), quality.TierPreRelease, "Film.2026.720p.WEB-DL.x264-GRP", false},
		{"4K fallback file above the cutoff", withFallbacks(true, presets[quality.Preset4K]), quality.TierWebDL2160p, "Film.2026.1080p.WEB-DL.x264-GRP", true},
		// Without a fallback that allows it, a 4K file is past the cutoff as before.
		{"4K file, no fallback", withFallbacks(true), quality.TierWebDL2160p, "Film.2026.1080p.BluRay.x264-GRP", false},
		// A file the own profile allows follows the normal rules.
		{"own tier, normal upgrade", withFallbacks(true, presets[quality.PresetCinema]), quality.TierWebDL1080p, "Film.2026.1080p.BluRay.x264-GRP", true},
		{"own tier at cutoff", withFallbacks(true, presets[quality.PresetCinema]), quality.TierBluray1080p, "Film.2026.1080p.BluRay.x264-GRP", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.profile.IsUpgradeOverTier(tc.current, parser.Parse(tc.candidate)); got != tc.want {
				t.Fatalf("IsUpgradeOverTier(%s, %s) = %v, want %v", tc.current, tc.candidate, got, tc.want)
			}
		})
	}
	p := withFallbacks(false, presets[quality.PresetCinema])
	if !p.OnFallback(quality.TierPreRelease) || p.OnFallback(quality.TierWebDL1080p) || p.OnFallback(quality.TierWebDL720p) {
		t.Fatal("OnFallback should hold only for a tier outside the profile that a fallback allows")
	}
}

func TestRepoStoresFallback(t *testing.T) {
	repo := newRepo(t)
	mk := func(name string, tier quality.Tier, fallback ...int64) quality.Profile {
		t.Helper()
		p, err := repo.Create(quality.Profile{Name: name, Allowed: []quality.Tier{tier}, Cutoff: tier, Fallback: fallback})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return p
	}
	cinema := mk("Cinema", quality.TierPreRelease)
	hd := mk("720", quality.TierWebDL720p)
	if len(cinema.Fallback) != 0 {
		t.Fatalf("a new profile has no fallback, got %v", cinema.Fallback)
	}
	// Unknown ids, the profile itself and repeats are dropped; order is kept.
	main := mk("Main", quality.TierWebDL1080p, 9999, hd.ID, cinema.ID, hd.ID)
	if got := main.Fallback; len(got) != 2 || got[0] != hd.ID || got[1] != cinema.ID {
		t.Fatalf("fallback = %v, want [%d %d]", got, hd.ID, cinema.ID)
	}
	main.Fallback = []int64{cinema.ID, main.ID, hd.ID}
	if _, err := repo.Update(main); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(main.ID)
	if err != nil || len(got.Fallback) != 2 || got.Fallback[0] != cinema.ID || got.Fallback[1] != hd.ID {
		t.Fatalf("after update: %v %v", got.Fallback, err)
	}
	// Deleting a profile takes it out of every fallback list.
	if err := repo.Delete(cinema.ID, 0); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(main.ID)
	if len(got.Fallback) != 1 || got.Fallback[0] != hd.ID {
		t.Fatalf("after deleting a fallback profile: %v", got.Fallback)
	}
}
