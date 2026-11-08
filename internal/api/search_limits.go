package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/delay"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
)

// This file applies the size table and the delay profile to what automatic
// searches find. Both work on the list of releases before the best one is
// picked, so a release that is too small, too large or held back is simply
// not a candidate, as in Radarr and Sonarr.

// setupSearchExtras wires the pieces that need the database: the held-release
// store, the preferred protocol, and counting grabs for the indexer statistics.
func (s *Server) setupSearchExtras() {
	s.delays = delay.NewStore(s.db, nil)
	setPreferredProtocol(s.delayConfig().PreferredProtocol)
	s.loadMinSizeCheck()
	s.loadHoldUnreleased()
	s.setCachedPolicy()
	tr := s.IndexerRepo.Tracker()
	s.QueueRepo.SetOnEnqueue(func(it queue.Item) { tr.NoteGrab(it.NZBURL, it.SizeBytes) })
	s.QueueRepo.SetIndexerOf(s.indexerOfRelease)
}

// ---- size limits ----

// applySizeTable drops the releases the size table refuses. runtime is asked
// only when the table is on and a release has a size; episodes says how many
// episodes a release holds (0 for a movie). It also returns the reason the
// first refused release gave, "" when none was refused.
func (s *Server) applySizeTable(kind string, results []indexers.Result, runtime func() float64, episodes func(res indexers.Result) int) ([]indexers.Result, string) {
	table := s.sizeLimitsFor(kind)
	if !table.Enabled || len(results) == 0 {
		return results, ""
	}
	var (
		minutes float64
		first   string
	)
	out := make([]indexers.Result, 0, len(results))
	for _, res := range results {
		if res.SizeBytes > 0 {
			if minutes == 0 {
				minutes = runtime()
			}
			tier := quality.Classify(res.Parsed())
			if reason := table.Check(tier, res.SizeBytes, minutes, episodes(res)); reason != "" {
				if first == "" {
					first = reason
				}
				continue
			}
		}
		out = append(out, res)
	}
	return out, first
}

// sizeRejections explains, for the list of an interactive search, why a
// release is outside the size limits: the built-in minimum for its quality, the
// profile's largest size, and the size table. Grabbing it by hand is still
// allowed.
func (s *Server) sizeRejections(table quality.SizeLimits, minutes float64, profile quality.Profile, title string, size int64, episodes int) []string {
	if size <= 0 {
		return nil
	}
	var out []string
	tier := quality.Classify(parser.Parse(title))
	if !builtinSizeOK(tier, size, episodes, minutes) {
		out = append(out, fmt.Sprintf("smaller than a real %s release is likely to be (you can turn this check off in Settings > Library > Quality)", tier))
	}
	if !profile.SizeAllowed(size) {
		out = append(out, fmt.Sprintf("larger than the %q profile's limit of %s GB", profile.Name, trimFloat(profile.MaxSizeGB)))
	}
	if reason := table.Check(tier, size, minutes, episodes); reason != "" {
		out = append(out, reason)
	}
	return out
}

func trimFloat(v float64) string { return fmt.Sprintf("%g", v) }

// movieSizeRejections is sizeRejections for the releases of a movie.
func (s *Server) movieSizeRejections(ctx context.Context, m library.Movie, profile quality.Profile) func(title string, size int64) []string {
	table := s.sizeLimitsFor("movie")
	var minutes float64
	return func(title string, size int64) []string {
		if table.Enabled && minutes == 0 {
			minutes = s.movieRuntime(ctx, m)
		}
		return s.sizeRejections(table, minutes, profile, title, size, 0)
	}
}

// episodeCount is how many episodes a release holds, SeasonPack when it lists none.
func episodeCount(mapper func(parser.Release) parser.Release, rel parser.Release) int {
	if n := len(mapped(mapper, rel).Episodes); n > 0 {
		return n
	}
	return quality.SeasonPack
}

// tvSizeRejections is sizeRejections for the releases of a show.
func (s *Server) tvSizeRejections(ctx context.Context, series library.Series, profile quality.Profile) func(title string, size int64) []string {
	table := s.sizeLimitsFor("tv")
	mapper := s.releaseMapper(series)
	var minutes float64
	return func(title string, size int64) []string {
		if (table.Enabled || minSizeCheckOn()) && minutes == 0 {
			minutes = s.episodeRuntime(ctx, series)
		}
		return s.sizeRejections(table, minutes, profile, title, size, episodeCount(mapper, parser.Parse(title)))
	}
}

// ---- delay profile ----

// holdBack takes out of results the releases an automatic search must leave
// alone for now because of the delay profile, and remembers them so the title
// is searched again when the wait is over. relevant says which releases are
// for the item at all (the others are left in: the picker refuses them). note
// writes the line in the title's activity, once per release. With remember
// false it only works out what would be held (the IRC test box): nothing is
// stored and nothing is written. It also returns when the earliest held
// release is let through, the zero time when none was held.
func (s *Server) holdBack(remember bool, it delay.Item, profile quality.Profile, results []indexers.Result, relevant func(indexers.Result) bool, note func(string)) ([]indexers.Result, time.Time) {
	cfg := s.delayConfig()
	if !cfg.Active() || s.delays == nil {
		return results, time.Time{}
	}
	now := time.Now()
	kept := make([]indexers.Result, 0, len(results))
	var (
		held     []indexers.Result
		earliest time.Time
		anyFirst bool
	)
	for _, res := range results {
		if relevant != nil && !relevant(res) {
			kept = append(kept, res)
			continue
		}
		by, _, ok := profile.AcceptedBy(res.Title)
		if !ok {
			kept = append(kept, res) // not a candidate whatever the wait
			continue
		}
		rel := delay.Release{
			Protocol: string(res.Protocol),
			Posted:   res.PublishDate,
			Highest:  by.TierRank(quality.Classify(res.Parsed())) >= by.HighestRank(),
		}
		if res.PublishDate.IsZero() {
			rel.FirstSeen = s.delays.FirstSeen(it, res.Title)
		}
		d := cfg.Decide(rel, now)
		if !d.Hold {
			kept = append(kept, res)
			continue
		}
		var first bool
		if remember {
			var err error
			first, err = s.delays.Hold(it, res.Title, rel.Protocol, d.Until)
			if err != nil {
				slog.Warn("api: hold back a release", "release", res.Title, "err", err)
				kept = append(kept, res) // better a grab now than a lost release
				continue
			}
		}
		held = append(held, res)
		anyFirst = anyFirst || first
		if earliest.IsZero() || d.Until.Before(earliest) {
			earliest = d.Until
		}
	}
	if remember && anyFirst && note != nil {
		msg := delay.Note(earliest, now, time.Local)
		if len(held) == 1 {
			msg += fmt.Sprintf(": holding back %q (%s)", held[0].Title, delay.ProtocolName(string(held[0].Protocol)))
		} else {
			msg += fmt.Sprintf(": holding back %d releases", len(held))
		}
		note(msg)
	}
	return kept, earliest
}

// limitMode says how far a search's results are limited.
type limitMode int

const (
	// limitManual applies the size table only: a search or grab started by hand
	// is never delayed.
	limitManual limitMode = iota
	// limitAutomatic applies the size table and the delay profile, and
	// remembers what it holds back so the title is searched again later.
	limitAutomatic
	// limitDry decides like limitAutomatic but remembers and writes nothing
	// (the IRC test box).
	limitDry
)

// noteMode is how a search's note judges the limits: a search started by hand
// ignores the delay profile, an automatic one does not (and records nothing).
func noteMode(force bool) limitMode {
	if force {
		return limitManual
	}
	return limitDry
}

func modeFor(automatic bool) limitMode {
	if automatic {
		return limitAutomatic
	}
	return limitManual
}

// limitReport is what the limits took out of a list, for a log line.
type limitReport struct {
	size  string    // why the size table refused a release, "" when it refused none
	until time.Time // when a held release is let through, zero when none was held
}

// why is the report as a short sentence part, to follow "but".
func (r limitReport) why() string {
	switch {
	case !r.until.IsZero():
		when := strings.TrimPrefix(delay.Note(r.until, time.Now(), time.Local), "Waiting for a better protocol ")
		return "the delay profile is holding it back " + when + ", and it is searched for again then"
	case r.size != "":
		return "the release is " + r.size
	}
	return "the size table or the delay profile set it aside"
}

// limitMovieResults applies the size table and, for an automatic search, the
// delay profile to the matching releases of a movie.
func (s *Server) limitMovieResults(ctx context.Context, m library.Movie, profile quality.Profile, automatic bool, results []indexers.Result) []indexers.Result {
	out, _ := s.limitMovie(ctx, m, profile, modeFor(automatic), results)
	return out
}

// limitMovie is limitMovieResults that also says what it took out.
func (s *Server) limitMovie(ctx context.Context, m library.Movie, profile quality.Profile, mode limitMode, results []indexers.Result) ([]indexers.Result, limitReport) {
	var rep limitReport
	results, rep.size = s.applySizeTable("movie", results, func() float64 { return s.movieRuntime(ctx, m) }, func(indexers.Result) int { return 0 })
	if mode != limitDry {
		results = s.markCached(ctx, results) // only reorders: see debrid_cache.go
	}
	if mode == limitManual {
		return results, rep
	}
	// A title that is already downloaded only wants real upgrades: the other
	// releases are left in (the picker refuses them) and never held.
	var relevant func(indexers.Result) bool
	if m.Status == library.StatusDownloaded {
		current := quality.Tier(m.Quality)
		relevant = func(res indexers.Result) bool {
			return pickUpgradeResult([]indexers.Result{res}, profile, current, m.ReleaseTitle, m.Year) != nil
		}
	}
	results, rep.until = s.holdBack(mode == limitAutomatic, delay.Item{Kind: "movie", MovieID: m.ID}, profile, results, relevant, func(msg string) {
		s.itemEvent(m.ID, 0, "delayed", queue.LevelInfo, msg)
	})
	return results, rep
}

// limitTVResults is limitMovieResults for the releases of one season pack
// (episode 0) or episode of a show. current is what an upgrade search starts
// from, nil when the episode is missing.
func (s *Server) limitTVResults(series library.Series, season, episode int, profile quality.Profile, current *upgradeBase, automatic bool, results []indexers.Result) []indexers.Result {
	out, _ := s.limitTV(series, season, episode, profile, current, modeFor(automatic), results)
	return out
}

// limitTV is limitTVResults that also says what it took out.
func (s *Server) limitTV(series library.Series, season, episode int, profile quality.Profile, current *upgradeBase, mode limitMode, results []indexers.Result) ([]indexers.Result, limitReport) {
	var rep limitReport
	mapper := s.releaseMapper(series)
	s.warmEpisodeRuntime(series, results)
	results, rep.size = s.applySizeTable("tv", results, func() float64 { return s.episodeRuntime(context.Background(), series) },
		func(res indexers.Result) int { return episodeCount(mapper, res.Parsed()) })
	if mode != limitDry {
		results = s.markCached(context.Background(), results) // only reorders: see debrid_cache.go
	}
	if mode == limitManual {
		return results, rep
	}
	relevant := func(res indexers.Result) bool {
		parsed := res.Parsed()
		rel := mapped(mapper, parsed)
		if !matchesShowForRel(parsed, series, season) || !coversTarget(rel, season, episode) || (episode > 0 && len(rel.Episodes) == 0) {
			return false
		}
		return current == nil || profile.IsUpgradeOverRel(current.Tier, current.Release, res.Title, parsed)
	}
	results, rep.until = s.holdBack(mode == limitAutomatic, delay.Item{Kind: "episode", SeriesID: series.ID, Season: season, Episode: episode}, profile, results, relevant, func(msg string) {
		s.itemEvent(0, series.ID, "delayed", queue.LevelInfo, msg)
	})
	return results, rep
}

// forgetHeldMovie drops what the delay profile holds for a movie: it was
// removed, grabbed or became downloaded, so its waiting releases and their
// follow-up search no longer matter.
func (s *Server) forgetHeldMovie(movieID int64) {
	if s.delays == nil || movieID == 0 {
		return
	}
	if err := s.delays.Clear(movieID, 0); err != nil {
		slog.Warn("api: forget held releases of a movie", "movie", movieID, "err", err)
	}
}

// forgetHeldSeries is forgetHeldMovie for every episode and season pack of a show.
func (s *Server) forgetHeldSeries(seriesID int64) {
	if s.delays == nil || seriesID == 0 {
		return
	}
	if err := s.delays.Clear(0, seriesID); err != nil {
		slog.Warn("api: forget held releases of a show", "series", seriesID, "err", err)
	}
}

// forgetHeldEpisode drops what is held for one episode of a show. A season
// pack (episode 0) is forgotten the same way.
func (s *Server) forgetHeldEpisode(seriesID int64, season, episode int) {
	if s.delays == nil || seriesID == 0 {
		return
	}
	if err := s.delays.ClearItem(delay.Item{Kind: "episode", SeriesID: seriesID, Season: season, Episode: episode}); err != nil {
		slog.Warn("api: forget held releases of an episode", "series", seriesID, "err", err)
	}
}

// delayedGrabsJob runs once a minute. When the wait for a held release is over
// it searches that title again; the release is no longer held then, so the
// normal pick takes the best one there is. Like every automatic search it stops
// while the hourly cap of automatic downloads is reached. A title counts as
// searched only once its search ran: when the cap is reached, or every indexer
// is paused, it stays due for the next pass.
func (s *Server) delayedGrabsJob(ctx context.Context) {
	if s.delays == nil || !s.automationEnabled() {
		return
	}
	if _, quiet := s.quietUntil(); quiet {
		return // it asks the indexers again; the held releases stay held until it is over
	}
	_ = s.delays.Prune()
	due, err := s.delays.DueSearches()
	if err != nil {
		slog.Warn("api: find releases whose wait is over", "err", err)
		return
	}
	if len(due) == 0 {
		return
	}
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		slog.Warn("api: load quality profiles for held releases", "err", err)
		return
	}
	for _, it := range due {
		if !s.autoGrabRoom() {
			holdBack("delay")
			return
		}
		if !anyIndexerAvailable(instances) {
			return // every indexer is paused: try again next time
		}
		switch it.Kind {
		case "movie":
			m, err := s.MovieRepo.Get(it.MovieID)
			if err != nil {
				s.forgetHeldMovie(it.MovieID) // the movie is gone
				continue
			}
			s.huntMovie(ctx, m, instances, profiles, s.searchSkipKeys(), false)
		case "episode":
			if _, err := s.MovieRepo.GetSeries(it.SeriesID); err != nil {
				s.forgetHeldSeries(it.SeriesID) // the show is gone
				continue
			}
			scope := tvScope{seriesID: it.SeriesID, season: it.Season, episode: it.Episode, specials: it.Season == 0}
			s.autoGrabTV(s.targetedTVSearcher(ctx, instances, 3, false), "delay", profiles, scope)
		}
		if err := s.delays.MarkSearched(it); err != nil {
			slog.Warn("api: mark a held release as searched", "err", err)
		}
	}
}

// anyIndexerAvailable reports whether at least one enabled indexer is not
// paused by the back-off, so a search would ask someone.
func anyIndexerAvailable(instances []indexers.Instance) bool {
	for _, inst := range instances {
		if !inst.Enabled {
			continue
		}
		if _, paused := inst.Tracker.PausedUntil(inst.ID); !paused {
			return true
		}
	}
	return false
}

// waitingPayload is one release being held back, for the Wanted page and a
// title's page.
type waitingPayload struct {
	MovieID  int64  `json:"movieId,omitempty"`
	SeriesID int64  `json:"seriesId,omitempty"`
	Season   int    `json:"season,omitempty"`
	Episode  int    `json:"episode,omitempty"` // 0 = a whole season
	Release  string `json:"release"`
	Protocol string `json:"protocol"`
	Until    string `json:"until"` // RFC 3339
	Note     string `json:"note"`
}

// handleSearchWaiting lists the releases that are being held back by the delay
// profile right now.
func (s *Server) handleSearchWaiting(w http.ResponseWriter, r *http.Request) {
	out := []waitingPayload{}
	if s.delays != nil {
		list, err := s.delays.Waiting()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		now := time.Now()
		for _, p := range list {
			out = append(out, waitingPayload{
				MovieID: p.MovieID, SeriesID: p.SeriesID, Season: p.Season, Episode: p.Episode,
				Release: p.Release, Protocol: p.Protocol, Until: p.Due.UTC().Format(time.RFC3339),
				Note: delay.Note(p.Due, now, time.Local),
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// searchRoutes registers the routes of this file's features.
func (s *Server) searchRoutes(admin routeGroup, member routeGroup) {
	admin.HandleFunc("GET /api/search/delay", s.handleGetDelay)
	admin.HandleFunc("PUT /api/search/delay", s.handlePutDelay)
	admin.HandleFunc("GET /api/quality/size-limits", s.handleGetSizeLimits)
	admin.HandleFunc("PUT /api/quality/size-limits", s.handlePutSizeLimits)
	admin.HandleFunc("POST /api/quality/size-limits/reset", s.handleResetSizeLimits)
	admin.HandleFunc("GET /api/quality/min-size-check", s.handleGetMinSize)
	admin.HandleFunc("PUT /api/quality/min-size-check", s.handlePutMinSize)
	admin.HandleFunc("GET /api/quality/size-limits/trash/{list}", s.handleTrashSizes)
	admin.HandleFunc("POST /api/indexers/{id}/resume", s.handleResumeIndexer)
	admin.HandleFunc("GET /api/stats/indexers", s.handleIndexerStats)
	member.HandleFunc("GET /api/search/waiting", s.handleSearchWaiting)
}
