package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// rejectionsFor explains why automation would not pick a release: quality
// outside the profile, or (for something already downloaded) no better than
// what is on disk. It only informs the interactive search list; grabbing a
// rejected release by hand is always allowed.
// currentRelease is the release the file came from ("" when unknown).
func rejectionsFor(title string, profile quality.Profile, downloaded bool, currentQuality, currentRelease string) []string {
	return reasonTexts(rejectionReasonsFor(title, profile, downloaded, currentQuality, currentRelease, keepsNothing))
}

// rejectionReasonsFor is rejectionsFor with the kind of each reason: profile
// (the profile's own rules, including the minimum score), language, quality
// (not allowed, or only as a fallback) and upgrade (the file you have is as
// good or better, or the import would turn the release away: keeps).
func rejectionReasonsFor(title string, profile quality.Profile, downloaded bool, currentQuality, currentRelease string, keeps importKeeps) []releaseReason {
	var out []releaseReason
	rel := parser.Parse(title)
	tier := quality.Classify(rel)
	if ok, why := profile.TitleAllowed(title); !ok {
		out = append(out, releaseReason{"profile", why})
	}
	if reason := profile.LanguageReason(rel); reason != "" {
		out = append(out, releaseReason{"language", reason})
	}
	if !profile.AllowsTier(tier) {
		out = append(out, releaseReason{"quality", fmt.Sprintf("%s isn't allowed by the %q profile", tier, profile.Name)})
		if by, fallback, ok := profile.AcceptedBy(title); ok && fallback {
			if downloaded {
				out = append(out, releaseReason{"quality", fmt.Sprintf("allowed only as a fallback (%q), which is used only when nothing is downloaded yet", by.Name)})
			} else {
				out = append(out, releaseReason{"quality", fmt.Sprintf("allowed only as a fallback (%q): taken only when no release the %q profile accepts is found", by.Name, profile.Name)})
			}
		}
	}
	if downloaded && tierKnown(currentQuality) && !profile.OnFallback(quality.Tier(currentQuality)) {
		switch {
		case profile.WantsFormatUpgrade(currentRelease) && profile.IsUpgradeOver(quality.Tier(currentQuality), currentRelease, title):
			// same or better quality with a better custom-format score
		case profile.ReachedCutoffTier(quality.Tier(currentQuality)) && profile.WantsFormatUpgrade(currentRelease):
			out = append(out, releaseReason{"upgrade", "doesn't score higher with custom formats than the file you have"})
		case profile.ReachedCutoffTier(quality.Tier(currentQuality)):
			out = append(out, releaseReason{"upgrade", fmt.Sprintf("already at the profile cutoff (%s)", currentQuality)})
		case profile.TierRank(tier) <= profile.TierRank(quality.Tier(currentQuality)):
			out = append(out, releaseReason{"upgrade", fmt.Sprintf("not an upgrade over %s", currentQuality)})
		}
	}
	if r, ok := notPickedReason(keeps); ok && downloaded {
		out = append(out, r)
	}
	return out
}

// setFormatScore adds a release's custom-format score when the profile
// scores formats.
func setFormatScore(p *searchResultPayload, profile quality.Profile, title string) {
	if len(profile.Formats) == 0 {
		return
	}
	score, hits := profile.FormatBreakdown(title)
	p.FormatScore = &score
	p.Formats = hits
	p.MinScore, p.UpgradeUntil = profile.MinFormatScore, profile.CutoffFormatScore
}

// handleMovieSearch is the interactive search for one library movie: every
// matching release across the indexers, with the reasons automation would
// skip it.
func (s *Server) handleMovieSearch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
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
	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profile := s.profileForMovie(profiles, m)
	blocked := s.searchSkipKeys()

	_, beforeHome := s.homeReleaseHold(r.Context(), m, profile)
	sizeReasons := s.movieSizeRejections(r.Context(), m, profile)
	_, haveTier := existingTier(m.Quality, m.ReleaseTitle, m.FilePath)
	keeps := s.importKeepsFor(m.NoUpgrade, haveTier)
	m = s.movieWithAltTitles(r.Context(), m)
	// A site that lists no movie category is not asked, and is shown as left out.
	asked, left := indexers.SplitByCategories(r.Context(), instances, movieCategory)
	var notAsked []string
	for _, inst := range left {
		if inst.Enabled {
			notAsked = append(notAsked, inst.Name)
		}
	}
	// Words typed by hand are searched as they are, and every answer is shown:
	// the person chose to look for something else, such as a release posted under
	// another title, and the download still belongs to this movie.
	custom := customSearchQuery(r)
	var outcomes []indexers.Outcome
	if custom != "" {
		outcomes = indexers.SearchAll(indexers.AsManual(r.Context()), asked, custom, movieCategory)
	} else {
		outcomes = s.searchMovieTitles(r.Context(), asked, m, nil)
	}
	merged := indexers.MergeResults(outcomes)
	out := []searchResultPayload{}
	var found []indexers.Result
	var ignored ignoredReleases
	for _, res := range merged {
		if custom == "" && !matchesMovie(res.Title, m) {
			ignored.add(res.Title)
			continue
		}
		p := s.toSearchResultPayload(res)
		p.MatchedTitle = matchedOtherTitle(res.Title, m.Title, m.AltTitles)
		p.Blocklisted = blocked[blocklist.Key(res.Title)]
		p.addReasonList(rejectionReasonsFor(res.Title, profile, m.Status == library.StatusDownloaded, m.Quality, m.ReleaseTitle, keeps))
		p.addReasons("size", sizeReasons(res.Title, res.SizeBytes))
		if beforeHome && looksLikeHomeCopy(res.Title) {
			p.addReasons("release", []string{cinemaCopyWarning})
		}
		p.addStateReasons()
		setFormatScore(&p, profile, res.Title)
		p.AcceptedBy = acceptedBy(profile, res.Title)
		out = append(out, p)
		found = append(found, res)
	}
	markBestMatch(out, found, profile)
	s.markCachedPayloads(r.Context(), out)
	writeSearchIgnoring(w, r, out, len(instances), outcomes, notAsked, instances, ignored)
}

type searchNowResponse struct {
	Grabbed int    `json:"grabbed"`
	Message string `json:"message"`
}

func (s *Server) hasIndexers(w http.ResponseWriter) ([]indexers.Instance, bool) {
	instances, err := s.searchableIndexers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if len(instances) == 0 {
		writeError(w, http.StatusPreconditionFailed, "There are no indexers yet. Add one in Settings > Indexers & Search.")
		return nil, false
	}
	return instances, true
}

// handleMovieSearchNow runs the automatic search for one movie right now,
// even if it is unmonitored, grabbing the best acceptable release.
func (s *Server) handleMovieSearchNow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if m.Status == library.StatusDownloading {
		writeError(w, http.StatusConflict, "This movie is already downloading.")
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
	if s.huntMovie(r.Context(), m, instances, profiles, s.searchSkipKeys(), true) {
		writeJSON(w, http.StatusOK, searchNowResponse{Grabbed: 1, Message: "Started a download. Follow it in Activity."})
		return
	}
	writeJSON(w, http.StatusOK, searchNowResponse{Message: "Nothing suitable was found right now."})
}

type seriesSearchNowRequest struct {
	Season  *int `json:"season"` // 0 is the specials; leave it out for the whole show
	Episode int  `json:"episode"`
}

// searchNowScope turns a search-now request into what to look for.
func searchNowScope(seriesID int64, req seriesSearchNowRequest) tvScope {
	sc := tvScope{seriesID: seriesID, episode: req.Episode, force: true}
	if req.Season != nil {
		sc.season = *req.Season
		sc.specials = *req.Season == 0
	}
	return sc
}

// handleSeriesSearchNow searches for everything wanted in a series (or one
// season, or one episode) right now, even if unmonitored.
func (s *Server) handleSeriesSearchNow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	var req seriesSearchNowRequest
	_ = decodeJSON(r, &req) // an empty body means the whole series
	if _, err := s.MovieRepo.GetSeries(id); errors.Is(err, sql.ErrNoRows) {
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
	n := s.autoGrabTV(s.targetedTVSearcher(r.Context(), instances, maxTVSearchesPerHunt, true), "search-now", profiles,
		searchNowScope(id, req))
	msg := "Nothing suitable was found right now."
	if n > 0 {
		msg = fmt.Sprintf("Started %s. You can follow the progress in Activity.", plural(n, "download"))
	}
	writeJSON(w, http.StatusOK, searchNowResponse{Grabbed: n, Message: msg})
}

type wantedItemPayload struct {
	Kind        string `json:"kind"` // "movie" or "episode"
	ID          int64  `json:"id"`
	MovieID     int64  `json:"movieId,omitempty"`
	TMDBID      int    `json:"tmdbId,omitempty"`
	SeriesID    int64  `json:"seriesId,omitempty"`
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	Date        string `json:"date,omitempty"` // release / air date
	Quality     string `json:"quality,omitempty"`
	Cutoff      string `json:"cutoff,omitempty"`
	ProfileName string `json:"profileName,omitempty"`
	Copy        bool   `json:"copy,omitempty"` // a second copy of the title
	Year        int    `json:"year,omitempty"`
	PosterURL   string `json:"posterUrl,omitempty"`
	// The latest automatic search for this title, in plain words, so a
	// title waiting for a release says why ("100 releases, none acceptable: …").
	LastSearch   string `json:"lastSearch,omitempty"`
	LastSearchAt string `json:"lastSearchAt,omitempty"`
}

// wantedPoster is a poster's address, empty when the title has none.
func wantedPoster(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return metadata.PosterURL(posterPath)
}

// handleWanted lists what automation is still looking for: "missing"
// (monitored, nothing downloaded; episodes only once aired) or "cutoff"
// (downloaded but below the profile cutoff, upgrades allowed).
//
// Without paging it answers every movie and episode in one list, as it always
// has; on a big TV library that can be hundreds of thousands of episodes, so
// the app asks for ?group=1 (one row per movie or show, a page at a time; see
// handleWantedGroups) and ?series=ID for one show's episodes.
func (s *Server) handleWanted(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind == "" {
		kind = "missing"
	}
	if kind != "missing" && kind != "cutoff" {
		writeError(w, http.StatusBadRequest, `kind must be "missing" or "cutoff"`)
		return
	}
	var seriesID int64
	if v := q.Get("series"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
			return
		}
		seriesID = id
	}
	if q.Get("group") == "1" && seriesID == 0 {
		s.handleWantedGroups(w, r, kind)
		return
	}
	out, err := s.wantedItems(r.Context(), kind, seriesID)
	if err != nil {
		if r.Context().Err() == nil {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// wantedItems is the flat Wanted list: every movie, then every episode show by
// show. seriesID, when not 0, keeps it to that show's episodes.
func (s *Server) wantedItems(ctx context.Context, kind string, seriesID int64) ([]wantedItemPayload, error) {
	profiles, err := s.loadProfiles()
	if err != nil {
		return nil, err
	}
	out := []wantedItemPayload{}
	if seriesID == 0 {
		movies, err := s.wantedMovieItems(ctx, kind, profiles)
		if err != nil {
			return nil, err
		}
		out = append(out, movies...)
	}
	episodes, err := s.wantedEpisodeItems(ctx, kind, profiles, seriesID)
	if err != nil {
		return nil, err
	}
	out = append(out, episodes...)
	s.addLastSearches(out)
	return out, nil
}

// wantedStatus is the status a title has when it can be on the list: missing
// ones for "missing", downloaded ones (that may get a better version) for
// "cutoff".
func wantedStatus(kind string) library.Status {
	if kind == "cutoff" {
		return library.StatusDownloaded
	}
	return library.StatusMissing
}

// wantedMovieItems lists the wanted movies, newest added first.
//
// The database is asked only for the monitored titles that could be wanted,
// and the rules that need the profile or the disk are applied here.
func (s *Server) wantedMovieItems(ctx context.Context, kind string, profiles profileSet) ([]wantedItemPayload, error) {
	movies, err := s.MovieRepo.ListMonitoredWithStatus(ctx, wantedStatus(kind))
	if err != nil {
		return nil, err
	}
	var out []wantedItemPayload
	for _, m := range movies {
		p := profiles.resolve(m.ProfileID)
		switch {
		case kind == "missing" && movieWantedMissing(m),
			kind == "cutoff" && m.Status == library.StatusDownloaded && !m.NoUpgrade && wantsUpgrade(p, m.Quality, m.ReleaseTitle):
			title, poster := s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
			item := wantedItemPayload{Kind: "movie", ID: m.ID, MovieID: m.ID, TMDBID: m.TMDBID, Title: title, Date: m.ReleaseDate, ProfileName: p.Name, Copy: m.CopyOf != 0, Year: m.Year, PosterURL: poster}
			if item.Copy {
				item.Subtitle = "Second copy"
			}
			if kind == "cutoff" {
				item.Quality, item.Cutoff = m.Quality, p.CutoffName()
			}
			out = append(out, item)
		}
	}
	return out, nil
}

// wantedEpisodeItems lists the wanted episodes show by show, newest added
// show first (seriesID, when not 0: that show's only).
func (s *Server) wantedEpisodeItems(ctx context.Context, kind string, profiles profileSet, seriesID int64) ([]wantedItemPayload, error) {
	today := time.Now().UTC().Format("2006-01-02")
	episodes, err := s.MovieRepo.ListWantedEpisodes(ctx, wantedStatus(kind), today, seriesID)
	if err != nil {
		return nil, err
	}
	var out []wantedItemPayload
	faces := map[int64][2]string{}
	for _, ep := range episodes {
		p := profiles.resolveTV(ep.ProfileID)
		if kind == "missing" && fileExists(ep.FilePath) {
			continue // marked missing, but the file is there
		}
		if kind == "cutoff" && !wantsUpgrade(p, ep.Quality, ep.ReleaseTitle) {
			continue
		}
		label := fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode.Episode)
		if ep.Title != "" {
			label += " · " + ep.Title
		}
		if ep.CopyOf != 0 {
			label = "Second copy · " + label
		}
		title, poster := ep.SeriesTitle, wantedPoster(ep.SeriesPoster)
		if cached, seen := faces[ep.SeriesID]; seen {
			title, poster = cached[0], cached[1]
		} else {
			if sr, err := s.MovieRepo.GetSeries(ep.SeriesID); err == nil {
				title, poster = s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
			}
			faces[ep.SeriesID] = [2]string{title, poster}
		}
		item := wantedItemPayload{
			Kind: "episode", ID: ep.ID, SeriesID: ep.SeriesID, Season: ep.Season, Episode: ep.Episode.Episode,
			Title: title, Subtitle: label, Date: ep.AirDate, ProfileName: p.Name, Copy: ep.CopyOf != 0, Year: ep.SeriesYear, PosterURL: poster,
		}
		if kind == "cutoff" {
			item.Quality, item.Cutoff = ep.Quality, p.CutoffName()
		}
		out = append(out, item)
	}
	return out, nil
}

// addLastSearches fills in what the latest automatic search for each title
// said: a movie's own, and for an episode or a show row, the show's.
func (s *Server) addLastSearches(items []wantedItemPayload) {
	var movieIDs, seriesIDs []int64
	seen := map[int64]bool{}
	for _, it := range items {
		if it.Kind == "movie" {
			movieIDs = append(movieIDs, it.ID)
		} else if it.SeriesID != 0 && !seen[it.SeriesID] {
			seen[it.SeriesID] = true
			seriesIDs = append(seriesIDs, it.SeriesID)
		}
	}
	movieSearches, _ := s.QueueRepo.LastMovieSearches(movieIDs)
	showSearches, _ := s.QueueRepo.LastSeriesSearches(seriesIDs)
	for i := range items {
		it := &items[i]
		if it.Kind == "movie" {
			if e, ok := movieSearches[it.ID]; ok {
				it.LastSearch, it.LastSearchAt = e.Message, e.At
			}
		} else if e, ok := showSearches[it.SeriesID]; ok {
			it.LastSearch, it.LastSearchAt = e.Message, e.At
		}
	}
}

type monitoredRequest struct {
	Monitored bool `json:"monitored"`
}

// stoppedMonitoringNote is how a removed waiting download is recorded when a
// title stops being monitored.
const stoppedMonitoringNote = "Removed because the title is no longer monitored"

func (s *Server) handleSetMovieMonitored(w http.ResponseWriter, r *http.Request) {
	s.setMonitored(w, r, "movie", func(ctx context.Context, id int64, v bool) error {
		if err := s.MovieRepo.SetMonitored(id, v); err != nil {
			return err
		}
		if !v {
			s.clearWaitingDownloads(id, 0, false, stoppedMonitoringNote)
		}
		return nil
	})
}

func (s *Server) handleSetSeriesMonitored(w http.ResponseWriter, r *http.Request) {
	s.setMonitored(w, r, "series", func(ctx context.Context, id int64, v bool) error {
		if err := s.MovieRepo.SetSeriesMonitored(id, v); err != nil {
			return err
		}
		if !v {
			s.clearWaitingDownloads(0, id, false, stoppedMonitoringNote)
		}
		return nil
	})
}

func (s *Server) handleSetEpisodeMonitored(w http.ResponseWriter, r *http.Request) {
	s.setMonitored(w, r, "episode", func(ctx context.Context, id int64, v bool) error {
		if err := s.MovieRepo.SetEpisodeMonitored(id, v); err != nil {
			return err
		}
		if v {
			// A show with a monitored episode is monitored.
			return s.MovieRepo.EnableSeriesOfEpisode(id)
		}
		return nil
	})
}

func (s *Server) handleSetSeasonMonitored(w http.ResponseWriter, r *http.Request) {
	season, err := strconv.Atoi(r.PathValue("season"))
	if err != nil || season < 0 {
		writeError(w, http.StatusBadRequest, "That isn't a valid season number.")
		return
	}
	s.setMonitored(w, r, "series", func(ctx context.Context, id int64, v bool) error {
		if err := s.MovieRepo.SetSeasonMonitored(id, season, v); err != nil {
			return err
		}
		if v {
			return s.MovieRepo.SetSeriesMonitored(id, true)
		}
		return nil
	})
}

func (s *Server) setMonitored(w http.ResponseWriter, r *http.Request, what string, set func(ctx context.Context, id int64, v bool) error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid "+what+" ID.")
		return
	}
	var req monitoredRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if err := set(r.Context(), id, req.Monitored); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// customSearchQuery is the words a person typed into the search box of Choose a
// release, or "" when the release list is the usual one for the title.
func customSearchQuery(r *http.Request) string {
	q := strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	if len([]rune(q)) > 200 {
		q = string([]rune(q)[:200])
	}
	return q
}
