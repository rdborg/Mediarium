package api

import (
	"context"
	"log"
	"strings"
	"time"
)

const (
	// genreBackfillInterval is how often titles added without genres (before
	// genres were stored, or from a search grab) are looked up on TMDB.
	genreBackfillInterval = time.Hour
	// genreBackfillBatch caps the TMDB lookups one run makes, and
	// genreBackfillPause spaces them out, so a big existing library is filled
	// in gradually instead of in one burst against the shared TMDB key.
	genreBackfillBatch = 40
	genreBackfillPause = 250 * time.Millisecond
)

// backfillGenres fetches genres for library titles that do not have them
// yet. A lookup that fails for a passing reason ends the run (the next run
// tries again); a title TMDB no longer knows gets an empty list so it is not
// asked for again.
func (s *Server) backfillGenres(ctx context.Context) {
	if !s.TMDB().HasAPIKey() {
		return
	}
	gaps, err := s.MovieRepo.MissingGenres(genreBackfillBatch)
	if err != nil {
		log.Printf("automation: genre-backfill: %v", err)
		return
	}
	for i, g := range gaps {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(genreBackfillPause):
			}
		}
		var (
			genres []string
			err    error
		)
		if g.Kind == "movie" {
			m, gerr := s.TMDB().GetMovie(ctx, g.TMDBID)
			if err = gerr; err == nil {
				genres = s.TMDB().MovieGenres(ctx, *m)
			}
		} else {
			sh, gerr := s.TMDB().GetShow(ctx, g.TMDBID)
			if err = gerr; err == nil {
				genres = s.TMDB().ShowGenres(ctx, sh.Show)
			}
		}
		if err != nil {
			if !strings.Contains(err.Error(), "status 404") {
				log.Printf("automation: genre-backfill: %q: %v", g.Title, err)
				return
			}
			genres = []string{}
		}
		if g.Kind == "movie" {
			err = s.MovieRepo.SetMovieGenres(g.ID, genres)
		} else {
			err = s.MovieRepo.SetSeriesGenres(g.ID, genres)
		}
		if err != nil {
			log.Printf("automation: genre-backfill: %v", err)
			return
		}
	}
}
