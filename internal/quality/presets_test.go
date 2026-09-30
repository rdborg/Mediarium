package quality_test

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
)

func tiers(ts ...quality.Tier) []quality.Tier { return ts }

func sameTiers(a, b []quality.Tier) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[quality.Tier]bool{}
	for _, t := range a {
		seen[t] = true
	}
	for _, t := range b {
		if !seen[t] {
			return false
		}
	}
	return true
}

func TestPresetDefinitions(t *testing.T) {
	cases := []struct {
		key     string
		name    string
		allowed []quality.Tier
		cutoff  quality.Tier
	}{
		{quality.PresetCinema, "Cinema recordings", tiers(quality.TierPreRelease), quality.TierPreRelease},
		{quality.PresetAny, "Any", tiers(
			quality.TierSDTV, quality.TierDVD, quality.TierWebDL480p,
			quality.TierHDTV720p, quality.TierWebDL720p, quality.TierBluray720p,
			quality.TierHDTV1080p, quality.TierWebDL1080p, quality.TierBluray1080p, quality.TierRemux1080p,
			quality.TierHDTV2160p, quality.TierWebDL2160p, quality.TierBluray2160p, quality.TierRemux2160p,
		), quality.TierBluray1080p},
		{quality.Preset720p, "720p", tiers(quality.TierHDTV720p, quality.TierWebDL720p, quality.TierBluray720p), quality.TierBluray720p},
		{quality.Preset1080p, "1080p", tiers(quality.TierHDTV1080p, quality.TierWebDL1080p, quality.TierBluray1080p), quality.TierBluray1080p},
		{quality.Preset4K, "4K & over", tiers(quality.TierWebDL2160p, quality.TierBluray2160p, quality.TierRemux2160p), quality.TierBluray2160p},
	}
	presets := quality.Presets()
	if len(presets) != len(cases) || len(quality.PresetKeys()) != len(cases) {
		t.Fatalf("expected exactly %d presets, got %d (keys %v)", len(cases), len(presets), quality.PresetKeys())
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := presets[tc.key]
			if !ok {
				t.Fatalf("no preset %q", tc.key)
			}
			if p.Name != tc.name || p.Cutoff != tc.cutoff || p.UpgradeAllowed || !sameTiers(p.Allowed, tc.allowed) {
				t.Fatalf("preset %q = %+v", tc.key, p)
			}
			if _, err := quality.Validate(p); err != nil {
				t.Fatalf("preset %q does not validate: %v", tc.key, err)
			}
		})
	}
	if quality.Presets()[quality.DefaultPreset].Name != "1080p" {
		t.Fatalf("the default preset should be 1080p, got %q", quality.DefaultPreset)
	}
}

// A fresh install downloads a title once and leaves it alone: every built-in
// preset starts with the search for better versions off.
func TestPresetsStartWithUpgradesOff(t *testing.T) {
	for key, p := range quality.Presets() {
		if p.UpgradeAllowed {
			t.Errorf("preset %q should start with upgrades off", key)
		}
	}
}

func TestPresetName(t *testing.T) {
	cases := []struct {
		key, want string
		ok        bool
	}{
		{"1080p", "1080p", true},
		{"4k", "4K & over", true},
		{"any", "Any", true},
		{"any-1080p", "1080p", true},    // legacy key
		{"ultra-hd", "4K & over", true}, // legacy key
		{"nope", "", false},
	}
	for _, tc := range cases {
		got, ok := quality.PresetName(tc.key)
		if got != tc.want || ok != tc.ok {
			t.Errorf("PresetName(%q) = %q, %v; want %q, %v", tc.key, got, ok, tc.want, tc.ok)
		}
	}
}

// The presets earlier versions seeded, exactly as they were stored.
var (
	oldUpTo1080p = quality.Profile{Name: "Up to 1080p", UpgradeAllowed: true, Cutoff: quality.TierBluray1080p, Allowed: tiers(
		quality.TierHDTV720p, quality.TierWebDL720p, quality.TierBluray720p,
		quality.TierHDTV1080p, quality.TierWebDL1080p, quality.TierBluray1080p, quality.TierRemux1080p)}
	oldUltraHD = quality.Profile{Name: "Ultra-HD (up to 2160p)", UpgradeAllowed: true, Cutoff: quality.TierBluray2160p, Allowed: tiers(
		quality.TierWebDL1080p, quality.TierBluray1080p, quality.TierRemux1080p,
		quality.TierWebDL2160p, quality.TierBluray2160p, quality.TierRemux2160p)}
	oldAny = quality.Profile{Name: "Any", UpgradeAllowed: true, Cutoff: quality.TierRemux2160p, Allowed: withoutPreRelease(quality.AllTiers())}

	// Revision 2's resolution presets, which fell back to the resolution below.
	v2_1080p = quality.Profile{Name: "1080p", UpgradeAllowed: true, Cutoff: quality.TierBluray1080p, Allowed: tiers(
		quality.TierWebDL720p, quality.TierBluray720p,
		quality.TierHDTV1080p, quality.TierWebDL1080p, quality.TierBluray1080p)}
	v2_4K = quality.Profile{Name: "4K & over", UpgradeAllowed: true, Cutoff: quality.TierBluray2160p, Allowed: tiers(
		quality.TierWebDL1080p, quality.TierBluray1080p,
		quality.TierWebDL2160p, quality.TierBluray2160p, quality.TierRemux2160p)}
)

// Every resolution preset accepts only its own resolution.
func TestResolutionPresetsAreStrict(t *testing.T) {
	cases := []struct {
		key  string
		want string // resolution every allowed tier must have
	}{
		{quality.Preset720p, "720p"},
		{quality.Preset1080p, "1080p"},
		{quality.Preset4K, "2160p"},
	}
	for _, tc := range cases {
		for _, tier := range quality.Presets()[tc.key].Allowed {
			if !strings.HasSuffix(string(tier), tc.want) {
				t.Errorf("preset %q allows %s, want only %s", tc.key, tier, tc.want)
			}
		}
	}
}

func TestSeedPresetsDropsTheFallbackFromRevision2(t *testing.T) {
	repo := newRepo(t)
	ids := seedOld(t, repo, v2_1080p, v2_4K)

	list, err := repo.SeedPresets(2)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := byName(list)
	for _, key := range []string{quality.Preset1080p, quality.Preset4K} {
		want := quality.Presets()[key]
		p := got[want.Name]
		if p.ID != ids[want.Name] || !sameTiers(p.Allowed, want.Allowed) {
			t.Errorf("%q should be redefined in place, got %+v", want.Name, p)
		}
	}
}

// An untouched old preset moves to its new definition, but keeps the upgrade
// choice stored with it: nothing a person had set is changed silently.
func TestSeedPresetsKeepsTheStoredUpgradeChoice(t *testing.T) {
	repo := newRepo(t)
	seedOld(t, repo, oldUpTo1080p) // stored with upgrades on
	list, err := repo.SeedPresets(1)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if p := byName(list)["1080p"]; !p.UpgradeAllowed {
		t.Errorf("an old preset stored with upgrades on must keep them, got %+v", p)
	}
	// Presets created fresh start with upgrades off.
	if p := byName(list)["720p"]; p.UpgradeAllowed {
		t.Errorf("a preset created now starts with upgrades off, got %+v", p)
	}
}

func seedOld(t *testing.T, repo *quality.Repo, ps ...quality.Profile) map[string]int64 {
	t.Helper()
	ids := map[string]int64{}
	for _, p := range ps {
		created, err := repo.Create(p)
		if err != nil {
			t.Fatalf("create old preset %q: %v", p.Name, err)
		}
		ids[p.Name] = created.ID
	}
	return ids
}

func byName(list []quality.Profile) map[string]quality.Profile {
	m := map[string]quality.Profile{}
	for _, p := range list {
		m[p.Name] = p
	}
	return m
}

func TestSeedPresetsUpgradesUntouchedOldPresetsInPlace(t *testing.T) {
	repo := newRepo(t)
	ids := seedOld(t, repo, oldUpTo1080p, oldUltraHD, oldAny)

	list, err := repo.SeedPresets(1)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := byName(list)
	if len(list) != 5 {
		t.Fatalf("want exactly the five presets, got %d: %+v", len(list), list)
	}
	presets := quality.Presets()
	for _, tc := range []struct{ oldName, key string }{
		{"Up to 1080p", quality.Preset1080p},
		{"Ultra-HD (up to 2160p)", quality.Preset4K},
		{"Any", quality.PresetAny},
	} {
		want := presets[tc.key]
		p, ok := got[want.Name]
		if !ok {
			t.Fatalf("%q missing after upgrade", want.Name)
		}
		if p.ID != ids[tc.oldName] {
			t.Errorf("%q should keep the id of %q (%d), got %d", want.Name, tc.oldName, ids[tc.oldName], p.ID)
		}
		if p.Cutoff != want.Cutoff || !sameTiers(p.Allowed, want.Allowed) {
			t.Errorf("%q not redefined: %+v", want.Name, p)
		}
	}
	if _, ok := got["720p"]; !ok {
		t.Error("the new 720p preset should have been created")
	}
}

func TestSeedPresetsLeavesEditedOldPresetAlone(t *testing.T) {
	repo := newRepo(t)
	edited := oldUpTo1080p
	edited.Cutoff = quality.TierWebDL1080p
	editedAny := oldAny
	editedAny.MustNotContain = []string{"HDCAM"}
	ids := seedOld(t, repo, edited, oldUltraHD, editedAny)

	list, err := repo.SeedPresets(1)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := byName(list)
	if p := got["Up to 1080p"]; p.ID != ids["Up to 1080p"] || p.Cutoff != quality.TierWebDL1080p {
		t.Errorf("the edited preset should be untouched, got %+v", p)
	}
	if p := got["Any"]; p.ID != ids["Any"] || p.Cutoff != quality.TierRemux2160p || len(p.MustNotContain) != 1 {
		t.Errorf("the edited Any should be untouched (and not duplicated), got %+v", p)
	}
	if p := got["1080p"]; p.ID == 0 || p.ID == ids["Up to 1080p"] {
		t.Errorf("a new 1080p preset should be created beside the edited one, got %+v", p)
	}
	if p := got["4K & over"]; p.ID != ids["Ultra-HD (up to 2160p)"] {
		t.Errorf("the untouched Ultra-HD should still be upgraded in place, got %+v", p)
	}
	// Up to 1080p (kept), Any (kept), 4K & over (renamed), 720p, 1080p and
	// Cinema recordings (new).
	if len(list) != 6 {
		t.Fatalf("want 6 profiles, got %d: %+v", len(list), list)
	}
}

func TestSeedPresetsKeepsAUsersProfileWithANewName(t *testing.T) {
	repo := newRepo(t)
	mine := quality.Profile{Name: "1080p", Allowed: tiers(quality.TierBluray1080p), Cutoff: quality.TierBluray1080p}
	ids := seedOld(t, repo, oldUpTo1080p, mine)

	list, err := repo.SeedPresets(1)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := byName(list)
	if p := got["1080p"]; p.ID != ids["1080p"] || len(p.Allowed) != 1 {
		t.Errorf("the user's own 1080p profile must not be replaced, got %+v", p)
	}
	if p := got["Up to 1080p"]; p.ID != ids["Up to 1080p"] {
		t.Errorf("with its new name taken, the old preset stays as it was, got %+v", p)
	}
}

func TestSeedPresetsRunsOncePerVersion(t *testing.T) {
	repo := newRepo(t)
	list, err := repo.SeedPresets(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(byName(list)["720p"].ID, 0); err != nil {
		t.Fatal(err)
	}
	again, err := repo.SeedPresets(quality.PresetsVersion)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(quality.PresetKeys())-1 {
		t.Fatalf("a preset the user deleted must not come back once seeded, got %d", len(again))
	}
}

// Revision 4 only adds "Cinema recordings": a revision 3 install gets it, and
// its existing presets (edited, deleted or untouched) stay as they are.
func TestSeedPresetsRevision4AddsCinemaOnly(t *testing.T) {
	repo := newRepo(t)
	p := quality.Presets()
	edited := p[quality.Preset1080p]
	edited.Cutoff = quality.TierWebDL1080p
	ids := seedOld(t, repo, p[quality.Preset720p], edited, p[quality.PresetAny]) // 4K & over deleted by the user

	list, err := repo.SeedPresets(3)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := byName(list)
	if len(list) != 4 {
		t.Fatalf("want the three kept presets plus Cinema recordings, got %d: %+v", len(list), list)
	}
	if c, ok := got["Cinema recordings"]; !ok || !sameTiers(c.Allowed, tiers(quality.TierPreRelease)) {
		t.Fatalf("Cinema recordings should be created, got %+v", c)
	}
	if _, ok := got["4K & over"]; ok {
		t.Error("a preset the user deleted must not come back")
	}
	if e := got["1080p"]; e.ID != ids["1080p"] || e.Cutoff != quality.TierWebDL1080p {
		t.Errorf("the edited 1080p must stay as it was, got %+v", e)
	}
	if again, _ := repo.SeedPresets(quality.PresetsVersion); len(again) != 4 {
		t.Fatalf("seeding again must change nothing, got %d", len(again))
	}
}

// withoutPreRelease is the tier list as revision 1 stored it, before the
// CAM/TeleSync tier existed.
func withoutPreRelease(ts []quality.Tier) []quality.Tier {
	var out []quality.Tier
	for _, t := range ts {
		if t != quality.TierPreRelease {
			out = append(out, t)
		}
	}
	return out
}
