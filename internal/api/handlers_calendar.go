package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"
)

type calendarEntryPayload struct {
	Kind        string `json:"kind"` // "movie" or "episode"
	ID          int64  `json:"id"`
	MovieID     int64  `json:"movieId,omitempty"`
	SeriesID    int64  `json:"seriesId,omitempty"`
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	ReleaseDate string `json:"releaseDate"`
	Status      string `json:"status"`
}

const (
	calendarEpisodeLookback  = 30 * 24 * time.Hour
	calendarEpisodeLookahead = 120 * 24 * time.Hour
)

// handleCalendar is the unified calendar (PRD §7 Phase 3 — releases and
// episode airs in one list). Monitored movies with a known release date are
// always included; episodes only within a window around today, so a long-
// running show's whole back catalogue doesn't drown out what's coming up.
func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	entries, err := s.calendarEntries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// calendarEntries builds the merged, date-ordered movie-release and
// episode-air list shared by the calendar page and the dashboard.
func (s *Server) calendarEntries() ([]calendarEntryPayload, error) {
	movies, err := s.MovieRepo.List()
	if err != nil {
		return nil, err
	}

	entries := []calendarEntryPayload{} // never nil: a nil slice encodes as JSON null, which the frontend would confuse with "still loading"
	for _, m := range movies {
		if !m.Monitored || m.ReleaseDate == "" {
			continue
		}
		entries = append(entries, calendarEntryPayload{
			Kind: "movie", ID: m.ID, MovieID: m.ID, Title: m.Title, ReleaseDate: m.ReleaseDate, Status: string(m.Status),
		})
	}

	seriesList, err := s.MovieRepo.ListSeries()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	for _, sr := range seriesList {
		if !sr.Monitored {
			continue
		}
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			return nil, err
		}
		for _, ep := range eps {
			if !ep.Monitored || ep.AirDate == "" || ep.AirDate < from || ep.AirDate > to {
				continue
			}
			label := fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode)
			if ep.Title != "" {
				label += " · " + ep.Title
			}
			entries = append(entries, calendarEntryPayload{
				Kind: "episode", ID: ep.ID, SeriesID: sr.ID, Season: ep.Season, Episode: ep.Episode,
				Title: sr.Title, Subtitle: label, ReleaseDate: ep.AirDate, Status: string(ep.Status),
			})
		}
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ReleaseDate < entries[j].ReleaseDate })
	return entries, nil
}
