package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/origin"
)

type seriesPayload struct {
	ID              int64       `json:"id"`
	TMDBID          int         `json:"tmdbId"`
	Title           string      `json:"title"`
	Year            int         `json:"year"`
	Overview        string      `json:"overview,omitempty"`
	PosterURL       string      `json:"posterUrl,omitempty"`
	Monitored       bool        `json:"monitored"`
	EpisodeCount    int         `json:"episodeCount"`
	DownloadedCount int         `json:"downloadedCount"`
	ProfileID       int64       `json:"profileId"`
	Sources         string      `json:"sources"`
	Genres          []string    `json:"genres"`           // TMDB genre names; empty until fetched
	Tags            []string    `json:"tags"`             // the person's own tags ("Kids", "4K")
	AddedBy         *userRef    `json:"addedBy"`          // null when unknown
	Origin          *originView `json:"origin,omitempty"` // how it came into the library; left out when not recorded
	// For sorting the Library: when the show started, and the size of its
	// downloaded episodes on disk.
	FirstAirDate string `json:"firstAirDate,omitempty"`
	SizeBytes    int64  `json:"sizeBytes,omitempty"`
	// NoUpgrade: automation does not look for better versions of the episodes.
	NoUpgrade bool `json:"noUpgrade"`
	// SeasonPack is the show's own choice about season packs once a season has
	// ended: "" follows the setting, "on" or "off" decides for the show.
	SeasonPack string `json:"seasonPack"`
	// DetailsState is "pending" while an import is still fetching details and
	// "problem" when that failed (DetailsNote says why); empty otherwise.
	DetailsState string `json:"detailsState,omitempty"`
	DetailsNote  string `json:"detailsNote,omitempty"`
	// SeriesType is how releases number the episodes: standard, anime or daily.
	SeriesType string `json:"seriesType"`
	// EpisodeOrder is the list the show's regular seasons follow: "tmdb" or "tvdb".
	EpisodeOrder string `json:"episodeOrder"`
	// CopyOf and Copies work as on a movie.
	CopyOf int64 `json:"copyOf,omitempty"`
	Copies int   `json:"copies,omitempty"`
	// RootPath is the library folder chosen for the show ("" = the main TV folder).
	RootPath string `json:"rootPath,omitempty"`
	// ShortFiles is how many of the show's files are far shorter than their
	// episodes should be (fakes or cut-off files); left out when none.
	ShortFiles int `json:"shortFiles,omitempty"`
	// FolderPath is the full folder of the show (as Mediarium sees it): where
	// its episodes are, or where they go when it has none. Only filled in for
	// a single show.
	FolderPath string `json:"folderPath,omitempty"`
	// ShowStatus is where the show stands: "ended", "canceled", "continuing" or "upcoming"
	// (from TMDB); left out until the show has been refreshed once.
	ShowStatus string `json:"showStatus,omitempty"`
	// Media is what the downloaded episodes' files hold: the different values
	// across the show, for the Library's filters (see media_info.go).
	Media *mediaSummaryPayload `json:"media,omitempty"`
	// OriginCountry is where TMDB says the show comes from ("US", "GB"); empty
	// until the show is refreshed. NameTag is what tells the show apart from
	// another show of the same name in the library ("US", "UK" or the year),
	// "" when no other show has its name.
	OriginCountry []string `json:"originCountry"`
	NameTag       string   `json:"nameTag,omitempty"`
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
		Genres: genres, Tags: []string{}, AddedBy: who(s.AddedBy), Origin: originOut(s.Origin, who(s.AddedBy)),
		NoUpgrade: s.NoUpgrade, SeasonPack: s.SeasonPack, DetailsState: s.DetailsState, DetailsNote: s.DetailsNote, SeriesType: library.ValidSeriesType(s.SeriesType), EpisodeOrder: library.ValidEpisodeOrder(s.EpisodeOrder),
		FirstAirDate: s.FirstAirDate, CopyOf: s.CopyOf, RootPath: s.RootPath,
		ShowStatus:    showStatusOf(s.TMDBStatus),
		OriginCountry: append([]string{}, s.OriginCountry...),
	}
}

// showStatusOf is the show's status in the words the screens use, or "" when
// TMDB's status isn't known yet.
func showStatusOf(tmdbStatus string) string {
	if strings.TrimSpace(tmdbStatus) == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(tmdbStatus), "Canceled") || strings.EqualFold(strings.TrimSpace(tmdbStatus), "Cancelled") {
		return "canceled"
	}
	status, _ := showEndedStatus(tmdbStatus)
	return status
}

type episodePayload struct {
	ID       int64  `json:"id"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
	Title    string `json:"title,omitempty"`
	Overview string `json:"overview,omitempty"`
	AirDate  string `json:"airDate,omitempty"`
	// AirAt is the exact air time (RFC 3339, UTC) when TVmaze has one.
	AirAt     string `json:"airAt,omitempty"`
	Status    string `json:"status"`
	Quality   string `json:"quality,omitempty"`
	FilePath  string `json:"filePath,omitempty"`
	Monitored bool   `json:"monitored"`
	// SizeBytes is the size of the episode's file on disk (downloaded episodes only).
	SizeBytes int64 `json:"sizeBytes,omitempty"`
	// FileInfo is what the episode's file holds: video, audio and subtitle
	// tracks. Left out until the file has been read.
	FileInfo *fileInfoPayload `json:"fileInfo,omitempty"`
	// ShortFile is set when the file is far shorter than the episode should be.
	ShortFile *shortFileNote `json:"shortFile,omitempty"`
}

func toEpisodePayload(e library.Episode) episodePayload {
	return episodePayload{
		ID: e.ID, Season: e.Season, Episode: e.Episode, Title: e.Title, Overview: e.Overview,
		AirDate: e.AirDate, AirAt: e.AirAt, Status: string(e.Status), Quality: e.Quality, FilePath: e.FilePath, Monitored: e.Monitored,
	}
}

type seriesDetailPayload struct {
	seriesPayload
	Episodes []episodePayload `json:"episodes"`
	// TheTVDBKey says a TheTVDB key is saved, so the show can follow
	// TheTVDB's episode order. The key's settings are for administrators;
	// this lets a manager see whether the choice is open.
	TheTVDBKey bool `json:"thetvdbKey"`
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
	shows, err := s.displayTMDB().SearchTV(r.Context(), query)
	if err != nil {
		writeUpstreamError(w, "search", err)
		return
	}
	newestFirst(shows, query, func(sh metadata.Show) string { return sh.Name }, func(sh metadata.Show) int { return sh.Year() })
	out := make([]discoverPayload, len(shows))
	items := make([]nameItem, len(shows))
	for i, sh := range shows {
		out[i] = s.showDiscover(r.Context(), sh)
		items[i] = nameItem{id: int64(sh.TMDBID), title: sh.Name, year: sh.Year(), country: countryName(sh.OriginCountry)}
	}
	tags := nameTagsFor(items)
	for i := range out {
		out[i].NameTag = tags[int64(out[i].TMDBID)]
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListSeries lists the library's shows. With ?brief=1 each show's
// overview is left out, as for movies.
//
// The shows with their episode totals and file sizes are the costly part on a
// big library; they are shared between callers and kept until the library
// changes (seriesList). Tags and copies are read fresh each time.
func (s *Server) handleListSeries(w http.ResponseWriter, r *http.Request) {
	brief := r.URL.Query().Get("brief") == "1"
	data, err := s.seriesList(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list := data.list
	who := s.accountNames()
	tags, _ := s.MovieRepo.AllTitleTags(library.TagSeries)
	copies, _ := s.MovieRepo.CopyCounts("series")
	out := make([]seriesPayload, len(list))
	tvFolders := s.tvFolders()
	nameTags := sameNameTags(list)
	media := s.mediaSummaryNow(true).series
	short := s.incompleteNow().series
	for i, sr := range list {
		out[i] = toSeriesPayload(sr, who)
		out[i].Origin = adminOrigin(r, out[i].Origin)
		out[i].Title, out[i].PosterURL = s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
		out[i].Overview = s.shownOverview("tv", sr.TMDBID, sr.Overview)
		out[i].ShortFiles = len(short[sr.ID])
		out[i].Media = mediaSummaryOf(media[sr.ID]...)
		out[i].RootPath = folderShown(r, tvFolders, sr.RootPath)
		out[i].NameTag = nameTags[sr.ID]
		if brief {
			out[i].Overview = ""
		}
		out[i].Copies = copies[sr.ID]
		out[i].SizeBytes = data.sizes[sr.ID]
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
	out.Origin = adminOrigin(r, out.Origin)
	out.TheTVDBKey = s.tvdbKeyClient() != nil
	out.Title, out.PosterURL = s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
	out.Overview = s.shownOverview("tv", sr.TMDBID, sr.Overview)
	out.RootPath = folderShown(r, s.tvFolders(), sr.RootPath)
	if all, err := s.MovieRepo.ListSeriesNames(); err == nil {
		key := sr.ID
		if sr.CopyOf != 0 {
			key = sr.CopyOf
		}
		out.NameTag = sameNameTags(all)[key]
	}
	if t, err := s.MovieRepo.TitleTags(library.TagSeries, sr.ID); err == nil {
		out.Tags = t
	}
	now := time.Now()
	var files []string
	fileInfos, _ := s.MovieRepo.EpisodeMediaInfo(id)
	for i, e := range eps {
		out.Episodes[i] = toEpisodePayload(e)
		if row, ok := fileInfos[e.ID]; ok && e.Status == library.StatusDownloaded {
			out.Episodes[i].FileInfo = fileInfoOf(row)
			out.Episodes[i].ShortFile = s.shortFileOf(e.FilePath)
		}
		out.Episodes[i].FilePath = fileLabel(r, e.FilePath)
		if e.Status == library.StatusDownloaded {
			out.Episodes[i].SizeBytes = cachedFileSize(e.FilePath, now)
		}
		if e.FilePath != "" {
			files = append(files, e.FilePath)
		}
	}
	if isAdminRequest(r) { // only administrators see where the files lie
		out.FolderPath = s.seriesFolderFor(sr, files)
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
	// NoUpgrade leaves the episodes alone once they are downloaded. When left
	// out it follows "Keep looking for better versions" under Settings > Finding
	// releases > Automatic searching (off unless switched on).
	NoUpgrade *bool `json:"noUpgrade"`
	// RootPath is the TV folder to keep it in; left out = where new shows go
	// (Settings > Library and files > Folders).
	RootPath string `json:"rootPath"`
	// Episode asks for one episode instead of the whole show: the show is
	// added with nothing monitored but that episode, which is searched for.
	// A show that is in the library already only gets that episode wanted.
	Episode *episodeRef `json:"episode"`
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
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Library and files > Movie and show info).")
		return
	}
	if !s.mayAskForAge(w, r, "tv", req.TMDBID) {
		return
	}
	if req.Episode != nil && !req.Episode.valid() {
		writeError(w, http.StatusBadRequest, "Pick an episode from the show's seasons.")
		return
	}
	if existing, ok, err := s.MovieRepo.GetSeriesByTMDBID(req.TMDBID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		if req.Episode == nil {
			writeError(w, http.StatusConflict, fmt.Sprintf("%s is already in your library", existing.Title))
			return
		}
		if err := s.monitorOneEpisode(existing, *req.Episode); err != nil {
			writeRemoveError(w, err)
			return
		}
		_, byline := requester(r)
		_ = s.QueueRepo.LogSeriesActivity(existing.ID, "added", fmt.Sprintf("%s %s is now wanted%s", existing.Title, req.Episode.code(), byline))
		shown := toSeriesPayload(existing, s.accountNames())
		shown.Origin = adminOrigin(r, shown.Origin)
		writeJSON(w, http.StatusOK, shown)
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
	rootPath, problem := s.folderForNewShow(req.RootPath)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}

	userID, byline := requester(r)
	created, err := s.addSeriesFromTMDB(r.Context(), req.TMDBID, userID, boolOr(req.NoUpgrade, s.noUpgradeByDefault()), rootPath, addOrigin(r))
	if err != nil {
		writeUpstreamError(w, "add that show", err)
		return
	}
	if req.Episode != nil {
		// One episode: nothing else is wanted, and the show's own switch follows the episode.
		_, _ = s.MovieRepo.ApplyMonitorMode(created.ID, library.MonitorNone, todayUTC(), false)
		req.SearchNow = false
	} else if req.Monitor == library.MonitorFuture || req.Monitor == library.MonitorNone {
		_, _ = s.MovieRepo.ApplyMonitorMode(created.ID, req.Monitor, todayUTC(), false)
		if req.Monitor == library.MonitorFuture {
			// Watching for episodes that have not aired is monitoring: the show's own switch is on.
			if err := s.MovieRepo.SetSeriesMonitored(created.ID, true); err == nil {
				created.Monitored = true
			}
		}
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
	if req.Episode != nil {
		// After the quality and sources are set, so the search uses them.
		if err := s.monitorOneEpisode(created, *req.Episode); err != nil {
			log.Printf("api: add %q: couldn't want the episode: %v", created.Title, err)
		}
	}
	if req.SearchNow {
		by := userOrigin(r, origin.Search)
		go s.searchSeriesInBackgroundBy(created.ID, by)
	}
	added := toSeriesPayload(created, s.accountNames())
	added.Origin = adminOrigin(r, added.Origin)
	added.RootPath = folderShown(r, s.tvFolders(), created.RootPath)
	writeJSON(w, http.StatusCreated, added)
}

// addSeriesFromTMDB adds a show with its episode list and genres, recording
// userID (0 = unknown) as the account that added it. noUpgrade leaves the
// episodes alone once they are downloaded.
func (s *Server) addSeriesFromTMDB(ctx context.Context, tmdbID int, userID int64, noUpgrade bool, rootPath string, how origin.Origin) (library.Series, error) {
	detail, infos, err := s.TMDB().GetShowEpisodes(ctx, tmdbID)
	if err != nil {
		return library.Series{}, err
	}
	kind := guessSeriesType(detail.Show)
	show := library.Series{
		TMDBID: detail.TMDBID, Title: detail.Name, Year: detail.Year(), Overview: detail.Overview,
		PosterPath: detail.PosterPath, FirstAirDate: detail.FirstAirDate, Monitored: true,
		NoUpgrade: noUpgrade, AddedBy: userID, Genres: s.TMDB().ShowGenres(ctx, detail.Show),
		SeriesType: kind, RootPath: s.animeFolderFor(kind, rootPath), Origin: how,
	}
	sr, err := s.MovieRepo.AddSeries(show, s.withTVDBEpisodes(ctx, show, detail.ExternalIDs.TVDBID, toLibraryEpisodes(infos)))
	if err != nil {
		return sr, err
	}
	// An anime show gets the anime library's profile; a profile picked when
	// adding it is set afterwards and wins.
	if id := s.animeDefaultProfileID(); id != 0 && kind == library.SeriesAnime && sr.ProfileID == 0 {
		if err := s.MovieRepo.SetSeriesProfile(sr.ID, id); err == nil {
			sr.ProfileID = id
		}
	}
	s.saveSeriesIDs(&sr, detail)
	s.noteSeriesNumbering(sr)
	go s.rememberDisplayFace("tv", sr.TMDBID, sr.Title, sr.PosterPath, sr.Overview)
	return sr, nil
}

// saveSeriesIDs stores the TVDB and IMDb ids, the status and the origin
// country TMDB gave for a show.
func (s *Server) saveSeriesIDs(sr *library.Series, detail *metadata.ShowDetail) {
	s.saveSeriesCountry(sr, detail.Show)
	tvdb, imdb, status := detail.ExternalIDs.TVDBID, detail.ExternalIDs.IMDBID, detail.Status
	if (tvdb == 0 || tvdb == sr.TVDBID) && (imdb == "" || imdb == sr.IMDBID) && (status == "" || status == sr.TMDBStatus) {
		return
	}
	if err := s.MovieRepo.SetSeriesExternalIDs(sr.ID, tvdb, imdb, status); err != nil {
		log.Printf("api: %v", err)
		return
	}
	if tvdb > 0 {
		sr.TVDBID = tvdb
	}
	if imdb != "" {
		sr.IMDBID = imdb
	}
	if status != "" {
		sr.TMDBStatus = status
	}
}

func toLibraryEpisodes(infos []metadata.EpisodeInfo) []library.Episode {
	eps := make([]library.Episode, len(infos))
	for i, e := range infos {
		eps[i] = library.Episode{Season: e.Season, Episode: e.Episode, Title: e.Name, Overview: e.Overview, AirDate: e.AirDate}
	}
	return eps
}

type monitorModeRequest struct {
	Mode   string `json:"mode"`
	DryRun bool   `json:"dryRun"`
}

// handleSetSeriesMonitorMode applies the add-time choice of which episodes to
// monitor (all, only the ones that have not aired yet, none) to a show that is
// already in the library. With dryRun it only says how many episodes would
// change. Nothing is searched for and no file is touched.
func (s *Server) handleSetSeriesMonitorMode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	var req monitorModeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.Mode != library.MonitorAll && req.Mode != library.MonitorFuture && req.Mode != library.MonitorNone {
		writeError(w, http.StatusBadRequest, `mode must be "all", "future" or "none"`)
		return
	}
	if _, err := s.MovieRepo.GetSeries(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	change, err := s.MovieRepo.ApplyMonitorMode(id, req.Mode, todayUTC(), req.DryRun)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !req.DryRun && req.Mode != library.MonitorNone {
		// A show that tracks episodes is monitored, so a refresh keeps
		// monitoring the new ones it finds. "None" leaves the show's own
		// switch alone, as it does when a show is added that way.
		if err := s.MovieRepo.SetSeriesMonitored(id, true); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, change)
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
	refreshed := toSeriesPayload(updated, s.accountNames())
	refreshed.Origin = adminOrigin(r, refreshed.Origin)
	refreshed.RootPath = folderShown(r, s.tvFolders(), updated.RootPath)
	writeJSON(w, http.StatusOK, refreshed)
}

// refreshSeries brings a show's episode list and genres up to date from TMDB
// (or, for a show set to TheTVDB's episode order, its regular seasons from
// TheTVDB).
func (s *Server) refreshSeries(ctx context.Context, sr library.Series) error {
	_, _, err := s.refreshSeriesOrder(ctx, sr, false)
	return err
}

// refreshSeriesOrder is refreshSeries. With remap set (the show's episode
// order was just changed) the episodes the show already has are moved to the
// numbers the new list gives them, the way a show in TheTVDB's order always
// is. It returns the seasons left as they were because a file in them could
// not be matched safely, and whether the episodes were stored (an error after
// that is not a reason to undo the change).
func (s *Server) refreshSeriesOrder(ctx context.Context, sr library.Series, remap bool) (blocked []int, synced bool, err error) {
	detail, infos, err := s.TMDB().GetShowEpisodes(ctx, sr.TMDBID)
	if err != nil {
		return nil, false, err
	}
	if len(detail.Genres) > 0 || sr.Genres == nil {
		if err := s.MovieRepo.SetSeriesGenres(sr.ID, s.TMDB().ShowGenres(ctx, detail.Show)); err != nil {
			log.Printf("api: refresh %q genres: %v", sr.Title, err)
		}
	}
	tmdbEps := toLibraryEpisodes(infos)
	var eps []library.Episode
	reorder, prune := remap, true
	if sr.EpisodeOrder == library.EpisodeOrderTVDB {
		reorder = true
		ordered, err := s.tvdbOrderedEpisodes(ctx, sr, detail.ExternalIDs.TVDBID, tmdbEps)
		if err != nil {
			// Not TMDB's numbers over TheTVDB's: only the specials are
			// refreshed, the regular seasons stay as they are until
			// TheTVDB answers.
			if remap {
				return nil, false, err // changing the order needs TheTVDB's list
			}
			log.Printf("api: refresh %q: %v; its regular seasons are left as they are", sr.Title, err)
			for _, e := range tmdbEps {
				if e.Season == 0 {
					eps = append(eps, e)
				}
			}
			reorder, prune = false, false
		} else {
			eps = ordered
		}
	} else {
		eps = s.withTVDBEpisodes(ctx, sr, detail.ExternalIDs.TVDBID, tmdbEps)
	}
	_, blocked, err = s.syncEpisodes(sr, eps, reorder, prune)
	if err != nil {
		return nil, false, err
	}
	s.saveSeriesIDs(&sr, detail)
	if err := s.MovieRepo.MarkSeriesChecked(sr.ID, time.Now()); err != nil {
		log.Printf("api: refresh %q: %v", sr.Title, err)
	}
	s.refreshOtherTitles(ctx, "tv", sr.TMDBID, sr.Title)
	s.noteSeriesNumbering(sr)
	s.stopMonitoringIfFinished(sr.ID, detail.Status)
	// Second copies get the same new episodes.
	copies, err := s.MovieRepo.ListSeriesCopies(sr.ID)
	if err != nil {
		return blocked, true, err
	}
	for _, c := range copies {
		if _, _, err := s.syncEpisodes(c, eps, reorder, prune); err != nil {
			return blocked, true, err
		}
	}
	s.syncAirTimes(ctx, sr)
	go s.rememberDisplayFace("tv", sr.TMDBID, sr.Title, sr.PosterPath, sr.Overview)
	return blocked, true, nil
}

// syncEpisodes stores a show's episode list. With reorder the episodes the
// show already has are first moved to the numbers the list gives them (see
// library.PlanEpisodeOrder: an episode with a file is never removed, and a
// season where one can't be placed is left as it was). Episodes TMDB dropped
// or renumbered, that never got anything, are removed when prune is set. It
// returns the list as stored, and the seasons left as they were.
func (s *Server) syncEpisodes(sr library.Series, eps []library.Episode, reorder, prune bool) ([]library.Episode, []int, error) {
	var blocked []int
	if reorder {
		old, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			return nil, nil, err
		}
		plan := library.PlanEpisodeOrder(old, eps)
		if err := s.MovieRepo.ApplyEpisodeOrder(sr.ID, plan); err != nil {
			return nil, nil, fmt.Errorf("change the episode order of %q: %w", sr.Title, err)
		}
		if len(plan.Moves)+len(plan.Drop) > 0 {
			log.Printf("api: %q: episode order: moved %d episodes, removed %d that never got anything", sr.Title, len(plan.Moves), len(plan.Drop))
		}
		eps, blocked = plan.Final, plan.Blocked
	}
	if err := s.MovieRepo.UpsertEpisodes(sr.ID, eps, sr.Monitored); err != nil {
		return nil, nil, err
	}
	// Episodes TMDB dropped or renumbered would stay as missing for ever.
	if prune {
		if n, err := s.MovieRepo.PruneEpisodes(sr.ID, eps); err != nil {
			log.Printf("api: refresh %q: remove dropped episodes: %v", sr.Title, err)
		} else if n > 0 {
			log.Printf("api: refresh %q: removed %d missing episodes TMDB no longer lists", sr.Title, n)
		}
	}
	return eps, blocked, nil
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
	if req.ReplaceQueueID > 0 {
		replaceMu.Lock()
		defer replaceMu.Unlock()
		if err := s.replaceBlocking(req.ReplaceQueueID, func() error {
			return s.checkEpisodesNotDownloading(sr.ID, req.Season, req.Episode, req.ReleaseTitle)
		}); err != nil {
			writeGrabError(w, err, http.StatusBadRequest)
			return
		}
	}
	queueID, err := s.grabTVOpts(sr, req.Season, req.Episode, req.ReleaseTitle, req.DownloadURL, req.SizeBytes, req.protocol(), grabPicked, req.Replace, userOrigin(r, origin.Manual))
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
	// A main show's second copies go first, the same way.
	if series.CopyOf == 0 {
		copies, err := s.MovieRepo.ListSeriesCopies(series.ID)
		if err != nil {
			return series.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
		}
		for _, c := range copies {
			if _, err := s.removeSeries(c.ID, deleteFiles); err != nil {
				return series.Title, err
			}
		}
	}
	files, err := s.seriesFiles(id)
	if err != nil {
		return series.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	if deleteFiles {
		if err := s.checkFilesRemovable(s.seriesRootFor(series), files); err != nil {
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
			if err := s.checkFilesRemovable(s.seriesRootFor(series), files); err != nil {
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
	s.forgetHeldSeries(id)
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
	s.discoverShows(w, r, s.displayTMDB().TrendingTV)
}

func (s *Server) handlePopularTV(w http.ResponseWriter, r *http.Request) {
	s.discoverShows(w, r, s.displayTMDB().PopularTV)
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
