package api

import (
	"context"
	"fmt"

	"github.com/rdborg/mediarium/internal/indexers"
)

// belowMinSeeders reports whether a torrent release has fewer seeders than the
// minimum set on the indexer it came from. A release is never below the
// minimum when the minimum is off (0), when the indexer did not say how many
// seeders there are, when the debrid service already has it cached, or when it
// is not a torrent.
func belowMinSeeders(res indexers.Result) bool {
	if res.MinSeeders <= 0 || !res.SeedersKnown || res.Cached {
		return false
	}
	if res.Protocol != indexers.ProtocolTorrent && !res.Torrent {
		return false
	}
	return res.Seeders < res.MinSeeders
}

// keepSeeded drops the torrents from the results that have fewer seeders than
// their indexer's minimum, unless the debrid service already has them cached.
// The automatic searches use it, so they never grab a torrent that is likely to
// stall. It returns the rest and how many it dropped. A search by hand does not
// use it: the list shows every release and marks the thin ones.
func (s *Server) keepSeeded(ctx context.Context, results []indexers.Result) ([]indexers.Result, int) {
	var hashes []string
	thin := 0
	for _, res := range results {
		if belowMinSeeders(res) {
			thin++
			if h := releaseHash(res); h != "" {
				hashes = append(hashes, h)
			}
		}
	}
	if thin == 0 {
		return results, 0
	}
	cached := s.cachedAnswers(ctx, hashes)
	out := make([]indexers.Result, 0, len(results))
	dropped := 0
	for _, res := range results {
		if belowMinSeeders(res) && (!cached[releaseHash(res)] || !s.mayGoToDebrid(res)) {
			dropped++
			continue
		}
		out = append(out, res)
	}
	return out, dropped
}

// seededOnly is keepSeeded for a caller that doesn't need the count.
func (s *Server) seededOnly(ctx context.Context, results []indexers.Result) []indexers.Result {
	out, _ := s.keepSeeded(ctx, results)
	return out
}

// autoCandidates is what an automatic search may pick from: the results minus
// the Usenet posts older than the retention and the torrents with too few
// seeders.
func (s *Server) autoCandidates(ctx context.Context, results []indexers.Result) []indexers.Result {
	return s.seededOnly(ctx, s.freshOnly(results))
}

// thinTally is the reason counted in the notes of an automatic search.
func thinTally() string {
	return "with fewer seeders than the indexer's minimum"
}

// addThin counts torrents dropped for having too few seeders in the note of an
// automatic search.
func (v *searchVerdict) addThin(n int) {
	if n <= 0 {
		return
	}
	v.total += n
	if v.reasons == nil {
		v.reasons = map[string]int{}
	}
	v.reasons[thinTally()] += n
}

// maxMinSeeders is the largest minimum that can be set.
const maxMinSeeders = indexers.MinSeedersMax

// checkMinSeeders wants 0 (off) or a number of seeders.
func checkMinSeeders(n int) string {
	if n < 0 || n > maxMinSeeders {
		return fmt.Sprintf("Minimum seeders must be a whole number from 0 to %d. Use 0 to turn it off.", maxMinSeeders)
	}
	return ""
}
