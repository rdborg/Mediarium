package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/metadata"
)

type seriesPayload struct {
	ID              int64    `json:"id"`
	TMDBID          int      `json:"tmdbId"`
	Title           string   `json:"title"`
	Year            int      `json:"year"`
	Overview        string   `json:"overview,omitempty"`
	PosterURL       string   `json:"posterUrl,omitempty"`
	Monitored       bool     `json:"monitored"`
	EpisodeCount    int      `json:"episodeCount"`
	DownloadedCount int      `json:"downloadedCount"`
	ProfileID       int64    `json:"profileId"`
	Sources         string   `json:"sources"`
	Genres          []string `json:"genres"`  // TMDB genre names; empty until fetched
	AddedBy         *userRef `json:"addedBy"` // null when unknown
}

// toSeriesPayload builds a library show's JSON; who resolves the account
// that added it (see accountNames).
func toSeriesPayload(s library.Series, who func(int64) *userRef) seriesPayload {
	genres := s.Genres
	if genres == nil {
		genres = []string{}
	}
	return seriesPayload{
		ID: s.ID, TMDBID: s.TMDBID, Title: s.Title, Year: s.Year, Overview: s.Overview,
		PosterURL: metadata.PosterURL(s.PosterPath), Monitored: s.Monitored,
		EpisodeCount: s.EpisodeCount, DownloadedCount: s.DownloadedCount, ProfileID: s.ProfileID, Sources: s.SourcePref,
		Genres: genres, AddedBy: who(s.AddedBy),
	}
}

type episodePayload struct {
	ID        int64  `json:"id"`
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Title     string `json:"title,omitempty"`
	Overview  string `json:"overview,omitempty"`
	AirDate   string `json:"airDate,omitempty"`
	Status    string `json:"status"`
	Quality   string `json:"quality,omitempty"`
	FilePath  string `json:"filePath,omitempty"`
	Monitored bool   `json:"monitored"`
}

func toEpisodePayload(e library.Episode) episodePayload {
	return episodePayload{
		ID: e.ID, Season: e.Season, Episode: e.Episode, Title: e.Title, Overview: e.Overview,
		AirDate: e.AirDate, Status: string(e.Status), Quality: e.Quality, FilePath: e.FilePath, Monitored: e.Monitored,
	}
}

type seriesDetailPayload struct {
	seriesPayload
	Episodes []episodePayload `json:"episodes"`
}

// handleSearchTV is TMDB title search for shows — the "find something to
// add" half of the TV flow, same role /api/search plays for indexer
// results. Empty (not an error) without a TMDB key, matching Discover.
func (s *Server) handleSearchTV(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, []discoverPayload{})
		return
	}
	shows, err := s.TMDB().SearchTV(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusBadGateway, "search TMDB: "+err.Error())
		return
	}
	out := make([]discoverPayload, len(shows))
	for i, sh := range shows {
		out[i] = s.showDiscover(r.Context(), sh)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListSeries(w http.ResponseWriter, r *http.Request) {
	list, err := s.MovieRepo.ListSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	who := s.accountNames()
	out := make([]seriesPayload, len(list))
	for i, sr := range list {
		out[i] = toSeriesPayload(sr, who)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSeries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid series id")
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	eps, err := s.MovieRepo.ListEpisodes(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := seriesDetailPayload{seriesPayload: toSeriesPayload(sr, s.accountNames()), Episodes: make([]episodePayload, len(eps))}
	for i, e := range eps {
		out.Episodes[i] = toEpisodePayload(e)
	}
	writeJSON(w, http.StatusOK, out)
}

// addSeriesRequest is what the "Add to library" dialog sends for a show.
// Monitor decides which episodes automation goes after: "all" (default),
// "future" (only episodes that have not aired yet) or "none".
type addSeriesRequest struct {
	TMDBID    int    `json:"tmdbId"`
	ProfileID int64  `json:"profileId"`
	Monitor   string `json:"monitor"`
	Sources   string `json:"sources"`
	SearchNow bool   `json:"searchNow"`
}

// handleAddSeries adds a show and its complete episode list (every real
// season, specials excluded — see metadata.GetShowEpisodes) to the library,
// all monitored and "missing" until the hunt loop or a manual grab finds
// them.
func (s *Server) handleAddSeries(w http.ResponseWriter, r *http.Request) {
	var req addSeriesRequest
	if err := decodeJSON(r, &req); err != nil || req.TMDBID == 0 {
		writeError(w, http.StatusBadRequest, "tmdbId is required")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "TMDB API key not configured yet — set it in Settings")
		return
	}
	if existing, ok, err := s.MovieRepo.GetSeriesByTMDBID(req.TMDBID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		writeError(w, http.StatusConflict, existing.Title+" is already in your library")
		return
	}

	if !validSourcePref(req.Sources) {
		writeError(w, http.StatusBadRequest, `sources must be "usenet", "torrent" or "both"`)
		return
	}
	if req.Monitor != "" && req.Monitor != "all" && req.Monitor != "future" && req.Monitor != "none" {
		writeError(w, http.StatusBadRequest, `monitor must be "all", "future" or "none"`)
		return
	}
	if err := s.checkProfileChoice(req.ProfileID); err != nil {
		writeProfileError(w, err)
		return
	}

	userID, byline := requester(r)
	created, err := s.addSeriesFromTMDB(r.Context(), req.TMDBID, userID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "add series from TMDB: "+err.Error())
		return
	}
	switch req.Monitor {
	case "future":
		_ = s.MovieRepo.MonitorFromDate(created.ID, time.Now().UTC().Format("2006-01-02"))
	case "none":
		_ = s.MovieRepo.SetAllEpisodesMonitored(created.ID, false)
	}
	if req.ProfileID != 0 {
		if err := s.MovieRepo.SetSeriesProfile(created.ID, req.ProfileID); err == nil {
			created.ProfileID = req.ProfileID
		}
	}
	if req.Sources != "" {
		if err := s.MovieRepo.SetSeriesSourcePref(created.ID, req.Sources); err == nil {
			created.SourcePref = req.Sources
		}
	}
	_ = s.QueueRepo.LogSeriesActivity(created.ID, "added", created.Title+" (series) added to library"+byline)
	if req.SearchNow {
		go s.searchSeriesInBackground(created.ID)
	}
	writeJSON(w, http.StatusCreated, toSeriesPayload(created, s.accountNames()))
}

// addSeriesFromTMDB adds a show with its episode list and genres, recording
// userID (0 = unknown) as the account that added it.
func (s *Server) addSeriesFromTMDB(ctx context.Context, tmdbID int, userID int64) (library.Series, error) {
	detail, infos, err := s.TMDB().GetShowEpisodes(ctx, tmdbID)
	if err != nil {
		return library.Series{}, err
	}
	return s.MovieRepo.AddSeries(library.Series{
		TMDBID: detail.TMDBID, Title: detail.Name, Year: detail.Year(), Overview: detail.Overview,
		PosterPath: detail.PosterPath, FirstAirDate: detail.FirstAirDate, Monitored: true,
		AddedBy: userID, Genres: s.TMDB().ShowGenres(ctx, detail.Show),
	}, toLibraryEpisodes(infos))
}

func toLibraryEpisodes(infos []metadata.EpisodeInfo) []library.Episode {
	eps := make([]library.Episode, len(infos))
	for i, e := range infos {
		eps[i] = library.Episode{Season: e.Season, Episode: e.Episode, Title: e.Name, Overview: e.Overview, AirDate: e.AirDate}
	}
	return eps
}

// handleRefreshSeries re-fetches a show's episode list from TMDB — new
// seasons appear and air dates get announced or moved over a show's life.
// Only adds/updates episode metadata; never touches download state.
func (s *Server) handleRefreshSeries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid series id")
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.refreshSeries(r.Context(), sr); err != nil {
		writeError(w, http.StatusBadGateway, "refresh from TMDB: "+err.Error())
		return
	}
	updated, err := s.MovieRepo.GetSeries(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toSeriesPayload(updated, s.accountNames()))
}

// refreshSeries brings a show's episode list and genres up to date from TMDB.
func (s *Server) refreshSeries(ctx context.Context, sr library.Series) error {
	detail, infos, err := s.TMDB().GetShowEpisodes(ctx, sr.TMDBID)
	if err != nil {
		return err
	}
	if len(detail.Genres) > 0 || sr.Genres == nil {
		if err := s.MovieRepo.SetSeriesGenres(sr.ID, s.TMDB().ShowGenres(ctx, detail.Show)); err != nil {
			log.Printf("api: refresh %q genres: %v", sr.Title, err)
		}
	}
	return s.MovieRepo.UpsertEpisodes(sr.ID, toLibraryEpisodes(infos), sr.Monitored)
}

type tvGrabRequest struct {
	grabRequest
	// Season/Episode are hints for releases whose title carries no S01E02
	// marker; when it does, the title wins (see resolveTVTarget).
	Season  int `json:"season,omitempty"`
	Episode int `json:"episode,omitempty"`
}

// handleGrabSeries grabs a release for a series — a single episode, a
// multi-episode release, or a whole-season pack, decided by the release's
// own title. Returns immediately; progress shows up in the queue.
func (s *Server) handleGrabSeries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid series id")
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req tvGrabRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ReleaseTitle == "" || req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "releaseTitle and downloadUrl are required")
		return
	}
	if !s.checkGrabURL(w, r, &req.DownloadURL) {
		return
	}
	queueID, err := s.grabTVRelease(sr, req.Season, req.Episode, req.ReleaseTitle, req.DownloadURL, req.SizeBytes, req.protocol())
	if err != nil {
		writeGrabError(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": queueID})
}

// handleDeleteSeries removes a show from the library. Its downloads are
// always cancelled and their files in the downloads folder deleted. With
// ?deleteFiles=true its folder in the TV library goes too, with every
// season, subtitle, .nfo and artwork (or, when the episodes do not sit in a
// folder of the show's own, each episode file with the files named after
// it).
func (s *Server) handleDeleteSeries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid series id")
		return
	}
	series, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	eps, err := s.MovieRepo.ListEpisodes(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var files []string
	for _, ep := range eps {
		if ep.FilePath != "" {
			files = append(files, ep.FilePath)
		}
	}
	deleteFiles := r.URL.Query().Get("deleteFiles") == "true"
	if deleteFiles {
		if err := checkRemovable(s.tvRoot(), files); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}
	// Downloads are always cancelled and their working folders deleted.
	if _, err := s.removeDownloadsFor(0, series.ID, series.Title); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if deleteFiles {
		if err := s.removeSeriesFiles(series, files); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.MovieRepo.DeleteSeries(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(0, "removed", series.Title+" (series) removed from library")
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) handleTrendingTV(w http.ResponseWriter, r *http.Request) {
	s.discoverShows(w, r, s.TMDB().TrendingTV)
}

func (s *Server) handlePopularTV(w http.ResponseWriter, r *http.Request) {
	s.discoverShows(w, r, s.TMDB().PopularTV)
}

func (s *Server) discoverShows(w http.ResponseWriter, r *http.Request, fetch func(context.Context) ([]metadata.Show, error)) {
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, []discoverPayload{})
		return
	}
	shows, err := fetch(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "fetch from TMDB: "+err.Error())
		return
	}
	out := make([]discoverPayload, len(shows))
	for i, sh := range shows {
		out[i] = s.showDiscover(r.Context(), sh)
	}
	writeJSON(w, http.StatusOK, out)
}
