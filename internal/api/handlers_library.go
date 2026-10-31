package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/trakt"
)

// movieCategory is the Newznab category for movies (search
// results should be scoped to what the app actually manages).
var movieCategory = []int{2000}

type searchResultPayload struct {
	Title       string `json:"title"`
	IndexerName string `json:"indexerName"`
	Protocol    string `json:"protocol"` // "usenet" or "torrent" (protocol icon per result)
	DownloadURL string `json:"downloadUrl"`
	SizeBytes   int64  `json:"sizeBytes"`
	PublishDate string `json:"publishDate,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	Source      string `json:"source,omitempty"`
	Codec       string `json:"codec,omitempty"`
	Group       string `json:"group,omitempty"`
	Seeders     int    `json:"seeders,omitempty"`
	Peers       int    `json:"peers,omitempty"`
	InfoHash    string `json:"infoHash,omitempty"` // torrents, when the indexer reports it
	// Cached is true when the debrid service in use already has this torrent,
	// so it starts at once. Left out when it has not or it is not known
	// (debrid_cache.go).
	Cached bool   `json:"cached,omitempty"`
	hash   string // the info hash from the indexer or the magnet link, for the cache check
	// BelowMinSeeders is set on a torrent with fewer seeders than the minimum set
	// on its indexer (MinSeeders): automatic searches skip it, a grab by hand is
	// still allowed. A cached release is never marked.
	BelowMinSeeders bool `json:"belowMinSeeders,omitempty"`
	MinSeeders      int  `json:"minSeeders,omitempty"`
	// Season/Episodes are set for TV releases (Episodes empty = a season
	// pack), so the unified search UI can label them and route the grab.
	Season   int   `json:"season,omitempty"`
	Episodes []int `json:"episodes,omitempty"`
	// Blocklisted releases failed before for a reason that was the release
	// fault; automation skips them, but a manual grab is still allowed.
	Blocklisted bool `json:"blocklisted,omitempty"`
	// Rejections explain why automation would not pick this release (quality
	// not allowed by the profile, not an upgrade...). A manual grab is still
	// allowed.
	Rejections []string `json:"rejections,omitempty"`
	// FormatScore is the release's custom-format score in the item's
	// profile, and Formats the formats it matched; left out when the profile
	// scores no formats.
	// Best marks the release automatic search would take; the list puts it first.
	Best        bool                `json:"best,omitempty"`
	FormatScore *int                `json:"formatScore,omitempty"`
	Formats     []quality.FormatHit `json:"formats,omitempty"`
	// MinScore and UpgradeUntil are the profile's minimum custom-format score
	// and the score it stops upgrading at (0 is left out), for the "why this
	// score" details. Reasons lists, in plain words and by kind, everything that
	// makes an automatic search skip the release (see release_why.go).
	MinScore     int             `json:"minScore,omitempty"`
	UpgradeUntil int             `json:"upgradeUntil,omitempty"`
	Reasons      []releaseReason `json:"reasons,omitempty"`
	// Terms are the profile's preferred terms the release matched, each with
	// its score; they add to the score next to the custom formats.
	Terms []preferredHit `json:"terms,omitempty"`
	// Score is the whole score the pick compares (Profile.Score): the custom
	// formats, the preferred terms and FixScore added up. It is what the
	// Score column shows, and equals the parts listed in the details. Left
	// out when the profile scores nothing.
	Score    *int `json:"score,omitempty"`
	FixScore int  `json:"fixScore,omitempty"` // the bonus of a PROPER or REPACK when bad releases are skipped
	// AcceptedBy names the profile that accepts this release: the item's own
	// profile, or one of its fallbacks (fallback true). Left out when none
	// does. Only set for a search on a library item.
	AcceptedBy *acceptedByPayload `json:"acceptedBy,omitempty"`
	// Language names the audio languages the release says it has ("Italian +
	// English", "Multi"); left out when the release has no language tag.
	// LanguageFit is "ok" (the wanted language, or untagged), "mixed" (the
	// wanted language next to another, or a multi-language release) or
	// "other" (clearly another language only: automation skips it, a manual
	// grab is still allowed). Both use the Preferred audio language setting.
	Language    string `json:"language,omitempty"`
	LanguageFit string `json:"languageFit,omitempty"`
	// MatchedTitle is the other title of the movie or show the release is
	// named by ("Der Herr der Ringe"), when it is not named by the main one.
	// Only set for a search on a library item.
	MatchedTitle string `json:"matchedTitle,omitempty"`
	// Categories are the indexer's category numbers of the release. Only set for
	// a book search, where the list shows them.
	Categories []int `json:"categories,omitempty"`
}

// toSearchResultPayload builds a search result for the UI. Its download URL
// is remembered server side and sent as an opaque reference (see
// grab_urls.go), so the indexer's API key never reaches the browser.
func (s *Server) toSearchResultPayload(res indexers.Result) searchResultPayload {
	release := parser.Parse(res.Title)
	payload := searchResultPayload{
		Title:       res.Title,
		IndexerName: res.IndexerName,
		Protocol:    string(res.Protocol),
		DownloadURL: s.offered.ref(res.DownloadURL), // an opaque reference: the real URL can carry the indexer's API key
		SizeBytes:   res.SizeBytes,
		Resolution:  release.Resolution,
		Source:      release.Source,
		Codec:       release.Codec,
		Group:       release.Group,
		Seeders:     res.Seeders,
		Peers:       res.Peers,
		InfoHash:    res.InfoHash,
		Cached:      res.Cached,
		hash:        releaseHash(res),
		Season:      release.Season,
		Episodes:    release.Episodes,
	}
	if !res.PublishDate.IsZero() {
		payload.PublishDate = res.PublishDate.Format("2006-01-02T15:04:05Z07:00")
	}
	if belowMinSeeders(res) {
		payload.BelowMinSeeders, payload.MinSeeders = true, res.MinSeeders
	}
	fit := quality.FitLanguageOnly(release, s.preferredLanguage(), s.languageOnly())
	payload.Language = fit.Label
	if fit.Label == "" && fit.Rank < 3 {
		payload.Language = "Not stated"
	}
	switch {
	case !fit.Accepted:
		payload.LanguageFit = "other"
	case fit.Rank < 3:
		payload.LanguageFit = "mixed"
	case fit.Label != "":
		payload.LanguageFit = "ok"
	}
	// A Usenet post older than the retention is shown with the reason; the
	// automatic searches leave it out.
	if why := s.retentionRejection(res); why != "" {
		payload.addReasons("age", []string{why})
	}
	return payload
}

// handleSearch is the unified search endpoint: one result list across
// every indexer, clearly tagged by source.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	list, err := s.searchableIndexers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(list) == 0 {
		writeJSON(w, http.StatusOK, []searchResultPayload{})
		return
	}

	outcomes := indexers.SearchAll(indexers.AsManual(r.Context()), list, query, mediaCategories)
	merged := indexers.MergeResults(outcomes)

	blocked := s.searchSkipKeys()
	out := make([]searchResultPayload, len(merged))
	for i, res := range merged {
		out[i] = s.toSearchResultPayload(res)
		out[i].Blocklisted = blocked[blocklist.Key(res.Title)]
	}
	s.markCachedPayloads(r.Context(), out)
	writeJSON(w, http.StatusOK, out)
}

type moviePayload struct {
	ID        int64    `json:"id"`
	TMDBID    int      `json:"tmdbId"`
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Overview  string   `json:"overview,omitempty"`
	PosterURL string   `json:"posterUrl,omitempty"`
	Status    string   `json:"status"`
	Quality   string   `json:"quality,omitempty"`
	FilePath  string   `json:"filePath,omitempty"`
	ProfileID int64    `json:"profileId"`
	Monitored bool     `json:"monitored"`
	Sources   string   `json:"sources"`
	Genres    []string `json:"genres"`  // TMDB genre names; empty until fetched
	Tags      []string `json:"tags"`    // the person's own tags ("Kids", "4K")
	AddedBy   *userRef `json:"addedBy"` // null when unknown
	// For sorting the Library: the release date ("2024-03-01", "" when
	// unknown) and the size of the file on disk (0 when there is none).
	ReleaseDate string `json:"releaseDate,omitempty"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	// NoUpgrade: automation leaves this movie alone once it is downloaded.
	NoUpgrade bool `json:"noUpgrade"`
	// DetailsState is "pending" while an import is still fetching details and
	// "problem" when that failed (DetailsNote says why); empty otherwise.
	DetailsState string `json:"detailsState,omitempty"`
	DetailsNote  string `json:"detailsNote,omitempty"`
	// CopyOf is the main movie's id when this is a second copy; Copies is how
	// many second copies a main movie has.
	CopyOf int64 `json:"copyOf,omitempty"`
	Copies int   `json:"copies,omitempty"`
	// RootPath is the library folder chosen for the movie ("" = the main
	// movies folder).
	RootPath string `json:"rootPath,omitempty"`
	// FolderPath is the full folder the movie's files are in, or go to when it
	// has none (as Mediarium sees it). Only filled in for a single movie.
	FolderPath string `json:"folderPath,omitempty"`
	// Media is what the file holds, short lists the Library filters on (the
	// list); FileInfo is the full details of the file (a single movie). Both
	// are left out until the file has been read (media_info.go).
	Media    *mediaSummaryPayload `json:"media,omitempty"`
	FileInfo *fileInfoPayload     `json:"fileInfo,omitempty"`
	// ShortFile is set when the file is far shorter than the movie should be
	// (a fake or cut-off file).
	ShortFile *shortFileNote `json:"shortFile,omitempty"`
}

// toMoviePayload builds a library movie's JSON; who resolves the account
// that added it (see accountNames).
func toMoviePayload(m library.Movie, who func(int64) *userRef) moviePayload {
	genres := m.Genres
	if genres == nil {
		genres = []string{}
	}
	return moviePayload{
		ID: m.ID, TMDBID: m.TMDBID, Title: m.Title, Year: m.Year, Overview: m.Overview,
		PosterURL: metadata.PosterURL(m.PosterPath), Status: string(m.Status), Quality: m.Quality, FilePath: m.FilePath, ProfileID: m.ProfileID, Monitored: m.Monitored, Sources: m.SourcePref,
		Genres: genres, Tags: []string{}, AddedBy: who(m.AddedBy),
		NoUpgrade: m.NoUpgrade, DetailsState: m.DetailsState, DetailsNote: m.DetailsNote, ReleaseDate: m.ReleaseDate,
		CopyOf: m.CopyOf, RootPath: m.RootPath,
	}
}

// handleListMovies lists the library. With ?brief=1 each movie's overview is
// left out: it is most of the answer, and drawing the Library does not use it.
func (s *Server) handleListMovies(w http.ResponseWriter, r *http.Request) {
	brief := r.URL.Query().Get("brief") == "1"
	list, err := s.MovieRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	who := s.accountNames()
	tags, _ := s.MovieRepo.AllTitleTags(library.TagMovie)
	copies, _ := s.MovieRepo.CopyCounts("movies")
	out := make([]moviePayload, len(list))
	now := time.Now()
	movieFolders := s.movieFolders()
	media := s.mediaSummaryNow(true).movies
	short := s.incompleteNow().movies
	for i, m := range list {
		out[i] = toMoviePayload(m, who)
		out[i].Title, out[i].PosterURL = s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
		if e, ok := short[m.ID]; ok && e.Path == m.FilePath {
			n := e.note
			out[i].ShortFile = &n
		}
		if ms, ok := media[m.ID]; ok {
			out[i].Media = mediaSummaryOf(ms)
		}
		out[i].FilePath = fileLabel(r, m.FilePath)
		out[i].RootPath = folderShown(r, movieFolders, m.RootPath)
		if brief {
			out[i].Overview = ""
		}
		out[i].SizeBytes = cachedFileSize(m.FilePath, now)
		out[i].Copies = copies[m.ID]
		if t := tags[m.ID]; t != nil {
			out[i].Tags = t
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetMovie(w http.ResponseWriter, r *http.Request) {
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
	out := toMoviePayload(m, s.accountNames())
	out.Title, out.PosterURL = s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
	out.FilePath = fileLabel(r, m.FilePath)
	out.RootPath = folderShown(r, s.movieFolders(), m.RootPath)
	out.SizeBytes = fileSize(m.FilePath)
	out.FileInfo = s.movieFileInfo(m.FilePath)
	out.ShortFile = s.shortFileOf(m.FilePath)
	if isAdminRequest(r) { // only administrators see where the files lie
		out.FolderPath = s.movieFolderFor(m)
	}
	if t, err := s.MovieRepo.TitleTags(library.TagMovie, m.ID); err == nil {
		out.Tags = t
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeleteMovie removes a movie from the library. Its downloads are
// always cancelled and their files in the downloads folder deleted. With
// ?deleteFiles=true its folder in the movies library goes too (or, for a
// file loose in the library folder, the file and the subtitles, .nfo and
// artwork named after it); otherwise the library is left untouched.
func (s *Server) handleDeleteMovie(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	if _, err := s.removeMovie(id, r.URL.Query().Get("deleteFiles") == "true"); err != nil {
		writeRemoveError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// removeMovie takes one movie out of the library: its downloads are
// cancelled, its files are deleted only when deleteFiles is set, and the
// movie goes. It returns the movie's title. The error is a *removeProblem
// when the person can be told what went wrong.
func (s *Server) removeMovie(id int64, deleteFiles bool) (string, error) {
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", &removeProblem{http.StatusNotFound, "That movie isn't in your library."}
	}
	if err != nil {
		return "", &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	// A main movie's second copies go first, the same way.
	if m.CopyOf == 0 {
		copies, err := s.MovieRepo.ListCopies(m.ID)
		if err != nil {
			return m.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
		}
		for _, c := range copies {
			if _, err := s.removeMovie(c.ID, deleteFiles); err != nil {
				return m.Title, err
			}
		}
	}
	wantFiles := deleteFiles
	deleteFiles = deleteFiles && m.FilePath != ""
	tracked := 0
	if m.FilePath != "" {
		tracked = 1
	}
	var deleted removedFiles
	if deleteFiles {
		if err := s.checkFilesRemovable(s.movieRootFor(m), []string{m.FilePath}); err != nil {
			return m.Title, &removeProblem{http.StatusConflict, err.Error()}
		}
	}
	if _, err := s.removeDownloadsFor(m.ID, 0, m.Title); err != nil {
		return m.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	// A download that was importing when it was cancelled finishes its import
	// first, so the movie may have a file now that it did not have a moment
	// ago. With "delete files" asked for, that file goes too.
	if now, err := s.MovieRepo.Get(id); err == nil && now.FilePath != m.FilePath {
		m = now
		if wantFiles && m.FilePath != "" {
			deleteFiles, tracked = true, 1
			if err := s.checkFilesRemovable(s.movieRootFor(m), []string{m.FilePath}); err != nil {
				return m.Title, &removeProblem{http.StatusConflict, err.Error()}
			}
		}
	}
	if deleteFiles {
		var err error
		if deleted, err = s.removeMovieFiles(m); err != nil {
			return m.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
		}
	}
	if err := s.MovieRepo.Delete(id); err != nil {
		return m.Title, &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	s.forgetHeldMovie(id)
	_ = s.QueueRepo.LogActivity(0, "removed", removedLogLine(m.Title, deleteFiles, deleted, tracked))
	return m.Title, nil
}

// addMovieRequest is what the "Add to library" dialog sends. Everything but
// the TMDB id is optional: a bare {"tmdbId": n} adds a monitored movie on the
// default profile and default downloaders, without searching, and that is not
// searched again for better versions once it is downloaded.
type addMovieRequest struct {
	TMDBID    int    `json:"tmdbId"`
	ProfileID int64  `json:"profileId"`
	Monitored *bool  `json:"monitored"`
	Sources   string `json:"sources"`   // "", usenet, torrent or both
	SearchNow bool   `json:"searchNow"` // look for a release straight away
	// NoUpgrade leaves the movie alone once it is downloaded, so it is not
	// swapped for a better version later. On when left out: better versions
	// are something the person asks for.
	NoUpgrade *bool `json:"noUpgrade"`
	// RootPath is the movie folder to keep it in; left out = where new movies
	// go (Settings > Library > Folders).
	RootPath string `json:"rootPath"`
}

// handleAddMovie adds a movie to the library as missing, from a search result
// or a Discover pick, using the choices made in the add dialog: quality profile, whether to monitor it,
// which downloaders it may use, and whether to start searching now.
func (s *Server) handleAddMovie(w http.ResponseWriter, r *http.Request) {
	if !s.requireModule(w, moduleMovies) {
		return
	}
	if s.mustRequest(r) {
		s.fileRequest(w, r, "movie")
		return
	}
	var req addMovieRequest
	if err := decodeJSON(r, &req); err != nil || req.TMDBID <= 0 {
		writeError(w, http.StatusBadRequest, "Pick a movie from the search results to add.")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie and show info).")
		return
	}
	if !validSourcePref(req.Sources) {
		writeError(w, http.StatusBadRequest, `Choose where to download from: "usenet", "torrent" or "both".`)
		return
	}
	if err := s.checkProfileChoice(req.ProfileID); err != nil {
		writeProfileError(w, err)
		return
	}
	if existing, ok, err := s.MovieRepo.GetByTMDBID(req.TMDBID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		writeError(w, http.StatusConflict, fmt.Sprintf("%s is already in your library", existing.Title))
		return
	}
	tmdbMovie, err := s.TMDB().GetMovie(r.Context(), req.TMDBID)
	if err != nil {
		writeUpstreamError(w, "look up that movie", err)
		return
	}
	monitored := req.Monitored == nil || *req.Monitored
	rootPath, problem := s.folderForNewMovie(req.RootPath)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	userID, byline := requester(r)
	created, err := s.MovieRepo.Add(library.Movie{
		TMDBID: tmdbMovie.TMDBID, Title: tmdbMovie.Title, Year: tmdbMovie.Year(),
		Overview: tmdbMovie.Overview, PosterPath: tmdbMovie.PosterPath, Monitored: monitored, ReleaseDate: tmdbMovie.ReleaseDate,
		NoUpgrade: boolOr(req.NoUpgrade, true), AddedBy: userID, Genres: s.TMDB().MovieGenres(r.Context(), *tmdbMovie),
		RootPath: rootPath,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.noteMovieCollection(created.ID, tmdbMovie)
	if req.ProfileID != 0 {
		if err := s.MovieRepo.SetProfile(created.ID, req.ProfileID); err == nil {
			created.ProfileID = req.ProfileID
		}
	}
	if req.Sources != "" {
		if err := s.MovieRepo.SetSourcePref(created.ID, req.Sources); err == nil {
			created.SourcePref = req.Sources
		}
	}
	_ = s.QueueRepo.LogActivity(created.ID, "added", created.Title+" added to library"+byline)
	if req.SearchNow {
		go s.searchMovieInBackground(created.ID)
	}
	added := toMoviePayload(created, s.accountNames())
	added.RootPath = folderShown(r, s.movieFolders(), created.RootPath)
	writeJSON(w, http.StatusCreated, added)
}

// searchMovieInBackground runs the automatic search for a just-added movie,
// ignoring the monitored flag (the person asked for it explicitly).
func (s *Server) searchMovieInBackground(movieID int64) {
	m, err := s.MovieRepo.Get(movieID)
	if err != nil {
		return
	}
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return
	}
	if note, hold := s.homeReleaseHold(context.Background(), m, profiles.resolve(m.ProfileID)); hold {
		s.noteHomeHold(m, note) // still only in cinemas: the "Search now" button looks anyway
		return
	}
	s.huntMovie(context.Background(), m, instances, profiles, s.searchSkipKeys(), true)
}

// discoverPayload mirrors moviePayload's shape but for titles not yet in
// the library (Discover: TMDB trending/popular).
type discoverPayload struct {
	TMDBID    int    `json:"tmdbId"`
	MediaType string `json:"mediaType"` // "movie" | "tv"
	Title     string `json:"title"`
	Year      int    `json:"year"`
	// ReleaseDate is the movie's release date or the show's first air date,
	// "YYYY-MM-DD"; left out when TMDB does not know it.
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Overview    string   `json:"overview,omitempty"`
	PosterURL   string   `json:"posterUrl,omitempty"`
	Genres      []string `json:"genres"`
	Rating      float64  `json:"rating"` // TMDB vote average, one decimal
	VoteCount   int      `json:"voteCount"`
	// NameTag tells a search result from another of the same name in the
	// results ("US", "UK" or the year); left out when no other has its name.
	NameTag string `json:"nameTag,omitempty"`
}

// isoDate passes a TMDB "YYYY-MM-DD" date through, or "" for anything else
// (TMDB sends "" for unknown dates).
func isoDate(d string) string {
	if _, err := time.Parse("2006-01-02", d); err != nil {
		return ""
	}
	return d
}

// movieDiscover builds the list item for a TMDB movie, resolving its genre
// names (a cached lookup, see metadata.Client.GenreNames).
func (s *Server) movieDiscover(ctx context.Context, m metadata.Movie) discoverPayload {
	return discoverPayload{
		TMDBID: m.TMDBID, MediaType: "movie", Title: m.Title, Year: m.Year(), ReleaseDate: isoDate(m.ReleaseDate),
		Overview: m.Overview, PosterURL: metadata.PosterURL(m.PosterPath), Genres: s.displayTMDB().MovieGenres(ctx, m),
		Rating: metadata.RoundRating(m.VoteAverage), VoteCount: m.VoteCount,
	}
}

// showDiscover is movieDiscover's TV counterpart.
func (s *Server) showDiscover(ctx context.Context, sh metadata.Show) discoverPayload {
	return discoverPayload{
		TMDBID: sh.TMDBID, MediaType: "tv", Title: sh.Name, Year: sh.Year(), ReleaseDate: isoDate(sh.FirstAirDate),
		Overview: sh.Overview, PosterURL: metadata.PosterURL(sh.PosterPath), Genres: s.displayTMDB().ShowGenres(ctx, sh),
		Rating: metadata.RoundRating(sh.VoteAverage), VoteCount: sh.VoteCount,
	}
}

func (s *Server) handleTrending(w http.ResponseWriter, r *http.Request) {
	s.discoverList(w, r, s.displayTMDB().TrendingMovies)
}

func (s *Server) handlePopular(w http.ResponseWriter, r *http.Request) {
	s.discoverList(w, r, s.displayTMDB().PopularMovies)
}

// handleSimilarMovies lists movies like one in the library, for the "because
// you added ..." rail.
func (s *Server) handleSimilarMovies(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	movie, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, []discoverPayload{})
		return
	}
	similar, err := s.displayTMDB().SimilarMovies(r.Context(), movie.TMDBID)
	if err != nil {
		writeUpstreamError(w, "load similar movies", err)
		return
	}
	out := make([]discoverPayload, len(similar))
	for i, m := range similar {
		out[i] = s.movieDiscover(r.Context(), m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) discoverList(w http.ResponseWriter, r *http.Request, fetch func(context.Context) ([]metadata.Movie, error)) {
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, []discoverPayload{})
		return
	}
	movies, err := fetch(r.Context())
	if err != nil {
		writeUpstreamError(w, "load the titles", err)
		return
	}
	out := make([]discoverPayload, len(movies))
	for i, m := range movies {
		out[i] = s.movieDiscover(r.Context(), m)
	}
	writeJSON(w, http.StatusOK, out)
}

// maxImportListItems caps how many TMDB lookups a single list import does
// — a public list can run into the hundreds of items (e.g. "IMDb Top
// 250"), and enriching each one is a separate TMDB call; this keeps one
// import request from turning into hundreds of outbound requests.
const maxImportListItems = 50

// handleImportList turns a public Trakt list address into full TMDB movie
// details for the Discover page. Each Trakt item only carries a tmdb id, so every one is
// enriched via a real TMDB lookup (poster, overview, etc.) — items TMDB
// can't resolve are skipped rather than failing the whole import.
func (s *Server) handleImportList(w http.ResponseWriter, r *http.Request) {
	listURL := r.URL.Query().Get("url")
	if listURL == "" {
		writeError(w, http.StatusBadRequest, "url query parameter is required")
		return
	}
	if !s.Trakt().HasClientID() {
		writeError(w, http.StatusPreconditionFailed, "Add your Trakt client ID first (Settings > Info, lists and subtitles > Import lists).")
		return
	}
	user, listID, err := trakt.ParseListURL(listURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := s.Trakt().ListMovies(r.Context(), user, listID)
	if err != nil {
		writeUpstreamError(w, "load that list", err)
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, []discoverPayload{})
		return
	}
	if len(items) > maxImportListItems {
		items = items[:maxImportListItems]
	}
	out := make([]discoverPayload, 0, len(items))
	for _, item := range items {
		m, err := s.TMDB().GetMovie(r.Context(), item.TMDBID)
		if err != nil {
			continue
		}
		out = append(out, s.movieDiscover(r.Context(), *m))
	}
	writeJSON(w, http.StatusOK, out)
}
