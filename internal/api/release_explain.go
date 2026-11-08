package api

import (
	"fmt"
	"sort"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// The "Why this release?" details of the search list. Every line is worked out
// from the same functions the automatic pick uses (the quality ladder, the
// custom-format score, the size and language checks, pickKey), so what the
// screen says can never disagree with what a search does. Nothing is stored.

// releaseCheck is one thing that was checked on a release, in plain words.
// State is "ok" (it passed), "bad" (it is why an automatic search leaves the
// release out or ranks it lower) or "info" (only said). Kind groups the checks
// like the reasons do: quality, profile, language, size, seeders, age,
// blocklist, upgrade, release.
type releaseCheck struct {
	Kind  string `json:"kind"`
	State string `json:"state"`
	Text  string `json:"text"`
}

// rankStep names the first thing in which two accepted releases differ, in the
// order the pick compares them (see pickKey and preferOver).
type rankStep int

const (
	stepAlive rankStep = iota + 1
	stepLanguage
	stepTier
	stepGroup
	stepCached
	stepScore
	stepIndexer
	stepProtocol
	stepDebrid
	stepSeeders
	stepFirst // equal in everything: the one found first stays
)

// rankStepOf finds the first step in which hi is ahead of lo. Both are
// releases the profile accepts and hi is the one the pick puts first.
func rankStepOf(profile quality.Profile, hi *indexers.Result, lo *indexers.Result) (rankStep, pickKey, pickKey) {
	hk, lk := keyFor(profile, hi.Parsed(), hi), keyFor(profile, lo.Parsed(), lo)
	switch {
	case hk.alive != lk.alive:
		return stepAlive, hk, lk
	case hk.language != lk.language:
		return stepLanguage, hk, lk
	case hk.tier != lk.tier:
		return stepTier, hk, lk
	case hk.sub != lk.sub:
		return stepGroup, hk, lk
	case hk.cached != lk.cached:
		return stepCached, hk, lk
	case hk.score != lk.score:
		return stepScore, hk, lk
	case indexerRank(hi.Priority) < indexerRank(lo.Priority):
		return stepIndexer, hk, lk
	case indexerRank(hi.Priority) == indexerRank(lo.Priority) && hi.Protocol == preferredProtocol() && lo.Protocol != preferredProtocol():
		return stepProtocol, hk, lk
	case hi.Cached && !lo.Cached:
		return stepDebrid, hk, lk
	case hi.SeedersKnown && lo.SeedersKnown && hi.Seeders > lo.Seeders:
		return stepSeeders, hk, lk
	}
	return stepFirst, hk, lk
}

// whyAboveNext says in plain words why hi ranks above lo, the next release an
// automatic search would take.
func whyAboveNext(profile quality.Profile, hi, lo *indexers.Result) releaseReason {
	step, hk, lk := rankStepOf(profile, hi, lo)
	text := ""
	switch step {
	case stepAlive:
		text = "It ranks above the next release because it has seeders and the next one has none."
	case stepLanguage:
		text = "It ranks above the next release because it fits your preferred language better."
	case stepTier:
		text = fmt.Sprintf("It ranks above the next release because its quality is higher: %s against %s.", quality.Classify(hi.Parsed()), quality.Classify(lo.Parsed()))
	case stepGroup:
		text = fmt.Sprintf("It ranks above the next release because it is the better quality in the same group: %s against %s.", quality.Classify(hi.Parsed()), quality.Classify(lo.Parsed()))
	case stepCached, stepDebrid:
		text = "It ranks above the next release because it is cached at your debrid service."
	case stepScore:
		text = fmt.Sprintf("It ranks above the next release because its score is higher: %d against %d.", hk.score, lk.score)
	case stepIndexer:
		text = "It ranks above the next release because it comes from an indexer with a higher priority."
	case stepProtocol:
		text = "It ranks above the next release because your settings put its protocol first."
	case stepSeeders:
		text = fmt.Sprintf("It ranks above the next release because it has more seeders: %d against %d.", hi.Seeders, lo.Seeders)
	default:
		text = "It ranks above the next release because the two are equal and it was found first."
	}
	return releaseReason{Kind: "rank", Text: text}
}

// setRankNotes puts the candidates (the releases nothing speaks against) in the
// order the pick would take them and tells each one why it stands above the
// next. The order is the pick's: pickKey, then preferOver, then the order
// found.
func setRankNotes(payloads []searchResultPayload, found []indexers.Result, profile quality.Profile) {
	var order []int
	for i := range payloads {
		if bestCandidate(payloads[i], found[i], profile) {
			order = append(order, i)
		}
	}
	if len(order) == 0 {
		return
	}
	keys := map[int]pickKey{}
	for _, i := range order {
		keys[i] = keyFor(profile, found[i].Parsed(), &found[i])
	}
	sort.SliceStable(order, func(a, b int) bool {
		i, j := order[a], order[b]
		if keys[i] != keys[j] {
			return keys[i].above(keys[j])
		}
		return preferOver(&found[j], &found[i])
	})
	for n, i := range order {
		if n+1 < len(order) {
			payloads[i].Reasons = append(payloads[i].Reasons, whyAboveNext(profile, &found[i], &found[order[n+1]]))
			continue
		}
		text := "It is the last release an automatic search would take, so nothing ranks below it."
		if len(order) == 1 {
			text = "It is the only release an automatic search would take."
		}
		payloads[i].Reasons = append(payloads[i].Reasons, releaseReason{Kind: "rank", Text: text})
	}
}

// firstReason returns the text of the first reason of a kind.
func firstReason(rs []releaseReason, kinds ...string) (string, bool) {
	for _, r := range rs {
		for _, k := range kinds {
			if r.Kind == k {
				return r.Text, true
			}
		}
	}
	return "", false
}

// explainChecks lists what was checked on a release against its profile: the
// quality, the profile's word rules, the language, the size, the seeders and
// the age, each as a pass or as the reason it failed (the reasons already on
// the payload, so a check never says the opposite of a reason). Call it after
// the reasons are in.
func explainChecks(p searchResultPayload, res indexers.Result, profile quality.Profile) []releaseCheck {
	var out []releaseCheck
	add := func(kind, state, text string) { out = append(out, releaseCheck{Kind: kind, State: state, Text: text}) }
	rel := res.Parsed()
	tier := quality.Classify(rel)

	if text, bad := firstReason(p.Reasons, "quality"); bad {
		add("quality", "bad", text)
	} else {
		add("quality", "ok", fmt.Sprintf("Quality: %s. The %q profile accepts it.", tier, profile.Name))
		if profile.UpgradeAllowed && profile.ReachedCutoffTier(tier) {
			add("quality", "info", fmt.Sprintf("It is at or above the cutoff (%s), so once it is downloaded nothing better is searched for.", profile.CutoffName()))
		}
	}
	if text, bad := firstReason(p.Reasons, "profile"); bad {
		add("profile", "bad", text)
	} else if len(profile.MustContain)+len(profile.MustNotContain) > 0 {
		add("profile", "ok", "It passes the profile's word rules (what it must and must not contain).")
	}
	if text, bad := firstReason(p.Reasons, "language"); bad {
		add("language", "bad", text)
	} else if profile.Language != "" && (p.LanguageFit == "ok" || p.LanguageFit == "mixed") {
		add("language", "ok", fmt.Sprintf("Language: %s, which your language setting accepts.", p.Language))
	}
	if text, bad := firstReason(p.Reasons, "size"); bad {
		add("size", "bad", text)
	} else if res.SizeBytes > 0 {
		add("size", "ok", fmt.Sprintf("Size: %s, inside the limits.", humanBytes(res.SizeBytes)))
	} else {
		add("size", "info", "The indexer doesn't say how big it is, so the size check lets it through.")
	}
	if res.Protocol == indexers.ProtocolTorrent && res.SeedersKnown {
		if text, bad := firstReason(p.Reasons, "seeders"); bad {
			add("seeders", "bad", text)
		} else {
			add("seeders", "ok", fmt.Sprintf("Seeders: %d.", res.Seeders))
		}
	}
	if text, bad := firstReason(p.Reasons, "age"); bad {
		add("age", "bad", text)
	} else if !res.PublishDate.IsZero() {
		add("age", "info", fmt.Sprintf("Posted on %s.", res.PublishDate.Format("2 Jan 2006")))
	}
	for _, kind := range []string{"blocklist", "upgrade", "release"} {
		if text, bad := firstReason(p.Reasons, kind); bad {
			add(kind, "bad", text)
		}
	}
	return out
}

// parsedView is how a release name reads, for the profile simulator.
type parsedView struct {
	Title      string   `json:"title"`
	Quality    string   `json:"quality"`
	Resolution string   `json:"resolution,omitempty"`
	Source     string   `json:"source,omitempty"`
	Codec      string   `json:"codec,omitempty"`
	AudioCodec string   `json:"audioCodec,omitempty"`
	HDR        string   `json:"hdr,omitempty"`
	Edition    string   `json:"edition,omitempty"`
	Group      string   `json:"group,omitempty"`
	Year       int      `json:"year,omitempty"`
	Season     int      `json:"season,omitempty"`
	Episodes   []int    `json:"episodes,omitempty"`
	SeasonPack bool     `json:"seasonPack,omitempty"`
	Languages  []string `json:"languages,omitempty"`
	Multi      bool     `json:"multi,omitempty"`
	Fixed      bool     `json:"fixed,omitempty"` // PROPER or REPACK
}

func parsedViewOf(rel parser.Release) parsedView {
	v := parsedView{
		Title: rel.Title, Quality: string(quality.Classify(rel)), Resolution: rel.Resolution, Source: rel.Source, Codec: rel.Codec,
		AudioCodec: rel.AudioCodec, HDR: rel.HDR, Edition: rel.Edition, Group: rel.Group, Year: rel.Year, Season: rel.Season,
		Episodes: rel.Episodes, Languages: rel.Languages, Multi: rel.Multi, Fixed: rel.Proper || rel.Repack,
	}
	v.SeasonPack = rel.Season > 0 && len(rel.Episodes) == 0 && rel.AirDate == ""
	return v
}
