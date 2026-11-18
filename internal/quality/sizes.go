package quality

import (
	"fmt"
	"sort"
)

// Size limits per quality, in megabytes for each minute of runtime, as in the
// quality definitions of Radarr and Sonarr. A release is judged by its size
// divided by the length of what it holds: a movie's runtime, or an episode's
// runtime times the number of episodes in the release.
//
// They come on top of the built-in minimums in size.go and a profile's own
// largest size: a release has to pass all of them, so the tightest one wins.
// The table does nothing until it is switched on (SizeLimits.Enabled).

// SizeLimit is the smallest and largest size for one quality, in megabytes per
// minute of runtime. Max 0 means no upper limit.
type SizeLimit struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
	// Preferred is the size Radarr's and Sonarr's quality definitions call
	// preferred, kept for the apps that write it (Recyclarr). Nothing here
	// uses it; 0 means none.
	Preferred float64 `json:"preferred,omitempty"`
}

// SizeLimits is the editable table.
type SizeLimits struct {
	// Enabled switches the table on. Off, the table is only a list of numbers.
	Enabled bool `json:"enabled"`
	// Limits holds the tiers the person changed; a tier that is not in it uses
	// its default.
	Limits map[Tier]SizeLimit `json:"limits,omitempty"`
}

// Highest limit that can be set, in MB per minute (about 360 GB for two hours).
const maxLimitMBPerMin = 3000

// DefaultSizeLimits are the limits a fresh table shows. They are the ones that
// Sonarr ships (its minimums are what separates a real release from a fake or a
// sample; Radarr's minimums are 0), with no upper limit for remuxes and
// 2160p, where sizes vary the most.
func DefaultSizeLimits() map[Tier]SizeLimit {
	return map[Tier]SizeLimit{
		TierPreRelease:  {Min: 0, Max: 0},
		TierSDTV:        {Min: 2, Max: 100},
		TierDVD:         {Min: 2, Max: 100},
		TierWebDL480p:   {Min: 2, Max: 100},
		TierHDTV720p:    {Min: 3, Max: 125},
		TierWebDL720p:   {Min: 3, Max: 130},
		TierBluray720p:  {Min: 4, Max: 130},
		TierHDTV1080p:   {Min: 4, Max: 125},
		TierWebDL1080p:  {Min: 4, Max: 130},
		TierBluray1080p: {Min: 4, Max: 155},
		TierRemux1080p:  {Min: 35, Max: 0},
		TierHDTV2160p:   {Min: 35, Max: 200},
		TierWebDL2160p:  {Min: 35, Max: 0},
		TierBluray2160p: {Min: 35, Max: 0},
		TierRemux2160p:  {Min: 35, Max: 0},
	}
}

// Tiers listed in the table, worst to best. Unknown has no row: nothing can be
// said about a release whose quality is not known.
func SizeTiers() []Tier {
	var out []Tier
	for _, t := range AllTiers() {
		if t != TierUnknown {
			out = append(out, t)
		}
	}
	return out
}

// Limit is the limit in force for a tier: the person's, or the default.
func (s SizeLimits) Limit(t Tier) (SizeLimit, bool) {
	if l, ok := s.Limits[t]; ok {
		return l, true
	}
	l, ok := DefaultSizeLimits()[t]
	return l, ok
}

// Validate says what is wrong with the table, in plain words, or returns nil.
func (s SizeLimits) Validate() error {
	known := DefaultSizeLimits()
	tiers := make([]string, 0, len(s.Limits))
	for t := range s.Limits {
		tiers = append(tiers, string(t))
	}
	sort.Strings(tiers)
	for _, name := range tiers {
		t := Tier(name)
		l := s.Limits[t]
		if _, ok := known[t]; !ok {
			return fmt.Errorf("%q is not a quality Mediarium knows.", name)
		}
		switch {
		case l.Min < 0 || l.Max < 0:
			return fmt.Errorf("%s: the sizes can't be below 0.", name)
		case l.Min > maxLimitMBPerMin || l.Max > maxLimitMBPerMin:
			return fmt.Errorf("%s: the sizes can't be above %d MB per minute.", name, maxLimitMBPerMin)
		case l.Max != 0 && l.Max < l.Min:
			return fmt.Errorf("%s: the largest size can't be smaller than the smallest.", name)
		}
	}
	return nil
}

// Check judges a release of tier t. sizeBytes is the size of the whole
// release, runtimeMin the minutes of one movie or episode, and episodes how
// many episodes it holds (0 or 1 for a movie or a single episode;
// SeasonPack when the count is not known). It returns the reason the release
// is refused, or "" when it passes. A size of 0 (the indexer did not say), an
// unknown runtime, a quality without a row and an off table always pass.
func (s SizeLimits) Check(t Tier, sizeBytes int64, runtimeMin float64, episodes int) string {
	if !s.Enabled || sizeBytes <= 0 || runtimeMin <= 0 {
		return ""
	}
	l, ok := s.Limit(t)
	if !ok {
		return ""
	}
	n := float64(episodes)
	if n < 1 {
		n = 1
	}
	// A season pack with an unknown number of episodes: the smallest it could
	// be is three episodes, and no largest size can be judged.
	pack := episodes == SeasonPack
	if pack {
		n = 3
	}
	perMin := float64(sizeBytes) / float64(mb) / (runtimeMin * n)
	switch {
	case l.Min > 0 && perMin < l.Min:
		return fmt.Sprintf("too small for %s: %.1f MB a minute, the smallest is %s", t, perMin, mbText(l.Min))
	case l.Max > 0 && !pack && perMin > l.Max:
		return fmt.Sprintf("too large for %s: %.1f MB a minute, the largest is %s", t, perMin, mbText(l.Max))
	}
	return ""
}

func mbText(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d MB a minute", int64(v))
	}
	return fmt.Sprintf("%.1f MB a minute", v)
}
