package api

import (
	"sort"
	"strconv"

	"github.com/rdborg/mediarium/internal/library"
)

// latestArrivals is "Just added to your library", what arrived last first.
// A title's time is when its latest download finished (a show's, when its
// newest episode came in); a title with no download in the history (one
// imported from an existing library) counts from when it was added. recent
// is the recently added titles that are in the library; titles downloaded
// lately but added long ago are found through the download history.
func (s *Server) latestArrivals(recent []dashRecent) []dashRecent {
	key := func(kind string, id int64) string { return kind + ":" + strconv.FormatInt(id, 10) }
	byKey := map[string]dashRecent{}
	for _, d := range recent {
		byKey[key(d.Kind, d.ID)] = d
	}
	seen := map[string]bool{} // titles whose latest download was already used
	if done, err := s.QueueRepo.RecentCompleted(200); err == nil {
		// The movies and shows of all of them in two queries, not one each.
		var movieIDs, seriesIDs []int64
		for _, c := range done {
			switch {
			case c.MovieID > 0:
				movieIDs = append(movieIDs, c.MovieID)
			case c.SeriesID > 0:
				seriesIDs = append(seriesIDs, c.SeriesID)
			}
		}
		movies, _ := s.MovieRepo.MovieBriefs(movieIDs)
		shows, _ := s.MovieRepo.SeriesBriefs(seriesIDs)
		for _, c := range done { // newest first
			var d dashRecent
			switch {
			case c.MovieID > 0:
				m, ok := movies[c.MovieID]
				if !ok || m.CopyOf != 0 || m.Status != library.StatusDownloaded {
					continue
				}
				title, poster := s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
				d = dashRecent{Kind: "movie", ID: m.ID, TMDBID: m.TMDBID, Title: title, Year: m.Year, PosterURL: poster, State: "downloaded"}
			case c.SeriesID > 0:
				sr, ok := shows[c.SeriesID]
				if !ok || sr.CopyOf != 0 {
					continue
				}
				title, poster := s.shownFace("series", sr.TMDBID, sr.Title, sr.PosterPath)
				d = dashRecent{Kind: "series", ID: sr.ID, SeriesID: sr.ID, Title: title, Year: sr.Year, PosterURL: poster, State: "downloaded"}
			default:
				continue
			}
			k := key(d.Kind, d.ID)
			if seen[k] {
				continue
			}
			seen[k] = true
			if have, ok := byKey[k]; ok {
				d.State = have.State
			}
			d.At = c.CompletedAt
			byKey[k] = d
		}
	}
	out := make([]dashRecent, 0, len(byKey))
	for _, d := range byKey {
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At > out[j].At })
	if len(out) > recentShown {
		out = out[:recentShown]
	}
	return out
}
