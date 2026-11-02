package api

import (
	"fmt"
	"strings"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// queueNamer names download entries after what they are for (a movie, a show,
// an album, a book) with the title's poster. One namer serves one request: the
// movies and shows of a whole list are read in two queries up front, and
// anything else is looked up once and remembered, so a list of 300 downloads
// does not mean 600 separate lookups.
type queueNamer struct {
	s       *Server
	movies  map[int64]library.TitleBrief
	series  map[int64]library.TitleBrief
	albums  map[int64]albumName
	books   map[int64]books.Book
	missing map[string]bool // lookups that found nothing, so they are not repeated
}

type albumName struct{ title, subtitle, cover string }

// newQueueNamer reads the movies and shows that items refer to.
func (s *Server) newQueueNamer(lists ...[]queue.Item) *queueNamer {
	n := &queueNamer{
		s:       s,
		movies:  map[int64]library.TitleBrief{},
		series:  map[int64]library.TitleBrief{},
		albums:  map[int64]albumName{},
		books:   map[int64]books.Book{},
		missing: map[string]bool{},
	}
	var movieIDs, seriesIDs []int64
	seenMovie, seenSeries := map[int64]bool{}, map[int64]bool{}
	for _, items := range lists {
		for _, it := range items {
			switch {
			case it.AlbumID > 0 || it.BookID > 0:
			case it.SeriesID > 0:
				if !seenSeries[it.SeriesID] {
					seenSeries[it.SeriesID] = true
					seriesIDs = append(seriesIDs, it.SeriesID)
				}
			case it.MovieID > 0:
				if !seenMovie[it.MovieID] {
					seenMovie[it.MovieID] = true
					movieIDs = append(movieIDs, it.MovieID)
				}
			}
		}
	}
	if got, err := s.MovieRepo.MovieBriefs(movieIDs); err == nil {
		n.movies = got
	}
	if got, err := s.MovieRepo.SeriesBriefs(seriesIDs); err == nil {
		n.series = got
	}
	return n
}

func (n *queueNamer) movie(id int64) (library.TitleBrief, bool) {
	if b, ok := n.movies[id]; ok {
		return b, true
	}
	if n.missing[fmt.Sprintf("movie:%d", id)] {
		return library.TitleBrief{}, false
	}
	if got, err := n.s.MovieRepo.MovieBriefs([]int64{id}); err == nil {
		if b, ok := got[id]; ok {
			n.movies[id] = b
			return b, true
		}
	}
	n.missing[fmt.Sprintf("movie:%d", id)] = true
	return library.TitleBrief{}, false
}

func (n *queueNamer) show(id int64) (library.TitleBrief, bool) {
	if b, ok := n.series[id]; ok {
		return b, true
	}
	if n.missing[fmt.Sprintf("series:%d", id)] {
		return library.TitleBrief{}, false
	}
	if got, err := n.s.MovieRepo.SeriesBriefs([]int64{id}); err == nil {
		if b, ok := got[id]; ok {
			n.series[id] = b
			return b, true
		}
	}
	n.missing[fmt.Sprintf("series:%d", id)] = true
	return library.TitleBrief{}, false
}

func (n *queueNamer) album(id int64) (albumName, bool) {
	if a, ok := n.albums[id]; ok {
		return a, true
	}
	if n.missing[fmt.Sprintf("album:%d", id)] {
		return albumName{}, false
	}
	if t, sub, cover, ok := n.s.describeAlbum(id); ok {
		a := albumName{t, sub, cover}
		n.albums[id] = a
		return a, true
	}
	n.missing[fmt.Sprintf("album:%d", id)] = true
	return albumName{}, false
}

func (n *queueNamer) book(id int64) (books.Book, bool) {
	if b, ok := n.books[id]; ok {
		return b, true
	}
	if n.missing[fmt.Sprintf("book:%d", id)] {
		return books.Book{}, false
	}
	if b, err := n.s.BookRepo.Get(id); err == nil {
		n.books[id] = b
		return b, true
	}
	n.missing[fmt.Sprintf("book:%d", id)] = true
	return books.Book{}, false
}

// describe is describeQueueItem for a namer: the same words, from the
// remembered lookups.
func (n *queueNamer) describe(it queue.Item) (title, subtitle, poster string) {
	title = it.ReleaseTitle
	switch {
	case it.AlbumID > 0:
		if a, ok := n.album(it.AlbumID); ok {
			title, subtitle, poster = a.title, a.subtitle, a.cover
		}
	case it.BookID > 0:
		if b, ok := n.book(it.BookID); ok {
			title, poster = b.Title, b.CoverPath("M")
			subtitle = "Ebook · " + b.Author
			if it.BookFormat == string(books.Audiobook) {
				subtitle = "Audiobook · " + b.Author
			}
		}
	case it.SeriesID > 0:
		if sr, ok := n.show(it.SeriesID); ok {
			title, poster = n.s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
			switch {
			case it.Episode > 0:
				subtitle = fmt.Sprintf("S%02dE%02d", it.Season, it.Episode)
			case it.Season > 0:
				subtitle = fmt.Sprintf("Season %d", it.Season)
			}
			if sr.CopyOf != 0 {
				subtitle = strings.TrimSuffix("Second copy · "+subtitle, " · ")
			}
		}
	case it.MovieID > 0:
		if m, ok := n.movie(it.MovieID); ok {
			title, poster = n.s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
			if m.Year > 0 {
				subtitle = fmt.Sprintf("%d", m.Year)
			}
			if m.CopyOf != 0 {
				subtitle = strings.TrimSuffix("Second copy · "+subtitle, " · ")
			}
		}
	}
	return title, subtitle, poster
}
