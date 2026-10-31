package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
)

// Newznab/Torznab standard categories: 2000 = movies, 5000 = TV. The
// unified search bar queries both so one box finds either kind of release
// ("true single search bar across movies + TV").
var (
	tvCategory      = []int{5000}
	mediaCategories = []int{2000, 5000}
)

// matchesShow reports whether a release title is for series: normalized
// title equality (like matchesMovie) plus a year check only when both
// sides carry one — plenty of TV releases omit the year entirely.
func matchesShow(releaseTitle string, series library.Series) bool {
	return matchesShowRel(parser.Parse(releaseTitle), series)
}

// matchesShowRel is matchesShow for a release that is already parsed.
func matchesShowRel(rel parser.Release, series library.Series) bool {
	// A daily show's air date carries a year too ("Show.2024.03.15"): that
	// is the episode's, not the show's. So is a year after an episode's marker
	// ("Show.S09E01.2019.1080p"): it is more often the air year.
	if rel.Year != 0 && series.Year != 0 && rel.Year != series.Year && rel.AirDate == "" && !(rel.YearLate && len(rel.Episodes) > 0) {
		return false
	}
	want := normalizeTitle(rel.Title)
	if want == "" {
		return false // nothing to compare: a name with no readable title is nobody's
	}
	if want == normalizeTitle(series.Title) {
		return true
	}
	if tagged, ok := countryTagMatch(rel.Title, series); tagged {
		return ok
	}
	if withCountryTag(want, normalizeTitle(series.Title), rel.Year, series.Year) {
		return true
	}
	if len(want) < 3 {
		return false
	}
	// The show's other names (an anime's romaji title, the names of its seasons).
	for _, alias := range series.Aliases {
		if normalizeTitle(alias) == want {
			return true
		}
	}
	// The show's titles in the languages that are wanted ("Haus des Geldes"
	// for "Money Heist"): the same year rule as above applies.
	return matchingAltTitle(rel.Title, series.AltTitles) != ""
}

// withCountryTag reports whether a release title is the show's own title followed
// by a country tag, the way many releases name a show that has versions in several
// countries ("Hells.Kitchen.US.S24E01" for "Hell's Kitchen"). Both titles are
// already normalised. "us" counts always, since an English search finds the American
// show first; another country counts only when the release also names the show's
// year, which tells the versions apart ("The.Office.UK.2001").
func withCountryTag(release, show string, releaseYear, showYear int) bool {
	if show == "" || len(release) <= len(show) || !strings.HasPrefix(release, show) {
		return false
	}
	switch release[len(show):] {
	case "us", "usa":
		return true
	case "uk", "au", "ca", "nz", "de", "fr", "it", "es":
		return releaseYear != 0 && releaseYear == showYear
	}
	return false
}

// matchesShowFor is matchesShow, loosened for a special (season 0): one
// posted under its own title often has no marker to end the show's name at
// ("Show.The.Christmas.Invasion.720p"), so a name that starts with the show
// is enough; mapSpecial then checks the special's title is there.
func matchesShowFor(releaseTitle string, series library.Series, season int) bool {
	return matchesShowForRel(parser.Parse(releaseTitle), series, season)
}

// matchesShowForRel is matchesShowFor for a release that is already parsed.
func matchesShowForRel(rel parser.Release, series library.Series, season int) bool {
	if matchesShowRel(rel, series) {
		return true
	}
	if season != 0 {
		return false
	}
	words := strings.Fields(metadata.TitleKey(rel.Title))
	if startsWithWords(words, series.Title) {
		return true
	}
	// A special named by one of the show's other titles.
	relWords := words
	for _, alt := range series.AltTitles {
		if len(relWords) > 0 && startsWithKeyWords(relWords, alt) {
			return true
		}
	}
	return false
}

// startsWithWords reports whether words (lower case, already split) begin with
// all the words of title and go on after them.
func startsWithWords(words []string, title string) bool {
	show := strings.Fields(metadata.TitleKey(title))
	if len(show) == 0 || len(words) <= len(show) {
		return false
	}
	for i := range show {
		if words[i] != show[i] {
			return false
		}
	}
	return true
}

// startsWithKeyWords is startsWithWords for words that are title keys (any
// alphabet, accents dropped), as an other title of a show is compared.
func startsWithKeyWords(words []string, title string) bool {
	for _, key := range metadata.TitleKeys(title) {
		show := strings.Fields(key)
		if len(show) == 0 || len(words) <= len(show) {
			continue
		}
		same := true
		for i := range show {
			if words[i] != show[i] {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	return false
}

// tvSearchQuery builds the indexer query for a season pack ("Show S01") or
// one episode ("Show S01E02") — the shape Newznab-family indexers match
// best, same as Sonarr's own fallback text query.
func tvSearchQuery(title string, season, episode int) string {
	title = indexerQuery(title)
	if episode > 0 {
		return fmt.Sprintf("%s S%02dE%02d", title, season, episode)
	}
	return fmt.Sprintf("%s S%02d", title, season)
}

// handleSeriesSearch is the interactive search for one season or one episode
// of a library show: queries every indexer, then keeps only
// releases that are actually for this show and this season/episode (a
// text search for "Show S01E02" also returns other shows and other
// episodes). For an episode, season packs are included too since they'd
// satisfy it; for a season (no episode param) only packs are.
func (s *Server) handleSeriesSearch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	season, _ := strconv.Atoi(r.URL.Query().Get("season"))
	episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
	specials := season == 0 && r.URL.Query().Get("season") == "0"
	if specials && episode <= 0 {
		writeError(w, http.StatusBadRequest, "Choose a special to search for: specials are searched one at a time.")
		return
	}
	if season < 0 || (season == 0 && !specials) {
		writeError(w, http.StatusBadRequest, "Choose a season to search.")
		return
	}
	series, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	instances, err := s.searchableIndexers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(instances) == 0 {
		writeSearch(w, r, []searchResultPayload{}, 0, nil)
		return
	}

	series = s.seriesWithAltTitles(r.Context(), series)
	// A site that lists no TV category is not asked, and is shown as left out.
	asked, left := indexers.SplitByCategories(r.Context(), instances, tvCategory)
	var notAsked []string
	for _, inst := range left {
		if inst.Enabled {
			notAsked = append(notAsked, inst.Name)
		}
	}
	// Words typed by hand are searched as they are and every answer is shown; the
	// download still goes to the season or episode that was chosen.
	custom := customSearchQuery(r)
	var outcomes []indexers.Outcome
	if custom != "" {
		outcomes = indexers.SearchAll(indexers.AsManual(r.Context()), asked, custom, tvCategory)
	} else {
		outcomes = indexers.SearchAll(indexers.AsManual(r.Context()), asked, tvSearchQuery(series.Title, season, episode), tvCategory)
		for _, q := range s.extraTVQueries(series, season, episode) {
			outcomes = append(outcomes, indexers.SearchAll(indexers.AsManual(r.Context()), asked, q, tvCategory)...)
		}
	}
	merged := indexers.MergeResults(outcomes)
	mapper := s.releaseMapper(series)

	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profile := s.profileForSeries(profiles, series)
	blocked := s.searchSkipKeys()
	sizeReasons := s.tvSizeRejections(r.Context(), series, profile)
	keeps := s.tvImportKeeps(series, season, episode)
	out := []searchResultPayload{}
	var found []indexers.Result
	for _, res := range merged {
		parsed := res.Parsed()
		if custom == "" && (!matchesShowForRel(parsed, series, season) || !coversTarget(mapped(mapper, parsed), season, episode)) {
			continue
		}
		payload := s.toSearchResultPayload(res)
		payload.MatchedTitle = matchedOtherTitle(res.Title, series.Title, series.AltTitles)
		payload.Blocklisted = blocked[blocklist.Key(res.Title)]
		payload.addReasonList(rejectionReasonsFor(res.Title, profile, keeps != keepsNothing, "", "", keeps))
		payload.addReasons("size", sizeReasons(res.Title, res.SizeBytes))
		payload.addStateReasons()
		setFormatScore(&payload, profile, res.Title)
		payload.AcceptedBy = acceptedBy(profile, res.Title)
		out = append(out, payload)
		found = append(found, res)
	}
	markBestMatch(out, found, profile)
	s.markCachedPayloads(r.Context(), out)
	writeSearchAll(w, r, out, len(instances), outcomes, notAsked, instances)
}

// coversTarget reports whether a parsed release satisfies a season/episode
// request: for an episode, that episode itself, a multi-episode release
// containing it, or a whole-season pack; for a season, only a pack. A
// special (season 0) needs a release that is that special. A pack of
// several seasons (S01-S03) is never taken for one season.
func coversTarget(rel parser.Release, season, episode int) bool {
	if rel.Season != season || (season == 0 && (!rel.Special || episode <= 0 || len(rel.Episodes) == 0)) {
		return false
	}
	if rel.SeasonTo > rel.Season {
		return false
	}
	isPack := len(rel.Episodes) == 0
	if episode == 0 {
		return isPack
	}
	if isPack {
		return true
	}
	for _, e := range rel.Episodes {
		if e == episode {
			return true
		}
	}
	return false
}

// grabFromSearchTV handles a grab from the unified search bar when the
// release turns out to be a TV release: resolve the show on TMDB, find it
// in the library or add it (with its episode list) on the spot, then grab
// — the TV counterpart of the movie resolve-add-grab flow in
// handleSearchGrab.
func (s *Server) grabFromSearchTV(w http.ResponseWriter, r *http.Request, req grabRequest, release parser.Release) {
	query := release.Title
	if base, code := parser.SplitCountryTag(query); code != "" {
		query = base // the search finds the show by its name; the tag picks among the results
	}
	shows, err := s.TMDB().SearchTV(r.Context(), query)
	if err != nil {
		writeUpstreamError(w, "look up that show", err)
		return
	}
	match := bestTVMatch(shows, release)
	if match == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Couldn't find a match for %q in the movie database.", release.Title))
		return
	}

	series, ok, err := s.MovieRepo.GetSeriesByTMDBID(match.TMDBID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		if !s.mayAddNew(w, r, auth.PermTV) {
			return
		}
		userID, byline := requester(r)
		series, err = s.addSeriesFromTMDB(r.Context(), match.TMDBID, userID, true, s.newShowFolder())
		if err != nil {
			writeUpstreamError(w, "add that show", err)
			return
		}
		_ = s.QueueRepo.LogSeriesActivity(series.ID, "added", series.Title+" (series) added to library"+byline)
	}

	queueID, err := s.grabTVOpts(series, 0, 0, req.ReleaseTitle, req.DownloadURL, req.SizeBytes, req.protocol(), grabPicked, req.Replace)
	if err != nil {
		writeGrabError(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"seriesId": series.ID, "queueId": queueID})
}

// bestTVMatch picks the TMDB show for a parsed release title: an exact
// normalized-title match wins (preferring the release's year when it has
// one). When no show is named like the release it returns nil rather than
// take TMDB's top result, the same as bestTMDBMatch for movies.
func bestTVMatch(shows []metadata.Show, release parser.Release) *metadata.Show {
	var exact *metadata.Show
	want := normalizeTitle(release.Title)
	base, code := parser.SplitCountryTag(release.Title)
	for i := range shows {
		name := normalizeTitle(shows[i].Name)
		if want == "" {
			continue
		}
		if code != "" && len(shows[i].OriginCountry) > 0 && normalizeTitle(base) == name {
			// "Shameless.UK": the show of that name made in that country.
			if !hasCountry(shows[i].OriginCountry, code) {
				continue
			}
		} else if want != name && !withCountryTag(want, name, release.Year, shows[i].Year()) {
			continue
		}
		if release.Year != 0 && shows[i].Year() == release.Year {
			return &shows[i]
		}
		// Without a tag, shows of one name go to the American one first.
		if exact == nil || (code == "" && !hasCountry(exact.OriginCountry, "US") && hasCountry(shows[i].OriginCountry, "US")) {
			exact = &shows[i]
		}
	}
	return exact // none named like the release: no guess, the person picks
}

// indexerQuery is a title as it is searched for at an indexer. A lone "&"
// ("Law & Order") is left out: releases are named "Law.and.Order" as often as
// "Law.&.Order", and an indexer matches the words either way, while a search
// with the "&" in it finds only one of the two spellings, or none. An "&"
// inside a word ("AT&T", "R&B") is part of the name and stays. HTML's "&amp;"
// that some metadata sources leave in a title counts as the same "&".
func indexerQuery(title string) string {
	words := strings.Fields(strings.ReplaceAll(title, "&amp;", "&"))
	kept := words[:0]
	for _, w := range words {
		if w != "&" {
			kept = append(kept, w)
		}
	}
	if len(kept) == 0 {
		return title
	}
	return strings.Join(kept, " ")
}
