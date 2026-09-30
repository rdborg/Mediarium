package api

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/settings"
)

// publicLinks is the address the person set for Mediarium; without one,
// messages carry no links.
func (s *Server) publicLinks() notify.Links {
	v, _ := s.Settings.Get(settings.KeyPublicURL)
	return notify.Links{Base: strings.TrimRight(strings.TrimSpace(v), "/")}
}

// notifyItem sends a download-related event with its details (title, quality,
// size, where it went), a poster and a link.
func (s *Server) notifyItem(eventType string, it notify.Item) {
	s.sendNotification(notify.Compose(eventType, it, s.publicLinks(), time.Now()))
}

// smallPoster is the poster at the size an email or a phone message needs.
func smallPoster(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return strings.Replace(metadata.PosterURL(posterPath), "/w500", "/w185", 1)
}

// movieItem starts an Item for a movie: its name, poster and page.
func movieItem(m library.Movie) notify.Item {
	return notify.Item{Media: "movie", Title: m.Title, Year: m.Year, PosterURL: smallPoster(m.PosterPath), LinkPath: fmt.Sprintf("/title/%d", m.TMDBID)}
}

// movieItemByID is movieItem for a movie looked up by id; when the lookup fails
// the message still goes out, with the title it was given.
func (s *Server) movieItemByID(id int64, title string, year, tmdbID int) notify.Item {
	if m, err := s.MovieRepo.Get(id); err == nil {
		return movieItem(m)
	}
	return notify.Item{Media: "movie", Title: title, Year: year, LinkPath: fmt.Sprintf("/title/%d", tmdbID)}
}

// seriesItem starts an Item for a show, or for its episode(s) when episode is
// not empty ("S02E03" for one episode, "Season 2" for a pack).
func seriesItem(sr library.Series, season int, episodes []int) notify.Item {
	it := notify.Item{Media: "show", Title: sr.Title, Year: sr.Year, PosterURL: smallPoster(sr.PosterPath), LinkPath: fmt.Sprintf("/series/%d", sr.ID)}
	switch {
	case season > 0 && len(episodes) == 1:
		it.Media, it.Episode = "episode", fmt.Sprintf("S%02dE%02d", season, episodes[0])
	case season > 0 && len(episodes) > 1:
		it.Media, it.Episode = "episode", fmt.Sprintf("S%02dE%02d-E%02d", season, episodes[0], episodes[len(episodes)-1])
	case season > 0:
		it.Episode = fmt.Sprintf("Season %d", season)
	}
	return it
}

// seriesItemFor is seriesItem for the episodes a grab covers: one episode is
// named, several read as their season.
func seriesItemFor(sr library.Series, season int, eps []library.Episode) notify.Item {
	if len(eps) == 1 {
		return seriesItem(sr, eps[0].Season, []int{eps[0].Episode})
	}
	if len(eps) > 1 && season == 0 {
		season = eps[0].Season
	}
	return seriesItem(sr, season, nil)
}

// fileSize adds up the sizes of the files that exist.
func fileSize(paths ...string) int64 {
	var total int64
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			total += fi.Size()
		}
	}
	return total
}
