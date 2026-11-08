package api

import (
	"context"
	"errors"
	"log"
	"sort"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

const (
	seriesRefreshInterval = 12 * time.Hour
	// maxTVSearchesPerHunt bounds indexer queries per hunt run so a big
	// backlog of missing episodes can't hammer indexers (and their API
	// limits) in one go; the rest is picked up on the next run.
	maxTVSearchesPerHunt = 25
	// tvWantsNamesTimeout is how long working out what a show wants may wait for
	// TMDB to give its other titles.
	tvWantsNamesTimeout = 2 * time.Second
)

// tvWant is one season of one series that automation should try to fill:
// the aired, monitored, missing episodes, plus whether the whole season is
// missing and fully aired (in which case a season pack is worth looking for
// before falling back to per-episode releases).
type tvWant struct {
	profile     quality.Profile
	series      library.Series
	season      int
	episodes    []library.Episode
	wholeSeason bool
	// pack is set when the season has ended and a season pack is worth looking
	// for first (see season_pack.go). With no episodes it is all there is to do
	// for the season: nothing is missing, a pack could only be an upgrade.
	pack *packPlan
}

// tvUpgrade is a downloaded episode that hasn't reached the profile's cutoff.
type tvUpgrade struct {
	profile quality.Profile
	series  library.Series
	episode library.Episode
}

// tvScope narrows automation to part of the library. The zero value means
// everything monitored; force ignores monitored flags (a manual "search
// now" on something the user has unmonitored).
type tvScope struct {
	seriesID int64
	season   int
	episode  int
	force    bool
	// asMarked keeps the monitored boxes of the episodes even when force is set:
	// a search started by adding a show, changing its profile or blocklisting a
	// release looks only for the episodes the person chose to monitor.
	asMarked bool
	// specials narrows it to season 0 (season 0 above means every season).
	// Specials are only looked for when monitored, or asked for by name.
	specials bool
	// packs lets the search look for a season pack of a season that has ended
	// (for shows that take them). Only the scheduled hunt does: it is the one
	// run that can keep to the once-a-week spacing.
	packs bool
}

// packedSeason names one season of one show.
type packedSeason struct {
	seriesID int64
	season   int
}

// tvSearcher returns candidate releases for a season pack (episode == 0) or
// one episode. hunt implements it with a targeted indexer query; RSS sync
// filters one shared "latest releases" listing instead. A nil result means no
// search was made (the run is out of searches); a search that found nothing
// returns an empty, non-nil list.
type tvSearcher func(series library.Series, season, episode int) []indexers.Result

func (s *Server) tvWants(profiles profileSet, scope tvScope) (wants []tvWant, upgrades []tvUpgrade, err error) {
	seriesList, err := s.MovieRepo.ListSeriesWithCopies()
	if err != nil {
		return nil, nil, err
	}
	markSameName(seriesList)
	today := time.Now().UTC().Format("2006-01-02")
	for _, sr := range seriesList {
		if (!sr.Monitored && !scope.force) || (scope.seriesID != 0 && sr.ID != scope.seriesID) {
			continue
		}
		profile := s.profileForSeries(profiles, sr)
		// The show's other titles are looked up once it turns out something is
		// wanted from it, so releases named by one of them are taken for it.
		namesRead := false
		withNames := func() {
			if !namesRead {
				namesRead = true
				// This also runs on the IRC announce path, where a slow TMDB must not
				// hold a fresh release back: saved names come at once, a fetch gets
				// a couple of seconds.
				ctx, cancel := context.WithTimeout(context.Background(), tvWantsNamesTimeout)
				defer cancel()
				sr = s.seriesWithAltTitles(ctx, sr)
			}
		}
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			return nil, nil, err
		}
		packsOn := scope.packs && !scope.force && scope.episode == 0 && !scope.specials && s.wantsSeasonPacks(sr)
		var searches map[int]library.PackSearch // the searches for a pack of each season, read when first needed
		bySeason := map[int][]library.Episode{}
		var order []int
		var waiting *library.Episode // the next monitored episode that has not aired yet, for the history
		wantsSomething := false
		for _, ep := range eps {
			if _, seen := bySeason[ep.Season]; !seen {
				order = append(order, ep.Season)
			}
			bySeason[ep.Season] = append(bySeason[ep.Season], ep)
		}
		for _, season := range order {
			if (scope.specials && season != 0) || (scope.season != 0 && season != scope.season) {
				continue
			}
			all := bySeason[season]
			var missing []library.Episode
			// Specials are never fetched as a pack.
			whole := len(all) > 1 && scope.episode == 0 && season > 0
			for _, ep := range all {
				monitored := ep.Monitored || (scope.force && !scope.asMarked)
				if season == 0 {
					// Searching a whole show by hand leaves its specials alone
					// unless they are monitored; asking for specials does not.
					monitored = ep.Monitored || (scope.force && scope.specials)
				}
				aired := ep.AirDate != "" && (!holdUnreleasedOn() || (ep.AirDate <= today && !ep.AirsLater(time.Now())))
				if !aired && monitored && ep.Status == library.StatusMissing && ep.AirDate != "" && season > 0 && (waiting == nil || ep.AirDate < waiting.AirDate) {
					e := ep
					waiting = &e
				}
				if ep.Status != library.StatusMissing || !aired || !monitored {
					whole = false
				}
				if scope.episode != 0 && ep.Episode != scope.episode {
					continue
				}
				if monitored && ep.Status == library.StatusMissing && aired {
					missing = append(missing, ep)
				}
				if monitored && !sr.NoUpgrade && ep.Status == library.StatusDownloaded && wantsUpgrade(profile, ep.Quality, ep.ReleaseTitle) {
					withNames()
					upgrades = append(upgrades, tvUpgrade{profile: profile, series: sr, episode: ep})
				}
			}
			var plan *packPlan
			if packsOn && season > 0 {
				if searches == nil {
					var err error
					if searches, err = s.MovieRepo.SeasonPackSearches(sr.ID); err != nil {
						log.Printf("automation: season pack: %v", err)
						searches = map[int]library.PackSearch{}
					}
				}
				if p, ok := s.seasonPackPlan(profile, sr, season, all, searches); ok {
					plan = &p
				}
			}
			if len(missing) > 0 || plan != nil {
				wantsSomething = true
				withNames()
				// A season with nothing in it yet is fetched as a pack only for a show that
				// takes packs; "episode by episode" means what it says.
				wants = append(wants, tvWant{profile: profile, series: sr, season: season, episodes: missing, wholeSeason: whole && s.wantsSeasonPacks(sr), pack: plan})
			}
		}
		// Nothing to fetch yet because the next episode has not aired: say so in
		// the show's history, for episodes coming within two weeks.
		if waiting != nil && !wantsSomething && !scope.force && waiting.AirDate <= time.Now().UTC().AddDate(0, 0, 14).Format("2006-01-02") {
			s.noteHold(0, sr.ID, unairedHoldNote(*waiting))
		}
	}
	return wants, upgrades, nil
}

// pickTVResult returns the best release for a season pack (episode == 0) or
// a single episode of series. With current == nil (nothing on disk) it tries
// the profile and then, only when nothing is acceptable to it, each of its
// fallback profiles in order; upgrades (current set) use the profile alone.
// upgradeBase is what an upgrade has to beat: the file's quality and the
// release it came from ("" when unknown).
type upgradeBase struct {
	Tier    quality.Tier
	Release string
}

func pickTVResult(results []indexers.Result, series library.Series, season, episode int, profile quality.Profile, current *upgradeBase, mapper ...func(parser.Release) parser.Release) *indexers.Result {
	var m func(parser.Release) parser.Release
	if len(mapper) > 0 {
		m = mapper[0]
	}
	if current != nil {
		return pickTVResultFor(results, series, season, episode, profile, current, m)
	}
	for _, p := range profile.Chain() {
		if best := pickTVResultFor(results, series, season, episode, p, nil, m); best != nil {
			return best
		}
	}
	return nil
}

// pickTVResultFor is pickTVResult for one profile. With current == nil it
// takes the highest-ranked profile-accepted release; otherwise only genuine
// upgrades over current. Season packs are never offered for a single
// episode — that would download a whole season to fill one gap.
func pickTVResultFor(results []indexers.Result, series library.Series, season, episode int, profile quality.Profile, current *upgradeBase, mapper func(parser.Release) parser.Release) *indexers.Result {
	var best *indexers.Result
	var bestKey pickKey
	for i := range results {
		parsed := results[i].Parsed()
		rel := mapped(mapper, parsed)
		if ok, _ := profile.TitleAllowedRel(results[i].Title, parsed); !ok {
			continue
		}
		if !matchesShowForRel(parsed, series, season) || !coversTarget(rel, season, episode) || untaggedForOtherShow(parsed, series) {
			continue
		}
		if episode > 0 && len(rel.Episodes) == 0 {
			continue
		}
		if current == nil {
			if !profile.Accepts(rel) {
				continue
			}
		} else if !profile.IsUpgradeOverRel(current.Tier, current.Release, results[i].Title, parsed) {
			continue
		}
		eps := len(rel.Episodes)
		if eps == 0 {
			eps = quality.SeasonPack
		}
		if !builtinSizeOK(quality.Classify(rel), results[i].SizeBytes, eps, knownEpisodeMinutes(series)) || !profile.SizeAllowed(results[i].SizeBytes) {
			continue
		}
		key := keyFor(profile, rel, &results[i])
		if best == nil || key.above(bestKey) || (key == bestKey && preferOver(best, &results[i])) {
			bestKey = key
			best = &results[i]
		}
	}
	return best
}

// autoGrabTV fills every wanted season/episode (and upgrades) using the
// given searcher. logPrefix names the calling job in log lines.
func (s *Server) autoGrabTV(searcher tvSearcher, logPrefix string, profiles profileSet, scope tvScope) int {
	wants, upgrades, err := s.tvWants(profiles, scope)
	if err != nil {
		log.Printf("automation: %s: tv: %v", logPrefix, err)
		return 0
	}
	grabs := 0
	rotating := !scope.force && scope.seriesID == 0
	if rotating {
		s.leastRecentlySearchedFirst(wants, upgrades)
		s.huntAt.orderEpisodes(wants)
	}

	blocked := s.searchSkipKeys()
	rawSearcher := searcher
	// limited is the search with the blocklist, the sources, the size table and
	// the delay profile applied; current is set for an upgrade search.
	refine := func(series library.Series, season, episode int, current *upgradeBase, raw []indexers.Result) []indexers.Result {
		found := filterSources(dropBlocked(raw, blocked), s.sourcesFor(series.SourcePref))
		return s.limitTVResults(series, season, episode, s.profileForSeries(profiles, series), current, !scope.force, found)
	}
	limited := func(series library.Series, season, episode int, current *upgradeBase) []indexers.Result {
		return refine(series, season, episode, current, rawSearcher(series, season, episode))
	}
	searcher = func(series library.Series, season, episode int) []indexers.Result {
		return limited(series, season, episode, nil)
	}

	// grab queues best for season (and episode, 0 for a season pack) and
	// returns the episodes it covers, read the way the show numbers them.
	grab := func(series library.Series, season, episode int, best *indexers.Result) (bool, []int) {
		if _, err := s.grabTV(series, season, episode, best.Title, best.DownloadURL, best.SizeBytes, best.Protocol, searchKind(scope.force)); err != nil {
			if !errors.Is(err, errAlreadyGrabbed) && !errors.Is(err, errBlocklisted) {
				log.Printf("automation: %s: tv grab %q for %q: %v", logPrefix, best.Title, series.Title, err)
			}
			return false, nil
		}
		grabs++
		return true, parseFor(s.releaseMapper(series), best.Title).Episodes
	}

	packed := map[packedSeason]bool{} // seasons a pack was queued for in this run
	for _, w := range wants {
		if grabs > 0 && !scope.force && !s.autoGrabRoom() {
			holdBack(logPrefix)
			return grabs
		}
		if w.pack != nil {
			// The season has ended: a pack of it comes before its episodes one by
			// one. With none good enough the episodes go on as usual.
			pack := func() ([]indexers.Result, bool) {
				raw := rawSearcher(w.series, w.season, 0)
				var current *upgradeBase
				if w.pack.over != nil {
					current = w.pack.over
				}
				return refine(w.series, w.season, 0, current, raw), raw != nil
			}
			if s.trySeasonPack(w, pack, func(best *indexers.Result) bool {
				ok, _ := grab(w.series, w.season, 0, best)
				if ok {
					s.noteFallbackGrab(0, w.series.ID, w.series.Title, w.profile, best.Title)
				}
				return ok
			}) {
				packed[packedSeason{w.series.ID, w.season}] = true
				continue
			}
			if len(w.episodes) == 0 {
				continue
			}
		}
		if w.wholeSeason && w.pack == nil { // with a pack plan the pack was just looked for
			if best := pickTVResult(searcher(w.series, w.season, 0), w.series, w.season, 0, w.profile, nil, s.releaseMapper(w.series)); best != nil {
				if ok, _ := grab(w.series, w.season, 0, best); ok {
					s.noteFallbackGrab(0, w.series.ID, w.series.Title, w.profile, best.Title)
					continue
				}
			}
		}
		covered := map[int]bool{}
		for _, ep := range w.episodes {
			if covered[ep.Episode] {
				continue
			}
			if grabs > 0 && !scope.force && !s.autoGrabRoom() {
				holdBack(logPrefix)
				return grabs
			}
			raw := rawSearcher(w.series, w.season, ep.Episode)
			if rotating && raw != nil { // nil: out of searches, nothing was asked
				s.huntAt.noteEpisode(ep.ID, time.Now())
			}
			best := pickTVResult(refine(w.series, w.season, ep.Episode, nil, raw), w.series, w.season, ep.Episode, w.profile, nil, s.releaseMapper(w.series))
			if best == nil {
				continue
			}
			_, grabbed := grab(w.series, w.season, ep.Episode, best)
			if len(grabbed) > 0 {
				s.noteFallbackGrab(0, w.series.ID, w.series.Title, w.profile, best.Title)
			}
			for _, n := range grabbed {
				covered[n] = true
			}
		}
	}

	for _, u := range upgrades {
		if grabs > 0 && !scope.force && !s.autoGrabRoom() {
			holdBack(logPrefix)
			return grabs
		}
		if packed[packedSeason{u.series.ID, u.episode.Season}] {
			continue // the season pack that was queued for it will do the upgrading
		}
		current := upgradeBase{Tier: quality.Tier(u.episode.Quality), Release: u.episode.ReleaseTitle}
		best := pickTVResult(limited(u.series, u.episode.Season, u.episode.Episode, &current), u.series, u.episode.Season, u.episode.Episode, u.profile, &current, s.releaseMapper(u.series))
		if best == nil {
			continue
		}
		grab(u.series, u.episode.Season, u.episode.Episode, best)
	}
	return grabs
}

// leastRecentlySearchedFirst puts the shows that were searched for longest ago
// (or never) first, so a run that can only afford so many searches does not
// spend them all on the same few shows every time. Shows keep their order
// among those searched at the same time.
func (s *Server) leastRecentlySearchedFirst(wants []tvWant, upgrades []tvUpgrade) {
	seen := map[int64]bool{}
	var ids []int64
	for _, id := range seriesIDsOf(wants, upgrades) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	last, err := s.QueueRepo.LastSeriesSearches(ids)
	if err != nil {
		log.Printf("automation: tv: last searches: %v", err)
		return
	}
	sort.SliceStable(wants, func(i, j int) bool { return last[wants[i].series.ID].At < last[wants[j].series.ID].At })
	sort.SliceStable(upgrades, func(i, j int) bool { return last[upgrades[i].series.ID].At < last[upgrades[j].series.ID].At })
}

func seriesIDsOf(wants []tvWant, upgrades []tvUpgrade) []int64 {
	var ids []int64
	for _, w := range wants {
		ids = append(ids, w.series.ID)
	}
	for _, u := range upgrades {
		ids = append(ids, u.series.ID)
	}
	return ids
}

// targetedTVSearcher queries the indexers for one season or episode at a
// time, returning nothing once budget searches have been spent.
func (s *Server) targetedTVSearcher(ctx context.Context, instances []indexers.Instance, budget int, force bool) tvSearcher {
	return func(series library.Series, season, episode int) []indexers.Result {
		if budget <= 0 || ctx.Err() != nil {
			return nil // out of searches for this run, or the app is shutting down
		}
		budget--
		series = s.seriesWithAltTitles(ctx, series)
		outcomes := indexers.SearchAll(ctx, instances, tvSearchQuery(series.Title, season, episode), tvCategory)
		// The other numberings (an anime's absolute number, a daily show's date)
		// and the other titles are only tried when nothing for this season or
		// episode turned up yet (a person's own search tries them all).
		mapper := s.releaseMapper(series)
		for _, q := range s.extraNumberingQueries(series, season, episode) {
			if ctx.Err() != nil || (!force && tvTargetFound(indexers.MergeResults(outcomes), series, season, episode, mapper)) {
				break
			}
			outcomes = append(outcomes, indexers.SearchAll(ctx, instances, q, tvCategory)...)
		}
		for _, q := range altTVQueries(series, season, episode) {
			if ctx.Err() != nil || (!force && tvTargetFound(indexers.MergeResults(outcomes), series, season, episode, mapper)) {
				break
			}
			outcomes = append(outcomes, indexers.SearchAll(ctx, instances, q, tvCategory)...)
		}
		s.noteTVSearch(ctx, force, series, season, episode, outcomes)
		found := s.autoCandidates(ctx, indexers.MergeResults(outcomes))
		if found == nil {
			found = []indexers.Result{} // searched, found nothing: not the same as "no search made"
		}
		return found
	}
}

// tvTargetFound reports whether any of the releases is for this show and covers
// the season or episode looked for.
func tvTargetFound(results []indexers.Result, series library.Series, season, episode int, mapper func(parser.Release) parser.Release) bool {
	for _, res := range results {
		parsed := res.Parsed()
		if matchesShowForRel(parsed, series, season) && !untaggedForOtherShow(parsed, series) && coversTarget(mapped(mapper, parsed), season, episode) {
			return true
		}
	}
	return false
}

// searchSeriesInBackground runs the automatic search for a just-added show (or
// one whose profile changed, or whose release was just blocklisted). The person
// asked for it, so it does not wait for the hourly limit, but it looks only for
// the episodes that are monitored: "future episodes only" must not fetch the
// back catalogue.
func (s *Server) searchSeriesInBackground(seriesID int64) {
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return
	}
	s.autoGrabTV(s.targetedTVSearcher(context.Background(), instances, maxTVSearchesPerHunt, true), "add", profiles, tvScope{seriesID: seriesID, force: true, asMarked: true})
}

func (s *Server) huntTV(ctx context.Context, instances []indexers.Instance, profiles profileSet) {
	s.autoGrabTV(s.targetedTVSearcher(ctx, instances, maxTVSearchesPerHunt, false), "hunt", profiles, tvScope{packs: true})
}

func (s *Server) rssSyncTV(ctx context.Context, instances []indexers.Instance, profiles profileSet) {
	wants, upgrades, err := s.tvWants(profiles, tvScope{})
	if err != nil || (len(wants) == 0 && len(upgrades) == 0) {
		return
	}
	latest := s.autoCandidates(ctx, indexers.MergeResults(indexers.SearchAll(ctx, instances, "", tvCategory)))
	s.autoGrabTV(func(library.Series, int, int) []indexers.Result { return latest }, "rss-sync", profiles, tvScope{})
}

// A show that has ended (or was cancelled) hardly ever changes, so it is read
// again once a week; the others are read once a day, and a monitored one on
// every run so new episodes are found quickly.
const (
	endedShowRefresh      = 7 * 24 * time.Hour
	continuingShowRefresh = 24 * time.Hour
)

// seriesRefreshPause is the wait between two shows, so TMDB isn't hit with
// the whole library at once. A variable so tests needn't wait.
var seriesRefreshPause = 300 * time.Millisecond

// seriesRefreshDue says whether a show's details should be read from TMDB
// again now. Shows never read (or read before the status was kept) always are.
func seriesRefreshDue(sr library.Series, now time.Time) bool {
	checked, err := time.Parse(time.RFC3339, sr.StatusCheckedAt)
	if err != nil || sr.TMDBStatus == "" {
		return true
	}
	age := now.Sub(checked)
	switch {
	case showEnded(sr.TMDBStatus):
		return age >= endedShowRefresh
	case sr.Monitored:
		return true
	default:
		return age >= continuingShowRefresh-time.Hour
	}
}

// refreshAllSeries re-reads every series' details and episode list from TMDB
// so newly announced episodes appear (and become huntable) and the show's
// status (running, ended, cancelled) stays right, without anyone clicking
// Refresh.
func (s *Server) refreshAllSeries(ctx context.Context) {
	if !s.TMDB().HasAPIKey() {
		return
	}
	seriesList, err := s.MovieRepo.ListSeries()
	if err != nil {
		log.Printf("automation: series-refresh: list series: %v", err)
		return
	}
	failures := 0
	now := time.Now()
	asked := false
	for _, sr := range seriesList {
		if sr.CopyOf != 0 || !seriesRefreshDue(sr, now) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if asked {
			select {
			case <-ctx.Done():
				return
			case <-time.After(seriesRefreshPause):
			}
		}
		asked = true
		err := s.refreshSeries(ctx, sr)
		if err == nil {
			failures = 0
			continue
		}
		log.Printf("automation: series-refresh: %q: %v", sr.Title, err)
		var status *metadata.StatusError
		if errors.As(err, &status) && status.Code == 404 {
			continue // TMDB no longer has this one: says nothing about the rest
		}
		// A rejected key fails for every show, and a dead network for most:
		// stop instead of one failed request and one log line per show.
		failures++
		if errors.Is(err, metadata.ErrInvalidKey) || failures >= 5 {
			log.Printf("automation: series-refresh: stopped after %d failures, the rest will be tried on the next run", failures)
			return
		}
	}
}
