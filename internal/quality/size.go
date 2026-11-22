package quality

// Size checks. Two simple rules, so nobody has to fill in a size table:
//
//   - A built-in minimum per quality catches fakes and samples: a "1080p
//     Bluray" movie of 40 MB is not what it says.
//   - A quality profile may set the largest release it accepts (MaxSizeGB),
//     for people with little disk space or a slow line.
//
// A release whose size the indexer did not report (0) always passes.

const mb = int64(1) << 20

// minMovie is the smallest believable size of a whole movie per quality.
var minMovie = map[Tier]int64{
	TierSDTV: 150 * mb, TierDVD: 300 * mb, TierWebDL480p: 150 * mb,
	TierHDTV720p: 300 * mb, TierWebDL720p: 300 * mb, TierBluray720p: 400 * mb,
	TierHDTV1080p: 600 * mb, TierWebDL1080p: 600 * mb, TierBluray1080p: 900 * mb, TierRemux1080p: 6000 * mb,
	TierHDTV2160p: 1500 * mb, TierWebDL2160p: 1500 * mb, TierBluray2160p: 3000 * mb, TierRemux2160p: 12000 * mb,
}

// minEpisode is the smallest believable size of one TV episode per quality.
var minEpisode = map[Tier]int64{
	TierSDTV: 40 * mb, TierDVD: 60 * mb, TierWebDL480p: 40 * mb,
	TierHDTV720p: 80 * mb, TierWebDL720p: 80 * mb, TierBluray720p: 100 * mb,
	TierHDTV1080p: 150 * mb, TierWebDL1080p: 150 * mb, TierBluray1080p: 200 * mb, TierRemux1080p: 1000 * mb,
	TierHDTV2160p: 400 * mb, TierWebDL2160p: 400 * mb, TierBluray2160p: 800 * mb, TierRemux2160p: 2500 * mb,
}

// SeasonPack is the episode count to pass for a whole-season release whose
// episodes are not listed. It counts as three episodes, which is safe for
// any real season.
const SeasonPack = -1

// shortEpisodeMinutes is the length under which the built-in episode minimum
// shrinks with the runtime. A real 5 minute episode is far below the 150 MB
// that a 45 minute WEB-DL 1080p one needs.
const shortEpisodeMinutes = 30.0

// minEpisodeFloor is the least a single episode can be asked for, however short.
const minEpisodeFloor = 15 * mb

// SizePlausible reports whether size bytes is believable for a release of
// tier t. episodes is 0 for a movie, the number of episodes it holds for TV,
// or SeasonPack.
func SizePlausible(t Tier, size int64, episodes int) bool {
	return SizePlausibleRuntime(t, size, episodes, 0)
}

// SizePlausibleRuntime is SizePlausible that also knows how long an episode
// is. When minutes is known (above 0) and under 30, the minimum for one
// episode is the table value scaled by minutes/30, but never under 15 MB.
// Movies, longer episodes and an unknown runtime use the table as it is.
func SizePlausibleRuntime(t Tier, size int64, episodes int, minutes float64) bool {
	if size <= 0 {
		return true
	}
	if episodes == 0 {
		return size >= minMovie[t]
	}
	n := int64(episodes)
	if episodes == SeasonPack {
		n = 3
	}
	each := minEpisode[t]
	if minutes > 0 && minutes < shortEpisodeMinutes {
		scaled := int64(float64(each) * minutes / shortEpisodeMinutes)
		if scaled < minEpisodeFloor {
			scaled = minEpisodeFloor
		}
		if scaled < each {
			each = scaled
		}
	}
	return size >= each*n
}

// OwnSizeAllowed reports whether size bytes is within the profile's own
// largest size. No limit (0) or an unknown size (0) always passes.
func (p Profile) OwnSizeAllowed(size int64) bool {
	if p.MaxSizeGB <= 0 || size <= 0 {
		return true
	}
	return float64(size) <= p.MaxSizeGB*float64(int64(1)<<30)
}

// CapAllowed reports whether size bytes is within the largest release the
// install allows, whatever the profile says. No cap or an unknown size passes.
func (p Profile) CapAllowed(size int64) bool {
	if p.CapGB <= 0 || size <= 0 {
		return true
	}
	return float64(size) <= p.CapGB*float64(int64(1)<<30)
}

// RoomAllowed reports whether size bytes still leaves the disk the free space
// it was asked to keep. Not known (0) or an unknown size passes.
func (p Profile) RoomAllowed(size int64) bool {
	return p.RoomBytes <= 0 || size <= 0 || size <= p.RoomBytes
}

// PackSizeAllowed is SizeAllowed for a pack of several seasons: the profile's
// own largest size is per season (per), the install's cap and the room on the
// disk are about the whole download (total).
func (p Profile) PackSizeAllowed(total, per int64) bool {
	return p.OwnSizeAllowed(per) && p.CapAllowed(total) && p.RoomAllowed(total)
}

// SizeAllowed reports whether a release of size bytes is acceptable: within the
// profile's largest size, within the largest release Settings allows, and small
// enough to leave the free space the disk was asked to keep.
func (p Profile) SizeAllowed(size int64) bool {
	return p.OwnSizeAllowed(size) && p.CapAllowed(size) && p.RoomAllowed(size)
}
