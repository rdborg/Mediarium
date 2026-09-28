package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"

	"github.com/ryanborg/mediarium/internal/blocklist"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/metadata"
	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/trakt"
)

// movieCategory is the Newznab category for movies (PRD §6 — search
// results should be scoped to what the app actually manages).
var movieCategory = []int{2000}

type searchResultPayload struct {
	Title       string `json:"title"`
	IndexerName string `json:"indexerName"`
	Protocol    string `json:"protocol"` // "usenet" or "torrent" (PRD §6 — protocol icon per result)
	DownloadURL string `json:"downloadUrl"`
	SizeBytes   int64  `json:"sizeBytes"`
	PublishDate string `json:"publishDate,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	Source      string `json:"source,omitempty"`
	Codec       string `json:"codec,omitempty"`
	Group       string `json:"group,omitempty"`
	Seeders     int    `json:"seeders,omitempty"`
	Peers       int    `json:"peers,omitempty"`
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
}

func toSearchResultPayload(res indexers.Result) searchResultPayload {
	release := parser.Parse(res.Title)
	payload := searchResultPayload{
		Title:       res.Title,
		IndexerName: res.IndexerName,
		Protocol:    string(res.Protocol),
		DownloadURL: res.DownloadURL,
		SizeBytes:   res.SizeBytes,
		Resolution:  release.Resolution,
		Source:      release.Source,
		Codec:       release.Codec,
		Group:       release.Group,
		Seeders:     res.Seeders,
		Peers:       res.Peers,
		Season:      release.Season,
		Episodes:    release.Episodes,
	}
	if !res.PublishDate.IsZero() {
		payload.PublishDate = res.PublishDate.Format("2006-01-02T15:04:05Z07:00")
	}
	return payload
}

// handleSearch is the unified search endpoint (PRD §6 — "one unified
// result list ... clearly tagged by source").
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

	outcomes := indexers.SearchAll(r.Context(), list, query, mediaCategories)
	merged := indexers.MergeResults(outcomes)

	blocked := s.blockedKeys()
	out := make([]searchResultPayload, len(merged))
	for i, res := range merged {
		out[i] = toSearchResultPayload(res)
		out[i].Blocklisted = blocked[blocklist.Key(res.Title)]
	}
	writeJSON(w, http.StatusOK, out)
}

type moviePayload struct {
	ID        int64  `json:"id"`
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Overview  string `json:"overview,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
	Status    string `json:"status"`
	Quality   string `json:"quality,omitempty"`
	FilePath  string `json:"filePath,omitempty"`
	ProfileID int64  `json:"profileId"`
	Monitored bool   `json:"monitored"`
	Sources   string `json:"sources"`
}

func toMoviePayload(m library.Movie) moviePayload {
	return moviePayload{
		ID: m.ID, TMDBID: m.TMDBID, Title: m.Title, Year: m.Year, Overview: m.Overview,
		PosterURL: metadata.PosterURL(m.PosterPath), Status: string(m.Status), Quality: m.Quality, FilePath: m.FilePath, ProfileID: m.ProfileID, Monitored: m.Monitored, Sources: m.SourcePref,
	}
}

func (s *Server) handleListMovies(w http.ResponseWriter, r *http.Request) {
	list, err := s.MovieRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]moviePayload, len(list))
	for i, m := range list {
		out[i] = toMoviePayload(m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetMovie(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, toMoviePayload(m))
}

// handleDeleteMovie removes a movie from the library. With ?deleteFiles=true
// its file on disk is deleted too; otherwise the file is left untouched.
func (s *Server) handleDeleteMovie(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusConflict, "this movie is downloading right now — wait for it to finish or fail first")
		return
	}
	if r.URL.Query().Get("deleteFiles") == "true" && m.FilePath != "" {
		if err := removeFiles([]string{m.FilePath}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.MovieRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(0, "removed", m.Title+" removed from library")
	writeJSON(w, http.StatusOK, nil)
}

// removeFiles deletes each path, treating an already-missing file as done.
func removeFiles(paths []string) error {
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", p, err)
		}
	}
	return nil
}

// addMovieRequest is what the "Add to library" dialog sends. Everything but
// the TMDB id is optional: a bare {"tmdbId": n} adds a monitored movie on the
// default profile and default downloaders, without searching.
type addMovieRequest struct {
	TMDBID    int    `json:"tmdbId"`
	ProfileID int64  `json:"profileId"`
	Monitored *bool  `json:"monitored"`
	Sources   string `json:"sources"`   // "", usenet, torrent or both
	SearchNow bool   `json:"searchNow"` // look for a release straight away
}

// handleAddMovie adds a title to the library as "missing" from either a
// TMDB search result or a Discover pick (PRD §5.2 step 6 / §7), applying the
// choices made in the add dialog: quality profile, whether to monitor it,
// which downloaders it may use, and whether to start searching now.
func (s *Server) handleAddMovie(w http.ResponseWriter, r *http.Request) {
	var req addMovieRequest
	if err := decodeJSON(r, &req); err != nil || req.TMDBID == 0 {
		writeError(w, http.StatusBadRequest, "tmdbId is required")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "TMDB API key not configured yet — set it in Settings")
		return
	}
	if !validSourcePref(req.Sources) {
		writeError(w, http.StatusBadRequest, `sources must be "usenet", "torrent" or "both"`)
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
		writeError(w, http.StatusConflict, existing.Title+" is already in your library")
		return
	}
	tmdbMovie, err := s.TMDB().GetMovie(r.Context(), req.TMDBID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "look up movie on TMDB: "+err.Error())
		return
	}
	monitored := req.Monitored == nil || *req.Monitored
	created, err := s.MovieRepo.Add(library.Movie{
		TMDBID: tmdbMovie.TMDBID, Title: tmdbMovie.Title, Year: tmdbMovie.Year(),
		Overview: tmdbMovie.Overview, PosterPath: tmdbMovie.PosterPath, Monitored: monitored, ReleaseDate: tmdbMovie.ReleaseDate,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	_ = s.QueueRepo.LogActivity(created.ID, "added", created.Title+" added to library")
	if req.SearchNow {
		go s.searchMovieInBackground(created.ID)
	}
	writeJSON(w, http.StatusCreated, toMoviePayload(created))
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
	s.huntMovie(context.Background(), m, instances, profiles, s.blockedKeys(), true)
}

// discoverPayload mirrors moviePayload's shape but for titles not yet in
// the library (PRD §7 — Discover: TMDB trending/popular).
type discoverPayload struct {
	TMDBID    int      `json:"tmdbId"`
	MediaType string   `json:"mediaType"` // "movie" | "tv"
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Overview  string   `json:"overview,omitempty"`
	PosterURL string   `json:"posterUrl,omitempty"`
	Genres    []string `json:"genres"`
	Rating    float64  `json:"rating"` // TMDB vote average, one decimal
	VoteCount int      `json:"voteCount"`
}

// movieDiscover builds the list item for a TMDB movie, resolving its genre
// names (a cached lookup, see metadata.Client.GenreNames).
func (s *Server) movieDiscover(ctx context.Context, m metadata.Movie) discoverPayload {
	return discoverPayload{
		TMDBID: m.TMDBID, MediaType: "movie", Title: m.Title, Year: m.Year(), Overview: m.Overview,
		PosterURL: metadata.PosterURL(m.PosterPath), Genres: s.TMDB().MovieGenres(ctx, m),
		Rating: metadata.RoundRating(m.VoteAverage), VoteCount: m.VoteCount,
	}
}

// showDiscover is movieDiscover's TV counterpart.
func (s *Server) showDiscover(ctx context.Context, sh metadata.Show) discoverPayload {
	return discoverPayload{
		TMDBID: sh.TMDBID, MediaType: "tv", Title: sh.Name, Year: sh.Year(), Overview: sh.Overview,
		PosterURL: metadata.PosterURL(sh.PosterPath), Genres: s.TMDB().ShowGenres(ctx, sh),
		Rating: metadata.RoundRating(sh.VoteAverage), VoteCount: sh.VoteCount,
	}
}

func (s *Server) handleTrending(w http.ResponseWriter, r *http.Request) {
	s.discoverList(w, r, s.TMDB().TrendingMovies)
}

func (s *Server) handlePopular(w http.ResponseWriter, r *http.Request) {
	s.discoverList(w, r, s.TMDB().PopularMovies)
}

// handleSimilarMovies powers Discover's "because you added X" rail (PRD
// §7 Phase 3).
func (s *Server) handleSimilarMovies(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	movie, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "movie not found")
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
	similar, err := s.TMDB().SimilarMovies(r.Context(), movie.TMDBID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "fetch similar movies from TMDB: "+err.Error())
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
		writeError(w, http.StatusBadGateway, "fetch from TMDB: "+err.Error())
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

// handleImportList resolves a public Trakt list URL into full TMDB movie
// details for the Discover page (PRD §7 Phase 3 — "curated/public list
// import"). Each Trakt item only carries a tmdb id, so every one is
// enriched via a real TMDB lookup (poster, overview, etc.) — items TMDB
// can't resolve are skipped rather than failing the whole import.
func (s *Server) handleImportList(w http.ResponseWriter, r *http.Request) {
	listURL := r.URL.Query().Get("url")
	if listURL == "" {
		writeError(w, http.StatusBadRequest, "url query parameter is required")
		return
	}
	if !s.Trakt().HasClientID() {
		writeError(w, http.StatusPreconditionFailed, "Trakt client ID not configured yet — set it in Settings")
		return
	}
	user, listID, err := trakt.ParseListURL(listURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := s.Trakt().ListMovies(r.Context(), user, listID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "fetch Trakt list: "+err.Error())
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

// forYouSeedCount is how many recently-added library movies are sampled as
// seeds for handleForYou — enough to get a varied aggregate without
// turning one page load into dozens of TMDB calls.
const forYouSeedCount = 5

// forYouMaxResults caps the aggregate recommendation list returned.
const forYouMaxResults = 20

// handleForYou powers Discover's "More like your library" rail (PRD §7
// Phase 3) — distinct from handleSimilarMovies, which is per-title
// ("because you added X") and only reachable from a single movie's detail
// page. This aggregates across several library titles at once: samples
// the most recently added movies as seeds, merges each one's TMDB
// "similar" results (excluding anything already in the library), and
// ranks by how many seeds recommended the same movie — an item multiple
// seeds agree on is a stronger signal than any single title's own list.
func (s *Server) handleForYou(w http.ResponseWriter, r *http.Request) {
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, []discoverPayload{})
		return
	}
	libraryMovies, err := s.MovieRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(libraryMovies) == 0 {
		writeJSON(w, http.StatusOK, []discoverPayload{})
		return
	}
	inLibrary := make(map[int]bool, len(libraryMovies))
	for _, m := range libraryMovies {
		inLibrary[m.TMDBID] = true
	}

	seeds := libraryMovies
	if len(seeds) > forYouSeedCount {
		seeds = seeds[:forYouSeedCount]
	}

	type scored struct {
		movie metadata.Movie
		votes int
	}
	byID := make(map[int]*scored)
	var order []int // first-seen order, kept stable when sorting by votes
	for _, seed := range seeds {
		similar, err := s.TMDB().SimilarMovies(r.Context(), seed.TMDBID)
		if err != nil {
			continue // one seed failing shouldn't sink the whole rail
		}
		for _, m := range similar {
			if inLibrary[m.TMDBID] {
				continue
			}
			if existing, ok := byID[m.TMDBID]; ok {
				existing.votes++
				continue
			}
			byID[m.TMDBID] = &scored{movie: m, votes: 1}
			order = append(order, m.TMDBID)
		}
	}

	sort.SliceStable(order, func(i, j int) bool {
		return byID[order[i]].votes > byID[order[j]].votes
	})
	if len(order) > forYouMaxResults {
		order = order[:forYouMaxResults]
	}
	out := make([]discoverPayload, len(order))
	for i, id := range order {
		m := byID[id].movie
		out[i] = s.movieDiscover(r.Context(), m)
	}
	writeJSON(w, http.StatusOK, out)
}
