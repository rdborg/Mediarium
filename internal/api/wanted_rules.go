package api

import (
	"fmt"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// The rules for "wanted, and missing" live here so the Wanted page and the
// Dashboard number can never disagree: a title only counts when it is
// monitored, has no file, and (for an episode) has already aired.

// movieWantedMissing reports whether a movie belongs in Wanted > Missing.
func movieWantedMissing(m library.Movie) bool {
	return m.Monitored && m.Status == library.StatusMissing && !fileExists(m.FilePath)
}

// episodeWantedMissing reports whether an episode belongs in Wanted >
// Missing. today is a "YYYY-MM-DD" date.
func episodeWantedMissing(sr library.Series, ep library.Episode, today string) bool {
	if !sr.Monitored || !ep.Monitored || ep.Status != library.StatusMissing {
		return false
	}
	aired := ep.AirDate != "" && ep.AirDate <= today
	return aired && !fileExists(ep.FilePath)
}

// wantedMissingCounts counts what Wanted > Missing would list.
func (s *Server) wantedMissingCounts() (movies, episodes int, err error) {
	all, err := s.MovieRepo.List()
	if err != nil {
		return 0, 0, fmt.Errorf("list movies: %w", err)
	}
	for _, m := range all {
		if movieWantedMissing(m) {
			movies++
		}
	}
	seriesList, err := s.MovieRepo.ListSeries()
	if err != nil {
		return 0, 0, fmt.Errorf("list series: %w", err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, sr := range seriesList {
		if !sr.Monitored {
			continue
		}
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			return 0, 0, fmt.Errorf("list episodes of %d: %w", sr.ID, err)
		}
		for _, ep := range eps {
			if episodeWantedMissing(sr, ep, today) {
				episodes++
			}
		}
	}
	return movies, episodes, nil
}
