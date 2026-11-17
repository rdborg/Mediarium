package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/metadata"
)

// discoverKind reads ?kind=movie|tv (default movie).
func discoverKind(r *http.Request) (string, bool) {
	switch k := r.URL.Query().Get("kind"); k {
	case "", "movie":
		return "movie", true
	case "tv":
		return "tv", true
	}
	return "", false
}

// handleDiscoverGenres lists TMDB's genres for movies or shows, for the
// Discover genre filter. Empty without a TMDB key, like the other Discover
// lists.
func (s *Server) handleDiscoverGenres(w http.ResponseWriter, r *http.Request) {
	kind, ok := discoverKind(r)
	if !ok {
		writeError(w, http.StatusBadRequest, `kind must be "movie" or "tv"`)
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, []metadata.Genre{})
		return
	}
	genres, err := s.displayTMDB().Genres(r.Context(), kind)
	if err != nil {
		writeUpstreamError(w, "load the genres", err)
		return
	}
	writeJSON(w, http.StatusOK, genres)
}

// browseItem is a Discover list item plus whether it is already in the
// library (as the title search reports it).
type browseItem struct {
	discoverPayload
	InLibrary bool   `json:"inLibrary"`
	LibraryID int64  `json:"libraryId,omitempty"`
	Status    string `json:"status,omitempty"` // movie status, or "downloaded/total" episodes for a show
}

type browsePayload struct {
	Page         int          `json:"page"`
	TotalPages   int          `json:"totalPages"`
	TotalResults int          `json:"totalResults"`
	Results      []browseItem `json:"results"`
}

// moviePagePayload turns a TMDB movie page into Discover items, marking the
// titles already in the library.
func (s *Server) moviePagePayload(ctx context.Context, page *metadata.MoviePage) browsePayload {
	inLib := map[int]browseItem{}
	if o, err := s.ownedLibrary(ctx); err == nil {
		for _, m := range o.movies {
			inLib[m.TMDBID] = browseItem{InLibrary: true, LibraryID: m.ID, Status: string(m.Status)}
		}
	}
	out := browsePayload{Page: page.Page, TotalPages: page.TotalPages, TotalResults: page.TotalResults,
		Results: make([]browseItem, 0, len(page.Results))}
	for _, m := range page.Results {
		it := inLib[m.TMDBID]
		it.discoverPayload = s.movieDiscover(ctx, m)
		out.Results = append(out.Results, it)
	}
	return out
}

// showPagePayload is moviePagePayload for shows; a show's status is its
// "downloaded/total" episode count.
func (s *Server) showPagePayload(ctx context.Context, page *metadata.ShowPage) browsePayload {
	inLib := map[int]browseItem{}
	if o, err := s.ownedLibrary(ctx); err == nil {
		for _, sr := range o.shows {
			inLib[sr.TMDBID] = browseItem{InLibrary: true, LibraryID: sr.ID,
				Status: strconv.Itoa(sr.DownloadedCount) + "/" + strconv.Itoa(sr.EpisodeCount)}
		}
	}
	out := browsePayload{Page: page.Page, TotalPages: page.TotalPages, TotalResults: page.TotalResults,
		Results: make([]browseItem, 0, len(page.Results))}
	for _, sh := range page.Results {
		it := inLib[sh.TMDBID]
		it.discoverPayload = s.showDiscover(ctx, sh)
		out.Results = append(out.Results, it)
	}
	return out
}

// handleDiscoverList pages through one of Discover's rails: trending,
// popular or upcoming (coming soon) movies or shows, or the titles like the
// ones in the library (similar), 20 per page, up to page 500.
func (s *Server) handleDiscoverList(w http.ResponseWriter, r *http.Request) {
	kind, ok := discoverKind(r)
	if !ok {
		writeError(w, http.StatusBadRequest, `kind must be "movie" or "tv"`)
		return
	}
	list := r.URL.Query().Get("list")
	if list != listSimilar && !metadata.ValidList(list) {
		writeError(w, http.StatusBadRequest, `list must be "trending", "popular", "upcoming" or "similar"`)
		return
	}
	page, ok := queryInt(r, "page")
	if !ok {
		writeError(w, http.StatusBadRequest, "page must be a whole number")
		return
	}
	page = metadata.ClampPage(page)
	hide, ok := s.hidingFromRequest(w, r)
	if !ok {
		return
	}
	if list == listSimilar {
		s.handleDiscoverSimilar(w, r, kind, page)
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, browsePayload{Page: page, Results: []browseItem{}})
		return
	}
	ctx := r.Context()
	if kind == "movie" {
		p, err := s.displayTMDB().MovieListHiding(ctx, list, page, hide)
		if err != nil {
			writeUpstreamError(w, "load the "+list+" movies", err)
			return
		}
		writeJSON(w, http.StatusOK, s.moviePagePayload(ctx, p))
		return
	}
	p, err := s.displayTMDB().TVListHiding(ctx, list, page, hide)
	if err != nil {
		writeUpstreamError(w, "load the "+list+" shows", err)
		return
	}
	writeJSON(w, http.StatusOK, s.showPagePayload(ctx, p))
}

// hidingFromRequest reads the optional hide parameter of a Discover request:
// streaming service keys separated by commas, whose titles are left out. It
// answers a 400 itself and returns false for a key it does not know.
func (s *Server) hidingFromRequest(w http.ResponseWriter, r *http.Request) (metadata.Hiding, bool) {
	keys, ok := metadata.ParseServices(r.URL.Query().Get("hide"))
	if !ok {
		var names []string
		for _, svc := range metadata.StreamingServices() {
			names = append(names, svc.Key)
		}
		writeError(w, http.StatusBadRequest, "hide must be streaming service names separated by commas: "+strings.Join(names, ", "))
		return metadata.Hiding{}, false
	}
	if len(keys) == 0 {
		return metadata.Hiding{}, true
	}
	return metadata.Hiding{Services: keys, Region: s.releaseRegion()}, true
}

// queryInt reads an optional non-negative integer parameter.
func queryInt(r *http.Request, name string) (int, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	return n, err == nil && n >= 0
}

// handleDiscoverBrowse pages through TMDB's catalogue of movies or shows by
// genre, year (exact, or a from/to range), original language, lowest rating,
// runtime and sort order: popular, rating, newest or oldest.
func (s *Server) handleDiscoverBrowse(w http.ResponseWriter, r *http.Request) {
	kind, ok := discoverKind(r)
	if !ok {
		writeError(w, http.StatusBadRequest, `kind must be "movie" or "tv"`)
		return
	}
	var q metadata.DiscoverQuery
	for _, p := range []struct {
		name string
		dst  *int
	}{{"genre", &q.Genre}, {"year", &q.Year}, {"yearFrom", &q.YearFrom}, {"yearTo", &q.YearTo}, {"minRating", &q.MinRating}, {"page", &q.Page}} {
		n, ok := queryInt(r, p.name)
		if !ok {
			writeError(w, http.StatusBadRequest, p.name+" must be a whole number")
			return
		}
		*p.dst = n
	}
	q.Sort = r.URL.Query().Get("sort")
	if !metadata.ValidSort(q.Sort) {
		writeError(w, http.StatusBadRequest, `sort must be "popular", "rating", "newest" or "oldest"`)
		return
	}
	q.Language = r.URL.Query().Get("language")
	if !metadata.ValidLanguage(q.Language) {
		writeError(w, http.StatusBadRequest, "language must be a two or three letter code in lower case, like ko")
		return
	}
	if !metadata.ValidMinRating(q.MinRating) {
		writeError(w, http.StatusBadRequest, "minRating must be 6, 7, 8 or 9")
		return
	}
	q.Runtime = r.URL.Query().Get("runtime")
	if !metadata.ValidRuntime(q.Runtime) {
		writeError(w, http.StatusBadRequest, `runtime must be "under90", "90to120" or "over120"`)
		return
	}
	hide, ok := s.hidingFromRequest(w, r)
	if !ok {
		return
	}
	q.Hide = hide
	if q.YearFrom > 0 && q.YearTo > 0 && q.YearFrom > q.YearTo {
		writeError(w, http.StatusBadRequest, "yearFrom must not be after yearTo")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, browsePayload{Page: 1, Results: []browseItem{}})
		return
	}

	ctx := r.Context()
	if kind == "movie" {
		page, err := s.displayTMDB().DiscoverMovies(ctx, q)
		if err != nil {
			writeUpstreamError(w, "browse titles", err)
			return
		}
		writeJSON(w, http.StatusOK, s.moviePagePayload(ctx, page))
		return
	}
	page, err := s.displayTMDB().DiscoverTV(ctx, q)
	if err != nil {
		writeUpstreamError(w, "browse titles", err)
		return
	}
	writeJSON(w, http.StatusOK, s.showPagePayload(ctx, page))
}

// discoverServicesPayload is a person's streaming services for Discover.
// Saved is false until they have picked once (so the app may move over what
// an older version kept in the browser); Account is false when nobody is
// signed in (sign-in switched off), where nothing can be kept on the server.
type discoverServicesPayload struct {
	Services []string `json:"services"`
	On       bool     `json:"on"`
	Saved    bool     `json:"saved"`
	Account  bool     `json:"account"`
}

// handleGetDiscoverServices answers GET /api/auth/discover-services.
func (s *Server) handleGetDiscoverServices(w http.ResponseWriter, r *http.Request) {
	uid, _ := requester(r)
	out := discoverServicesPayload{Services: []string{}}
	if uid == 0 {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Account = true
	var csv sql.NullString
	var on int
	err := s.db.QueryRow(`SELECT discover_services, discover_hide FROM users WHERE id = ?`, uid).Scan(&csv, &on)
	if err != nil && err != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if csv.Valid {
		out.Saved = true
		if keys, ok := metadata.ParseServices(csv.String); ok && keys != nil {
			out.Services = keys
		}
		out.On = on != 0 && len(out.Services) > 0
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePutDiscoverServices answers PUT /api/auth/discover-services
// {"services": ["netflix"], "on": true}. The switch cannot be on with no
// service picked.
func (s *Server) handlePutDiscoverServices(w http.ResponseWriter, r *http.Request) {
	uid, _ := requester(r)
	if uid == 0 {
		writeError(w, http.StatusBadRequest, "Sign in to keep your streaming services with your account.")
		return
	}
	var body struct {
		Services []string `json:"services"`
		On       bool     `json:"on"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	keys, ok := metadata.ParseServices(strings.Join(body.Services, ","))
	if !ok {
		names := make([]string, 0, 8)
		for _, svc := range metadata.StreamingServices() {
			names = append(names, svc.Key)
		}
		writeError(w, http.StatusBadRequest, "services must be streaming service names: "+strings.Join(names, ", "))
		return
	}
	on := body.On && len(keys) > 0
	if _, err := s.db.Exec(`UPDATE users SET discover_services = ?, discover_hide = ? WHERE id = ?`, strings.Join(keys, ","), b2i(on), uid); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.handleGetDiscoverServices(w, r)
}
