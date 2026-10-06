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
	Status      string `json:"status"`
}

const (
	calendarEpisodeLookback  = 30 * 24 * time.Hour
	calendarEpisodeLookahead = 120 * 24 * time.Hour
)

// handleCalendar is the unified calendar (releases and
// episode airs in one list). Monitored movies with a known release date are
// always included; episodes only within a window around today, so a long-
// running show's whole back catalogue doesn't drown out what's coming up.
func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	entries, err := s.calendarEntries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	list, err := s.BookRepo.List()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	var out []calendarEntryPayload
	for _, b := range list {
		if len(b.ReleaseDate) != len("2006-01-02") || b.ReleaseDate < from || b.ReleaseDate > to || (!b.WantEbook && !b.WantAudiobook) {
			continue
		}
		subtitle := bookFormatsLabel(b)
		if b.SeriesName != "" && b.SeriesPosition != "" {
			subtitle = fmt.Sprintf("Book %s of %s · %s", b.SeriesPosition, b.SeriesName, subtitle)
		}
		title := b.Title
		if b.Author != "" {
			title = b.Author + " — " + b.Title
		}
		out = append(out, calendarEntryPayload{Kind: "book", ID: b.ID, BookID: b.ID, Title: title, Subtitle: subtitle, ReleaseDate: b.ReleaseDate, Status: bookCalendarStatus(b)})
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
	artists, err := s.MusicRepo.ListArtists()
	if err != nil {
		return nil, err
	}
	albums, err := s.MusicRepo.ListAllAlbums()
	if err != nil {
		return nil, err
	}
	monitored := map[int64]string{}
	for _, a := range artists {
		monitored[a.ID] = a.Name
	}
	now := time.Now().UTC()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	var out []calendarEntryPayload
	for _, al := range albums {
		name, ok := monitored[al.ArtistID]
		if !ok || !al.Monitored || len(al.ReleaseDate) != len("2006-01-02") || al.ReleaseDate < from || al.ReleaseDate > to {
			continue
		}
		subtitle := "Album"
		switch al.Type {
		case "ep":
			subtitle = "EP"
		case "single":
			subtitle = "Single"
		}
		out = append(out, calendarEntryPayload{
			Kind: "album", ID: al.ID, AlbumID: al.ID, ArtistID: al.ArtistID, Title: name + " — " + al.Title, Subtitle: subtitle,
			ReleaseDate: al.ReleaseDate, Status: string(al.Status),
		})
	}
	return out, nil
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
			Kind: "movie", ID: m.ID, MovieID: m.ID, TMDBID: m.TMDBID, Title: m.Title, ReleaseDate: m.ReleaseDate, Status: string(m.Status),
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
