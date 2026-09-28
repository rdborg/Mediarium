// Package quality implements quality profiles (PRD.md §7 Phase 2 —
// "Quality profiles (custom formats, TRaSH-Guides-inspired sane defaults
// shipped out of the box)"). A profile ranks known quality tiers from
// worst to best, decides whether a given release is acceptable at all,
// and whether one release is an upgrade over another (for the
// missing/upgrade hunting loop in internal/automation).
package quality

import (
	"fmt"
	"strings"

	"github.com/ryanborg/mediarium/internal/parser"
)

// Tier is one recognized quality level, ordered worst-to-best. This
// mirrors the widely-used TRaSH-Guides quality ladder (resolution +
// source combined, since a 1080p WEBRip and a 1080p Bluray are not
// equivalent quality despite sharing a resolution).
type Tier string

const (
	TierUnknown     Tier = "Unknown"
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
// upgrades (PRD §7 Phase 2).
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

// Accepts reports whether a release's tier is in this profile's allow-list
// at all (regardless of whether it's an upgrade over anything).
func (p Profile) Accepts(r parser.Release) bool {
	tier := Classify(r)
	for _, t := range p.Allowed {
		if t == tier {
			return true
		}
	}
	return false
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
func (p Profile) IsUpgradeOverTier(currentTier Tier, candidate parser.Release) bool {
	if !p.Accepts(candidate) {
		return false
	}
	if p.ReachedCutoffTier(currentTier) {
		return false
	}
	return Rank(Classify(candidate)) > Rank(currentTier)
}

// Presets ships a couple of TRaSH-Guides-inspired sane defaults (PRD §7)
// so most users never need to hand-build a profile.
func Presets() map[string]Profile {
	return map[string]Profile{
		"any-1080p": {
			Name:    "Up to 1080p",
			Allowed: []Tier{TierHDTV720p, TierWebDL720p, TierBluray720p, TierHDTV1080p, TierWebDL1080p, TierBluray1080p, TierRemux1080p},
			Cutoff:  TierBluray1080p, UpgradeAllowed: true,
		},
		"ultra-hd": {
			Name:    "Ultra-HD (up to 2160p)",
			Allowed: []Tier{TierWebDL1080p, TierBluray1080p, TierRemux1080p, TierWebDL2160p, TierBluray2160p, TierRemux2160p},
			Cutoff:  TierBluray2160p, UpgradeAllowed: true,
		},
		"any": {
			Name:    "Any",
			Allowed: AllTiers(),
			Cutoff:  TierRemux2160p, UpgradeAllowed: true,
		},
	}
}
