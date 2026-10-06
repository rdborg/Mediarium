package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
)

// Anime and daily shows. Anime releases count episodes from the start of the
// show ("[Group] Show - 105"); daily shows (talk shows, news) are named by
// air date ("Show.2024.03.15"). A show's type says which, and releases are
// mapped to season and episode with its episode list, so searching, picking
// and importing work as for any show.

// guessSeriesType picks a type for a newly added show: Japanese animation is
// anime; talk shows and news are daily.
func guessSeriesType(show metadata.Show) string {
	ids := make([]int, 0, len(show.Genres))
	names := make([]string, 0, len(show.Genres))
	for _, g := range show.Genres {
		ids = append(ids, g.ID)
		names = append(names, g.Name)
	}
	return library.GuessSeriesType(ids, names, show.OriginCountry, show.Type)
}

// mapRelease gives an anime or daily release the season and episodes it is
// for, from the show's episode list. A release with a season marker, or of a
// standard show, comes back as it was.
func mapRelease(rel parser.Release, seriesType string, episodes []library.Episode) parser.Release {
	if rel.Season > 0 {
		return rel
	}
	switch seriesType {
	case library.SeriesAnime:
		if len(rel.Absolute) == 0 {
			return rel
		}
		order := library.AbsoluteOrder(episodes)
		var eps []int
		season := 0
		for _, n := range rel.Absolute {
			if n < 1 || n > len(order) {
				continue
			}
			e := order[n-1]
			if season == 0 {
				season = e.Season
			}
			if e.Season != season {
				break // a batch running into the next season: its first season only
			}
			eps = append(eps, e.Episode)
		}
		if season > 0 && len(eps) > 0 {
			rel.Season, rel.Episodes, rel.Episode = season, eps, eps[0]
		}
	case library.SeriesDaily:
		if rel.AirDate == "" {
			return rel
		}
		for _, e := range episodes {
			if e.Season > 0 && e.AirDate == rel.AirDate {
				rel.Season, rel.Episode, rel.Episodes = e.Season, e.Episode, []int{e.Episode}
				break
			}
		}
	}
	return rel
}

// releaseMapper maps release names for one show; nil for a standard show.
// The episode list is read once, on first use.
func (s *Server) releaseMapper(series library.Series) func(parser.Release) parser.Release {
	if series.SeriesType != library.SeriesAnime && series.SeriesType != library.SeriesDaily {
		return nil
	}
	var episodes []library.Episode
	loaded := false
	return func(rel parser.Release) parser.Release {
		if rel.Season > 0 {
			return rel
		}
		if !loaded {
			episodes, _ = s.MovieRepo.ListEpisodes(series.ID)
			loaded = true
		}
		return mapRelease(rel, series.SeriesType, episodes)
	}
}

// parseFor parses a release name for series (mapped when it is anime or daily).
func parseFor(mapper func(parser.Release) parser.Release, title string) parser.Release {
	rel := parser.Parse(title)
	if mapper != nil {
		rel = mapper(rel)
	}
	return rel
}

// extraTVQueries are the searches an anime or daily show needs besides
// "Show S01E05": the absolute number ("Show 105") or the air date
// ("Show 2024 03 15") of the episode.
func (s *Server) extraTVQueries(series library.Series, season, episode int) []string {
	if episode <= 0 || (series.SeriesType != library.SeriesAnime && series.SeriesType != library.SeriesDaily) {
		return nil
	}
	episodes, err := s.MovieRepo.ListEpisodes(series.ID)
	if err != nil {
		return nil
	}
	if series.SeriesType == library.SeriesAnime {
		if n := library.AbsoluteNumber(episodes, season, episode); n > 0 {
			return []string{fmt.Sprintf("%s %02d", series.Title, n)}
		}
		return nil
	}
	for _, e := range episodes {
		if e.Season == season && e.Episode == episode && len(e.AirDate) == 10 {
			return []string{series.Title + " " + strings.ReplaceAll(e.AirDate, "-", " ")}
		}
	}
	return nil
}

// PUT /api/series/{id}/type {"type": "standard" | "anime" | "daily"}
func (s *Server) handleSetSeriesType(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	var req struct {
		Type string `json:"type"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Type != library.SeriesStandard && req.Type != library.SeriesAnime && req.Type != library.SeriesDaily) {
		writeError(w, http.StatusBadRequest, `Choose "standard", "anime" or "daily".`)
		return
	}
	if _, err := s.MovieRepo.GetSeries(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.MovieRepo.SetSeriesType(id, req.Type); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"type": req.Type})
}
