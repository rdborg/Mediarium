package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// overviewTTL is the longest the library's totals are kept. They are worked
// out again sooner when a title changes (see sharedCache).
const overviewTTL = 30 * time.Second

// libraryOverview is the library's totals: what the Dashboard, the metrics
// page and the weekly summary count.
type libraryOverview struct {
	Movies         library.MovieCounts
	Series         library.SeriesCounts
	WantedMovies   int // what Wanted > Missing lists (see wantedMissingCounts)
	WantedEpisodes int
}

// libraryOverview returns the library's totals. On a big library they take a
// moment to count, so a few reloads of the Dashboard, or several tabs, share
// one count instead of piling up behind each other.
func (s *Server) libraryOverview(ctx context.Context) (libraryOverview, error) {
	return s.overviewCache.get(ctx, overviewTTL, func(ctx context.Context) (libraryOverview, error) {
		var o libraryOverview
		var err error
		if o.Movies, err = s.MovieRepo.MovieCounts(ctx); err != nil {
			return o, err
		}
		if o.Series, err = s.MovieRepo.SeriesCounts(ctx); err != nil {
			return o, err
		}
		if o.WantedMovies, o.WantedEpisodes, err = s.wantedMissingCounts(ctx); err != nil {
			return o, err
		}
		return o, nil
	})
}

// seriesListData is the part of the TV list that is costly on a big library:
// the shows with their episode totals, and the size of each show's files.
type seriesListData struct {
	list  []library.Series
	sizes map[int64]int64
}

// seriesList returns the shows for the TV list, kept like libraryOverview.
func (s *Server) seriesList(ctx context.Context) (seriesListData, error) {
	return s.seriesListCache.get(ctx, overviewTTL, func(ctx context.Context) (seriesListData, error) {
		list, err := s.MovieRepo.ListSeries()
		if err != nil {
			return seriesListData{}, err
		}
		if err := ctx.Err(); err != nil {
			return seriesListData{}, err
		}
		files, err := s.MovieRepo.EpisodeFilesBySeries()
		if err != nil {
			return seriesListData{}, err
		}
		sizes := make(map[int64]int64, len(files))
		now := time.Now()
		for id, paths := range files {
			for _, f := range paths {
				sizes[id] += cachedFileSize(f, now)
			}
		}
		return seriesListData{list: list, sizes: sizes}, nil
	})
}

type libraryCountsPayload struct {
	Movies int `json:"movies"`
	Shows  int `json:"shows"`
	// Anime is how many of the shows are anime, with the anime library on;
	// Shows then counts the others.
	Anime int `json:"anime,omitempty"`
}

// handleLibraryCounts answers how many movies and shows the library has, for
// the Library's tabs, so a tab does not have to load the other tab's whole
// list just to show its number.
func (s *Server) handleLibraryCounts(w http.ResponseWriter, r *http.Request) {
	o, err := s.libraryOverview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("count the library: %v", err))
		return
	}
	out := libraryCountsPayload{Movies: o.Movies.Total, Shows: o.Series.Series}
	if s.animeEnabled() {
		out.Shows, out.Anime = o.Series.Series-o.Series.Anime, o.Series.Anime
	}
	writeJSON(w, http.StatusOK, out)
}
