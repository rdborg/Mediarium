package api

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/settings"
)

// The calendar shows a monitored movie on its digital release date as well as
// on its cinema date. TMDB gives the digital date in the detail call of each
// title, so Mediarium asks slowly in the background (a few titles every few
// minutes, only for monitored movies released lately or still to come) and
// keeps the answer. A movie gets its second marker once it has been asked
// about; until then it only has the cinema one.

const (
	releaseDatesEvery    = 10 * time.Minute
	releaseDatesBatch    = 12                     // titles asked about in one run
	releaseDatesPause    = 400 * time.Millisecond // between two calls, to go easy on TMDB
	releaseDatesAgain    = 3 * 24 * time.Hour     // dates move, so a title is asked about again after this
	releaseDatesLookback = 150 * 24 * time.Hour   // a film released longer ago is no longer waiting for its digital date
	releaseDatesStamp    = "2006-01-02"           // the day format of release dates
	releaseCinema        = "cinema"               // calendar entry labels
	releaseDigital       = "digital"
)

// releaseCountrySetting is the country chosen for release dates (Settings >
// Info, lists and subtitles), or "" when it follows the info language.
func (s *Server) releaseCountrySetting() string {
	if v, _ := s.Settings.Get(settings.KeyReleaseCountry); metadata.IsReleaseCountry(v) {
		return v
	}
	return ""
}

// releaseRegion is the country release dates are read for: the one chosen in
// Settings > Info, lists and subtitles, or else the one of the language titles
// are shown in.
func (s *Server) releaseRegion() string {
	if c := s.releaseCountrySetting(); c != "" {
		return c
	}
	return metadata.RegionOfLanguage(s.infoLanguage())
}

// withDigitalDates adds a "digital" entry for each monitored movie whose
// digital date is known, and labels the movie's own entry "cinema" so the two
// can be told apart. A movie whose two dates fall on the same day keeps its one
// entry, unlabelled.
func (s *Server) withDigitalDates(entries []calendarEntryPayload) []calendarEntryPayload {
	from := time.Now().UTC().Add(-calendarEpisodeLookback).Format(releaseDatesStamp)
	movies, err := s.MovieRepo.DigitalCalendarMovies(from)
	if err != nil {
		slog.Warn("calendar: couldn't read the digital release dates", "err", err)
		return entries
	}
	if len(movies) == 0 {
		return entries
	}
	labelled := map[int64]bool{}
	var added []calendarEntryPayload
	for _, m := range movies {
		if m.Digital == m.ReleaseDate {
			continue
		}
		labelled[m.ID] = true
		title, poster := s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
		added = append(added, calendarEntryPayload{
			Kind: "movie", ID: m.ID, MovieID: m.ID, TMDBID: m.TMDBID, Title: title, ReleaseDate: m.Digital, Status: string(m.Status),
			PosterURL: poster, Release: releaseDigital,
		})
	}
	if len(added) == 0 {
		return entries
	}
	for i := range entries {
		if entries[i].Kind == "movie" && labelled[entries[i].ID] {
			entries[i].Release = releaseCinema
		}
	}
	s.attachArt(added)
	entries = append(entries, added...)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ReleaseDate < entries[j].ReleaseDate })
	return entries
}

// releaseDatesJob reads the release dates of a few monitored movies that have
// none saved yet (or only for another country, or from long ago).
func (s *Server) releaseDatesJob(ctx context.Context) {
	if !s.TMDB().HasAPIKey() {
		return
	}
	now := time.Now()
	ids, err := s.MovieRepo.ReleaseDateCandidates(now.Add(-releaseDatesLookback).Format(releaseDatesStamp))
	if err != nil || len(ids) == 0 {
		return
	}
	saved, err := s.MovieRepo.MovieReleaseDates(ids)
	if err != nil {
		return
	}
	region := s.releaseRegion()
	fetched := 0
	for _, id := range ids {
		if fetched >= releaseDatesBatch || ctx.Err() != nil {
			return
		}
		if d, ok := saved[id]; ok && d.Region == region {
			if at, err := time.Parse(time.RFC3339, d.CheckedAt); err == nil && now.Sub(at) < releaseDatesAgain {
				continue
			}
		}
		if fetched > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(releaseDatesPause):
			}
		}
		fetched++
		detail, err := s.TMDB().GetMovieDetail(ctx, id)
		var dates metadata.MovieReleaseDates
		if err != nil {
			var se *metadata.StatusError
			if !errors.As(err, &se) || se.Code != 404 {
				slog.Debug("calendar: couldn't fetch the release dates", "tmdb", id, "err", err)
				continue // asked again on the next run
			}
			// TMDB does not have the title. Remember an empty answer, or it takes a place in every batch.
		} else {
			dates = detail.ReleaseDatesFor(region)
		}
		if err := s.MovieRepo.SaveMovieReleaseDates(id, region, dates.Cinema, dates.Digital, now); err != nil {
			slog.Warn("calendar: couldn't save the release dates", "err", err)
			return
		}
	}
}
