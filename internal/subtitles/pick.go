package subtitles

import (
	"strings"

	"github.com/rdborg/mediarium/internal/parser"
)

func normalizeRelease(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// HashMatchScore is what being made for the exact video file adds to Score.
const HashMatchScore = 500

func hasToken(normalizedRelease, token string) bool {
	token = normalizeRelease(token)
	return token != "" && strings.Contains(normalizedRelease, token)
}

// Score rates how well a subtitle result fits a video file: one made for the
// exact file (a hash match) beats everything; a subtitle timed against the
// same release (matching group, source, resolution, codec) is
// far more likely to be in sync than one for a different rip, so those
// matches dominate; rating and popularity break ties.
func Score(r Result, videoName string) float64 {
	video := parser.Parse(videoName)
	rel := normalizeRelease(r.Release)

	score := 0.0
	if r.HashMatch {
		score += HashMatchScore
	}
	if hasToken(rel, video.Group) {
		score += 100
	}
	if hasToken(rel, video.Source) {
		score += 30
	}
	if hasToken(rel, video.Resolution) {
		score += 30
	}
	if hasToken(rel, video.Codec) {
		score += 20
	}
	dl := float64(r.DownloadsAll) / 1000
	if dl > 40 {
		dl = 40
	}
	return score + dl + r.Rating*2
}

// Pick returns the best-fitting result for videoName, or nil if there are
// none.
func Pick(results []Result, videoName string) *Result {
	var best *Result
	bestScore := -1.0
	for i := range results {
		if s := Score(results[i], videoName); s > bestScore {
			bestScore = s
			best = &results[i]
		}
	}
	return best
}
