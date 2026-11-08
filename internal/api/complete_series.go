package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
)

// "Search for the complete series". For a show that has finished, one click
// looks for a season pack of every monitored season that still has gaps, and
// when some season has no pack of its own it looks for a pack of the whole
// series ("Complete Series", "S01-S09", "Seasons 1-9") that covers all the
// seasons that are wanted and fits the quality profile.

// openEnded is the last season of a pack that says "complete" without naming
// the seasons.
const openEnded = 1 << 20

// releaseSeasonRange is the seasons a release delivers: its own season, or the
// first to the last of a pack of several seasons or of a whole series.
func releaseSeasonRange(releaseTitle string, season int) (lo, hi int) {
	if from, to, ok := parser.Parse(releaseTitle).SeasonSpan(); ok {
		if to == 0 {
			to = openEnded
		}
		return from, to
	}
	return season, season
}

// releaseCoversEpisode is coversEpisode for a download that may hold several
// seasons.
func releaseCoversEpisode(releaseTitle string, season int, episodes []int, ep library.Episode) bool {
	lo, hi := releaseSeasonRange(releaseTitle, season)
	if hi > lo {
		return ep.Season >= lo && ep.Season <= hi && len(episodes) == 0
	}
	return coversEpisode(season, episodes, ep)
}

// seasonRangeEpisodes lists the episodes a pack of seasons lo to hi is
// expected to deliver: those not downloaded yet, or all of them when every
// one is in already (a pack picked to replace them goes through the import
// settings). Specials are only in it when the range starts at season 0.
func (s *Server) seasonRangeEpisodes(seriesID int64, lo, hi int) ([]library.Episode, error) {
	all, err := s.MovieRepo.ListEpisodes(seriesID)
	if err != nil {
		return nil, err
	}
	var open, every []library.Episode
	for _, ep := range all {
		if ep.Season < lo || ep.Season > hi {
			continue
		}
		every = append(every, ep)
		if ep.Status != library.StatusDownloaded {
			open = append(open, ep)
		}
	}
	switch {
	case len(open) > 0:
		return open, nil
	case len(every) > 0:
		return every, nil
	}
	return nil, fmt.Errorf("no episodes to fill in seasons %d to %d", lo, min(hi, 99))
}

// tvGrabTargetsFor is tvGrabTargets for a release: a pack of several seasons
// (or of the whole series) is expected to deliver the episodes of all of them.
func (s *Server) tvGrabTargetsFor(seriesID int64, releaseTitle string, season int, episodes []int) ([]library.Episode, error) {
	if len(episodes) == 0 {
		if lo, hi := releaseSeasonRange(releaseTitle, season); hi > lo {
			return s.seasonRangeEpisodes(seriesID, lo, hi)
		}
	}
	return s.tvGrabTargets(seriesID, season, episodes)
}

// seasonsLabel names the seasons of a pack for a notification or a message.
func seasonsLabel(lo, hi int) string {
	if hi == openEnded {
		return "Complete series"
	}
	return fmt.Sprintf("Seasons %d-%d", lo, hi)
}

// seasonNeed is one season that still has gaps.
type seasonNeed struct {
	season  int
	missing int
	// floor is the rank of the best quality among the episodes already in, so a
	// pack below it is not taken.
	floor int
}

// completeNeeds lists the seasons that have monitored, aired, missing episodes,
// and the number of seasons left alone because something is downloading for
// them already. Specials are never part of it.
func completeNeeds(profile quality.Profile, eps []library.Episode, now time.Time) (needs []seasonNeed, busy int) {
	today := now.UTC().Format("2006-01-02")
	bySeason := map[int][]library.Episode{}
	for _, ep := range eps {
		if ep.Season > 0 {
			bySeason[ep.Season] = append(bySeason[ep.Season], ep)
		}
	}
	seasons := make([]int, 0, len(bySeason))
	for n := range bySeason {
		seasons = append(seasons, n)
	}
	sort.Ints(seasons)
	for _, n := range seasons {
		need := seasonNeed{season: n}
		downloading := false
		for _, ep := range bySeason[n] {
			switch ep.Status {
			case library.StatusDownloading:
				downloading = true
			case library.StatusDownloaded:
				if r := profile.TierRank(quality.Tier(ep.Quality)); r > need.floor {
					need.floor = r
				}
			case library.StatusMissing:
				aired := ep.AirDate != "" && (!holdUnreleasedOn() || (ep.AirDate <= today && !ep.AirsLater(now)))
				if ep.Monitored && aired {
					need.missing++
				}
			}
		}
		switch {
		case need.missing == 0:
		case downloading:
			busy++
		default:
			needs = append(needs, need)
		}
	}
	return needs, busy
}

// packCovers reports whether a pack from season from to season to (0 for
// "complete", every season) holds all the seasons wanted (sorted).
func packCovers(from, to int, seasons []int) bool {
	if len(seasons) == 0 {
		return false
	}
	return from <= seasons[0] && (to == 0 || to >= seasons[len(seasons)-1])
}

// packSeasonCount is how many seasons a pack holds, for judging its size
// against limits that are meant for one season. A pack that says "complete"
// holds the show's seasons from its first on.
func packSeasonCount(from, to, showSeasons int) int {
	if to == 0 {
		to = showSeasons
	}
	return max(to-from+1, 1)
}

// pickCompletePack chooses the pack of a whole series (or of several seasons)
// to take: one that is for this show, holds all the seasons wanted, and that a
// profile of the chain accepts. Sizes are judged per season: sizeOK says
// whether the size table allows a release (it is given the number of seasons).
func pickCompletePack(results []indexers.Result, series library.Series, seasons []int, showSeasons int, profile quality.Profile, sizeOK func(res indexers.Result, seasonCount int) bool) *indexers.Result {
	for _, p := range profile.Chain() {
		if p.NoSeasonPacks {
			continue
		}
		var best *indexers.Result
		var bestKey pickKey
		for i := range results {
			res := &results[i]
			parsed := res.Parsed()
			from, to, ok := parsed.SeasonSpan()
			if !ok || !packCovers(from, to, seasons) {
				continue
			}
			if ok, _ := p.TitleAllowedRel(res.Title, parsed); !ok {
				continue
			}
			if !matchesShowRel(parsed, series) || untaggedForOtherShow(parsed, series) || !p.Accepts(parsed) {
				continue
			}
			count := packSeasonCount(from, to, showSeasons)
			per := res.SizeBytes / int64(count)
			if !builtinSizeOK(quality.Classify(parsed), res.SizeBytes, quality.SeasonPack, knownEpisodeMinutes(series)) || !p.SizeAllowed(per) || (sizeOK != nil && !sizeOK(*res, count)) {
				continue
			}
			key := keyFor(p, parsed, res)
			if best == nil || key.above(bestKey) || (key == bestKey && preferOver(best, res)) {
				bestKey = key
				best = res
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}

// wholeSeriesQueries are the searches for a pack of the whole series.
func wholeSeriesQueries(title string, lastSeason int) []string {
	t := indexerQuery(title)
	q := []string{t + " complete series"}
	if lastSeason > 1 {
		q = append(q, fmt.Sprintf("%s S01-S%02d", t, lastSeason), fmt.Sprintf("%s seasons 1-%d", t, lastSeason))
	}
	return q
}

// seasonsText is "season 4" or "seasons 1, 2 and 5".
func seasonsText(seasons []int) string {
	parts := make([]string, len(seasons))
	for i, n := range seasons {
		parts[i] = strconv.Itoa(n)
	}
	switch len(parts) {
	case 0:
		return "no seasons"
	case 1:
		return "season " + parts[0]
	}
	return "seasons " + strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// completeSearchResult is what a search for the complete series did.
type completeSearchResult struct {
	Grabbed int
	Message string
}

// wholeSeriesSizeOK is the size table applied per season to a pack of several.
func (s *Server) wholeSeriesSizeOK(ctx context.Context, series library.Series) func(res indexers.Result, seasonCount int) bool {
	table := s.sizeLimitsFor("tv")
	var minutes float64
	return func(res indexers.Result, seasonCount int) bool {
		if !table.Enabled || res.SizeBytes <= 0 {
			return true
		}
		if minutes == 0 {
			minutes = s.episodeRuntime(ctx, series)
		}
		per := res.SizeBytes / int64(max(seasonCount, 1))
		return table.Check(quality.Classify(res.Parsed()), per, minutes, quality.SeasonPack) == ""
	}
}

// searchCompleteSeries looks for the packs that fill the gaps of a show and
// grabs them: a pack for each season that has gaps, or one pack of the whole
// series when some season has no pack of its own. Everything it does and the
// reason when it finds nothing goes into the show's history.
func (s *Server) searchCompleteSeries(ctx context.Context, sr library.Series, instances []indexers.Instance, profiles profileSet) (completeSearchResult, error) {
	profile := s.profileForSeries(profiles, sr)
	say := func(level queue.Level, msg string) { s.itemEvent(0, sr.ID, "searched", level, msg) }

	if profile.NoSeasonPacks {
		say(queue.LevelWarn, fmt.Sprintf("Searched for the complete series: not done, the %q profile never downloads season packs", profile.Name))
		return completeSearchResult{Message: fmt.Sprintf("The %q profile never downloads season packs, so there is nothing to look for. Turn that off in the profile to search for packs.", profile.Name)}, nil
	}
	eps, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return completeSearchResult{}, fmt.Errorf("list episodes: %w", err)
	}
	needs, busy := completeNeeds(profile, eps, time.Now())
	if len(needs) == 0 {
		msg := "Nothing is missing from the seasons you monitor, so there was nothing to look for."
		if busy > 0 {
			msg = "Every season with missing episodes is already downloading."
		}
		say(queue.LevelInfo, "Searched for the complete series: "+strings.TrimSuffix(msg, "."))
		return completeSearchResult{Message: msg}, nil
	}
	seasons := make([]int, len(needs))
	showSeasons := 0
	for i, n := range needs {
		seasons[i] = n.season
	}
	for _, ep := range eps {
		showSeasons = max(showSeasons, ep.Season)
	}
	_ = s.QueueRepo.LogSeriesActivity(sr.ID, "searching", fmt.Sprintf("Looking for the complete series: a season pack for %s first, then a pack of the whole series", seasonsText(seasons)))

	blocked := s.searchSkipKeys()
	mapper := s.releaseMapper(sr)
	sources := s.sourcesFor(sr.SourcePref)

	// Step one: a pack for each season that has gaps.
	searcher := s.targetedTVSearcher(ctx, instances, len(needs)+1, true)
	picks := map[int]*indexers.Result{}
	for _, n := range needs {
		raw := searcher(sr, n.season, 0)
		if raw == nil {
			continue // out of searches, or the app is shutting down
		}
		found := filterSources(dropBlocked(raw, blocked), sources)
		found = s.limitTVResults(sr, n.season, 0, profile, nil, false, found)
		if best := pickSeasonPack(found, sr, n.season, profile, packPlan{floor: n.floor}, mapper); best != nil {
			picks[n.season] = best
		}
	}

	// Step two: a pack of the whole series, when some season has no pack.
	var whole *indexers.Result
	if len(picks) < len(needs) && ctx.Err() == nil {
		whole = s.findWholeSeriesPack(ctx, sr, instances, profile, seasons, showSeasons, blocked, sources)
	}

	var res completeSearchResult
	var failures []string
	grab := func(best *indexers.Result, season int) bool {
		if _, err := s.grabTV(sr, season, 0, best.Title, best.DownloadURL, best.SizeBytes, best.Protocol, grabSearchNow); err != nil {
			if !errors.Is(err, errAlreadyGrabbed) && !errors.Is(err, errBlocklisted) {
				log.Printf("search-complete: tv grab %q for %q: %v", best.Title, sr.Title, err)
				failures = append(failures, fmt.Sprintf("%s: %s", best.Title, plainerror.Message(err)))
			}
			return false
		}
		s.noteFallbackGrab(0, sr.ID, sr.Title, profile, best.Title)
		res.Grabbed++
		return true
	}

	var got, lacking []int
	switch {
	case whole != nil && grab(whole, seasons[0]):
		got = seasons
		from, to, _ := whole.Parsed().SeasonSpan()
		if to == 0 {
			say(queue.LevelInfo, fmt.Sprintf("Searched for the complete series: no season pack for %s, so took the pack of the whole series, %q", seasonsText(lackingOf(seasons, picks)), whole.Title))
		} else {
			say(queue.LevelInfo, fmt.Sprintf("Searched for the complete series: no season pack for %s, so took %q, which holds seasons %d to %d", seasonsText(lackingOf(seasons, picks)), whole.Title, from, to))
		}
		res.Message = fmt.Sprintf("Found a pack of the whole series and started it: %s. You can follow it in Activity.", whole.Title)
	default:
		for _, n := range seasons {
			if best, ok := picks[n]; ok && grab(best, n) {
				got = append(got, n)
			} else {
				lacking = append(lacking, n)
			}
		}
	}

	if res.Grabbed > 0 && len(lacking) == 0 {
		if res.Message == "" {
			res.Message = fmt.Sprintf("Started %s, for %s. You can follow the progress in Activity.", plural(res.Grabbed, "download"), seasonsText(got))
		}
		return res, nil
	}
	// Some seasons, or all, got nothing.
	why := "no pack of the whole series that fits your quality profile was found either"
	if len(picks) == len(needs) {
		why = "a season pack was found but could not be started"
	}
	if len(failures) > 0 {
		why = failures[0]
	}
	if res.Grabbed > 0 {
		res.Message = fmt.Sprintf("Started %s, for %s. For %s, %s. The Find missing episodes button can look for those one episode at a time.", plural(res.Grabbed, "download"), seasonsText(got), seasonsText(lacking), why)
		say(queue.LevelWarn, fmt.Sprintf("Searched for the complete series: started packs for %s; nothing for %s (%s)", seasonsText(got), seasonsText(lacking), why))
		return res, nil
	}
	res.Message = fmt.Sprintf("Nothing suitable was found for %s: %s. The history of this show says what was looked at.", seasonsText(seasons), why)
	if hint := foreignShowHint(sr); hint != "" {
		res.Message += " " + hint
	}
	say(queue.LevelWarn, fmt.Sprintf("Searched for the complete series: nothing taken for %s (%s)", seasonsText(seasons), why))
	return res, nil
}

// lackingOf lists the seasons that have no pick.
func lackingOf(seasons []int, picks map[int]*indexers.Result) []int {
	var out []int
	for _, n := range seasons {
		if picks[n] == nil {
			out = append(out, n)
		}
	}
	return out
}

// findWholeSeriesPack searches for a pack of the whole series and returns the
// one to take, if any. What was found, and why none was good enough, goes into
// the show's history.
func (s *Server) findWholeSeriesPack(ctx context.Context, sr library.Series, instances []indexers.Instance, profile quality.Profile, seasons []int, showSeasons int, blocked map[string]bool, sources string) *indexers.Result {
	sr = s.seriesWithAltTitles(ctx, sr)
	var outcomes []indexers.Outcome
	for _, q := range wholeSeriesQueries(sr.Title, showSeasons) {
		if ctx.Err() != nil {
			return nil
		}
		outcomes = append(outcomes, indexers.SearchAll(ctx, instances, q, tvCategory)...)
	}
	merged := indexers.MergeResults(outcomes)
	sizeOK := s.wholeSeriesSizeOK(ctx, sr)

	// What the history says: judged like the pick, release by release.
	fresh, expired := s.freshResults(merged)
	fresh, thin := s.keepSeeded(ctx, fresh)
	keep := func(res indexers.Result) (bool, string) {
		parsed := res.Parsed()
		if !matchesShowRel(parsed, sr) || untaggedForOtherShow(parsed, sr) {
			return false, "for another show"
		}
		from, to, ok := parsed.SeasonSpan()
		switch {
		case !ok:
			return false, "not a pack of several seasons"
		case !packCovers(from, to, seasons):
			return false, "missing some of the seasons wanted"
		}
		return true, ""
	}
	limit := func(res indexers.Result) string {
		from, to, _ := res.Parsed().SeasonSpan()
		count := packSeasonCount(from, to, showSeasons)
		if !profile.SizeAllowed(res.SizeBytes/int64(count)) || !sizeOK(res, count) {
			return reasonSize
		}
		return ""
	}
	v := judgeSearchLimited(fresh, blocked, sources, profile, keep, "", "", limit)
	v.addExpired(expired, s.retentionDays())
	v.addThin(thin)
	v.failedIndexers = failedIndexers(outcomes)

	candidates := filterSources(dropBlocked(s.autoCandidates(ctx, merged), blocked), sources)
	best := pickCompletePack(candidates, sr, seasons, showSeasons, profile, sizeOK)
	msg, level := v.message("Searched for a pack of the whole series", pickedText(profile, best))
	if hint := foreignShowHint(sr); best == nil && hint != "" && (strings.Contains(msg, "none acceptable") || strings.Contains(msg, "no releases found")) {
		msg += ". " + hint
	}
	s.itemEvent(0, sr.ID, "searched", level, msg)
	return best
}

// handleSeriesSearchComplete is the "Search for the complete series" button of a
// show: it looks for season packs of the seasons with gaps and, failing that,
// for a pack of the whole series.
func (s *Server) handleSeriesSearchComplete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	instances, ok := s.hasIndexers(w)
	if !ok {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	res, err := s.searchCompleteSeries(r.Context(), sr, instances, profiles)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, searchNowResponse{Grabbed: res.Grabbed, Message: res.Message})
}
