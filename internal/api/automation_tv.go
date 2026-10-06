package api

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

const (
	seriesRefreshInterval = 12 * time.Hour
	// maxTVSearchesPerHunt bounds indexer queries per hunt run so a big
	// backlog of missing episodes can't hammer indexers (and their API
	// limits) in one go; the rest is picked up on the next run.
	maxTVSearchesPerHunt = 25
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
}

// tvSearcher returns candidate releases for a season pack (episode == 0) or
// one episode. hunt implements it with a targeted indexer query; RSS sync
// filters one shared "latest releases" listing instead.
type tvSearcher func(series library.Series, season, episode int) []indexers.Result

func (s *Server) tvWants(profiles profileSet, scope tvScope) (wants []tvWant, upgrades []tvUpgrade, err error) {
	seriesList, err := s.MovieRepo.ListSeries()
	if err != nil {
		return nil, nil, err
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, sr := range seriesList {
		if (!sr.Monitored && !scope.force) || (scope.seriesID != 0 && sr.ID != scope.seriesID) {
			continue
		}
		profile := profiles.resolve(sr.ProfileID)
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			return nil, nil, err
		}
		bySeason := map[int][]library.Episode{}
		var order []int
		for _, ep := range eps {
			if _, seen := bySeason[ep.Season]; !seen {
				order = append(order, ep.Season)
			}
			bySeason[ep.Season] = append(bySeason[ep.Season], ep)
		}
		for _, season := range order {
			if scope.season != 0 && season != scope.season {
				continue
			}
			all := bySeason[season]
			var missing []library.Episode
			whole := len(all) > 1 && scope.episode == 0
			for _, ep := range all {
				monitored := ep.Monitored || scope.force
				aired := ep.AirDate != "" && ep.AirDate <= today
				if ep.Status != library.StatusMissing || !aired || !monitored {
					whole = false
				}
				if scope.episode != 0 && ep.Episode != scope.episode {
					continue
				}
				if monitored && ep.Status == library.StatusMissing && aired {
					missing = append(missing, ep)
				}
				if monitored && !sr.NoUpgrade && ep.Status == library.StatusDownloaded && wantsUpgrade(profile, ep.Quality) {
					upgrades = append(upgrades, tvUpgrade{profile: profile, series: sr, episode: ep})
				}
			}
			if len(missing) > 0 {
				wants = append(wants, tvWant{profile: profile, series: sr, season: season, episodes: missing, wholeSeason: whole})
			}
		}
	}
	return wants, upgrades, nil
}

// pickTVResult returns the best release for a season pack (episode == 0) or
// a single episode of series. With current == nil (nothing on disk) it tries
// the profile and then, only when nothing is acceptable to it, each of its
// fallback profiles in order; upgrades (current set) use the profile alone.
func pickTVResult(results []indexers.Result, series library.Series, season, episode int, profile quality.Profile, current *quality.Tier, mapper ...func(parser.Release) parser.Release) *indexers.Result {
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
func pickTVResultFor(results []indexers.Result, series library.Series, season, episode int, profile quality.Profile, current *quality.Tier, mapper func(parser.Release) parser.Release) *indexers.Result {
	var best *indexers.Result
	var bestKey pickKey
	for i := range results {
		rel := parseFor(mapper, results[i].Title)
		if ok, _ := profile.TitleAllowed(results[i].Title); !ok {
			continue
		}
		if !matchesShow(results[i].Title, series) || !coversTarget(rel, season, episode) {
			continue
		}
		if episode > 0 && len(rel.Episodes) == 0 {
			continue
		}
		if current == nil {
			if !profile.Accepts(rel) {
				continue
			}
		} else if !profile.IsUpgradeOverTier(*current, rel) {
			continue
		}
		eps := len(rel.Episodes)
		if eps == 0 {
			eps = quality.SeasonPack
		}
		if !quality.SizePlausible(quality.Classify(rel), results[i].SizeBytes, eps) || !profile.SizeAllowed(results[i].SizeBytes) {
			continue
		}
		key := pickKey{profile.LanguageRank(rel), quality.Rank(quality.Classify(rel)), profile.Score(results[i].Title)}
		if best == nil || key.above(bestKey) || (key == bestKey && preferUsenet(best, &results[i])) {
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

	blocked := s.blockedKeys()
	rawSearcher := searcher
	searcher = func(series library.Series, season, episode int) []indexers.Result {
		return filterSources(dropBlocked(rawSearcher(series, season, episode), blocked), s.sourcesFor(series.SourcePref))
	}

	grab := func(series library.Series, season int, best *indexers.Result) (bool, []int) {
		if _, err := s.grabTV(series, season, 0, best.Title, best.DownloadURL, best.SizeBytes, best.Protocol, searchKind(scope.force)); err != nil {
			if !errors.Is(err, errAlreadyGrabbed) && !errors.Is(err, errBlocklisted) {
				log.Printf("automation: %s: tv grab %q for %q: %v", logPrefix, best.Title, series.Title, err)
			}
			return false, nil
		}
		grabs++
		return true, parser.Parse(best.Title).Episodes
	}

	for _, w := range wants {
		if grabs > 0 && !scope.force && !s.autoGrabRoom() {
			holdBack(logPrefix)
			return grabs
		}
		if w.wholeSeason {
			if best := pickTVResult(searcher(w.series, w.season, 0), w.series, w.season, 0, w.profile, nil, s.releaseMapper(w.series)); best != nil {
				if ok, _ := grab(w.series, w.season, best); ok {
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
			best := pickTVResult(searcher(w.series, w.season, ep.Episode), w.series, w.season, ep.Episode, w.profile, nil, s.releaseMapper(w.series))
			if best == nil {
				continue
			}
			_, grabbed := grab(w.series, w.season, best)
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
		current := quality.Tier(u.episode.Quality)
		best := pickTVResult(searcher(u.series, u.episode.Season, u.episode.Episode), u.series, u.episode.Season, u.episode.Episode, u.profile, &current, s.releaseMapper(u.series))
		if best == nil {
			continue
		}
		grab(u.series, u.episode.Season, best)
	}
	return grabs
}

// targetedTVSearcher queries the indexers for one season or episode at a
// time, returning nothing once budget searches have been spent.
func (s *Server) targetedTVSearcher(ctx context.Context, instances []indexers.Instance, budget int) tvSearcher {
	return func(series library.Series, season, episode int) []indexers.Result {
		if budget <= 0 {
			return nil
		}
		budget--
		outcomes := indexers.SearchAll(ctx, instances, tvSearchQuery(series.Title, season, episode), tvCategory)
		for _, q := range s.extraTVQueries(series, season, episode) {
			outcomes = append(outcomes, indexers.SearchAll(ctx, instances, q, tvCategory)...)
		}
		s.noteTVSearch(series, season, episode, outcomes)
		return indexers.MergeResults(outcomes)
	}
}

// searchSeriesInBackground runs the automatic search for a just-added show,
// ignoring monitoring (the person asked for it explicitly).
func (s *Server) searchSeriesInBackground(seriesID int64) {
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return
	}
	s.autoGrabTV(s.targetedTVSearcher(context.Background(), instances, maxTVSearchesPerHunt), "add", profiles, tvScope{seriesID: seriesID, force: true})
}

func (s *Server) huntTV(ctx context.Context, instances []indexers.Instance, profiles profileSet) {
	s.autoGrabTV(s.targetedTVSearcher(ctx, instances, maxTVSearchesPerHunt), "hunt", profiles, tvScope{})
}

func (s *Server) rssSyncTV(ctx context.Context, instances []indexers.Instance, profiles profileSet) {
	wants, upgrades, err := s.tvWants(profiles, tvScope{})
	if err != nil || (len(wants) == 0 && len(upgrades) == 0) {
		return
	}
	latest := indexers.MergeResults(indexers.SearchAll(ctx, instances, "", tvCategory))
	s.autoGrabTV(func(library.Series, int, int) []indexers.Result { return latest }, "rss-sync", profiles, tvScope{})
}

// refreshAllSeries re-reads every monitored series' episode list from TMDB
// so newly announced episodes appear (and become huntable) without anyone
// clicking Refresh.
func (s *Server) refreshAllSeries(ctx context.Context) {
	if !s.TMDB().HasAPIKey() {
		return
	}
	seriesList, err := s.MovieRepo.ListSeries()
	if err != nil {
		log.Printf("automation: series-refresh: list series: %v", err)
		return
	}
	for _, sr := range seriesList {
		if !sr.Monitored {
			continue
		}
		if err := s.refreshSeries(ctx, sr); err != nil {
			log.Printf("automation: series-refresh: %q: %v", sr.Title, err)
		}
	}
}
