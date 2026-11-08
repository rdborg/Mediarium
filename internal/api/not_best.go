package api

import (
	"fmt"
	"strings"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// preferredHit is one preferred term of the profile that a release matched,
// with the score it adds.
type preferredHit struct {
	Term  string `json:"term"`
	Score int    `json:"score"`
}

// setPreferredHits lists the preferred terms a release matches, for the
// "why this score" details, and sets Score: the number the pick compares
// (Profile.Score), which is what the Score column shows. Score is only set
// when the profile scores something, so a list with nothing to score has no
// Score column.
func (p *searchResultPayload) setPreferredHits(profile quality.Profile, title string) {
	for _, h := range profile.PreferredHits(title) {
		p.Terms = append(p.Terms, preferredHit{Term: h.Term, Score: h.Score})
	}
	if len(profile.Formats) > 0 || len(profile.Preferred) > 0 {
		score := profile.Score(title)
		p.Score = &score
		p.FixScore = profile.FixScore(title)
	}
}

// signedScore writes a score the way the lists do: +100, -50, 0.
func signedScore(n int) string {
	if n > 0 {
		return fmt.Sprintf("+%d", n)
	}
	return fmt.Sprintf("%d", n)
}

// scoreParts words where a release's score comes from: the custom formats and
// preferred terms it matched, each with its score ("x265-FuN +100, x265 +50"),
// or 0 when none matched.
func scoreParts(profile quality.Profile, title string) string {
	var parts []string
	_, formats := profile.FormatBreakdown(title)
	for _, f := range formats {
		parts = append(parts, f.Name+" "+signedScore(f.Score))
	}
	for _, h := range profile.PreferredHits(title) {
		parts = append(parts, h.Term+" "+signedScore(h.Score))
	}
	if fix := profile.FixScore(title); fix != 0 {
		parts = append(parts, "fixed release "+signedScore(fix))
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, ", ")
}

// whyNotBest says in plain words which step made the best match win over a
// release that was accepted too. It walks the pick key in the order the pick
// does (alive, language, quality tier, the order inside a group, cached, score) and names the first step
// where the two differ; an exact tie is settled the way preferOver settles it
// (indexer priority, protocol, cached, seeders), or by the order they were found.
func whyNotBest(profile quality.Profile, best *indexers.Result, bestRel parser.Release, res *indexers.Result, rel parser.Release) releaseReason {
	step, bk, k := rankStepOf(profile, best, res)
	text := ""
	switch step {
	case stepAlive:
		text = "It has no seeders, so an automatic search only takes it when nothing alive is left."
	case stepLanguage:
		text = "The best match fits your preferred language better."
		if len(quality.ParseLanguages(profile.Language)) > 1 {
			text = "The best match names your languages better."
		}
	case stepTier:
		text = fmt.Sprintf("The best match has a higher quality: %s against %s.", quality.Classify(bestRel), quality.Classify(rel))
	case stepGroup:
		text = fmt.Sprintf("Both are in the same quality group, but the best match is the better one in it: %s against %s.", quality.Classify(bestRel), quality.Classify(rel))
	case stepCached:
		text = "The best match is cached at your debrid service, and your settings put a cached release ahead of a higher score."
	case stepScore:
		text = fmt.Sprintf("The best match has a higher score: %d against %d. Best match: %s. This release: %s.", bk.score, k.score, scoreParts(profile, best.Title), scoreParts(profile, res.Title))
	case stepIndexer:
		text = "It is an equal match, but the best match comes from an indexer with a higher priority."
	case stepProtocol:
		text = "It is an equal match, but your settings put the protocol of the best match first."
	case stepDebrid:
		text = "It is an equal match, but the best match is cached at your debrid service."
	case stepSeeders:
		text = fmt.Sprintf("It is an equal match, but the best match has more seeders: %d against %d.", best.Seeders, res.Seeders)
	default:
		text = "It is an equal match that was found after the best match, so the first one was kept."
	}
	return releaseReason{Kind: "notBest", Text: text}
}
