package api

import (
	"context"
	"strconv"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/libimport"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
)

// Shows that share a name ("Shameless" the American and the British one,
// "The Office", "Top Gear", "Doctor Who" 1963 and 2005) are told apart by the
// country TMDB gives each (library.Series.OriginCountry) and by the year.
//
// The rules for a release name:
//   - It ends in a country tag ("Shameless.US.S01E01", "Shameless (UK) S02E03"):
//     a show with a known origin country takes it only when that is the show's
//     country. UK and GB, US and USA are the same. A show whose country is not
//     read yet keeps the older rule: "US" is accepted, any other country only
//     when the release also names the show's year.
//   - It has no tag and two shows in the library share the name: the American
//     one gets it, or the one added first when none is American (see
//     keepSameNameWinner). Otherwise nothing changes.

// countryTagMatch checks a release title that ends in a country tag against a
// show whose own title is the rest of it. decided is false when the rule does
// not apply (no tag, a different title, or the show's country is not known);
// the caller then falls back on the other rules. When decided, match says
// whether the tag is the show's country.
func countryTagMatch(releaseTitle string, series library.Series) (decided, match bool) {
	if len(series.OriginCountry) == 0 {
		return false, false
	}
	base, code := parser.SplitCountryTag(releaseTitle)
	if code == "" {
		return false, false
	}
	key := normalizeTitle(series.Title)
	if key == "" || normalizeTitle(base) != key {
		return false, false
	}
	return true, hasCountry(series.OriginCountry, code)
}

// hasCountry reports whether code (as release names write it) is one of the
// show's countries.
func hasCountry(countries []string, code string) bool {
	want := parser.CanonicalCountry(code)
	if want == "" {
		return false
	}
	for _, c := range countries {
		if parser.CanonicalCountry(c) == want {
			return true
		}
	}
	return false
}

// releaseHasCountryTag reports whether a parsed release title ends in a country tag.
func releaseHasCountryTag(title string) bool {
	_, code := parser.SplitCountryTag(title)
	return code != ""
}

// showsForRelease lists the shows a release belongs to: the ones that match it
// (matchesShowFor), with the same-name rule applied.
func showsForRelease(release string, season int, series []library.Series) []library.Series {
	rel := parser.Parse(release)
	var matched []library.Series
	for _, sr := range series {
		if matchesShowForRel(rel, sr, season) {
			matched = append(matched, sr)
		}
	}
	return keepSameNameWinner(rel.Title, matched)
}

// keepSameNameWinner trims the shows that matched one release: when several
// different shows (different TMDB ids, not second copies of one show) share
// the same name, a release without a country tag goes to only one of them, the
// American show, or the one added first (the lowest id) when none is
// American. A tagged release is left alone, since the match already took the
// tag into account.
func keepSameNameWinner(parsedTitle string, matched []library.Series) []library.Series {
	if len(matched) < 2 || releaseHasCountryTag(parsedTitle) {
		return matched
	}
	groups := map[string][]library.Series{}
	for _, sr := range matched {
		k := normalizeTitle(sr.Title)
		groups[k] = append(groups[k], sr)
	}
	out := make([]library.Series, 0, len(matched))
	for _, sr := range matched {
		g := groups[normalizeTitle(sr.Title)]
		if sameNameWinnerID(g) == sr.TMDBID || !differentShows(g) {
			out = append(out, sr)
		}
	}
	return out
}

// markSameName sets YieldsUntagged on the shows of list that share their name
// with another show (a different TMDB id) and are not the one that takes
// releases without a country tag: the American show, or the one added first
// when none is. Second copies follow their main show. The automatic searches
// use it, so a release with no tag is picked for one show of the name only.
func markSameName(list []library.Series) {
	groups := map[string][]library.Series{}
	for _, sr := range list {
		if k := normalizeTitle(sr.Title); k != "" {
			groups[k] = append(groups[k], sr)
		}
	}
	for i := range list {
		g := groups[normalizeTitle(list[i].Title)]
		list[i].YieldsUntagged = len(g) > 1 && differentShows(g) && sameNameWinnerID(g) != list[i].TMDBID
	}
}

// untaggedForOtherShow reports whether a release is not for series: the show
// gives way to another one of its name, and the release neither carries a
// country tag nor the show's own year.
func untaggedForOtherShow(rel parser.Release, series library.Series) bool {
	if !series.YieldsUntagged || releaseHasCountryTag(rel.Title) {
		return false
	}
	if rel.Year != 0 && rel.Year == series.Year {
		return false
	}
	return normalizeTitle(rel.Title) == normalizeTitle(series.Title)
}

// differentShows reports whether the rows are more than one show.
func differentShows(rows []library.Series) bool {
	for _, r := range rows[1:] {
		if r.TMDBID != rows[0].TMDBID {
			return true
		}
	}
	return false
}

// sameNameWinnerID picks the TMDB id of the show an untagged release goes to.
// A monitored show is chosen before an unmonitored one, so a monitored show
// never gives way to a show that is not searched for; the American one, then
// the one added first, decides among the rest (among all of them when none is
// monitored).
func sameNameWinnerID(rows []library.Series) int {
	var monitored []library.Series
	for _, r := range rows {
		if r.Monitored {
			monitored = append(monitored, r)
		}
	}
	if len(monitored) > 0 {
		rows = monitored
	}
	best := rows[0]
	for _, r := range rows[1:] {
		bu, ru := hasCountry(best.OriginCountry, "US"), hasCountry(r.OriginCountry, "US")
		switch {
		case ru && !bu:
			best = r
		case bu && !ru:
		case r.CopyOf == 0 && best.CopyOf != 0, r.CopyOf == best.CopyOf && r.ID < best.ID:
			best = r
		}
	}
	return best.TMDBID
}

// sameNameTags names what tells shows that share a name apart, for the screens
// ("Shameless (UK)", "Doctor Who (2005)"). It only has an entry for a show
// whose name another show in the list has too. Second copies are left out.
func sameNameTags(list []library.Series) map[int64]string {
	items := make([]nameItem, 0, len(list))
	for _, sr := range list {
		if sr.CopyOf == 0 {
			items = append(items, nameItem{id: sr.ID, title: sr.Title, year: sr.Year, country: countryName(sr.OriginCountry)})
		}
	}
	return nameTagsFor(items)
}

// nameItem is one show or search result for nameTagsFor.
type nameItem struct {
	id      int64
	title   string
	year    int
	country string // as people write it ("UK"); "" when not known
}

// nameTagsFor gives each item that shares its name with another a tag that
// tells it apart: the country when every item of the name has a different
// known one, else the year when the years differ; when neither tells them
// apart there is no tag.
func nameTagsFor(items []nameItem) map[int64]string {
	groups := map[string][]nameItem{}
	for _, it := range items {
		if k := normalizeTitle(it.title); k != "" {
			groups[k] = append(groups[k], it)
		}
	}
	out := map[int64]string{}
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		tags := make([]string, len(g))
		for i, it := range g {
			tags[i] = it.country
		}
		if !allDistinct(tags) {
			for i, it := range g {
				tags[i] = ""
				if it.year != 0 {
					tags[i] = strconv.Itoa(it.year)
				}
			}
		}
		if !allDistinct(tags) {
			continue
		}
		for i, it := range g {
			out[it.id] = tags[i]
		}
	}
	return out
}

// countryName is the first of the countries as people write it ("UK", not
// "GB"), or "" when there are none.
func countryName(countries []string) string {
	if len(countries) == 0 {
		return ""
	}
	if countries[0] == "GB" {
		return "UK"
	}
	return countries[0]
}

// tagImportCandidates fills in NameTag on the candidates of one folder.
func tagImportCandidates(payload []importCandidatePayload, cands []libimport.Candidate) {
	items := make([]nameItem, len(cands))
	for i, c := range cands {
		items[i] = nameItem{id: int64(c.TMDBID), title: c.Title, year: c.Year, country: countryName(c.Countries)}
	}
	tags := nameTagsFor(items)
	for i := range payload {
		payload[i].NameTag = tags[int64(payload[i].TMDBID)]
	}
}

// allDistinct reports whether every tag is set and none repeats.
func allDistinct(tags []string) bool {
	seen := map[string]bool{}
	for _, t := range tags {
		if t == "" || seen[t] {
			return false
		}
		seen[t] = true
	}
	return true
}

// withSeriesIDs lets the indexers that take a show's id be asked by id too
// ("The Girlfriend S01" finds the wrong show, tt32267726 season 1 does not).
// Only a season or an episode is asked for this way, not the specials.
func withSeriesIDs(ctx context.Context, series library.Series, season, episode int) context.Context {
	if season <= 0 {
		return ctx
	}
	return indexers.WithIDs(ctx, indexers.IDs{IMDB: series.IMDBID, TVDB: series.TVDBID, Season: season, Episode: episode})
}
