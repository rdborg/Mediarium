package api

import (
	"context"
	"net/http"
	"strconv"

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
	genres, err := s.TMDB().Genres(r.Context(), kind)
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
	if list, err := s.MovieRepo.List(); err == nil {
		for _, m := range list {
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
	if list, err := s.MovieRepo.ListSeries(); err == nil {
		for _, sr := range list {
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
		p, err := s.TMDB().MovieList(ctx, list, page)
		if err != nil {
			writeUpstreamError(w, "load the "+list+" movies", err)
			return
		}
		writeJSON(w, http.StatusOK, s.moviePagePayload(ctx, p))
		return
	}
	p, err := s.TMDB().TVList(ctx, list, page)
	if err != nil {
		writeUpstreamError(w, "load the "+list+" shows", err)
		return
	}
	writeJSON(w, http.StatusOK, s.showPagePayload(ctx, p))
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
// genre, year (exact, or a from/to range) and sort order: popular, rating,
// newest or oldest.
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
	}{{"genre", &q.Genre}, {"year", &q.Year}, {"yearFrom", &q.YearFrom}, {"yearTo", &q.YearTo}, {"page", &q.Page}} {
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
		page, err := s.TMDB().DiscoverMovies(ctx, q)
		if err != nil {
			writeUpstreamError(w, "browse titles", err)
			return
		}
		writeJSON(w, http.StatusOK, s.moviePagePayload(ctx, page))
		return
	}
	page, err := s.TMDB().DiscoverTV(ctx, q)
	if err != nil {
		writeUpstreamError(w, "browse titles", err)
		return
	}
	writeJSON(w, http.StatusOK, s.showPagePayload(ctx, page))
}
