package api

import (
	"context"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/ryanborg/mediarium/internal/automation"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/quality"
	"github.com/ryanborg/mediarium/internal/settings"
)

const (
	huntInterval = 30 * time.Minute
	rssInterval  = 15 * time.Minute
)

// StartAutomation wires the missing/upgrade hunting loop and RSS-style
// latest-releases sync into the generic scheduler from
// internal/automation. Called once from cmd/app/main.go after the server
// is constructed.
func (s *Server) StartAutomation() *automation.Scheduler {
	sched := automation.NewScheduler(
		automation.Job{Name: "hunt", Interval: huntInterval, Run: s.hunt},
		automation.Job{Name: "rss-sync", Interval: rssInterval, Run: s.rssSync},
		automation.Job{Name: "series-refresh", Interval: seriesRefreshInterval, Run: s.refreshAllSeries},
		automation.Job{Name: "subtitle-sweep", Interval: subtitleSweepInterval, Run: s.subtitleSweepJob},
		automation.Job{Name: "genre-backfill", Interval: genreBackfillInterval, Run: s.backfillGenres},
		automation.Job{Name: "cleanup", Interval: cleanupCheckInterval, Run: s.cleanupJob},
	)
	sched.Start()
	return sched
}

func (s *Server) automationEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyAutomationEnabled)
	return v != "0"
}

// tierKnown reports whether a stored quality is a real tier. Files imported
// from an existing library often carry nothing in their names to classify
// them; with no baseline to compare against, automation leaves them alone
// rather than replacing a library the user already has.
func tierKnown(q string) bool { return q != "" && quality.Tier(q) != quality.TierUnknown }

// wantsUpgrade reports whether a downloaded item with stored quality q
// should be searched for a better release under profile. A file that is
// there only because of a fallback profile is always searched for a
// replacement, even with upgrades off.
func wantsUpgrade(profile quality.Profile, q string) bool {
	if !tierKnown(q) {
		return false
	}
	if profile.OnFallback(quality.Tier(q)) {
		return true
	}
	return profile.UpgradeAllowed && !profile.ReachedCutoffTier(quality.Tier(q))
}

// hunt actively searches by title for every monitored movie that's either
// missing outright or downloaded-but-below-cutoff
// ("missing/upgrade hunting loop"). Each movie is judged against its own
// quality profile (falling back to the default). Missing movies search
// unconditionally; downloaded movies only search once they haven't already
// reached the profile's cutoff (and only if the profile allows upgrades),
// and only grab a result that's an actual upgrade over what's already on
// disk — never a same-or-lower quality re-grab.
func (s *Server) hunt(ctx context.Context) {
	if !s.automationEnabled() {
		return
	}
	movies, err := s.MovieRepo.List()
	if err != nil {
		log.Printf("automation: hunt: list movies: %v", err)
		return
	}
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		log.Printf("automation: hunt: load quality profiles: %v", err)
		return
	}

	blocked := s.blockedKeys()
	for _, m := range movies {
		s.huntMovie(ctx, m, instances, profiles, blocked, false)
	}
	s.huntTV(ctx, instances, profiles)
}

// huntMovie searches for one movie if it is monitored (or force is set, for
// a manual "search now") and wanted (missing, or downloaded below its
// profile cutoff), grabbing the best release that is not blocklisted. It
// reports whether something was grabbed.
func (s *Server) huntMovie(ctx context.Context, m library.Movie, instances []indexers.Instance, profiles profileSet, blocked map[string]bool, force bool) bool {
	if !m.Monitored && !force {
		return false
	}
	profile := profiles.resolve(m.ProfileID)
	sources := s.sourcesFor(m.SourcePref)
	switch m.Status {
	case library.StatusMissing:
		if !force && unreleased(m.ReleaseDate) {
			return false // nothing to download yet
		}
		outcomes := indexers.SearchAll(ctx, instances, m.Title, movieCategory)
		// A title search also returns other films with similar words; only
		// releases of this movie may be grabbed.
		best := pickBestResult(filterSources(matchingResults(dropBlocked(indexers.MergeResults(outcomes), blocked), m), sources), profile, m.Year)
		s.noteMovieSearch(m, outcomes, blocked, sources, profile, best, "")
		if best == nil {
			return false
		}
		if _, err := s.grabMovie(m, best.Title, best.DownloadURL, best.SizeBytes, best.Protocol, true); err != nil {
			log.Printf("automation: hunt: grab %q for %q: %v", best.Title, m.Title, err)
			return false
		}
		s.noteFallbackGrab(m.ID, 0, m.Title, profile, best.Title)
		return true
	case library.StatusDownloaded:
		if !wantsUpgrade(profile, m.Quality) {
			return false // upgrades off, unknown baseline, or already at/above cutoff: skip the search entirely
		}
		currentTier := quality.Tier(m.Quality)
		outcomes := indexers.SearchAll(ctx, instances, m.Title, movieCategory)
		best := pickUpgradeResult(filterSources(matchingResults(dropBlocked(indexers.MergeResults(outcomes), blocked), m), sources), profile, currentTier, m.Year)
		s.noteMovieSearch(m, outcomes, blocked, sources, profile, best, currentTier)
		if best == nil {
			return false
		}
		if _, err := s.grabMovie(m, best.Title, best.DownloadURL, best.SizeBytes, best.Protocol, true); err != nil {
			log.Printf("automation: hunt: upgrade grab %q for %q: %v", best.Title, m.Title, err)
			return false
		}
		return true
	}
	return false
}

// rssSync checks each indexer's "latest releases" feed (an empty-query
// search — Newznab/Torznab return their most recent posts rather than an
// error when q is omitted) against the wanted list, so a release posted
// moments ago can be grabbed without waiting for the next targeted search
// ("RSS sync"). Covers both missing movies and upgrade
// candidates, same as hunt — a release that just appeared could easily be
// a quality upgrade for something already downloaded, not just a fill for
// something missing.
func (s *Server) rssSync(ctx context.Context) {
	if !s.automationEnabled() {
		return
	}
	movies, err := s.MovieRepo.List()
	if err != nil {
		log.Printf("automation: rss-sync: list movies: %v", err)
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		log.Printf("automation: rss-sync: load quality profiles: %v", err)
		return
	}

	var missing, upgradeCandidates []library.Movie
	for _, m := range movies {
		if !m.Monitored {
			continue
		}
		switch m.Status {
		case library.StatusMissing:
			if !unreleased(m.ReleaseDate) {
				missing = append(missing, m)
			}
		case library.StatusDownloaded:
			if wantsUpgrade(profiles.resolve(m.ProfileID), m.Quality) {
				upgradeCandidates = append(upgradeCandidates, m)
			}
		}
	}

	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	s.rssSyncTV(ctx, instances, profiles)
	if len(missing) == 0 && len(upgradeCandidates) == 0 {
		return
	}

	outcomes := indexers.SearchAll(ctx, instances, "", movieCategory)
	results := dropBlocked(indexers.MergeResults(outcomes), s.blockedKeys())

	for _, m := range missing {
		best := pickBestResult(filterSources(matchingResults(results, m), s.sourcesFor(m.SourcePref)), profiles.resolve(m.ProfileID), m.Year)
		if best == nil {
			continue
		}
		if _, err := s.grabMovie(m, best.Title, best.DownloadURL, best.SizeBytes, best.Protocol, true); err != nil {
			log.Printf("automation: rss-sync: grab %q for %q: %v", best.Title, m.Title, err)
			continue
		}
		s.noteFallbackGrab(m.ID, 0, m.Title, profiles.resolve(m.ProfileID), best.Title)
	}
	for _, m := range upgradeCandidates {
		currentTier := quality.Tier(m.Quality)
		best := pickUpgradeResult(filterSources(matchingResults(results, m), s.sourcesFor(m.SourcePref)), profiles.resolve(m.ProfileID), currentTier, m.Year)
		if best == nil {
			continue
		}
		if _, err := s.grabMovie(m, best.Title, best.DownloadURL, best.SizeBytes, best.Protocol, true); err != nil {
			log.Printf("automation: rss-sync: upgrade grab %q for %q: %v", best.Title, m.Title, err)
		}
	}
}

func matchingResults(results []indexers.Result, m library.Movie) []indexers.Result {
	var out []indexers.Result
	for _, res := range results {
		if matchesMovie(res.Title, m) {
			out = append(out, res)
		}
	}
	return out
}

// pickBestResult returns the best result acceptable to profile or, when
// nothing is, the best result acceptable to the first of its fallback
// profiles (in order) that accepts anything. Returns nil if no profile in
// the chain accepts a result.
func pickBestResult(results []indexers.Result, profile quality.Profile, wantYear int) *indexers.Result {
	for _, p := range profile.Chain() {
		if best := pickBestFor(results, p, wantYear); best != nil {
			return best
		}
	}
	return nil
}

// pickBestFor returns the highest-quality-tier result one profile accepts,
// preferring a matching release year when the movie's year is known.
// Returns nil if nothing in results is acceptable.
func pickBestFor(results []indexers.Result, profile quality.Profile, wantYear int) *indexers.Result {
	var best *indexers.Result
	bestRank, bestScore := -1, 0
	for i := range results {
		release := parser.Parse(results[i].Title)
		if !profile.Accepts(release) {
			continue
		}
		if ok, _ := profile.TitleAllowed(results[i].Title); !ok {
			continue
		}
		if wantYear != 0 && release.Year != 0 && release.Year != wantYear {
			continue
		}
		rank, score := quality.Rank(quality.Classify(release)), profile.Score(results[i].Title)
		if rank > bestRank || (rank == bestRank && score > bestScore) || (rank == bestRank && score == bestScore && preferUsenet(best, &results[i])) {
			bestRank, bestScore = rank, score
			best = &results[i]
		}
	}
	return best
}

// pickUpgradeResult is pickBestResult with the extra requirement that the
// result must actually be a quality upgrade over currentTier (and that
// currentTier hasn't already reached the profile's cutoff) — used for
// re-checking already-downloaded movies, as opposed to pickBestResult's
// "anything profile-accepted" bar for movies with nothing downloaded yet.
func pickUpgradeResult(results []indexers.Result, profile quality.Profile, currentTier quality.Tier, wantYear int) *indexers.Result {
	var best *indexers.Result
	bestRank, bestScore := -1, 0
	for i := range results {
		release := parser.Parse(results[i].Title)
		if !profile.IsUpgradeOverTier(currentTier, release) {
			continue
		}
		if ok, _ := profile.TitleAllowed(results[i].Title); !ok {
			continue
		}
		if wantYear != 0 && release.Year != 0 && release.Year != wantYear {
			continue
		}
		rank, score := quality.Rank(quality.Classify(release)), profile.Score(results[i].Title)
		if rank > bestRank || (rank == bestRank && score > bestScore) || (rank == bestRank && score == bestScore && preferUsenet(best, &results[i])) {
			bestRank, bestScore = rank, score
			best = &results[i]
		}
	}
	return best
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// matchesMovie does a normalized (lowercased, punctuation-stripped) title
// comparison plus a year check when both sides have one. This is a
// simplification, not fuzzy/edit-distance matching — a known
// approximation worth revisiting if it mismatches often enough to matter
// in practice.
func matchesMovie(releaseTitle string, movie library.Movie) bool {
	release := parser.Parse(releaseTitle)
	if release.Year != 0 && movie.Year != 0 && release.Year != movie.Year {
		return false
	}
	return normalizeTitle(release.Title) == normalizeTitle(movie.Title)
}

// normalizeTitle reduces a title to letters and digits for comparison,
// treating "&" as "and" and ignoring a leading "The", "A" or "An", so
// "The Lord of the Rings" matches "Lord.of.the.Rings" and "Fast & Furious"
// matches "Fast.and.Furious".
func normalizeTitle(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "&", " and ")
	s = strings.TrimSpace(nonAlnum.ReplaceAllString(s, " ")) // dots, dashes and the like count as spaces
	for _, article := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(s, article) {
			s = s[len(article):]
			break
		}
	}
	return nonAlnum.ReplaceAllString(s, "")
}
