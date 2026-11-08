package api

import (
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/quality"
)

// markBestMatch flags the one release in an interactive search that automatic
// search would take: of the releases nothing speaks against (no rejection, not
// blocklisted), the one with the best pickKey, which is the language, then the
// quality, then the preferred-term and custom-format score, with the same
// tie-breaks. The search list shows it first with a "Best match" label.
// payloads and found run in step; nothing is marked when no release qualifies.
func markBestMatch(payloads []searchResultPayload, found []indexers.Result, profile quality.Profile) {
	best := -1
	var bestKey pickKey
	for i := range payloads {
		payloads[i].setPreferredHits(profile, found[i].Title)
		payloads[i].Checks = explainChecks(payloads[i], found[i], profile)
		if !bestCandidate(payloads[i], found[i], profile) {
			continue
		}
		key := keyFor(profile, found[i].Parsed(), &found[i])
		if best < 0 || key.above(bestKey) || (key == bestKey && preferOver(&found[best], &found[i])) {
			best, bestKey = i, key
		}
	}
	if best < 0 {
		return
	}
	payloads[best].Best = true
	setRankNotes(payloads, found, profile)
	// Every other release the pick could have taken says why it did not.
	for i := range payloads {
		if i == best || !bestCandidate(payloads[i], found[i], profile) {
			continue
		}
		payloads[i].Reasons = append(payloads[i].Reasons, whyNotBest(profile, &found[best], found[best].Parsed(), &found[i], found[i].Parsed()))
	}
}

// bestCandidate reports whether nothing speaks against taking a release: no
// rejection, not blocklisted, not below the minimum seeders, and a quality and
// language the profile accepts.
func bestCandidate(p searchResultPayload, res indexers.Result, profile quality.Profile) bool {
	if len(p.Rejections) > 0 || p.Blocklisted || p.BelowMinSeeders {
		return false
	}
	return profile.Accepts(res.Parsed())
}
