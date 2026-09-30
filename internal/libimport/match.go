package libimport

// MatchStatus says how much a metadata search result can be trusted for a
// group found on disk.
type MatchStatus string

const (
	// MatchMatched is an unambiguous title (+year) match, safe to pre-select.
	MatchMatched MatchStatus = "matched"
	// MatchAmbiguous means candidates exist but none is clearly the one, so
	// the user has to choose.
	MatchAmbiguous MatchStatus = "ambiguous"
	// MatchUnmatched means nothing came back.
	MatchUnmatched MatchStatus = "unmatched"
)

const maxCandidates = 5

// Candidate is one metadata search result.
type Candidate struct {
	TMDBID     int    `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	PosterPath string `json:"-"`
}

// Decide ranks cands for a group titled title (year 0 = unknown) and
// reports how confident the top choice is. The returned candidates are
// best-first, capped for display; for MatchMatched the first is the match.
func Decide(title string, year int, cands []Candidate) (MatchStatus, []Candidate) {
	want := normalize(title)
	var exactYear, nearYear, exactTitle, rest []Candidate
	for _, c := range cands {
		switch {
		case normalize(c.Title) != want:
			rest = append(rest, c)
		case year > 0 && c.Year == year:
			exactYear = append(exactYear, c)
		case year > 0 && c.Year != 0 && abs(c.Year-year) <= 1:
			nearYear = append(nearYear, c)
		default:
			exactTitle = append(exactTitle, c)
		}
	}
	ordered := make([]Candidate, 0, len(cands))
	for _, list := range [][]Candidate{exactYear, nearYear, exactTitle, rest} {
		ordered = append(ordered, list...)
	}
	if len(ordered) > maxCandidates {
		ordered = ordered[:maxCandidates]
	}

	switch {
	case len(cands) == 0:
		return MatchUnmatched, nil
	case len(exactYear) > 0:
		return MatchMatched, ordered
	case year == 0 && len(exactTitle) == 1:
		return MatchMatched, ordered
	case year > 0 && len(nearYear) == 1:
		return MatchMatched, ordered
	}
	return MatchAmbiguous, ordered
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
