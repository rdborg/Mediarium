package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ryanborg/mediarium/internal/blocklist"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/quality"
	"github.com/ryanborg/mediarium/internal/queue"
)

// rejectionsFor explains why automation would not pick a release: quality
// outside the profile, or (for something already downloaded) no better than
// what is on disk. It only informs the interactive search list; grabbing a
// rejected release by hand is always allowed.
func rejectionsFor(title string, profile quality.Profile, downloaded bool, currentQuality string) []string {
	var out []string
	rel := parser.Parse(title)
	tier := quality.Classify(rel)
	if ok, why := profile.TitleAllowed(title); !ok {
		out = append(out, why)
	}
	if !profile.Accepts(rel) {
		out = append(out, fmt.Sprintf("%s is not allowed by the %q profile", tier, profile.Name))
		if by, fallback, ok := profile.AcceptedBy(title); ok && fallback {
			if downloaded {
				out = append(out, fmt.Sprintf("allowed only as a fallback (%q), which is used only when nothing is downloaded yet", by.Name))
			} else {
				out = append(out, fmt.Sprintf("allowed only as a fallback (%q): taken only when no release the %q profile accepts is found", by.Name, profile.Name))
			}
		}
	}
	if downloaded && tierKnown(currentQuality) && !profile.OnFallback(quality.Tier(currentQuality)) {
		switch {
		case profile.ReachedCutoffTier(quality.Tier(currentQuality)):
			out = append(out, fmt.Sprintf("already at the profile cutoff (%s)", currentQuality))
		case quality.Rank(tier) <= quality.Rank(quality.Tier(currentQuality)):
			out = append(out, fmt.Sprintf("not an upgrade over %s", currentQuality))
		}
	}
	return out
}

// handleMovieSearch is Radarr-style interactive search for one library
// movie: every matching release across the indexers, with the reasons
// automation would skip it.
func (s *Server) handleMovieSearch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "movie not found")
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
		writeJSON(w, http.StatusOK, []searchResultPayload{})
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profile := profiles.resolve(m.ProfileID)
	blocked := s.blockedKeys()

	merged := indexers.MergeResults(indexers.SearchAll(r.Context(), instances, m.Title, movieCategory))
	out := []searchResultPayload{}
	for _, res := range merged {
		if !matchesMovie(res.Title, m) {
			continue
		}
		p := s.toSearchResultPayload(res)
		p.Blocklisted = blocked[blocklist.Key(res.Title)]
		p.Rejections = rejectionsFor(res.Title, profile, m.Status == library.StatusDownloaded, m.Quality)
		p.AcceptedBy = acceptedBy(profile, res.Title)
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
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
		writeError(w, http.StatusPreconditionFailed, "no indexers configured yet — add one in Settings")
		return nil, false
	}
	return instances, true
}

// handleMovieSearchNow runs the automatic search for one movie right now,
// even if it is unmonitored, grabbing the best acceptable release.
func (s *Server) handleMovieSearchNow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "movie not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if m.Status == library.StatusDownloading {
		writeError(w, http.StatusConflict, "this movie is already downloading")
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
	if s.huntMovie(r.Context(), m, instances, profiles, s.blockedKeys(), true) {
		writeJSON(w, http.StatusOK, searchNowResponse{Grabbed: 1, Message: "Grabbed a release — track it in Activity / Queue."})
		return
	}
	writeJSON(w, http.StatusOK, searchNowResponse{Message: "No acceptable release found right now."})
}

type seriesSearchNowRequest struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

// handleSeriesSearchNow searches for everything wanted in a series (or one
// season, or one episode) right now, even if unmonitored.
func (s *Server) handleSeriesSearchNow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid series id")
		return
	}
	var req seriesSearchNowRequest
	_ = decodeJSON(r, &req) // an empty body means the whole series
	if _, err := s.MovieRepo.GetSeries(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "series not found")
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
	n := s.autoGrabTV(s.targetedTVSearcher(r.Context(), instances, maxTVSearchesPerHunt), "search-now", profiles,
		tvScope{seriesID: id, season: req.Season, episode: req.Episode, force: true})
	msg := "No acceptable release found right now."
	if n > 0 {
		msg = fmt.Sprintf("Grabbed %d release(s) — track them in Activity / Queue.", n)
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
	// The latest automatic search for this title, in plain words, so a
	// title waiting for a release says why ("100 releases, none acceptable: …").
	LastSearch   string `json:"lastSearch,omitempty"`
	LastSearchAt string `json:"lastSearchAt,omitempty"`
}

// lastSearch returns the newest "searched" event among events (newest first).
func lastSearch(events []queue.Event) (string, string) {
	for _, e := range events {
		if e.Kind == "searched" {
			return e.Message, e.At
		}
	}
	return "", ""
}

// handleWanted lists what automation is still looking for: "missing"
// (monitored, nothing downloaded; episodes only once aired) or "cutoff"
// (downloaded but below the profile cutoff, upgrades allowed).
func (s *Server) handleWanted(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "missing"
	}
	if kind != "missing" && kind != "cutoff" {
		writeError(w, http.StatusBadRequest, `kind must be "missing" or "cutoff"`)
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []wantedItemPayload{}

	movies, err := s.MovieRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, m := range movies {
		if !m.Monitored {
			continue
		}
		p := profiles.resolve(m.ProfileID)
		switch {
		case kind == "missing" && m.Status == library.StatusMissing,
			kind == "cutoff" && m.Status == library.StatusDownloaded && wantsUpgrade(p, m.Quality):
			item := wantedItemPayload{Kind: "movie", ID: m.ID, MovieID: m.ID, TMDBID: m.TMDBID, Title: m.Title, Date: m.ReleaseDate, ProfileName: p.Name}
			if events, err := s.QueueRepo.MovieEvents(m.ID, 40); err == nil {
				item.LastSearch, item.LastSearchAt = lastSearch(events)
			}
			if kind == "cutoff" {
				item.Quality, item.Cutoff = m.Quality, string(p.Cutoff)
			}
			out = append(out, item)
		}
	}

	seriesList, err := s.MovieRepo.ListSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, sr := range seriesList {
		if !sr.Monitored {
			continue
		}
		p := profiles.resolve(sr.ProfileID)
		var seriesSearch, seriesSearchAt string
		if events, err := s.QueueRepo.SeriesEvents(sr.ID, 40); err == nil {
			seriesSearch, seriesSearchAt = lastSearch(events)
		}
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, ep := range eps {
			if !ep.Monitored {
				continue
			}
			aired := ep.AirDate != "" && ep.AirDate <= today
			if !(kind == "missing" && ep.Status == library.StatusMissing && aired) &&
				!(kind == "cutoff" && ep.Status == library.StatusDownloaded && wantsUpgrade(p, ep.Quality)) {
				continue
			}
			label := fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode)
			if ep.Title != "" {
				label += " · " + ep.Title
			}
			item := wantedItemPayload{
				Kind: "episode", ID: ep.ID, SeriesID: sr.ID, Season: ep.Season, Episode: ep.Episode,
				Title: sr.Title, Subtitle: label, Date: ep.AirDate, ProfileName: p.Name,
				LastSearch: seriesSearch, LastSearchAt: seriesSearchAt,
			}
			if kind == "cutoff" {
				item.Quality, item.Cutoff = ep.Quality, string(p.Cutoff)
			}
			out = append(out, item)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type monitoredRequest struct {
	Monitored bool `json:"monitored"`
}

func (s *Server) handleSetMovieMonitored(w http.ResponseWriter, r *http.Request) {
	s.setMonitored(w, r, "movie", func(ctx context.Context, id int64, v bool) error { return s.MovieRepo.SetMonitored(id, v) })
}

func (s *Server) handleSetSeriesMonitored(w http.ResponseWriter, r *http.Request) {
	s.setMonitored(w, r, "series", func(ctx context.Context, id int64, v bool) error { return s.MovieRepo.SetSeriesMonitored(id, v) })
}

func (s *Server) handleSetEpisodeMonitored(w http.ResponseWriter, r *http.Request) {
	s.setMonitored(w, r, "episode", func(ctx context.Context, id int64, v bool) error { return s.MovieRepo.SetEpisodeMonitored(id, v) })
}

func (s *Server) handleSetSeasonMonitored(w http.ResponseWriter, r *http.Request) {
	season, err := strconv.Atoi(r.PathValue("season"))
	if err != nil || season < 0 {
		writeError(w, http.StatusBadRequest, "invalid season")
		return
	}
	s.setMonitored(w, r, "series", func(ctx context.Context, id int64, v bool) error {
		return s.MovieRepo.SetSeasonMonitored(id, season, v)
	})
}

func (s *Server) setMonitored(w http.ResponseWriter, r *http.Request, what string, set func(ctx context.Context, id int64, v bool) error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+what+" id")
		return
	}
	var req monitoredRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := set(r.Context(), id, req.Monitored); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
