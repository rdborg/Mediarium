package api

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/quality"
)

// The Radarr half of the arr-style API (see arr.go): movies.

type arrMovieFile struct {
	ID           int64           `json:"id"`
	MovieID      int64           `json:"movieId"`
	RelativePath string          `json:"relativePath"`
	Path         string          `json:"path"`
	Size         int64           `json:"size"`
	DateAdded    string          `json:"dateAdded"`
	ReleaseGroup string          `json:"releaseGroup,omitempty"`
	Quality      arrQualityModel `json:"quality"`
	Languages    []arrLanguage   `json:"languages"`
	// MediaInfo is what Mediarium read from the file's headers; left out until it has.
	MediaInfo *arrMediaInfo `json:"mediaInfo,omitempty"`
}

type arrMovie struct {
	ID                  int64          `json:"id,omitempty"`
	Title               string         `json:"title"`
	OriginalTitle       string         `json:"originalTitle"`
	SortTitle           string         `json:"sortTitle"`
	CleanTitle          string         `json:"cleanTitle"`
	TitleSlug           string         `json:"titleSlug"`
	SizeOnDisk          int64          `json:"sizeOnDisk"`
	Status              string         `json:"status"`
	Overview            string         `json:"overview"`
	InCinemas           string         `json:"inCinemas,omitempty"`
	Images              []arrImage     `json:"images"`
	Website             string         `json:"website"`
	Year                int            `json:"year"`
	HasFile             bool           `json:"hasFile"`
	YouTubeTrailerID    string         `json:"youTubeTrailerId"`
	Studio              string         `json:"studio"`
	Path                string         `json:"path,omitempty"`
	FolderName          string         `json:"folderName,omitempty"`
	RootFolderPath      string         `json:"rootFolderPath,omitempty"`
	QualityProfileID    int64          `json:"qualityProfileId"`
	Monitored           bool           `json:"monitored"`
	MinimumAvailability string         `json:"minimumAvailability"`
	IsAvailable         bool           `json:"isAvailable"`
	Runtime             int            `json:"runtime"`
	TMDBID              int            `json:"tmdbId"`
	Genres              []string       `json:"genres"`
	Tags                []int64        `json:"tags"`
	Added               string         `json:"added"`
	AlternateTitles     []any          `json:"alternateTitles"`
	Ratings             map[string]any `json:"ratings"`
	Statistics          map[string]any `json:"statistics"`
	MovieFile           *arrMovieFile  `json:"movieFile,omitempty"`
	OriginalLanguage    arrLanguage    `json:"originalLanguage"`
	// RemotePoster is the poster's address for a movie that is not in the
	// library (what a lookup carries next to images).
	RemotePoster string `json:"remotePoster,omitempty"`
}

// arrMovieContext is what turning many movies into arr objects shares.
type arrMovieContext struct {
	defaultProfile int64
	tags           map[int64][]string
	numbers        map[string]int64
	added          map[int64]string
	backdrops      map[int]string // saved wide pictures by TMDB id
}

func (s *Server) newArrMovieContext() arrMovieContext {
	c := arrMovieContext{defaultProfile: s.defaultProfileID(), numbers: s.tagNumbers(), backdrops: s.arrArtOf("movie")}
	c.tags, _ = s.MovieRepo.AllTitleTags(library.TagMovie)
	c.added, _ = s.MovieRepo.AddedTimes(false)
	return c
}

// movieStatus is Radarr's status for a movie released on date ("" unknown).
func movieStatus(date string, hasFile bool) (status string, available bool) {
	switch {
	case hasFile:
		return "released", true
	case date == "":
		return "tba", false
	case date <= today():
		return "released", true
	}
	return "announced", false
}

func (s *Server) toArrMovie(m library.Movie, c arrMovieContext) arrMovie {
	hasFile := m.Status == library.StatusDownloaded && m.FilePath != ""
	status, available := movieStatus(m.ReleaseDate, hasFile)
	out := arrMovie{
		ID: m.ID, Title: m.Title, OriginalTitle: m.Title, SortTitle: arrSortTitle(m.Title), CleanTitle: arrCleanTitle(m.Title),
		TitleSlug: strconv.Itoa(m.TMDBID), Status: status, Overview: m.Overview, InCinemas: arrTime(m.ReleaseDate),
		Images: arrCoverImages(arrRadarr, m.ID, m.PosterPath, c.backdrops[m.TMDBID]), Year: m.Year, HasFile: hasFile,
		QualityProfileID: s.arrProfileID(m.ProfileID, c.defaultProfile), Monitored: m.Monitored, MinimumAvailability: "released",
		IsAvailable: available, TMDBID: m.TMDBID, Genres: m.Genres, Tags: tagIDs(c.tags[m.ID], c.numbers),
		Added: arrTime(c.added[m.ID]), AlternateTitles: []any{}, Ratings: map[string]any{},
		OriginalLanguage: arrEnglish, RootFolderPath: s.movieRootFor(m),
	}
	if out.Genres == nil {
		out.Genres = []string{}
	}
	if out.Added == "" {
		out.Added = "2000-01-01T00:00:00Z"
	}
	if hasFile {
		out.Path = filepath.Dir(m.FilePath)
		out.FolderName = filepath.Base(out.Path)
		var size int64
		fileAdded := out.Added
		if fi, err := os.Stat(m.FilePath); err == nil {
			size = fi.Size()
			// When the file arrived, so an app sorting by it ("recently downloaded") orders by the file, not by when the movie was added.
			fileAdded = fi.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
		out.SizeOnDisk = size
		out.MovieFile = &arrMovieFile{
			ID: m.ID, MovieID: m.ID, RelativePath: filepath.Base(m.FilePath), Path: m.FilePath, Size: size,
			DateAdded: fileAdded, Quality: arrQualityModelOf(arrRadarr, quality.Tier(m.Quality)), Languages: []arrLanguage{arrEnglish},
			MediaInfo: s.arrMediaInfoFor(m.FilePath),
		}
	}
	files := 0
	if hasFile {
		files = 1
	}
	out.Statistics = map[string]any{"movieFileCount": files, "sizeOnDisk": out.SizeOnDisk, "releaseGroups": []string{}}
	return out
}

// tmdbArrMovie is a movie that isn't in the library, as a lookup answers it.
func tmdbArrMovie(m metadata.Movie) arrMovie {
	status, available := movieStatus(m.ReleaseDate, false)
	genres := []string{}
	for _, g := range m.Genres {
		genres = append(genres, g.Name)
	}
	return arrMovie{
		Title: m.Title, OriginalTitle: m.Title, SortTitle: arrSortTitle(m.Title), CleanTitle: arrCleanTitle(m.Title),
		TitleSlug: strconv.Itoa(m.TMDBID), Status: status, Overview: m.Overview, InCinemas: arrTime(m.ReleaseDate),
		Images: arrCoverImages(arrRadarr, 0, m.PosterPath, m.BackdropPath), RemotePoster: metadata.PosterURL(m.PosterPath), Year: m.Year(), MinimumAvailability: "released",
		IsAvailable: available, TMDBID: m.TMDBID, Genres: genres, Tags: []int64{}, Added: "0001-01-01T00:00:00Z",
		AlternateTitles: []any{}, Ratings: map[string]any{"tmdb": map[string]any{"value": m.VoteAverage, "votes": m.VoteCount}},
		Statistics: map[string]any{"movieFileCount": 0, "sizeOnDisk": 0, "releaseGroups": []string{}}, OriginalLanguage: arrEnglish,
	}
}

// arrListMovies answers GET /movie, optionally only the movie with ?tmdbId=.
func (s *Server) arrListMovies(w http.ResponseWriter, r *http.Request) {
	movies, err := s.MovieRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	want, _ := strconv.Atoi(r.URL.Query().Get("tmdbid"))
	if want == 0 {
		want, _ = strconv.Atoi(r.URL.Query().Get("tmdbId"))
	}
	c := s.newArrMovieContext()
	out := []arrMovie{}
	for _, m := range movies {
		if want != 0 && m.TMDBID != want {
			continue
		}
		a := s.toArrMovie(m, c)
		// The real app has no such filters, but a phone app that asks for
		// ?monitored=true&hasFile=false (its "missing" lists) means just that.
		if v, ok := arrBoolParam(r, "monitored"); ok && a.Monitored != v {
			continue
		}
		if v, ok := arrBoolParam(r, "hasFile"); ok && a.HasFile != v {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SortTitle < out[j].SortTitle })
	writeJSON(w, http.StatusOK, out)
}

// arrBoolParam reads a query value that is true or false (ok is false when the
// key is missing or says neither).
func arrBoolParam(r *http.Request, key string) (value, ok bool) {
	for k, vals := range r.URL.Query() {
		if strings.EqualFold(k, key) && len(vals) > 0 {
			switch strings.ToLower(vals[0]) {
			case "true", "1":
				return true, true
			case "false", "0":
				return false, true
			}
		}
	}
	return false, false
}

func (s *Server) arrLoadMovie(w http.ResponseWriter, id int64) (library.Movie, bool) {
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		arrNotFound(w)
		return m, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return m, false
	}
	return m, true
}

func (s *Server) arrGetMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	m, ok := s.arrLoadMovie(w, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.toArrMovie(m, s.newArrMovieContext()))
}

// arrMovieLookup answers ?term=tmdb:603, imdb:tt0133093 or a title: the
// library movie (with its id) when there is one, otherwise TMDB's.
func (s *Server) arrMovieLookup(w http.ResponseWriter, r *http.Request) {
	kind, value := arrQueryTerm(r)
	switch kind {
	case "tmdb":
		id, _ := strconv.Atoi(value)
		s.arrMovieByTMDB(w, r, id, true)
		return
	case "imdb":
		writeJSON(w, http.StatusOK, []arrMovie{})
		return
	}
	if value == "" {
		writeJSON(w, http.StatusOK, []arrMovie{})
		return
	}
	results, err := s.TMDB().SearchMovies(r.Context(), value)
	if err != nil {
		writeUpstreamError(w, "search for movies", err)
		return
	}
	c := s.newArrMovieContext()
	out := []arrMovie{}
	for _, m := range results {
		if have, ok, _ := s.MovieRepo.GetByTMDBID(m.TMDBID); ok {
			out = append(out, s.toArrMovie(have, c))
			continue
		}
		out = append(out, tmdbArrMovie(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// arrMovieLookupTMDB answers /movie/lookup/tmdb?tmdbId=603 with one movie.
func (s *Server) arrMovieLookupTMDB(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("tmdbid"))
	if id == 0 {
		id, _ = strconv.Atoi(r.URL.Query().Get("tmdbId"))
	}
	s.arrMovieByTMDB(w, r, id, false)
}

func (s *Server) arrMovieByTMDB(w http.ResponseWriter, r *http.Request, tmdbID int, asList bool) {
	answer := func(m arrMovie) {
		if asList {
			writeJSON(w, http.StatusOK, []arrMovie{m})
		} else {
			writeJSON(w, http.StatusOK, m)
		}
	}
	if tmdbID <= 0 {
		if asList {
			writeJSON(w, http.StatusOK, []arrMovie{})
		} else {
			arrNotFound(w)
		}
		return
	}
	if m, ok, err := s.MovieRepo.GetByTMDBID(tmdbID); err == nil && ok {
		answer(s.toArrMovie(m, s.newArrMovieContext()))
		return
	}
	m, err := s.TMDB().GetMovie(r.Context(), tmdbID)
	if err != nil {
		if asList {
			writeJSON(w, http.StatusOK, []arrMovie{})
		} else {
			arrNotFound(w)
		}
		return
	}
	answer(tmdbArrMovie(*m))
}

// arrMovieRequest is what apps send to add or change a movie. Pointer fields
// are left alone when missing.
type arrMovieRequest struct {
	ID               int64   `json:"id"`
	TMDBID           int     `json:"tmdbId"`
	Title            string  `json:"title"`
	QualityProfileID int64   `json:"qualityProfileId"`
	ProfileID        int64   `json:"profileId"`
	Monitored        *bool   `json:"monitored"`
	Tags             []int64 `json:"tags"`
	RootFolderPath   string  `json:"rootFolderPath"`
	AddOptions       *struct {
		SearchForMovie bool   `json:"searchForMovie"`
		Monitor        string `json:"monitor"`
	} `json:"addOptions"`
}

func (req arrMovieRequest) profile() int64 {
	if req.QualityProfileID > 0 {
		return req.QualityProfileID
	}
	return req.ProfileID
}

func (s *Server) arrAddMovie(w http.ResponseWriter, r *http.Request) {
	var req arrMovieRequest
	if err := decodeJSON(r, &req); err != nil || req.TMDBID <= 0 {
		arrBadRequest(w, "TmdbId", "A movie needs its TMDB id (tmdbId).")
		return
	}
	if !s.TMDB().HasAPIKey() {
		arrBadRequest(w, "TmdbId", "Mediarium has no TMDB API key yet.")
		return
	}
	if _, ok, err := s.MovieRepo.GetByTMDBID(req.TMDBID); err == nil && ok {
		arrAlreadyAdded(w, "TmdbId", "This movie has already been added", "MovieExistsValidator")
		return
	}
	tm, err := s.TMDB().GetMovie(r.Context(), req.TMDBID)
	if err != nil {
		arrBadRequest(w, "TmdbId", "TMDB doesn't know a movie with that id.")
		return
	}
	monitored := req.Monitored == nil || *req.Monitored
	userID, byline := requester(r)
	// The app's root folder when it is one of ours; else where new movies go.
	rootPath, problem := s.folderForNewMovie(req.RootFolderPath)
	if problem != "" {
		rootPath = s.newMovieFolder()
	}
	rootPath = s.animeMovieFolderFor(*tm, rootPath)
	created, err := s.MovieRepo.Add(library.Movie{
		TMDBID: tm.TMDBID, Title: tm.Title, Year: tm.Year(), Overview: tm.Overview, PosterPath: tm.PosterPath,
		Monitored: monitored, ReleaseDate: tm.ReleaseDate, NoUpgrade: true, AddedBy: userID,
		Genres: s.TMDB().MovieGenres(r.Context(), *tm), RootPath: rootPath,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p := s.arrPickProfile(req.profile()); p != 0 && p != s.defaultProfileID() {
		if err := s.MovieRepo.SetProfile(created.ID, p); err == nil {
			created.ProfileID = p
		}
	}
	if len(req.Tags) > 0 {
		s.setArrTags(library.TagMovie, created.ID, created.TMDBID, req.Tags)
	}
	_ = s.QueueRepo.LogActivity(created.ID, "added", created.Title+" added to library"+byline+" (through the Radarr-style API)")
	if monitored && req.AddOptions != nil && req.AddOptions.SearchForMovie {
		id := created.ID
		s.background(func() { s.searchMovieInBackground(id) })
	}
	writeJSON(w, http.StatusCreated, s.toArrMovie(created, s.newArrMovieContext()))
}

// arrUpdateMovie answers PUT /movie (id in the body) and PUT /movie/{id}:
// monitoring, quality profile and tags; addOptions.searchForMovie searches.
func (s *Server) arrUpdateMovie(w http.ResponseWriter, r *http.Request) {
	var req arrMovieRequest
	if err := decodeJSON(r, &req); err != nil {
		arrBadRequest(w, "Id", "Couldn't read that movie.")
		return
	}
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil && id > 0 {
		req.ID = id
	}
	if req.ID <= 0 {
		arrBadRequest(w, "Id", "Which movie? Send its id.")
		return
	}
	m, ok := s.arrLoadMovie(w, req.ID)
	if !ok {
		return
	}
	s.arrApplyMovie(&m, req.Monitored, req.profile(), req.Tags)
	if m.Monitored && req.AddOptions != nil && req.AddOptions.SearchForMovie {
		id := m.ID
		s.background(func() { s.searchMovieInBackground(id) })
	}
	if fresh, err := s.MovieRepo.Get(m.ID); err == nil {
		m = fresh
	}
	writeJSON(w, http.StatusAccepted, s.toArrMovie(m, s.newArrMovieContext()))
}

// arrApplyMovie changes a movie's monitoring, profile and tags (nil tags:
// left alone).
func (s *Server) arrApplyMovie(m *library.Movie, monitored *bool, profileID int64, tags []int64) {
	if monitored != nil && *monitored != m.Monitored {
		if err := s.MovieRepo.SetMonitored(m.ID, *monitored); err == nil {
			m.Monitored = *monitored
			if !*monitored {
				s.clearWaitingDownloads(m.ID, 0, false, stoppedMonitoringNote)
			}
		}
	}
	if p := s.arrPickProfile(profileID); p != 0 && p != s.arrProfileID(m.ProfileID, s.defaultProfileID()) {
		if err := s.MovieRepo.SetProfile(m.ID, p); err == nil {
			m.ProfileID = p
		}
	}
	if tags != nil {
		s.setArrTags(library.TagMovie, m.ID, m.TMDBID, tags)
	}
}

// arrMovieEditor answers PUT /movie/editor: the same changes for many movies.
func (s *Server) arrMovieEditor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MovieIDs         []int64 `json:"movieIds"`
		Monitored        *bool   `json:"monitored"`
		QualityProfileID int64   `json:"qualityProfileId"`
		Tags             []int64 `json:"tags"`
		ApplyTags        string  `json:"applyTags"`
	}
	if err := decodeJSON(r, &req); err != nil {
		arrBadRequest(w, "MovieIds", "Send movieIds and what to change.")
		return
	}
	c := s.newArrMovieContext()
	out := []arrMovie{}
	for _, id := range req.MovieIDs {
		m, err := s.MovieRepo.Get(id)
		if err != nil {
			continue
		}
		var tags []int64
		if req.Tags != nil {
			tags = editTags(req.ApplyTags, tagIDs(c.tags[m.ID], c.numbers), req.Tags)
		}
		s.arrApplyMovie(&m, req.Monitored, req.QualityProfileID, tags)
		out = append(out, s.toArrMovie(m, s.newArrMovieContext()))
	}
	writeJSON(w, http.StatusAccepted, out)
}

// editTags applies an editor's tag change ("add", "remove" or "replace").
func editTags(mode string, have, given []int64) []int64 {
	switch strings.ToLower(mode) {
	case "add":
		seen := map[int64]bool{}
		out := []int64{}
		for _, id := range append(append([]int64{}, have...), given...) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		return out
	case "remove":
		drop := map[int64]bool{}
		for _, id := range given {
			drop[id] = true
		}
		out := []int64{}
		for _, id := range have {
			if !drop[id] {
				out = append(out, id)
			}
		}
		return out
	}
	return given
}

// arrDeleteMovie removes a movie, with its files when deleteFiles is set
// (they go to the recycle bin like any removal).
func (s *Server) arrDeleteMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	if _, ok := s.arrLoadMovie(w, id); !ok {
		return
	}
	if _, err := s.removeMovie(id, queryBool(r, "deleteFiles") || queryBool(r, "deletefiles")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// arrMovieFiles answers GET /moviefile?movieId=.
func (s *Server) arrMovieFiles(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("movieid"), 10, 64)
	if id == 0 {
		id, _ = strconv.ParseInt(r.URL.Query().Get("movieId"), 10, 64)
	}
	out := []*arrMovieFile{}
	if m, err := s.MovieRepo.Get(id); err == nil {
		if f := s.toArrMovie(m, s.newArrMovieContext()).MovieFile; f != nil {
			out = append(out, f)
		}
	}
	writeJSON(w, http.StatusOK, out)
}
