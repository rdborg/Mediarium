package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

// The Books part of Discover: Open Library's trending lists and a browse by
// subject, year and order, each book marked when it is already in the
// library.

// bookResults adds covers and the library id to a list of books.
func (s *Server) bookResults(found []books.Found) []bookSearchResult {
	out := make([]bookSearchResult, 0, len(found))
	covers := make([]int, 0, len(found))
	hcCovers := make([]int, 0, len(found))
	var library []books.Book
	for _, f := range found {
		if f.HardcoverID > 0 {
			library, _ = s.BookRepo.List()
			break
		}
	}
	for _, f := range found {
		covers = append(covers, f.CoverID)
		if f.CoverID <= 0 && f.HardcoverID > 0 && f.CoverImage != "" {
			hcCovers = append(hcCovers, f.HardcoverID)
		}
		res := bookSearchResult{Found: f, CoverURL: f.CoverAddress("M")}
		if b, ok, _ := s.BookRepo.GetByKey(f.Key); ok {
			res.LibraryID = b.ID
		} else if f.HardcoverID > 0 {
			for _, b := range library {
				if books.SameTitle(b.Title, f.Title, true) && books.SameAuthor(b.Author, f.Author) {
					res.LibraryID = b.ID
					break
				}
			}
		}
		out = append(out, res)
	}
	s.warmBookCovers("M", covers)
	s.warmHardcoverCovers(hcCovers)
	return out
}

// GET /api/books/discover?list=trending&period=weekly&page=1
// GET /api/books/discover?list=browse&subject=fantasy&from=2000&to=2010&sort=popular&page=1
func (s *Server) handleBookDiscover(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 50 {
		writeJSON(w, http.StatusOK, []bookSearchResult{})
		return
	}
	var (
		found []books.Found
		err   error
	)
	switch q.Get("list") {
	case "", "trending":
		period := q.Get("period")
		if period == "" {
			period = "weekly"
		}
		if !books.TrendingPeriods[period] {
			writeError(w, http.StatusBadRequest, "Choose daily, weekly, monthly, yearly or forever.")
			return
		}
		found, err = s.OpenLibrary.Trending(r.Context(), period, page, 40)
	case "browse":
		from, _ := strconv.Atoi(q.Get("from"))
		to, _ := strconv.Atoi(q.Get("to"))
		found, err = s.OpenLibrary.Browse(r.Context(), books.BrowseQuery{Subject: q.Get("subject"), YearFrom: from, YearTo: to, Sort: q.Get("sort"), Page: page, Limit: 40})
	default:
		writeError(w, http.StatusBadRequest, `Choose the list: "trending" or "browse".`)
		return
	}
	if err != nil {
		writeUpstreamError(w, "load books from Open Library", err)
		return
	}
	writeJSON(w, http.StatusOK, s.bookResults(found))
}

// GET /api/books/subjects: the genres the Discover filter offers.
func (s *Server) handleBookSubjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, books.Subjects)
}

// GET /api/books/{id}/links: where the book can be read or listened to, the
// connected Audiobookshelf and Kavita servers that have it.
func (s *Server) handleBookLinks(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	servers, err := s.MediaServers.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = []mediaservers.Link{}
	)
	for _, m := range servers {
		if !m.Enabled || !m.Kind.BookServer() {
			continue
		}
		wg.Add(1)
		go func(m mediaservers.Server) {
			defer wg.Done()
			item, found, err := s.mediaClient.FindBook(ctx, m, b.Title, b.Author)
			if err != nil || !found {
				return
			}
			mu.Lock()
			out = append(out, mediaservers.Link{ServerID: m.ID, Name: m.Name, Kind: m.Kind, URL: item.URL})
			mu.Unlock()
		}(m)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, out)
}

type workPayload struct {
	books.WorkInfo
	CoverURL  string `json:"coverUrl,omitempty"`
	LibraryID int64  `json:"libraryId,omitempty"`
}

// GET /api/book-works/{key}: one book from Open Library for its info page,
// or from Hardcover when the key is one of theirs, marked when it is already
// in the library.
func (s *Server) handleBookWork(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if id, ok := books.ParseHardcoverKey(key); ok {
		s.handleHardcoverWork(w, r, id)
		return
	}
	info, err := s.OpenLibrary.WorkInfo(r.Context(), key)
	if errors.Is(err, books.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Open Library doesn't have that book.")
		return
	}
	if err != nil {
		writeUpstreamError(w, "look the book up on Open Library", err)
		return
	}
	out := workPayload{WorkInfo: info, CoverURL: info.CoverAddress("L")}
	if b, ok, _ := s.BookRepo.GetByKey(info.Key); ok {
		out.LibraryID = b.ID
	}
	writeJSON(w, http.StatusOK, out)
}

// handleHardcoverWork is the info page of a book that was found on Hardcover
// and not on Open Library.
func (s *Server) handleHardcoverWork(w http.ResponseWriter, r *http.Request, id int) {
	hc := s.hardcover()
	if hc == nil {
		writeError(w, http.StatusNotFound, "That book was found on Hardcover, and no Hardcover token is saved.")
		return
	}
	info, err := hc.Book(r.Context(), id)
	if errors.Is(err, books.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Hardcover doesn't have that book.")
		return
	}
	if err != nil {
		writeUpstreamError(w, "look the book up on Hardcover", err)
		return
	}
	out := workPayload{WorkInfo: info, CoverURL: info.CoverAddress("L")}
	if b, ok, _ := s.BookRepo.GetByKey(info.Key); ok {
		out.LibraryID = b.ID
	} else if library, err := s.BookRepo.List(); err == nil {
		for _, b := range library {
			if books.SameTitle(b.Title, info.Title, true) && books.SameAuthor(b.Author, info.Author) {
				out.LibraryID = b.ID
				break
			}
		}
	}
	if info.CoverImage != "" {
		s.warmHardcoverCovers([]int{info.HardcoverID})
	}
	writeJSON(w, http.StatusOK, out)
}
