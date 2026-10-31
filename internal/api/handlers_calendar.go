package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

type calendarEntryPayload struct {
	Kind        string `json:"kind"` // "movie", "episode", "album" (music module) or "book" (ebooks and audiobooks)
	ID          int64  `json:"id"`
	MovieID     int64  `json:"movieId,omitempty"`
	TMDBID      int    `json:"tmdbId,omitempty"` // movies: the movie page is addressed by TMDB id
	SeriesID    int64  `json:"seriesId,omitempty"`
	AlbumID     int64  `json:"albumId,omitempty"`  // albums
	ArtistID    int64  `json:"artistId,omitempty"` // albums
	BookID      int64  `json:"bookId,omitempty"`   // books
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	ReleaseDate string `json:"releaseDate"`
	// AirAt is the exact moment an episode airs (RFC 3339, UTC), when TVmaze
	// has a time for it. The page shows the episode on the viewer's own day
	// and time; without it, the episode stays on ReleaseDate.
	AirAt string `json:"airAt,omitempty"`
	// Release says which date of a movie this entry is, "cinema" or "digital",
	// when the movie has both on different days; empty otherwise.
	Release   string `json:"release,omitempty"`
	Status    string `json:"status"`
	PosterURL string `json:"posterUrl,omitempty"` // the poster or cover the calendar shows next to the title
	// BackdropURL and LogoURL are the wide still and the title logo of a movie or
	// show, for the Backdrops view. They appear once they have been fetched in
	// the background (see artwork.go). For an episode, TMDBID is the show's.
	BackdropURL string `json:"backdropUrl,omitempty"`
	LogoURL     string `json:"logoUrl,omitempty"`
}

const (
	calendarEpisodeLookback  = 30 * 24 * time.Hour
	calendarEpisodeLookahead = 120 * 24 * time.Hour
)

// handleCalendar is the unified calendar (releases and
// episode airs in one list). Monitored movies with a known release date are
// included from 30 days back on, and episodes only within a window around
// today, so a long-running show's whole back catalogue and a big movie
// library's old releases don't drown out what's coming up.
func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	entries, err := s.calendarEntries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries = s.withDigitalDates(entries)
	// Album and book releases, while their modules are on (the dashboard's
	// "coming up" list stays movies and episodes).
	more, err := s.moduleCalendarEntries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(more) > 0 {
		entries = append(entries, more...)
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].ReleaseDate < entries[j].ReleaseDate })
	}
	writeJSON(w, http.StatusOK, entries)
}

// entryMoment is when an entry happens, for ordering: the exact air time when
// known, else the start of its day.
func entryMoment(e calendarEntryPayload) string {
	if at, err := time.Parse(time.RFC3339, e.AirAt); err == nil {
		return at.UTC().Format(time.RFC3339)
	}
	return e.ReleaseDate + "T00:00:00Z"
}

// upcomingEntries keeps the first limit entries that are not in the past:
// those dated today or later, plus an episode dated earlier whose exact air
// time is today or later (UTC). Dated order, with exact times placing an
// episode within its day.
func upcomingEntries(entries []calendarEntryPayload, now time.Time, limit int) []calendarEntryPayload {
	today := now.UTC().Format("2006-01-02")
	out := make([]calendarEntryPayload, 0, limit)
	for _, e := range entries {
		day := e.ReleaseDate
		if at, err := time.Parse(time.RFC3339, e.AirAt); err == nil {
			// The later of the two days: a US evening show is dated the day
			// before its UTC time, a late-night one after.
			if d := at.UTC().Format("2006-01-02"); d > day {
				day = d
			}
		}
		if day >= today {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return entryMoment(out[i]) < entryMoment(out[j]) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// moduleCalendarEntries are the album and book releases on the calendar.
func (s *Server) moduleCalendarEntries() ([]calendarEntryPayload, error) {
	albums, err := s.albumCalendarEntries()
	if err != nil {
		return nil, err
	}
	bookEntries, err := s.bookCalendarEntries()
	if err != nil {
		return nil, err
	}
	return append(albums, bookEntries...), nil
}

// bookCalendarEntries lists the library's books with a release date in the
// calendar's window (30 days back, 120 ahead), while ebooks or audiobooks are
// on. Release dates come from Hardcover (see books_series.go).
func (s *Server) bookCalendarEntries() ([]calendarEntryPayload, error) {
	if !s.booksEnabled() {
		return nil, nil
	}
	now := time.Now().UTC()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	list, err := s.BookRepo.ListReleasing(from, to)
	if err != nil {
		return nil, err
	}
	var out []calendarEntryPayload
	for _, b := range list {
		subtitle := bookFormatsLabel(b)
		if b.SeriesName != "" && b.SeriesPosition != "" {
			subtitle = fmt.Sprintf("Book %s of %s · %s", b.SeriesPosition, b.SeriesName, subtitle)
		}
		title := b.Title
		if b.Author != "" {
			title = b.Author + " — " + b.Title
		}
		out = append(out, calendarEntryPayload{Kind: "book", ID: b.ID, BookID: b.ID, Title: title, Subtitle: subtitle, ReleaseDate: b.ReleaseDate, Status: bookCalendarStatus(b), PosterURL: b.CoverPath("M")})
	}
	return out, nil
}

// bookFormatsLabel says which formats of a book are wanted.
func bookFormatsLabel(b books.Book) string {
	switch {
	case b.WantEbook && b.WantAudiobook:
		return "Ebook and audiobook"
	case b.WantAudiobook:
		return "Audiobook"
	default:
		return "Ebook"
	}
}

// bookCalendarStatus is "downloaded" when every wanted format is there,
// "downloading" while one is on its way, else "missing".
func bookCalendarStatus(b books.Book) string {
	all, busy := true, false
	for _, f := range []books.Format{books.Ebook, books.Audiobook} {
		if !b.Wants(f) {
			continue
		}
		switch b.Status(f) {
		case books.StatusDownloaded:
		case books.StatusDownloading:
			busy, all = true, false
		default:
			all = false
		}
	}
	switch {
	case all:
		return "downloaded"
	case busy:
		return "downloading"
	}
	return "missing"
}

// albumCalendarEntries lists the monitored albums that
// come out (or came out) within the same window as episodes: 30 days back,
// 120 ahead. Only albums with a full release date can be placed on a day;
// nothing is listed while the music module is off.
func (s *Server) albumCalendarEntries() ([]calendarEntryPayload, error) {
	if !s.musicEnabled() {
		return nil, nil
	}
	now := time.Now().UTC()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	albums, err := s.MusicRepo.ListMonitoredReleasing(from, to)
	if err != nil {
		return nil, err
	}
	var out []calendarEntryPayload
	for _, al := range albums {
		name := al.ArtistName
		subtitle := "Album"
		switch al.Type {
		case "ep":
			subtitle = "EP"
		case "single":
			subtitle = "Single"
		}
		out = append(out, calendarEntryPayload{
			Kind: "album", ID: al.ID, AlbumID: al.ID, ArtistID: al.ArtistID, Title: name + " — " + al.Title, Subtitle: subtitle,
			ReleaseDate: al.ReleaseDate, Status: string(al.Status), PosterURL: albumCoverURL(al.ID),
		})
	}
	return out, nil
}

// calendarEntries builds the merged, date-ordered movie-release and
// episode-air list shared by the calendar page and the dashboard.
func (s *Server) calendarEntries() ([]calendarEntryPayload, error) {
	return s.calendarEntriesSince("")
}

// calendarEntriesSince is calendarEntries with everything before the date
// since ("YYYY-MM-DD", or "" for no limit) left out. The dashboard only wants
// what is still to come, and asking the database for just that is much
// cheaper than building the whole list and cutting it down.
func (s *Server) calendarEntriesSince(since string) ([]calendarEntryPayload, error) {
	now := time.Now().UTC()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	if since > from {
		from = since
	}
	// Movies start at the same date as episodes: a library of thousands of
	// films would otherwise put every old release in each calendar answer.
	// Releases further ahead are all kept.
	movies, err := s.MovieRepo.CalendarMovies(from)
	if err != nil {
		return nil, err
	}

	entries := make([]calendarEntryPayload, 0, len(movies)) // never nil: a nil slice encodes as JSON null, which the frontend would confuse with "still loading"
	for _, m := range movies {
		title, poster := s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
		entries = append(entries, calendarEntryPayload{
			Kind: "movie", ID: m.ID, MovieID: m.ID, TMDBID: m.TMDBID, Title: title, ReleaseDate: m.ReleaseDate, Status: string(m.Status),
			PosterURL: poster,
		})
	}

	eps, err := s.MovieRepo.CalendarEpisodes(from, to)
	if err != nil {
		return nil, err
	}
	for _, ep := range eps {
		label := fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode)
		if ep.Title != "" {
			label += " · " + ep.Title
		}
		showTitle, poster := s.shownFace("tv", ep.SeriesTMDB, ep.SeriesName, ep.PosterPath)
		entries = append(entries, calendarEntryPayload{
			Kind: "episode", ID: ep.ID, SeriesID: ep.SeriesID, Season: ep.Season, Episode: ep.Episode,
			Title: showTitle, Subtitle: label, ReleaseDate: ep.AirDate, AirAt: ep.AirAt, Status: string(ep.Status),
			PosterURL: poster, TMDBID: ep.SeriesTMDB,
		})
	}
	s.attachArt(entries)

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ReleaseDate < entries[j].ReleaseDate })
	return entries, nil
}
