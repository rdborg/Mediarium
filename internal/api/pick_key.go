package api

import (
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// pickKey is what automation compares releases by when choosing the best one:
// the language first (a release with no language tag or only English before
// one that adds another language, whatever their quality), then the quality
// tier (its rank in the profile's own order, so the qualities of a group tie
// and are told apart by the built-in order), then the profile's preferred-term score. Equal keys are settled by
// preferOver (indexer priority, protocol, then a cached release).
//
// cached sits between the tier and the score, and is only ever non-zero when
// the person allowed "a lower score for a cached release": a cached release
// then beats an uncached one of the same language and tier whatever the score.
// It never lifts a release over a better language or tier.
//
// A torrent its indexer reports with no seeders at all is dead: dead goes
// below everything else, so it is only picked when nothing alive is left, and
// among two otherwise equal torrents the one with more seeders wins (see
// preferOver). A release whose seeders are not known is never counted as dead.
type pickKey struct {
	alive, language, tier, sub, cached, score int
}

// above reports whether k beats other.
func (k pickKey) above(other pickKey) bool {
	if k.alive != other.alive {
		return k.alive > other.alive
	}
	if k.language != other.language {
		return k.language > other.language
	}
	if k.tier != other.tier {
		return k.tier > other.tier
	}
	if k.sub != other.sub {
		return k.sub > other.sub
	}
	if k.cached != other.cached {
		return k.cached > other.cached
	}
	return k.score > other.score
}

// picker takes the best of the releases handed to it, one call to add for
// each release that qualifies. Among releases with the best key it first sets
// aside the older upload of a name that was posted again later (see
// newerSameName), then settles what is left by preferAttr, so the answer is the
// same in whatever order the releases were found. Judging the repost rule and
// the indexer rules one pair at a time instead would depend on that order: A
// beats B for being newer, C beats A for its indexer, and B and C were never
// compared with each other.
type picker struct {
	cands []pickCand
}

type pickCand struct {
	idx int
	key pickKey
	res *indexers.Result
}

func (p *picker) add(idx int, key pickKey, res *indexers.Result) {
	p.cands = append(p.cands, pickCand{idx, key, res})
}

// best is the index given to add for the winner, and false when nothing was added.
func (p *picker) best() (int, bool) {
	if len(p.cands) == 0 {
		return -1, false
	}
	top := p.cands[0].key
	for _, c := range p.cands[1:] {
		if c.key.above(top) {
			top = c.key
		}
	}
	var tied []pickCand
	for _, c := range p.cands {
		if c.key == top {
			tied = append(tied, c)
		}
	}
	win := -1
	for i, c := range tied {
		repost := false
		for j, d := range tied {
			if i != j && newerSameName(c.res, d.res) == d.res {
				repost = true
				break
			}
		}
		if repost {
			continue
		}
		if win < 0 || preferAttr(tied[win].res, c.res) {
			win = i
		}
	}
	if win < 0 { // every one is the older of some other: cannot happen, but never return nothing
		win = 0
	}
	return tied[win].idx, true
}

// keyFor is the pickKey of a release that profile accepts.
func keyFor(profile quality.Profile, rel parser.Release, res *indexers.Result) pickKey {
	k := pickKey{alive: 1, language: profile.LanguageRank(rel), tier: profile.TierRank(quality.Classify(rel)), sub: quality.Rank(quality.Classify(rel)), score: profile.Score(res.Title)}
	if res.SeedersKnown && !res.Cached && res.Seeders <= 0 {
		k.alive = 0
	}
	if res.Cached && cachedOverScore.Load() {
		k.cached = 1
	}
	return k
}
