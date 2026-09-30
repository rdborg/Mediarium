// Package quality implements quality profiles
// ("Quality profiles (custom formats, TRaSH-Guides-inspired sane defaults
// shipped out of the box)"). A profile ranks known quality tiers from
// worst to best, decides whether a given release is acceptable at all,
// and whether one release is an upgrade over another (for the
// missing/upgrade hunting loop in internal/automation).
package quality

import (
	"fmt"
	"strings"

	"github.com/rdborg/mediarium/internal/parser"
)

// Tier is one recognized quality level, ordered worst-to-best. This
// mirrors the widely-used TRaSH-Guides quality ladder (resolution +
// source combined, since a 1080p WEBRip and a 1080p Bluray are not
// equivalent quality despite sharing a resolution).
type Tier string

const (
	TierUnknown     Tier = "Unknown"
	TierPreRelease  Tier = "CAM/TeleSync" // cinema recordings and screeners, whatever resolution they claim
	TierSDTV        Tier = "SDTV"
	TierDVD         Tier = "DVD"
	TierWebDL480p   Tier = "WEBDL-480p"
	TierHDTV720p    Tier = "HDTV-720p"
	TierWebDL720p   Tier = "WEBDL-720p"
	TierBluray720p  Tier = "Bluray-720p"
	TierHDTV1080p   Tier = "HDTV-1080p"
	TierWebDL1080p  Tier = "WEBDL-1080p"
	TierBluray1080p Tier = "Bluray-1080p"
	TierRemux1080p  Tier = "Remux-1080p"
	TierHDTV2160p   Tier = "HDTV-2160p"
	TierWebDL2160p  Tier = "WEBDL-2160p"
	TierBluray2160p Tier = "Bluray-2160p"
	TierRemux2160p  Tier = "Remux-2160p"
)

// ladder is ordered worst to best; its index is the tier's rank.
var ladder = []Tier{
	TierUnknown,
	TierPreRelease,
	TierSDTV,
	TierDVD,
	TierWebDL480p,
	TierHDTV720p,
	TierWebDL720p,
	TierBluray720p,
	TierHDTV1080p,
	TierWebDL1080p,
	TierBluray1080p,
	TierRemux1080p,
	TierHDTV2160p,
	TierWebDL2160p,
	TierBluray2160p,
	TierRemux2160p,
}

// Rank returns a tier's position in the ladder (higher = better quality).
func Rank(t Tier) int {
	for i, lt := range ladder {
		if lt == t {
			return i
		}
	}
	return 0
}

// AllTiers returns every known tier, worst to best — used to populate a
// profile-editing UI's checkbox list.
func AllTiers() []Tier {
	out := make([]Tier, len(ladder))
	copy(out, ladder)
	return out
}

// Classify maps a parsed release into the closest known Tier. Releases
// with no recognizable resolution/source combination map to TierUnknown.
func Classify(r parser.Release) Tier {
	res := r.Resolution
	src := r.Source

	// A camera or telesync copy stays one however high its claimed
	// resolution; no built-in preset accepts it.
	if parser.IsPreRelease(src) {
		return TierPreRelease
	}

	switch res {
	case "2160p":
		switch src {
		case "Remux":
			return TierRemux2160p
		case "BluRay", "BDRip", "BRRip":
			return TierBluray2160p
		case "WEB-DL", "WEBRip", "WEB":
			return TierWebDL2160p
		case "HDTV":
			return TierHDTV2160p
		}
		return TierWebDL2160p // resolution known, source ambiguous — assume a reasonable digital tier
	case "1080p":
		switch src {
		case "Remux":
			return TierRemux1080p
		case "BluRay", "BDRip", "BRRip":
			return TierBluray1080p
		case "WEB-DL", "WEBRip", "WEB":
			return TierWebDL1080p
		case "HDTV":
			return TierHDTV1080p
		}
		return TierWebDL1080p
	case "720p":
		switch src {
		case "BluRay", "BDRip", "BRRip":
			return TierBluray720p
		case "WEB-DL", "WEBRip", "WEB":
			return TierWebDL720p
		case "HDTV":
			return TierHDTV720p
		}
		return TierWebDL720p
	case "480p":
		return TierWebDL480p
	}

	switch src {
	case "DVD", "DVDRip", "DVDR":
		return TierDVD
	case "HDTV", "SDTV", "PDTV":
		return TierSDTV
	}
	return TierUnknown
}

// Profile is a named allow-list of tiers plus a cutoff — once a movie has
// a file at or above the cutoff, the hunting loop stops looking for
// upgrades.
type Profile struct {
	ID      int64 // 0 for a profile that isn't stored (the built-in presets in tests)
	Name    string
	Allowed []Tier
	Cutoff  Tier
	// UpgradeAllowed is false for "grab once and stop": a downloaded item is
	// never searched again for a better release.
	UpgradeAllowed bool

	// Release restrictions, matched case-insensitively against the release
	// title (dots, underscores and dashes count as spaces). A release must
	// contain at least one MustContain term (when any are set) and none of
	// the MustNotContain terms.
	MustContain    []string
	MustNotContain []string
	// Preferred terms add their score to a matching release; among releases
	// of the same quality tier the highest total score wins.
	Preferred []Preferred

	// Fallback lists other profiles (by id) that an automatic search tries,
	// in this order, when nothing it found is acceptable to this profile.
	// Only this profile's own list is used (a fallback's fallbacks are not
	// followed). Empty means no fallback.
	Fallback []int64
	// FallbackProfiles is Fallback resolved to the profiles themselves, filled
	// in by whoever loads the profile for a decision; it is not stored.
	FallbackProfiles []Profile
	// Language is the audio language wanted, filled in by whoever loads the
	// profile for a decision from the "Preferred audio language" setting; it
	// is not stored with the profile. A release clearly in another language
	// only is not accepted (see FitLanguage). Empty means any language.
	Language string
}

// Chain is the profile followed by its resolved fallbacks, in the order an
// automatic search tries them.
func (p Profile) Chain() []Profile {
	out := make([]Profile, 0, 1+len(p.FallbackProfiles))
	out = append(out, p)
	return append(out, p.FallbackProfiles...)
}

// AcceptsTitle reports whether a release title passes both this profile's
// quality allow-list and its release restrictions.
func (p Profile) AcceptsTitle(title string) bool {
	if ok, _ := p.TitleAllowed(title); !ok {
		return false
	}
	return p.Accepts(parser.Parse(title))
}

// AcceptedBy returns the first profile in the chain (this profile, then its
// fallbacks in order) that accepts a release title, and whether it is a
// fallback. ok is false when none does.
func (p Profile) AcceptedBy(title string) (by Profile, fallback, ok bool) {
	for i, c := range p.Chain() {
		if c.AcceptsTitle(title) {
			return c, i > 0, true
		}
	}
	return Profile{}, false, false
}

// OnFallback reports whether a file of tier t is there only because of a
// fallback: this profile does not allow t but one of its fallbacks does.
// Such a file is always replaced by a release this profile accepts, whatever
// its rank, the cutoff or the upgrade switch.
func (p Profile) OnFallback(t Tier) bool {
	if p.allows(t) {
		return false
	}
	for _, f := range p.FallbackProfiles {
		if f.allows(t) {
			return true
		}
	}
	return false
}

func (p Profile) allows(t Tier) bool {
	for _, a := range p.Allowed {
		if a == t {
			return true
		}
	}
	return false
}

// Preferred is a release-title term with a score (negative to disfavour).
type Preferred struct {
	Term  string
	Score int
}

func normalizeForMatch(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func containsTerm(normalizedTitle, term string) bool {
	t := normalizeForMatch(term)
	return t != "" && strings.Contains(normalizedTitle, t)
}

// TitleAllowed applies the profile's release restrictions to a release
// title, returning the reason when it is not allowed.
func (p Profile) TitleAllowed(title string) (bool, string) {
	norm := normalizeForMatch(title)
	for _, term := range p.MustNotContain {
		if containsTerm(norm, term) {
			return false, fmt.Sprintf("contains %q, which the %q profile excludes", term, p.Name)
		}
	}
	if len(p.MustContain) > 0 {
		for _, term := range p.MustContain {
			if containsTerm(norm, term) {
				return true, ""
			}
		}
		return false, fmt.Sprintf("has none of the terms the %q profile requires", p.Name)
	}
	return true, ""
}

// Score is the summed score of every preferred term the title matches.
func (p Profile) Score(title string) int {
	norm := normalizeForMatch(title)
	total := 0
	for _, pr := range p.Preferred {
		if containsTerm(norm, pr.Term) {
			total += pr.Score
		}
	}
	return total
}

// AllowsTier reports whether a quality tier is in this profile's allow-list.
func (p Profile) AllowsTier(t Tier) bool { return p.allows(t) }

// Accepts reports whether a release's tier is in this profile's allow-list
// at all (regardless of whether it's an upgrade over anything) and its audio
// language is not clearly another one than the wanted language.
func (p Profile) Accepts(r parser.Release) bool {
	return p.allows(Classify(r)) && p.LanguageOK(r)
}

// ReachedCutoff reports whether a release already meets or exceeds this
// profile's cutoff — the hunting loop should stop upgrading past this.
func (p Profile) ReachedCutoff(r parser.Release) bool {
	return Rank(Classify(r)) >= Rank(p.Cutoff)
}

// IsUpgrade reports whether candidate is a better (and still profile-
// accepted) quality than current, and current hasn't already reached the
// cutoff.
func (p Profile) IsUpgrade(current, candidate parser.Release) bool {
	return p.IsUpgradeOverTier(Classify(current), candidate)
}

// ReachedCutoffTier is ReachedCutoff for a tier that's already on hand
// (e.g. a stored movie's quality column) rather than a fresh release to
// classify.
func (p Profile) ReachedCutoffTier(t Tier) bool {
	return Rank(t) >= Rank(p.Cutoff)
}

// IsUpgradeOverTier is IsUpgrade for call sites that only have a
// previously-classified tier on hand (e.g. the automation hunt loop
// re-checking an already-downloaded movie) rather than a fresh
// parser.Release for "current".
//
// A current file that is there only because of a fallback (see OnFallback)
// is never "at the cutoff": any release this profile accepts replaces it.
func (p Profile) IsUpgradeOverTier(currentTier Tier, candidate parser.Release) bool {
	if !p.Accepts(candidate) {
		return false
	}
	if p.OnFallback(currentTier) {
		return true
	}
	if p.ReachedCutoffTier(currentTier) {
		return false
	}
	return Rank(Classify(candidate)) > Rank(currentTier)
}

// Keys of the built-in presets returned by Presets.
const (
	PresetCinema = "cinema"
	PresetAny    = "any"
	Preset720p   = "720p"
	Preset1080p  = "1080p"
	Preset4K     = "4k"

	// DefaultPreset is the preset a fresh install uses for every item that has
	// no profile of its own.
	DefaultPreset = Preset1080p
)

// PresetKeys lists the built-in presets in the order they are created (and
// so listed): from lowest to best, then "Any".
func PresetKeys() []string {
	return []string{PresetCinema, Preset720p, Preset1080p, Preset4K, PresetAny}
}

// presetSince is the PresetsVersion that first shipped each preset under its
// current name. An install already seeded at that version or later has had
// the chance to create it, so a missing one was deleted on purpose.
func presetSince(key string) int {
	switch key {
	case PresetCinema:
		return 4
	case Preset720p, Preset1080p, Preset4K:
		return 2
	}
	return 1
}

// Presets ships TRaSH-Guides-inspired sane defaults so most
// users never need to hand-build a profile. Every preset starts with the
// search for better versions switched off (UpgradeAllowed false): a title is
// downloaded once and left alone, and the person turns upgrades on for the
// profiles where they want them. Titles that were only downloaded because of
// a fallback profile are still searched for a replacement.
//
// "Cinema recordings" is the only preset that takes CAM/TeleSync copies, and
// takes nothing else: it is meant as an opt-in fallback for a film that is
// still only in cinemas, so the normal upgrade hunt of the title's own
// profile replaces it once a proper release appears.
//
// Each resolution preset accepts only its own resolution, so what it says is
// what you get: "1080p" never settles for 720p and "4K & over" never settles
// for 1080p. A title with no release at that resolution waits until one
// appears; "Any" is the preset that takes whatever exists.
//
// Remux tiers are deliberately left out of "1080p": a 1080p remux is an
// untouched copy of the disc, typically 20-40 GB per movie, several times the
// size of a Bluray encode that looks the same on most screens. Someone who
// wants remuxes is choosing large files on purpose and can build a profile
// for it; the everyday default should not fill a disk by surprise. "4K &
// over" does include Remux-2160p, but its cutoff (Bluray-2160p) means a remux
// is only grabbed when it is the best release found, never hunted as an
// upgrade.
func Presets() map[string]Profile {
	return map[string]Profile{
		// Cinema recordings and screeners only, see above.
		PresetCinema: {
			Name:    "Cinema recordings",
			Allowed: []Tier{TierPreRelease},
			Cutoff:  TierPreRelease, UpgradeAllowed: false,
		},
		// Anything recognisable, SD included. The cutoff stops the upgrade
		// hunt at Bluray-1080p so an "Any" item is not chased all the way to
		// a 4K remux.
		PresetAny: {
			Name:    "Any",
			Allowed: []Tier{TierSDTV, TierDVD, TierWebDL480p, TierHDTV720p, TierWebDL720p, TierBluray720p, TierHDTV1080p, TierWebDL1080p, TierBluray1080p, TierRemux1080p, TierHDTV2160p, TierWebDL2160p, TierBluray2160p, TierRemux2160p},
			Cutoff:  TierBluray1080p, UpgradeAllowed: false,
		},
		// 720p only, for small screens or limited storage and bandwidth.
		Preset720p: {
			Name:    "720p",
			Allowed: []Tier{TierHDTV720p, TierWebDL720p, TierBluray720p},
			Cutoff:  TierBluray720p, UpgradeAllowed: false,
		},
		// The default. 1080p only: TV, WEB and Bluray. No remux, see above.
		Preset1080p: {
			Name:    "1080p",
			Allowed: []Tier{TierHDTV1080p, TierWebDL1080p, TierBluray1080p},
			Cutoff:  TierBluray1080p, UpgradeAllowed: false,
		},
		// 4K only: WEB, Bluray and remux.
		Preset4K: {
			Name:    "4K & over",
			Allowed: []Tier{TierWebDL2160p, TierBluray2160p, TierRemux2160p},
			Cutoff:  TierBluray2160p, UpgradeAllowed: false,
		},
	}
}

// legacyPreset is a built-in preset shipped by earlier versions, exactly as
// it was seeded, and the current preset it becomes.
type legacyPreset struct {
	Key   string // the old Presets() key, still found in the legacy library.quality_profile setting
	Old   Profile
	NewTo string // current preset key
}

// legacyPresets are the presets earlier versions seeded. An install that
// still has one of them unchanged is moved to the new equivalent in place
// (see Repo.SeedPresets).
func legacyPresets() []legacyPreset {
	return []legacyPreset{
		// Revision 2: "1080p" and "4K & over" with a lower-resolution fallback.
		{Key: Preset1080p, NewTo: Preset1080p, Old: Profile{
			Name:    "1080p",
			Allowed: []Tier{TierWebDL720p, TierBluray720p, TierHDTV1080p, TierWebDL1080p, TierBluray1080p},
			Cutoff:  TierBluray1080p, UpgradeAllowed: true,
		}},
		{Key: Preset4K, NewTo: Preset4K, Old: Profile{
			Name:    "4K & over",
			Allowed: []Tier{TierWebDL1080p, TierBluray1080p, TierWebDL2160p, TierBluray2160p, TierRemux2160p},
			Cutoff:  TierBluray2160p, UpgradeAllowed: true,
		}},
		// Revision 1.
		{Key: "any-1080p", NewTo: Preset1080p, Old: Profile{
			Name:    "Up to 1080p",
			Allowed: []Tier{TierHDTV720p, TierWebDL720p, TierBluray720p, TierHDTV1080p, TierWebDL1080p, TierBluray1080p, TierRemux1080p},
			Cutoff:  TierBluray1080p, UpgradeAllowed: true,
		}},
		{Key: "ultra-hd", NewTo: Preset4K, Old: Profile{
			Name:    "Ultra-HD (up to 2160p)",
			Allowed: []Tier{TierWebDL1080p, TierBluray1080p, TierRemux1080p, TierWebDL2160p, TierBluray2160p, TierRemux2160p},
			Cutoff:  TierBluray2160p, UpgradeAllowed: true,
		}},
		{Key: "any", NewTo: PresetAny, Old: Profile{
			Name:    "Any",
			Allowed: tiersBeforePreRelease(),
			Cutoff:  TierRemux2160p, UpgradeAllowed: true,
		}},
	}
}

// sameDefinition reports whether two profiles have the same name, tiers,
// cutoff, upgrade switch and release restrictions (ids are ignored; tier
// order is not significant).
func sameDefinition(a, b Profile) bool {
	if a.Name != b.Name || a.Cutoff != b.Cutoff || a.UpgradeAllowed != b.UpgradeAllowed ||
		len(a.MustContain) != 0 || len(a.MustNotContain) != 0 || len(a.Preferred) != 0 || len(a.Fallback) != 0 ||
		len(b.MustContain) != 0 || len(b.MustNotContain) != 0 || len(b.Preferred) != 0 || len(b.Fallback) != 0 {
		return false
	}
	set := func(ts []Tier) map[Tier]bool {
		m := map[Tier]bool{}
		for _, t := range ts {
			m[t] = true
		}
		return m
	}
	as, bs := set(a.Allowed), set(b.Allowed)
	if len(as) != len(bs) {
		return false
	}
	for t := range as {
		if !bs[t] {
			return false
		}
	}
	return true
}

// tiersBeforePreRelease is AllTiers as it was before the CAM/TeleSync tier
// existed, which is what revision 1's "Any" preset stored.
func tiersBeforePreRelease() []Tier {
	var out []Tier
	for _, t := range ladder {
		if t != TierPreRelease {
			out = append(out, t)
		}
	}
	return out
}
