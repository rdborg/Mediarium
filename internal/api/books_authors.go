package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

// Authors: the other books by a book's author, and following an author so
// their new books are added by themselves (checked twice a day).

// authorCheck remembers when followed authors were last looked at.
type authorCheck struct {
	mu   sync.Mutex
	last time.Time
}

const authorCheckEvery = 12 * time.Hour

func authorKeyFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.PathValue("key")
	if !books.ValidAuthorKey(key) {
		writeError(w, http.StatusBadRequest, "That isn't an Open Library author.")
		return "", false
	}
	return key, true
}

// GET /api/book-authors/{key}/works?sort=popular|newest
func (s *Server) handleAuthorWorks(w http.ResponseWriter, r *http.Request) {
	key, ok := authorKeyFromPath(w, r)
	if !ok {
		return
	}
	found, err := s.OpenLibrary.AuthorWorks(r.Context(), key, r.URL.Query().Get("sort"), 40)
	if err != nil {
		writeUpstreamError(w, "load the author's books from Open Library", err)
		return
	}
	writeJSON(w, http.StatusOK, s.bookResults(found))
}

// GET /api/book-authors: the authors followed.
func (s *Server) handleFollowedAuthors(w http.ResponseWriter, r *http.Request) {
	list, err := s.BookRepo.FollowedAuthors()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type followAuthorRequest struct {
	Name      string `json:"name"`
	Ebook     bool   `json:"ebook"`
	Audiobook bool   `json:"audiobook"`
}

// PUT /api/book-authors/{key}/follow: follow an author (or change the formats).
func (s *Server) handleFollowAuthor(w http.ResponseWriter, r *http.Request) {
	key, ok := authorKeyFromPath(w, r)
	if !ok {
		return
	}
	var req followAuthorRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" || len(req.Name) > 200 {
		writeError(w, http.StatusBadRequest, "Send the author's name.")
		return
	}
	if !req.Ebook && !req.Audiobook {
		writeError(w, http.StatusBadRequest, "Choose ebook, audiobook or both.")
		return
	}
	if (req.Ebook && !s.ebooksEnabled()) || (req.Audiobook && !s.audiobooksEnabled()) {
		writeError(w, http.StatusConflict, "That format is switched off. Switch it on in Settings > Media types.")
		return
	}
	if err := s.BookRepo.FollowAuthor(books.Author{Key: key, Name: strings.TrimSpace(req.Name), WantEbook: req.Ebook, WantAudiobook: req.Audiobook}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, byline := requester(r)
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("Following %s: new books are added by themselves%s", strings.TrimSpace(req.Name), byline))
	writeJSON(w, http.StatusOK, nil)
}

// DELETE /api/book-authors/{key}/follow: stop following. Their books stay.
func (s *Server) handleUnfollowAuthor(w http.ResponseWriter, r *http.Request) {
	key, ok := authorKeyFromPath(w, r)
	if !ok {
		return
	}
	if err := s.BookRepo.UnfollowAuthor(key); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// huntAuthors adds the new books of followed authors: books first published
// in or after the year they were followed, with a cover, that are not a box
// set and not already in the library (under another Open Library entry
// either). It runs at most every 12 hours.
func (s *Server) huntAuthors(ctx context.Context) {
	s.authorCheck.mu.Lock()
	if time.Since(s.authorCheck.last) < authorCheckEvery {
		s.authorCheck.mu.Unlock()
		return
	}
	s.authorCheck.last = time.Now()
	s.authorCheck.mu.Unlock()
	s.checkFollowedAuthors(ctx)
}

func (s *Server) checkFollowedAuthors(ctx context.Context) int {
	authors, err := s.BookRepo.FollowedAuthors()
	if err != nil || len(authors) == 0 {
		return 0
	}
	library, err := s.BookRepo.List()
	if err != nil {
		return 0
	}
	added := 0
	for _, a := range authors {
		since := time.Now().Year()
		if t, err := time.Parse(time.RFC3339, a.FollowedAt); err == nil {
			since = t.Year()
		}
		works, err := s.OpenLibrary.AuthorWorks(ctx, a.Key, "newest", 20)
		if err != nil {
			slog.Info("books: check followed author", "author", a.Name, "err", err)
			continue
		}
		for _, f := range works {
			if f.Year < since || f.CoverID == 0 || haveBook(library, f) {
				continue
			}
			b, err := s.BookRepo.Add(books.Book{OLKey: f.Key, Title: f.Title, Author: f.Author, AuthorKey: f.AuthorKey, Year: f.Year, CoverID: f.CoverID,
				WantEbook: a.WantEbook && s.ebooksEnabled(), WantAudiobook: a.WantAudiobook && s.audiobooksEnabled()})
			if errors.Is(err, books.ErrExists) || (err == nil && !b.WantEbook && !b.WantAudiobook) {
				continue
			}
			if err != nil {
				slog.Warn("books: add a followed author's book", "author", a.Name, "err", err)
				continue
			}
			library = append(library, b)
			added++
			_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("New book by %s added: %s", a.Name, b.Name()))
			for _, bf := range []books.Format{books.Ebook, books.Audiobook} {
				if b.Wants(bf) {
					bf := bf
					s.background(func() { s.searchBook(b.ID, bf, false) })
				}
			}
		}
	}
	return added
}

// haveBook reports whether the library has this book, by its Open Library
// key or by the same title from the same author.
func haveBook(library []books.Book, f books.Found) bool {
	for _, b := range library {
		if b.OLKey == f.Key || (books.SameTitle(b.Title, f.Title, true) && books.SameAuthor(b.Author, f.Author)) {
			return true
		}
	}
	return false
}
