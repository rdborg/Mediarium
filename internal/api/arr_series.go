package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/origin"
)

// The Sonarr half of the arr-style API (see arr.go): shows and episodes.
// Apps built for Sonarr name shows by their TheTVDB id; Mediarium keeps it
// next to the TMDB id (read from TMDB when a show is added or refreshed).

type arrSeasonStats struct {
	EpisodeFileCount  int     `json:"episodeFileCount"`
	EpisodeCount      int     `json:"episodeCount"`
	TotalEpisodeCount int     `json:"totalEpisodeCount"`
	SizeOnDisk        int64   `json:"sizeOnDisk"`
	PercentOfEpisodes float64 `json:"percentOfEpisodes"`
}

type arrSeason struct {
	SeasonNumber int             `json:"seasonNumber"`
	Monitored    bool            `json:"monitored"`
	Statistics   *arrSeasonStats `json:"statistics,omitempty"`
}

type arrSeriesStats struct {
	SeasonCount       int     `json:"seasonCount"`
	EpisodeFileCount  int     `json:"episodeFileCount"`
	EpisodeCount      int     `json:"episodeCount"`
	TotalEpisodeCount int     `json:"totalEpisodeCount"`
	SizeOnDisk        int64   `json:"sizeOnDisk"`
	PercentOfEpisodes float64 `json:"percentOfEpisodes"`
}

type arrSeries struct {
	ID                int64           `json:"id,omitempty"`
	Title             string          `json:"title"`
	SortTitle         string          `json:"sortTitle"`
	CleanTitle        string          `json:"cleanTitle"`
	TitleSlug         string          `json:"titleSlug"`
	Status            string          `json:"status"`
	Ended             bool            `json:"ended"`
	Overview          string          `json:"overview"`
	Network           string          `json:"network"`
	Images            []arrImage      `json:"images"`
	Seasons           []arrSeason     `json:"seasons"`
	Year              int             `json:"year"`
	Path              string          `json:"path,omitempty"`
	RootFolderPath    string          `json:"rootFolderPath,omitempty"`
	QualityProfileID  int64           `json:"qualityProfileId"`
	LanguageProfileID int             `json:"languageProfileId"`
	SeasonFolder      bool            `json:"seasonFolder"`
	Monitored         bool            `json:"monitored"`
	MonitorNewItems   string          `json:"monitorNewItems"`
	UseSceneNumbering bool            `json:"useSceneNumbering"`
	Runtime           int             `json:"runtime"`
	TVDBID            int             `json:"tvdbId"`
	TVRageID          int             `json:"tvRageId"`
	TVMazeID          int             `json:"tvMazeId"`
	TMDBID            int             `json:"tmdbId"`
	IMDBID            string          `json:"imdbId,omitempty"`
	FirstAired        string          `json:"firstAired,omitempty"`
	SeriesType        string          `json:"seriesType"`
	Genres            []string        `json:"genres"`
	Tags              []int64         `json:"tags"`
	Added             string          `json:"added"`
	Ratings           map[string]any  `json:"ratings"`
	Statistics        *arrSeriesStats `json:"statistics,omitempty"`
	// OriginalLanguage is what Sonarr v4 apps (Arrmatey, for one) insist on.
	OriginalLanguage arrLanguage `json:"originalLanguage"`
}

// arrSeriesContext is what turning many shows into arr objects shares.
type arrSeriesContext struct {
	defaultProfile int64
	tags           map[int64][]string
	numbers        map[string]int64
	added          map[int64]string
	stats          map[int64]map[int]library.SeasonStat
	backdrops      map[int]string // saved wide pictures by TMDB id
}

func (s *Server) newArrSeriesContext() arrSeriesContext {
	c := arrSeriesContext{defaultProfile: s.defaultTVProfileOrDefault(), numbers: s.tagNumbers(), backdrops: s.arrArtOf("tv")}
	c.tags, _ = s.MovieRepo.AllTitleTags(library.TagSeries)
	c.added, _ = s.MovieRepo.AddedTimes(true)
	c.stats, _ = s.MovieRepo.SeasonStats(today())
	return c
}

// showEndedStatus is Sonarr's status for TMDB's.
func showEndedStatus(tmdbStatus string) (status string, ended bool) {
	if showEnded(tmdbStatus) {
		return "ended", true
	}
	if strings.EqualFold(tmdbStatus, "In Production") || strings.EqualFold(tmdbStatus, "Planned") {
		return "upcoming", false
	}
	return "continuing", false
}

func arrPercent(have, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(have) * 100 / float64(of)
}

func (s *Server) toArrSeries(sr library.Series, c arrSeriesContext) arrSeries {
	status, ended := showEndedStatus(sr.TMDBStatus)
	out := arrSeries{
		ID: sr.ID, Title: sr.Title, SortTitle: arrSortTitle(sr.Title), CleanTitle: arrCleanTitle(sr.Title),
		TitleSlug: arrSlug(sr.Title, sr.TMDBID), Status: status, Ended: ended, Overview: sr.Overview,
		Images: arrCoverImages(arrSonarr, sr.ID, sr.PosterPath, c.backdrops[sr.TMDBID]), Seasons: []arrSeason{}, Year: sr.Year,
		RootFolderPath: s.seriesRootFor(sr), QualityProfileID: s.arrProfileID(sr.ProfileID, c.defaultProfile),
		LanguageProfileID: 1, SeasonFolder: true, Monitored: sr.Monitored, MonitorNewItems: "all",
		TVDBID: sr.TVDBID, TMDBID: sr.TMDBID, IMDBID: sr.IMDBID, TVMazeID: max(sr.TVmazeID, 0), FirstAired: arrTime(sr.FirstAirDate),
		SeriesType: sr.SeriesType, Genres: sr.Genres, Tags: tagIDs(c.tags[sr.ID], c.numbers),
		Added: arrTime(c.added[sr.ID]), Ratings: map[string]any{"votes": 0, "value": 0}, OriginalLanguage: arrEnglish,
	}
	if out.SeriesType == "" {
		out.SeriesType = library.SeriesStandard
	}
	if out.Genres == nil {
		out.Genres = []string{}
	}
	if out.Added == "" {
		out.Added = "2000-01-01T00:00:00Z"
	}
	stats := c.stats[sr.ID]
	var numbers []int
	for n := range stats {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	total := arrSeriesStats{}
	for _, n := range numbers {
		st := stats[n]
		ss := &arrSeasonStats{EpisodeFileCount: st.Downloaded, EpisodeCount: st.Aired, TotalEpisodeCount: st.Total,
			PercentOfEpisodes: arrPercent(st.Downloaded, st.Aired)}
		out.Seasons = append(out.Seasons, arrSeason{SeasonNumber: n, Monitored: st.Monitored > 0, Statistics: ss})
		if n > 0 {
			total.SeasonCount++
		}
		total.EpisodeFileCount += st.Downloaded
		total.EpisodeCount += st.Aired
		total.TotalEpisodeCount += st.Total
	}
	total.PercentOfEpisodes = arrPercent(total.EpisodeFileCount, total.EpisodeCount)
	out.Statistics = &total
	return out
}

// tmdbArrSeries is a show that isn't in the library, as a lookup answers it.
func tmdbArrSeries(d *metadata.ShowDetail) arrSeries {
	status, ended := showEndedStatus(d.Status)
	genres := []string{}
	for _, g := range d.Genres {
		genres = append(genres, g.Name)
	}
	out := arrSeries{
		Title: d.Name, SortTitle: arrSortTitle(d.Name), CleanTitle: arrCleanTitle(d.Name), TitleSlug: arrSlug(d.Name, d.TMDBID),
		Status: status, Ended: ended, Overview: d.Overview, Images: posterImages(metadata.PosterURL(d.PosterPath)),
		Seasons: []arrSeason{}, Year: d.Year(), LanguageProfileID: 1, SeasonFolder: true, MonitorNewItems: "all",
		TVDBID: d.ExternalIDs.TVDBID, TMDBID: d.TMDBID, IMDBID: d.ExternalIDs.IMDBID, FirstAired: arrTime(d.FirstAirDate),
		SeriesType: library.SeriesStandard, Genres: genres, Tags: []int64{}, Added: "0001-01-01T00:00:00Z",
		Ratings: map[string]any{"votes": d.VoteCount, "value": d.VoteAverage}, OriginalLanguage: arrEnglish,
	}
	for _, sn := range d.Seasons {
		out.Seasons = append(out.Seasons, arrSeason{SeasonNumber: sn.SeasonNumber, Statistics: &arrSeasonStats{TotalEpisodeCount: sn.EpisodeCount}})
	}
	return out
}

func (s *Server) arrListSeries(w http.ResponseWriter, r *http.Request) {
	list, err := s.MovieRepo.ListSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.fillSeriesIDsSoon(list)
	want, _ := strconv.Atoi(r.URL.Query().Get("tvdbId"))
	if want == 0 {
		want, _ = strconv.Atoi(r.URL.Query().Get("tvdbid"))
	}
	c := s.newArrSeriesContext()
	out := []arrSeries{}
	for _, sr := range list {
		if want != 0 && sr.TVDBID != want {
			continue
		}
		out = append(out, s.toArrSeries(sr, c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SortTitle < out[j].SortTitle })
	writeJSON(w, http.StatusOK, out)
}

// fillSeriesIDsSoon reads the TVDB ids of shows added before Mediarium kept
// them, in the background, a few at a time (apps built for Sonarr can't
// match a show without one). At most once every ten minutes.
func (s *Server) fillSeriesIDsSoon(list []library.Series) {
	var missing []library.Series
	for _, sr := range list {
		if sr.TVDBID == 0 {
			missing = append(missing, sr)
		}
	}
	if len(missing) == 0 || !s.TMDB().HasAPIKey() {
		return
	}
	s.arrFill.Lock()
	if time.Since(s.arrFillAt) < 10*time.Minute {
		s.arrFill.Unlock()
		return
	}
	s.arrFillAt = time.Now()
	s.arrFill.Unlock()
	s.background(func() {
		for _, sr := range missing {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			d, err := s.TMDB().GetShow(ctx, sr.TMDBID)
			cancel()
			if err != nil {
				log.Printf("arr api: read the ids of %q: %v", sr.Title, err)
				continue
			}
			sr := sr
			s.saveSeriesIDs(&sr, d)
		}
	})
}

// arrFillState is the field set fillSeriesIDsSoon uses.
type arrFillState struct {
	arrFill   sync.Mutex
	arrFillAt time.Time
}

func (s *Server) arrLoadSeries(w http.ResponseWriter, id int64) (library.Series, bool) {
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && sr.CopyOf != 0) {
		arrNotFound(w)
		return sr, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return sr, false
	}
	return sr, true
}

func (s *Server) arrGetSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	sr, ok := s.arrLoadSeries(w, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.toArrSeries(sr, s.newArrSeriesContext()))
}

// seriesByTVDB finds a library show by its TVDB id, falling back to TMDB's
// /find so a show whose TVDB id isn't stored yet is still recognised.
func (s *Server) seriesByTVDB(ctx context.Context, tvdbID int) (library.Series, *metadata.Show, bool) {
	list, err := s.MovieRepo.ListSeries()
	if err == nil {
		for _, sr := range list {
			if sr.TVDBID == tvdbID {
				return sr, nil, true
			}
		}
	}
	show, ok, err := s.TMDB().FindShowByTVDBID(ctx, tvdbID)
	if err != nil || !ok {
		return library.Series{}, nil, false
	}
	if sr, ok, err := s.MovieRepo.GetSeriesByTMDBID(show.TMDBID); err == nil && ok {
		_ = s.MovieRepo.SetSeriesExternalIDs(sr.ID, tvdbID, "", "")
		sr.TVDBID = tvdbID
		return sr, nil, true
	}
	return library.Series{}, &show, true
}

// arrSeriesLookup answers ?term=tvdb:N, tmdb:N or a title.
func (s *Server) arrSeriesLookup(w http.ResponseWriter, r *http.Request) {
	kind, value := arrQueryTerm(r)
	ctx := r.Context()
	c := s.newArrSeriesContext()
	switch kind {
	case "tvdb":
		id, _ := strconv.Atoi(value)
		if id <= 0 {
			writeJSON(w, http.StatusOK, []arrSeries{})
			return
		}
		sr, show, ok := s.seriesByTVDB(ctx, id)
		switch {
		case !ok:
			writeJSON(w, http.StatusOK, []arrSeries{})
		case show == nil:
			writeJSON(w, http.StatusOK, []arrSeries{s.toArrSeries(sr, c)})
		default:
			d, err := s.TMDB().GetShow(ctx, show.TMDBID)
			if err != nil {
				writeJSON(w, http.StatusOK, []arrSeries{})
				return
			}
			out := tmdbArrSeries(d)
			if out.TVDBID == 0 {
				out.TVDBID = id
			}
			writeJSON(w, http.StatusOK, []arrSeries{out})
		}
		return
	case "tmdb":
		id, _ := strconv.Atoi(value)
		if sr, ok, err := s.MovieRepo.GetSeriesByTMDBID(id); err == nil && ok {
			writeJSON(w, http.StatusOK, []arrSeries{s.toArrSeries(sr, c)})
			return
		}
		d, err := s.TMDB().GetShow(ctx, id)
		if err != nil {
			writeJSON(w, http.StatusOK, []arrSeries{})
			return
		}
		writeJSON(w, http.StatusOK, []arrSeries{tmdbArrSeries(d)})
		return
	}
	if value == "" {
		writeJSON(w, http.StatusOK, []arrSeries{})
		return
	}
	results, err := s.TMDB().SearchTV(ctx, value)
	if err != nil {
		writeUpstreamError(w, "search for shows", err)
		return
	}
	if len(results) > 10 {
		results = results[:10]
	}
	// The details (for the TVDB id and seasons) of each result, a few at a time.
	out := make([]arrSeries, len(results))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, res := range results {
		if sr, ok, err := s.MovieRepo.GetSeriesByTMDBID(res.TMDBID); err == nil && ok {
			out[i] = s.toArrSeries(sr, c)
			continue
		}
		wg.Add(1)
		go func(i int, id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if d, err := s.TMDB().GetShow(ctx, id); err == nil {
				out[i] = tmdbArrSeries(d)
			}
		}(i, res.TMDBID)
	}
	wg.Wait()
	kept := []arrSeries{}
	for _, sr := range out {
		if sr.Title != "" {
			kept = append(kept, sr)
		}
	}
	writeJSON(w, http.StatusOK, kept)
}

// arrSeriesRequest is what apps send to add or change a show.
type arrSeriesRequest struct {
	ID               int64       `json:"id"`
	TVDBID           int         `json:"tvdbId"`
	TMDBID           int         `json:"tmdbId"`
	Title            string      `json:"title"`
	QualityProfileID int64       `json:"qualityProfileId"`
	Monitored        *bool       `json:"monitored"`
	Seasons          []arrSeason `json:"seasons"`
	Tags             []int64     `json:"tags"`
	SeriesType       string      `json:"seriesType"`
	RootFolderPath   string      `json:"rootFolderPath"`
	AddOptions       *struct {
		Monitor                  string `json:"monitor"`
		SearchForMissingEpisodes bool   `json:"searchForMissingEpisodes"`
	} `json:"addOptions"`
}

func (s *Server) arrAddSeries(w http.ResponseWriter, r *http.Request) {
	var req arrSeriesRequest
	if err := decodeJSON(r, &req); err != nil || (req.TVDBID <= 0 && req.TMDBID <= 0) {
		arrBadRequest(w, "TvdbId", "A show needs its TVDB id (tvdbId).")
		return
	}
	if !s.TMDB().HasAPIKey() {
		arrBadRequest(w, "TvdbId", "Mediarium has no TMDB API key yet.")
		return
	}
	ctx := r.Context()
	tmdbID := req.TMDBID
	if tmdbID <= 0 {
		sr, show, ok := s.seriesByTVDB(ctx, req.TVDBID)
		if ok && show == nil {
			_ = sr
			arrAlreadyAdded(w, "TvdbId", "This series has already been added", "SeriesExistsValidator")
			return
		}
		if !ok {
			arrBadRequest(w, "TvdbId", "TMDB doesn't know a show with that TVDB id.")
			return
		}
		tmdbID = show.TMDBID
	}
	if _, ok, err := s.MovieRepo.GetSeriesByTMDBID(tmdbID); err == nil && ok {
		arrAlreadyAdded(w, "TvdbId", "This series has already been added", "SeriesExistsValidator")
		return
	}
	userID, byline := requester(r)
	// The app's root folder when it is one of ours; else where new shows go.
	rootPath, problem := s.folderForNewShow(req.RootFolderPath)
	if problem != "" {
		rootPath = s.newShowFolder()
	}
	created, err := s.addSeriesFromTMDB(ctx, tmdbID, userID, true, rootPath, origin.Origin{Kind: origin.API})
	if err != nil {
		writeUpstreamError(w, "add that show", err)
		return
	}
	if created.TVDBID == 0 && req.TVDBID > 0 {
		_ = s.MovieRepo.SetSeriesExternalIDs(created.ID, req.TVDBID, "", "")
	}
	if p := s.arrPickProfile(req.QualityProfileID); p != 0 && p != s.defaultTVProfileOrDefault() {
		_ = s.MovieRepo.SetSeriesProfile(created.ID, p)
	}
	if t := strings.ToLower(req.SeriesType); t == library.SeriesAnime || t == library.SeriesDaily {
		_ = s.MovieRepo.SetSeriesType(created.ID, t)
	}
	// Which episodes to watch for: the seasons the app ticked, else its
	// monitor option.
	switch {
	case len(req.Seasons) > 0:
		ticked := map[int]bool{}
		for _, sn := range req.Seasons {
			ticked[sn.SeasonNumber] = sn.Monitored
		}
		eps, _ := s.MovieRepo.ListEpisodes(created.ID)
		seen := map[int]bool{}
		for _, ep := range eps {
			if seen[ep.Season] {
				continue
			}
			seen[ep.Season] = true
			want := ticked[ep.Season]
			if ep.Season == 0 && !want {
				continue // specials start unmonitored anyway
			}
			_ = s.MovieRepo.SetSeasonMonitored(created.ID, ep.Season, want)
		}
	case req.AddOptions != nil:
		if mode := strings.ToLower(req.AddOptions.Monitor); mode == library.MonitorFuture || mode == library.MonitorNone {
			_, _ = s.MovieRepo.ApplyMonitorMode(created.ID, mode, today(), false)
			if mode == library.MonitorFuture {
				_ = s.MovieRepo.SetSeriesMonitored(created.ID, true) // watching for new episodes is monitoring
			}
		}
	}
	if req.Monitored != nil && !*req.Monitored {
		_ = s.MovieRepo.SetSeriesMonitored(created.ID, false)
	}
	if len(req.Tags) > 0 {
		s.setArrTags(library.TagSeries, created.ID, created.TMDBID, req.Tags)
	}
	_ = s.QueueRepo.LogSeriesActivity(created.ID, "added", created.Title+" (series) added to library"+byline+" (through the Sonarr-style API)")
	if (req.Monitored == nil || *req.Monitored) && req.AddOptions != nil && req.AddOptions.SearchForMissingEpisodes {
		id := created.ID
		s.background(func() { s.arrSearchTV(tvScope{seriesID: id}) })
	}
	fresh, err := s.MovieRepo.GetSeries(created.ID)
	if err != nil {
		fresh = created
	}
	writeJSON(w, http.StatusCreated, s.toArrSeries(fresh, s.newArrSeriesContext()))
}

// arrUpdateSeries answers PUT /series (id in the body) and PUT /series/{id}:
// monitoring (the show's and each season's), quality profile, type and
// tags; addOptions.searchForMissingEpisodes searches.
func (s *Server) arrUpdateSeries(w http.ResponseWriter, r *http.Request) {
	var req arrSeriesRequest
	if err := decodeJSON(r, &req); err != nil {
		arrBadRequest(w, "Id", "Couldn't read that show.")
		return
	}
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil && id > 0 {
		req.ID = id
	}
	if req.ID <= 0 {
		arrBadRequest(w, "Id", "Which show? Send its id.")
		return
	}
	sr, ok := s.arrLoadSeries(w, req.ID)
	if !ok {
		return
	}
	s.arrApplySeries(&sr, req.Monitored, req.QualityProfileID, req.Tags, req.SeriesType)
	if len(req.Seasons) > 0 {
		stats, _ := s.MovieRepo.SeasonStats(today())
		for _, sn := range req.Seasons {
			st, have := stats[sr.ID][sn.SeasonNumber]
			if !have {
				continue
			}
			// Only a season whose state really changes is touched, so
			// episodes monitored one by one keep their choice.
			if sn.Monitored && st.Monitored == 0 {
				_ = s.MovieRepo.SetSeasonMonitored(sr.ID, sn.SeasonNumber, true)
			} else if !sn.Monitored && st.Monitored > 0 {
				_ = s.MovieRepo.SetSeasonMonitored(sr.ID, sn.SeasonNumber, false)
			}
		}
	}
	if sr.Monitored && req.AddOptions != nil && req.AddOptions.SearchForMissingEpisodes {
		id := sr.ID
		s.background(func() { s.arrSearchTV(tvScope{seriesID: id}) })
	}
	if fresh, err := s.MovieRepo.GetSeries(sr.ID); err == nil {
		sr = fresh
	}
	writeJSON(w, http.StatusAccepted, s.toArrSeries(sr, s.newArrSeriesContext()))
}

func (s *Server) arrApplySeries(sr *library.Series, monitored *bool, profileID int64, tags []int64, seriesType string) {
	if monitored != nil && *monitored != sr.Monitored {
		if err := s.MovieRepo.SetSeriesMonitored(sr.ID, *monitored); err == nil {
			sr.Monitored = *monitored
			if !*monitored {
				s.clearWaitingDownloads(0, sr.ID, false, stoppedMonitoringNote)
			}
		}
	}
	if p := s.arrPickProfile(profileID); p != 0 && p != s.arrProfileID(sr.ProfileID, s.defaultTVProfileOrDefault()) {
		if err := s.MovieRepo.SetSeriesProfile(sr.ID, p); err == nil {
			sr.ProfileID = p
		}
	}
	if t := strings.ToLower(seriesType); t != "" && t != sr.SeriesType && library.ValidSeriesType(t) == t {
		if err := s.MovieRepo.SetSeriesType(sr.ID, t); err == nil {
			sr.SeriesType = t
		}
	}
	if tags != nil {
		s.setArrTags(library.TagSeries, sr.ID, sr.TMDBID, tags)
	}
}

func (s *Server) arrSeriesEditor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SeriesIDs        []int64 `json:"seriesIds"`
		Monitored        *bool   `json:"monitored"`
		QualityProfileID int64   `json:"qualityProfileId"`
		SeriesType       string  `json:"seriesType"`
		Tags             []int64 `json:"tags"`
		ApplyTags        string  `json:"applyTags"`
	}
	if err := decodeJSON(r, &req); err != nil {
		arrBadRequest(w, "SeriesIds", "Send seriesIds and what to change.")
		return
	}
	c := s.newArrSeriesContext()
	out := []arrSeries{}
	for _, id := range req.SeriesIDs {
		sr, err := s.MovieRepo.GetSeries(id)
		if err != nil {
			continue
		}
		var tags []int64
		if req.Tags != nil {
			tags = editTags(req.ApplyTags, tagIDs(c.tags[sr.ID], c.numbers), req.Tags)
		}
		s.arrApplySeries(&sr, req.Monitored, req.QualityProfileID, tags, req.SeriesType)
		out = append(out, s.toArrSeries(sr, s.newArrSeriesContext()))
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (s *Server) arrDeleteSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	if _, ok := s.arrLoadSeries(w, id); !ok {
		return
	}
	if _, err := s.removeSeries(id, queryBool(r, "deleteFiles") || queryBool(r, "deletefiles")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// --- episodes ---

type arrEpisode struct {
	ID                       int64           `json:"id"`
	SeriesID                 int64           `json:"seriesId"`
	TVDBID                   int             `json:"tvdbId"`
	EpisodeFileID            int64           `json:"episodeFileId"`
	SeasonNumber             int             `json:"seasonNumber"`
	EpisodeNumber            int             `json:"episodeNumber"`
	Title                    string          `json:"title"`
	AirDate                  string          `json:"airDate,omitempty"`
	AirDateUTC               string          `json:"airDateUtc,omitempty"`
	Overview                 string          `json:"overview"`
	HasFile                  bool            `json:"hasFile"`
	Monitored                bool            `json:"monitored"`
	UnverifiedSceneNumbering bool            `json:"unverifiedSceneNumbering"`
	Grabbed                  bool            `json:"grabbed"`
	Images                   []arrImage      `json:"images"`
	Series                   *arrSeries      `json:"series,omitempty"`
	EpisodeFile              *arrEpisodeFile `json:"episodeFile,omitempty"`
}

// arrAirDateUTC is the moment an episode airs for the *arr apps: the exact
// time when TVmaze gave one, else midnight of the air date, as before.
func arrAirDateUTC(ep library.Episode) string {
	if at, err := time.Parse(time.RFC3339, ep.AirAt); err == nil {
		return at.UTC().Format("2006-01-02T15:04:05Z")
	}
	return arrTime(ep.AirDate)
}

func toArrEpisode(ep library.Episode) arrEpisode {
	out := arrEpisode{ID: ep.ID, SeriesID: ep.SeriesID, SeasonNumber: ep.Season, EpisodeNumber: ep.Episode, Title: ep.Title,
		AirDate: ep.AirDate, AirDateUTC: arrAirDateUTC(ep), Overview: ep.Overview, HasFile: ep.Status == library.StatusDownloaded,
		Monitored: ep.Monitored, Grabbed: ep.Status == library.StatusDownloading, Images: []arrImage{}}
	if out.HasFile {
		out.EpisodeFileID = ep.ID
	}
	if out.Title == "" {
		out.Title = "TBA"
	}
	return out
}

func (s *Server) arrListEpisodes(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("seriesId"), 10, 64)
	if id == 0 {
		id, _ = strconv.ParseInt(r.URL.Query().Get("seriesid"), 10, 64)
	}
	if id <= 0 {
		// Sonarr also answers a bare list of episode ids (apps such as nzb360
		// look up the episodes of their history rows this way).
		if ids := arrEpisodeIDList(r); len(ids) > 0 {
			s.arrEpisodesByID(w, r, ids)
			return
		}
		arrBadRequest(w, "SeriesId", "Ask for one show's episodes with seriesId.")
		return
	}
	eps, err := s.MovieRepo.ListEpisodes(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var series *arrSeries
	// Always the show, whether includeSeries is there or not: an app that
	// forgets it still needs the show for the picture.
	if sr, err := s.MovieRepo.GetSeries(id); err == nil {
		a := s.toArrSeries(sr, s.newArrSeriesContext())
		series = &a
	}
	// Sonarr's own filters: one season (0 is the specials), a list of episode
	// ids, one episode file. Tools such as Maintainerr ask for a single season
	// and then act on everything that comes back, so leaving a filter out
	// would hand them every season of the show.
	season, hasSeason := 0, false
	if v := r.URL.Query().Get("seasonNumber"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			arrBadRequest(w, "SeasonNumber", "The season number must be a number.")
			return
		}
		season, hasSeason = n, true
	}
	wantIDs := map[int64]bool{}
	for _, v := range r.URL.Query()["episodeIds"] {
		for _, part := range strings.Split(v, ",") {
			if n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && n > 0 {
				wantIDs[n] = true
			}
		}
	}
	fileID, _ := strconv.ParseInt(r.URL.Query().Get("episodeFileId"), 10, 64)
	out := []arrEpisode{}
	for _, ep := range eps {
		if hasSeason && ep.Season != season {
			continue
		}
		if len(wantIDs) > 0 && !wantIDs[ep.ID] {
			continue
		}
		e := toArrEpisode(ep)
		if fileID > 0 && (!e.HasFile || e.EpisodeFileID != fileID) {
			continue
		}
		e.Series = series
		if queryBool(r, "includeEpisodeFile") {
			e.EpisodeFile = s.toArrEpisodeFile(ep)
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, out)
}

// arrEpisodeIDList is the episode ids of ?episodeIds=1,2&episodeIds=3, capped
// so a careless app cannot ask for the whole database one row at a time.
func arrEpisodeIDList(r *http.Request) []int64 {
	var ids []int64
	for _, v := range r.URL.Query()["episodeIds"] {
		for _, part := range strings.Split(v, ",") {
			if n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && n > 0 && len(ids) < 500 {
				ids = append(ids, n)
			}
		}
	}
	return ids
}

// arrEpisodesByID answers GET /episode?episodeIds=... without a show: the
// episodes that exist, each with its show when asked for.
func (s *Server) arrEpisodesByID(w http.ResponseWriter, r *http.Request, ids []int64) {
	out := []arrEpisode{}
	shows := map[int64]*arrSeries{}
	for _, id := range ids {
		ep, err := s.MovieRepo.GetEpisodeByID(id)
		if err != nil {
			continue
		}
		e := toArrEpisode(ep)
		sr, seen := shows[ep.SeriesID]
		if !seen {
			if show, err := s.MovieRepo.GetSeries(ep.SeriesID); err == nil {
				a := s.toArrSeries(show, s.newArrSeriesContext())
				sr = &a
			}
			shows[ep.SeriesID] = sr
		}
		e.Series = sr
		if queryBool(r, "includeEpisodeFile") {
			e.EpisodeFile = s.toArrEpisodeFile(ep)
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) arrGetEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	ep, err := s.MovieRepo.GetEpisodeByID(id)
	if err != nil {
		arrNotFound(w)
		return
	}
	e := toArrEpisode(ep)
	if sr, err := s.MovieRepo.GetSeries(ep.SeriesID); err == nil {
		a := s.toArrSeries(sr, s.newArrSeriesContext())
		e.Series = &a
	}
	if queryBool(r, "includeEpisodeFile") {
		e.EpisodeFile = s.toArrEpisodeFile(ep)
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) arrUpdateEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Monitored *bool `json:"monitored"`
	}
	if err := decodeJSON(r, &req); err != nil {
		arrBadRequest(w, "Monitored", "Couldn't read that episode.")
		return
	}
	ep, err := s.MovieRepo.GetEpisodeByID(id)
	if err != nil {
		arrNotFound(w)
		return
	}
	if req.Monitored != nil {
		if err := s.MovieRepo.SetEpisodeMonitored(id, *req.Monitored); err == nil {
			ep.Monitored = *req.Monitored
		}
	}
	writeJSON(w, http.StatusAccepted, toArrEpisode(ep))
}

func (s *Server) arrMonitorEpisodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EpisodeIDs []int64 `json:"episodeIds"`
		Monitored  bool    `json:"monitored"`
	}
	if err := decodeJSON(r, &req); err != nil {
		arrBadRequest(w, "EpisodeIds", "Send episodeIds and monitored.")
		return
	}
	for _, id := range req.EpisodeIDs {
		_ = s.MovieRepo.SetEpisodeMonitored(id, req.Monitored)
	}
	writeJSON(w, http.StatusAccepted, []any{})
}
