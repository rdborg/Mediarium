package api

// pickKey is what automation compares releases by when choosing the best one:
// the language first (a release with no language tag or only English before
// one that adds another language, whatever their quality), then the quality
// tier, then the profile's preferred-term score. Equal keys are settled by
// preferUsenet.
type pickKey struct {
	language, tier, score int
}

// above reports whether k beats other.
func (k pickKey) above(other pickKey) bool {
	if k.language != other.language {
		return k.language > other.language
	}
	if k.tier != other.tier {
		return k.tier > other.tier
	}
	return k.score > other.score
}
