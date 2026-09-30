// Package music is the music module's own logic: the audio quality ladder
// and profiles, the release-name parser for music, naming, finding and
// matching audio files, scanning an existing music folder, and the
// artists/albums/tracks tables. It knows nothing about MusicBrainz or the
// download pipeline; internal/api wires those together.
package music

import (
	"fmt"
	"strings"
)

// Tier is one audio quality level. Music has its own ladder: the video
// tiers in internal/quality (resolution and source) mean nothing for audio.
type Tier string

const (
	TierUnknown Tier = "Unknown"
	TierMP3192  Tier = "MP3-192"
	TierMP3256  Tier = "MP3-256"
	TierAAC256  Tier = "AAC-256"
	TierMP3320  Tier = "MP3-320/V0" // 320 kbit/s CBR or the V0 VBR preset: transparent for most listeners
	TierFLAC    Tier = "FLAC"       // lossless, CD quality (16 bit); ALAC counts as FLAC
	TierFLAC24  Tier = "FLAC 24bit" // lossless hi-res (24 bit)
)

// ladder is ordered worst to best; the index is the tier's rank.
var ladder = []Tier{TierUnknown, TierMP3192, TierMP3256, TierAAC256, TierMP3320, TierFLAC, TierFLAC24}

// Rank is a tier's position on the ladder (higher is better); unknown
// strings rank as TierUnknown.
func Rank(t Tier) int {
	for i, l := range ladder {
		if l == t {
			return i
		}
	}
	return 0
}

// AllTiers lists every tier, worst to best.
func AllTiers() []Tier { return append([]Tier(nil), ladder...) }

// ValidTier reports whether s names a tier.
func ValidTier(s string) bool {
	for _, l := range ladder {
		if string(l) == s {
			return true
		}
	}
	return false
}

// Classify maps a parsed release onto the ladder. A release that names no
// format is Unknown; an MP3 release that names no bitrate is taken as the
// lowest MP3 tier rather than guessed upwards.
func Classify(r Release) Tier {
	switch r.Format {
	case FormatFLAC, FormatALAC:
		if r.BitDepth >= 24 {
			return TierFLAC24
		}
		return TierFLAC
	case FormatMP3:
		switch r.Bitrate {
		case "320", "V0":
			return TierMP3320
		case "256", "V1":
			return TierMP3256
		}
		return TierMP3192
	case FormatAAC:
		return TierAAC256
	}
	return TierUnknown
}

// TierForExtension is the tier a file's extension alone suggests, for files
// whose release name said nothing: lossless formats are FLAC, an MP3 is the
// lowest MP3 tier, AAC is AAC-256. Without a tag reader (see TagReader) the
// real bitrate and bit depth are not known.
func TierForExtension(ext string) Tier {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "flac", "alac":
		return TierFLAC
	case "mp3":
		return TierMP3192
	case "m4a", "aac":
		return TierAAC256
	}
	return TierUnknown
}

// Profile is an audio quality profile: which tiers are acceptable, where
// upgrading stops, and which other profiles an automatic search falls back
// to when nothing it finds is acceptable. It mirrors the movie/TV profiles
// (internal/quality) on the music ladder.
type Profile struct {
	ID             int64
	Name           string
	Allowed        []Tier
	Cutoff         Tier
	UpgradeAllowed bool
	// Fallback lists other profiles (by id) tried in order when nothing is
	// acceptable to this one; FallbackProfiles is that list resolved.
	Fallback         []int64
	FallbackProfiles []Profile
}

// Accepts reports whether a tier is in the profile's allow-list.
func (p Profile) Accepts(t Tier) bool {
	for _, a := range p.Allowed {
		if a == t {
			return true
		}
	}
	return false
}

// Chain is the profile followed by its resolved fallbacks.
func (p Profile) Chain() []Profile {
	return append([]Profile{p}, p.FallbackProfiles...)
}

// AcceptedBy returns the first profile of the chain that accepts t, and
// whether it is a fallback.
func (p Profile) AcceptedBy(t Tier) (by Profile, fallback, ok bool) {
	for i, c := range p.Chain() {
		if c.Accepts(t) {
			return c, i > 0, true
		}
	}
	return Profile{}, false, false
}

// OnFallback reports whether a file of tier t is there only because of a
// fallback: this profile does not accept it but a fallback does. Such an
// album is always searched for a release the profile itself accepts.
func (p Profile) OnFallback(t Tier) bool {
	if p.Accepts(t) {
		return false
	}
	for _, f := range p.FallbackProfiles {
		if f.Accepts(t) {
			return true
		}
	}
	return false
}

// ReachedCutoff reports whether t is at or above the cutoff.
func (p Profile) ReachedCutoff(t Tier) bool { return Rank(t) >= Rank(p.Cutoff) }

// WantsUpgrade reports whether an album on disk at tier current should
// still be searched for something better.
func (p Profile) WantsUpgrade(current Tier) bool {
	if current == TierUnknown || current == "" {
		return false // nothing to compare against: leave an existing collection alone
	}
	if p.OnFallback(current) {
		return true
	}
	return p.UpgradeAllowed && !p.ReachedCutoff(current)
}

// IsUpgrade reports whether a candidate tier replaces current: acceptable to
// this profile and either replacing a fallback file or better and below the
// cutoff.
func (p Profile) IsUpgrade(current, candidate Tier) bool {
	if !p.Accepts(candidate) {
		return false
	}
	if p.OnFallback(current) {
		return true
	}
	if p.ReachedCutoff(current) {
		return false
	}
	return Rank(candidate) > Rank(current)
}

// Rejections explains why automation would not take a release of tier t for
// an album (downloaded with quality current, or "" when missing).
func (p Profile) Rejections(t Tier, current Tier) []string {
	var out []string
	if !p.Accepts(t) {
		out = append(out, fmt.Sprintf("%s is not allowed by the %q profile", t, p.Name))
		if by, fallback, ok := p.AcceptedBy(t); ok && fallback {
			if current != "" {
				out = append(out, fmt.Sprintf("allowed only as a fallback (%q), which is used only when nothing is downloaded yet", by.Name))
			} else {
				out = append(out, fmt.Sprintf("allowed only as a fallback (%q): taken only when no release the %q profile accepts is found", by.Name, p.Name))
			}
		}
	}
	if current != "" && current != TierUnknown && !p.OnFallback(current) {
		switch {
		case p.ReachedCutoff(current):
			out = append(out, fmt.Sprintf("already at the profile cutoff (%s)", current))
		case Rank(t) <= Rank(current):
			out = append(out, fmt.Sprintf("not an upgrade over %s", current))
		}
	}
	return out
}

// Names of the built-in profiles.
const (
	PresetLossless = "Lossless (FLAC)"
	PresetLossy    = "Lossy (MP3 320)"
)

// Presets are the built-in profiles, in the order they are created. The
// first, Lossy, is the default because MP3 is the most common format: it
// takes MP3 320/V0 or AAC 256. Lossless takes FLAC (24-bit accepted when it
// is what turns up, but not hunted for) and falls back to Lossy when no
// lossless release exists.
func Presets() []Profile {
	return []Profile{
		{Name: PresetLossy, Allowed: []Tier{TierAAC256, TierMP3320}, Cutoff: TierMP3320, UpgradeAllowed: false},
		{Name: PresetLossless, Allowed: []Tier{TierFLAC, TierFLAC24}, Cutoff: TierFLAC, UpgradeAllowed: false},
	}
}
