package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
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
	Tags            []string `json:"tags"`    // the person's own tags ("Kids", "4K")
	AddedBy         *userRef `json:"addedBy"` // null when unknown
	// NoUpgrade: automation does not look for better versions of the episodes.
	NoUpgrade bool `json:"noUpgrade"`
	// DetailsState is "pending" while an import is still fetching details and
	// "problem" when that failed (DetailsNote says why); empty otherwise.
	DetailsState string `json:"detailsState,omitempty"`
	DetailsNote  string `json:"detailsNote,omitempty"`
	// SeriesType is how releases number the episodes: standard, anime or daily.
	SeriesType string `json:"seriesType"`
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
		Genres: genres, Tags: []string{}, AddedBy: who(s.AddedBy),
		NoUpgrade: s.NoUpgrade, DetailsState: s.DetailsState, DetailsNote: s.DetailsNote, SeriesType: library.ValidSeriesType(s.SeriesType),
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

// handleSearchTV searches for shows by title, to find something to add. It
// answers with an empty list (not an error) when there is no TMDB key, the
// same as Discover.
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
		writeUpstreamError(w, "search", err)
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
	tags, _ := s.MovieRepo.AllTitleTags(library.TagSeries)
	out := make([]seriesPayload, len(list))
	for i, sr := range list {
		out[i] = toSeriesPayload(sr, who)
		if t := tags[sr.ID]; t != nil {
			out[i].Tags = t
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSeries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
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
	if t, err := s.MovieRepo.TitleTags(library.TagSeries, sr.ID); err == nil {
		out.Tags = t
	}
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
	// NoUpgrade leaves the episodes alone once they are downloaded. On when
	// left out: better versions are something the person asks for.
	NoUpgrade *bool `json:"noUpgrade"`
}

// handleAddSeries adds a show and its complete episode list (every real
// season, specials left out) to the library. Every episode starts monitored
// and missing until the automatic search or a manual grab finds it.
func (s *Server) handleAddSeries(w http.ResponseWriter, r *http.Request) {
	if !s.requireModule(w, moduleTV) {
		return
	}
	if s.mustRequest(r) {
		s.fileRequest(w, r, "tv")
		return
	}
	var req addSeriesRequest
	if err := decodeJSON(r, &req); err != nil || req.TMDBID == 0 {
		writeError(w, http.StatusBadRequest, "tmdbId is required")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie info and lists).")
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
	created, err := s.addSeriesFromTMDB(r.Context(), req.TMDBID, userID, boolOr(req.NoUpgrade, true))
	if err != nil {
		writeUpstreamError(w, "add that show", err)
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
// userID (0 = unknown) as the account that added it. noUpgrade leaves the
// episodes alone once they are downloaded.
func (s *Server) addSeriesFromTMDB(ctx context.Context, tmdbID int, userID int64, noUpgrade bool) (library.Series, error) {
	detail, infos, err := s.TMDB().GetShowEpisodes(ctx, tmdbID)
	if err != nil {
		return library.Series{}, err
	}
	return s.MovieRepo.AddSeries(library.Series{
		TMDBID: detail.TMDBID, Title: detail.Name, Year: detail.Year(), Overview: detail.Overview,
		PosterPath: detail.PosterPath, FirstAirDate: detail.FirstAirDate, Monitored: true,
		NoUpgrade: noUpgrade, AddedBy: userID, Genres: s.TMDB().ShowGenres(ctx, detail.Show),
		SeriesType: guessSeriesType(detail.Show),
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
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.refreshSeries(r.Context(), sr); err != nil {
		writeUpstreamError(w, "refresh that show", err)
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
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req tvGrabRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
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
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	if _, err := s.removeSeries(id, r.URL.Query().Get("deleteFiles") == "true"); err != nil {
		writeRemoveError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// removeSeries takes one show out of the library, like removeMovie does for
// a movie. It returns the show's title.
func (s *Server) removeSeries(id int64, deleteFiles bool) (string, error) {
	series, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", &removeProblem{http.StatusNotFound, "That show isn't in your library."}
	}
	if err != nil {
		return "", &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	files, err := s.seriesFiles(id)
	if err != nil {
		return series.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	if deleteFiles {
		if err := checkRemovable(s.tvRoot(), files); err != nil {
			return series.Title, &removeProblem{http.StatusConflict, err.Error()}
		}
	}
	// Downloads are always cancelled and their working folders deleted.
	if _, err := s.removeDownloadsFor(0, series.ID, series.Title); err != nil {
		return series.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	// A download that was importing when it was cancelled finishes its import
	// first, so episodes may have files now that they did not have a moment
	// ago. With "delete files" asked for, those go too.
	if now, err := s.seriesFiles(id); err == nil && !slices.Equal(now, files) {
		files = now
		if deleteFiles {
			if err := checkRemovable(s.tvRoot(), files); err != nil {
				return series.Title, &removeProblem{http.StatusConflict, err.Error()}
			}
		}
	}
	var deleted removedFiles
	if deleteFiles {
		var err error
		if deleted, err = s.removeSeriesFiles(series, files); err != nil {
			return series.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
		}
	}
	if err := s.MovieRepo.DeleteSeries(id); err != nil {
		return series.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	_ = s.QueueRepo.LogActivity(0, "removed", removedLogLine(series.Title+" (series)", deleteFiles && len(files) > 0, deleted, countDistinct(files)))
	return series.Title, nil
}

// seriesFiles lists the files the show's episodes are saved as.
func (s *Server) seriesFiles(seriesID int64) ([]string, error) {
	eps, err := s.MovieRepo.ListEpisodes(seriesID)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, ep := range eps {
		if ep.FilePath != "" {
			files = append(files, ep.FilePath)
		}
	}
	return files, nil
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
		writeUpstreamError(w, "load the titles", err)
		return
	}
	out := make([]discoverPayload, len(shows))
	for i, sh := range shows {
		out[i] = s.showDiscover(r.Context(), sh)
	}
	writeJSON(w, http.StatusOK, out)
}
