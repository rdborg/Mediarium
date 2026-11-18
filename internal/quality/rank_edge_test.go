package quality

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/rdborg/mediarium/internal/parser"
)

func relOf(tier Tier) parser.Release {
	switch tier {
	case TierRemux2160p:
		return parser.Parse("M.2020.2160p.BluRay.REMUX-G")
	case TierBluray2160p:
		return parser.Parse("M.2020.2160p.BluRay.x265-G")
	case TierWebDL2160p:
		return parser.Parse("M.2020.2160p.WEB-DL.x265-G")
	case TierHDTV2160p:
		return parser.Parse("M.2020.2160p.HDTV.x265-G")
	case TierRemux1080p:
		return parser.Parse("M.2020.1080p.BluRay.REMUX-G")
	case TierBluray1080p:
		return parser.Parse("M.2020.1080p.BluRay.x264-G")
	case TierWebDL1080p:
		return parser.Parse("M.2020.1080p.WEB-DL.x264-G")
	case TierHDTV1080p:
		return parser.Parse("M.2020.1080p.HDTV.x264-G")
	case TierBluray720p:
		return parser.Parse("M.2020.720p.BluRay.x264-G")
	case TierWebDL720p:
		return parser.Parse("M.2020.720p.WEB-DL.x264-G")
	case TierHDTV720p:
		return parser.Parse("M.2020.720p.HDTV.x264-G")
	case TierWebDL480p:
		return parser.Parse("M.2020.480p.WEB-DL.x264-G")
	case TierDVD:
		return parser.Parse("M.2020.DVDRip.XviD-G")
	case TierSDTV:
		return parser.Parse("M.2020.HDTV.x264-G")
	case TierPreRelease:
		return parser.Parse("M.2020.HDCAM.x264-G")
	}
	return parser.Release{}
}

// Every built-in tier has a release name that classifies back to it, so the
// other tests can speak in tiers.
func TestRelOfRoundTrips(t *testing.T) {
	for _, tier := range AllTiers() {
		if tier == TierUnknown {
			continue
		}
		if got := Classify(relOf(tier)); got != tier {
			t.Errorf("relOf(%s) classifies as %s", tier, got)
		}
	}
}

func TestUpgradeEdges(t *testing.T) {
	std := Presets()[Preset1080p]
	std.UpgradeAllowed = true
	anyP := Presets()[PresetAny]
	grouped := Profile{
		Name:    "grouped",
		Allowed: []Tier{TierWebDL1080p, TierBluray1080p, TierWebDL2160p, TierBluray2160p},
		Cutoff:  TierBluray1080p,
		QualityOrder: []QualityItem{
			{Name: "UHD", Tiers: []Tier{TierWebDL2160p, TierBluray2160p}},
			{Name: "HD", Tiers: []Tier{TierWebDL1080p, TierBluray1080p}},
		},
		UpgradeAllowed: true,
	}
	fallback := std
	fallback.Allowed = []Tier{TierWebDL1080p}
	fallback.Cutoff = TierWebDL1080p
	fallback.FallbackProfiles = []Profile{{Allowed: []Tier{TierWebDL720p, TierHDTV720p}}}

	tests := []struct {
		name    string
		p       Profile
		current Tier
		cand    Tier
		want    bool
	}{
		{"web to bluray, same resolution", std, TierWebDL1080p, TierBluray1080p, true},
		{"hdtv to web", std, TierHDTV1080p, TierWebDL1080p, true},
		{"equal quality", std, TierWebDL1080p, TierWebDL1080p, false},
		{"lower quality", std, TierBluray1080p, TierWebDL1080p, false},
		{"cutoff reached, better candidate allowed by tier", anyP, TierBluray1080p, TierRemux1080p, false},
		{"just below the cutoff", anyP, TierWebDL1080p, TierBluray1080p, true},
		{"candidate not allowed", std, TierWebDL1080p, TierRemux2160p, false},
		{"candidate a cam copy", anyP, TierSDTV, TierPreRelease, false},
		{"current is a cam copy", anyP, TierPreRelease, TierWebDL1080p, true},
		{"current has no known quality", std, TierUnknown, TierWebDL1080p, true},
		{"group: inside a group it is no upgrade", grouped, TierWebDL1080p, TierBluray1080p, false},
		{"group: the next group is an upgrade", grouped, TierBluray1080p, TierWebDL2160p, false}, // the cutoff group is HD: it is reached
		{"group: current is in the cutoff group", grouped, TierWebDL1080p, TierWebDL2160p, false},
		{"fallback file is replaced by anything allowed", fallback, TierHDTV720p, TierWebDL1080p, true},
		{"fallback file is not replaced by a worse one the profile refuses", fallback, TierHDTV720p, TierWebDL720p, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.IsUpgradeOverTier(tt.current, relOf(tt.cand)); got != tt.want {
				t.Errorf("IsUpgradeOverTier(%s, %s) = %v, want %v", tt.current, tt.cand, got, tt.want)
			}
		})
	}
}

// For random profiles (built-in or with an order of groups): an upgrade must
// mean a strictly higher rank, never a file at the cutoff, never both ways at
// once, and walking upgrades from any tier must end.
func TestUpgradeProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	tiers := AllTiers()
	var real []Tier
	for _, tt := range tiers {
		if tt != TierUnknown {
			real = append(real, tt)
		}
	}
	checks := 0
	for n := 0; n < 300; n++ {
		perm := rng.Perm(len(real))
		var allowed []Tier
		for _, i := range perm[:2+rng.Intn(len(real)-2)] {
			allowed = append(allowed, real[i])
		}
		p := Profile{Name: "r", Allowed: allowed, Cutoff: allowed[rng.Intn(len(allowed))], UpgradeAllowed: true}
		if rng.Intn(2) == 0 {
			// A random order of the allowed tiers, with random groups.
			rng.Shuffle(len(allowed), func(i, j int) { allowed[i], allowed[j] = allowed[j], allowed[i] })
			var items []QualityItem
			for i := 0; i < len(allowed); {
				size := 1
				if rng.Intn(3) == 0 && i+1 < len(allowed) {
					size = 2
				}
				it := QualityItem{Tiers: append([]Tier(nil), allowed[i:i+size]...)}
				if size > 1 {
					it.Name = fmt.Sprintf("g%d", i)
				}
				items = append(items, it)
				i += size
			}
			cleaned, err := cleanOrder(items)
			if err != nil {
				t.Fatal(err)
			}
			p.QualityOrder = cleaned
		}
		for _, a := range real {
			for _, b := range real {
				ab := p.IsUpgradeOverTier(a, relOf(b))
				checks++
				if !ab {
					continue
				}
				if !p.Accepts(relOf(b)) {
					t.Fatalf("%v: upgrade to %s that the profile does not accept", p.Allowed, b)
				}
				if p.OnFallback(a) {
					continue
				}
				if p.TierRank(b) <= p.TierRank(a) {
					t.Fatalf("order %v allowed %v: %s -> %s is an upgrade but ranks %d -> %d", p.QualityOrder, p.Allowed, a, b, p.TierRank(a), p.TierRank(b))
				}
				if p.ReachedCutoffTier(a) {
					t.Fatalf("%s is at the cutoff %s but %s is an upgrade", a, p.Cutoff, b)
				}
				if ba := p.IsUpgradeOverTier(b, relOf(a)); ba {
					t.Fatalf("%s -> %s and back are both upgrades", a, b)
				}
			}
		}
		// The order is a total preorder of the tiers: the same rank for the
		// same tier, every time.
		for _, a := range real {
			first, second := p.TierRank(a), p.TierRank(a)
			if first != second {
				t.Fatal("TierRank is not stable")
			}
		}
	}
	t.Logf("%d upgrade decisions checked", checks)
}

func TestLanguageRankEdges(t *testing.T) {
	rel := func(name string) parser.Release { return parser.Parse(name) }
	const base = "Movie.2020.1080p.BluRay.x264-GRP"
	german := "Movie.2020.GERMAN.1080p.BluRay.x264-GRP"
	germanDL := "Movie.2020.GERMAN.DL.1080p.BluRay.x264-GRP"
	multi := "Movie.2020.MULTi.1080p.BluRay.x264-GRP"
	multiGerman := "Movie.2020.MULTi.GERMAN.1080p.BluRay.x264-GRP"
	itaEng := "Movie.2020.ITA.ENG.1080p.BluRay.x264-GRP"
	eng := "Movie.2020.ENGLISH.1080p.BluRay.x264-GRP"
	fr := "Movie.2020.FRENCH.1080p.BluRay.x264-GRP"
	tests := []struct {
		name      string
		wanted    string
		only      bool
		ignore    bool
		a, b      string // a should rank above b, "" for equal, "<" prefix not used
		wantOrder string // "a>b", "a=b" or "a<b"
	}{
		{"english: untagged beats english plus italian", "English", false, false, base, itaEng, "a>b"},
		{"english: english plus italian equals untagged when extras are ignored", "English", false, true, base, itaEng, "a=b"},
		{"english: tagged english equals untagged", "English", false, false, base, eng, "a=b"},
		{"german: german alone beats german dual", "German", false, false, german, germanDL, "a>b"},
		{"german: german alone equals german dual when extras are ignored", "German", false, true, german, germanDL, "a=b"},
		{"german: dual ties with untagged english (the fallback)", "German", false, false, germanDL, base, "a=b"},
		{"german: multi without a language ranks below german", "German", false, false, german, multi, "a>b"},
		{"german: multi german equals german when extras are ignored", "German", false, true, german, multiGerman, "a=b"},
		{"german only: untagged is refused", "German", true, false, german, base, "a>b"},
		{"german, french: german beats french", "German,French", false, false, german, fr, "a>b"},
		{"german, french: untagged is a fallback for the first language, so it stays above french", "German,French", false, false, base, fr, "a>b"},
		{"german, french, only: french beats a refused untagged", "German,French", true, false, fr, base, "a>b"},
		{"german, french: multi german beats german alone", "German,French", false, false, multiGerman, german, "a>b"},
		{"german, french: multi german equals german when extras are ignored", "German,French", false, true, multiGerman, german, "a=b"},
		{"german, french: order still counts when extras are ignored", "German,French", false, true, german, fr, "a>b"},
		{"french, german: the other way round", "French,German", false, false, fr, german, "a>b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ra := FitLanguageWith(rel(tt.a), tt.wanted, tt.only, tt.ignore)
			rb := FitLanguageWith(rel(tt.b), tt.wanted, tt.only, tt.ignore)
			got := "a="
			switch {
			case ra.Rank > rb.Rank:
				got = "a>"
			case ra.Rank < rb.Rank:
				got = "a<"
			}
			if got+"b" != tt.wantOrder {
				t.Errorf("ranks %d (%s) and %d (%s), want %s", ra.Rank, tt.a, rb.Rank, tt.b, tt.wantOrder)
			}
			// A rejected release always has rank 0 and an accepted one never does.
			for _, f := range []LanguageFit{ra, rb} {
				if f.Accepted == (f.Rank == 0) {
					t.Errorf("accepted %v with rank %d", f.Accepted, f.Rank)
				}
			}
		})
	}
}

func TestSizeLimitEdges(t *testing.T) {
	s := SizeLimits{Enabled: true, Limits: map[Tier]SizeLimit{TierWebDL1080p: {Min: 4, Max: 100}}}
	mbOf := func(n float64) int64 { return int64(n * float64(mb)) }
	tests := []struct {
		name     string
		tier     Tier
		size     int64
		runtime  float64
		episodes int
		wantFail bool
	}{
		{"just at the minimum", TierWebDL1080p, mbOf(4 * 100), 100, 0, false},
		{"just under the minimum", TierWebDL1080p, mbOf(4*100) - 1, 100, 0, true},
		{"just at the maximum", TierWebDL1080p, mbOf(100 * 100), 100, 0, false},
		{"just over the maximum", TierWebDL1080p, mbOf(100*100) + int64(mb), 100, 0, true},
		{"size unknown", TierWebDL1080p, 0, 100, 0, false},
		{"runtime unknown", TierWebDL1080p, 1, 0, 0, false},
		{"negative runtime", TierWebDL1080p, 1, -5, 0, false},
		{"a two-part episode needs twice the size", TierWebDL1080p, mbOf(4 * 40), 40, 2, true},
		{"a two-part episode at twice the size", TierWebDL1080p, mbOf(4 * 80), 40, 2, false},
		{"season pack of unknown length: counted as three episodes", TierWebDL1080p, mbOf(4 * 40 * 2), 40, SeasonPack, true},
		{"season pack of unknown length is never too large", TierWebDL1080p, mbOf(100 * 40 * 3 * 10), 40, SeasonPack, false},
		{"a tier without a row passes", TierUnknown, 1, 100, 0, false},
		{"the default row is used for a tier the person did not change", TierBluray1080p, mbOf(3 * 100), 100, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Check(tt.tier, tt.size, tt.runtime, tt.episodes)
			if (got != "") != tt.wantFail {
				t.Errorf("Check = %q, want failure %v", got, tt.wantFail)
			}
		})
	}
	off := SizeLimits{Limits: s.Limits}
	if off.Check(TierWebDL1080p, 1, 100, 0) != "" {
		t.Error("a table that is off refused a release")
	}
}
