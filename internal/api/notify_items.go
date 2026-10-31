package api

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
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
	if eventType == "imported" {
		s.scriptAfterImport(it)
	}
}

// smallPoster is the poster at the size an email or a phone message needs:
// big enough to look sharp when an email shows it large.
func smallPoster(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return strings.Replace(metadata.PosterURL(posterPath), "/w500", "/w342", 1)
}

// movieItem starts an Item for a movie: its name, poster and page.
func movieItem(m library.Movie) notify.Item {
	return notify.Item{Media: "movie", Title: m.Title, Year: m.Year, PosterURL: smallPoster(m.PosterPath), LinkPath: fmt.Sprintf("/title/%d", m.TMDBID)}
}

// localizedMovieItem is movieItem with the title and poster in the info language.
func (s *Server) localizedMovieItem(m library.Movie) notify.Item {
	it := movieItem(m)
	title, poster := s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
	it.Title = title
	it.PosterURL = strings.Replace(poster, "/w500", "/w342", 1)
	return it
}

// movieItemByID is movieItem for a movie looked up by id; when the lookup fails
// the message still goes out, with the title it was given.
func (s *Server) movieItemByID(id int64, title string, year, tmdbID int) notify.Item {
	if m, err := s.MovieRepo.Get(id); err == nil {
		return s.localizedMovieItem(m)
	}
	return notify.Item{Media: "movie", Title: title, Year: year, LinkPath: fmt.Sprintf("/title/%d", tmdbID)}
}

// seriesItem starts an Item for a show, or for its episode(s) when episode is
// not empty ("S02E03" for one episode, "Season 2" for a pack).
func seriesItem(sr library.Series, season int, episodes []int) notify.Item {
	it := notify.Item{Media: "show", Title: sr.Title, Year: sr.Year, PosterURL: smallPoster(sr.PosterPath), LinkPath: fmt.Sprintf("/series/%d", sr.ID)}
	switch {
	case len(episodes) == 1:
		it.Media, it.Episode = "episode", fmt.Sprintf("S%02dE%02d", season, episodes[0])
	case len(episodes) > 1:
		it.Media, it.Episode = "episode", fmt.Sprintf("S%02dE%02d-E%02d", season, episodes[0], episodes[len(episodes)-1])
	case season > 0:
		it.Episode = fmt.Sprintf("Season %d", season)
	}
	return it
}

// localizedSeriesItem is seriesItem with the show's title and poster in the info language.
func (s *Server) localizedSeriesItem(sr library.Series, season int, episodes []int) notify.Item {
	it := seriesItem(sr, season, episodes)
	title, poster := s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
	it.Title = title
	it.PosterURL = strings.Replace(poster, "/w500", "/w342", 1)
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

// localizedSeriesItemFor is seriesItemFor with the show's title and poster in the info language.
func (s *Server) localizedSeriesItemFor(sr library.Series, season int, eps []library.Episode) notify.Item {
	it := seriesItemFor(sr, season, eps)
	title, poster := s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
	it.Title = title
	it.PosterURL = strings.Replace(poster, "/w500", "/w342", 1)
	return it
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

// releaseDetails adds what a message says about the release: its custom
// format score and the formats it matched (when the profile has any), its
// group and the indexer it came from. Whatever isn't known is left out.
func (s *Server) releaseDetails(it *notify.Item, profile quality.Profile, releaseTitle, downloadURL string) {
	if g := parser.Parse(releaseTitle).Group; g != "" {
		it.Group = g
	}
	if id, ok := s.indexerOfRelease(downloadURL); ok {
		if inst, err := s.IndexerRepo.Get(id); err == nil {
			it.Indexer = inst.Name
		}
	}
	if len(profile.Formats) == 0 {
		return
	}
	total, hits := profile.FormatBreakdown(releaseTitle)
	it.ScoreKnown, it.Score, it.Formats = true, total, formatNamesByWeight(hits)
}

// formatNamesByWeight lists the matched formats' names, the ones that move the
// score most first.
func formatNamesByWeight(hits []quality.FormatHit) []string {
	sorted := append([]quality.FormatHit(nil), hits...)
	sort.SliceStable(sorted, func(i, j int) bool { return abs(sorted[i].Score) > abs(sorted[j].Score) })
	names := make([]string, len(sorted))
	for i, h := range sorted {
		names[i] = h.Name
	}
	return names
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// movieReleaseDetails is releaseDetails with the profile of a movie.
func (s *Server) movieReleaseDetails(it *notify.Item, m library.Movie, releaseTitle, downloadURL string) {
	profile := quality.Profile{}
	if ps, err := s.loadProfiles(); err == nil {
		profile = s.profileForMovie(ps, m)
	}
	s.releaseDetails(it, profile, releaseTitle, downloadURL)
}

// seriesReleaseDetails is releaseDetails with the profile of a show.
func (s *Server) seriesReleaseDetails(it *notify.Item, sr library.Series, releaseTitle, downloadURL string) {
	profile := quality.Profile{}
	if ps, err := s.loadProfiles(); err == nil {
		profile = s.profileForSeries(ps, sr)
	}
	s.releaseDetails(it, profile, releaseTitle, downloadURL)
}
