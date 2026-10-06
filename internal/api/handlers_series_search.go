package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

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
	rel := parser.Parse(releaseTitle)
	// A daily show's air date carries a year too ("Show.2024.03.15"): that
	// is the episode's, not the show's.
	if rel.Year != 0 && series.Year != 0 && rel.Year != series.Year && rel.AirDate == "" {
		return false
	}
	return normalizeTitle(rel.Title) == normalizeTitle(series.Title)
}

// tvSearchQuery builds the indexer query for a season pack ("Show S01") or
// one episode ("Show S01E02") — the shape Newznab-family indexers match
// best, same as Sonarr's own fallback text query.
func tvSearchQuery(title string, season, episode int) string {
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
	if season == 0 && r.URL.Query().Get("season") == "0" {
		writeError(w, http.StatusBadRequest, "Mediarium can't search for specials yet.")
		return
	}
	if season <= 0 {
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

	outcomes := indexers.SearchAll(r.Context(), instances, tvSearchQuery(series.Title, season, episode), tvCategory)
	for _, q := range s.extraTVQueries(series, season, episode) {
		outcomes = append(outcomes, indexers.SearchAll(r.Context(), instances, q, tvCategory)...)
	}
	merged := indexers.MergeResults(outcomes)
	mapper := s.releaseMapper(series)

	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profile := profiles.resolve(series.ProfileID)
	blocked := s.blockedKeys()
	out := []searchResultPayload{}
	for _, res := range merged {
		if !matchesShow(res.Title, series) || !coversTarget(parseFor(mapper, res.Title), season, episode) {
			continue
		}
		payload := s.toSearchResultPayload(res)
		payload.Blocklisted = blocked[blocklist.Key(res.Title)]
		payload.Rejections = rejectionsFor(res.Title, profile, false, "")
		payload.AcceptedBy = acceptedBy(profile, res.Title)
		out = append(out, payload)
	}
	writeSearch(w, r, out, len(instances), outcomes)
}

// coversTarget reports whether a parsed release satisfies a season/episode
// request: for an episode, that episode itself, a multi-episode release
// containing it, or a whole-season pack; for a season, only a pack.
func coversTarget(rel parser.Release, season, episode int) bool {
	if rel.Season != season {
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
	shows, err := s.TMDB().SearchTV(r.Context(), release.Title)
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
		series, err = s.addSeriesFromTMDB(r.Context(), match.TMDBID, userID, true)
		if err != nil {
			writeUpstreamError(w, "add that show", err)
			return
		}
		_ = s.QueueRepo.LogSeriesActivity(series.ID, "added", series.Title+" (series) added to library"+byline)
	}

	queueID, err := s.grabTVRelease(series, 0, 0, req.ReleaseTitle, req.DownloadURL, req.SizeBytes, req.protocol())
	if err != nil {
		writeGrabError(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"seriesId": series.ID, "queueId": queueID})
}

// bestTVMatch picks the TMDB show for a parsed release title: an exact
// normalized-title match wins (preferring the release's year when it has
// one), falling back to TMDB's own top result — same spirit as
// bestTMDBMatch for movies.
func bestTVMatch(shows []metadata.Show, release parser.Release) *metadata.Show {
	if len(shows) == 0 {
		return nil
	}
	want := normalizeTitle(release.Title)
	var exact *metadata.Show
	for i := range shows {
		if normalizeTitle(shows[i].Name) != want {
			continue
		}
		if release.Year != 0 && shows[i].Year() == release.Year {
			return &shows[i]
		}
		if exact == nil {
			exact = &shows[i]
		}
	}
	if exact != nil {
		return exact
	}
	return &shows[0]
}
