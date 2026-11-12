package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/settings"
)

// Google is asked after the other catalogues, and only for a few seconds, so a
// slow answer never holds the search. A result is remembered, including "no
// cover" and a failure, so typing does not ask again.
const (
	googleWait     = 4 * time.Second
	googleRemember = 10 * time.Minute
	googleRetry    = 2 * time.Minute
)

// Google Books is asked with no API key. A saved key is sent when there is
// one, for people who reach Google's limit. Results use the language chosen
// for movie and show info. Search fills a cover or a description the other
// catalogues don't have, and a book only Google Books knows can be added.

type googleBooksState struct {
	Set bool `json:"set"`
}

type googleBooksSearch struct {
	Configured  bool              `json:"configured"`
	Unavailable bool              `json:"unavailable,omitempty"`
	Results     []books.GoogleHit `json:"results"`
}

// handleGetGoogleBooks reports whether a Google Books API key is saved.
func (s *Server) handleGetGoogleBooks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, googleBooksState{Set: s.savedGoogleBooksKey() != ""})
}

// handlePutGoogleBooks checks a Google Books API key and saves it encrypted.
// An empty key removes it.
func (s *Server) handlePutGoogleBooks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	key, ok := books.CleanGoogleBooksKey(req.Key)
	if !ok {
		writeError(w, http.StatusBadRequest, "That doesn't look like a Google Books API key.")
		return
	}
	if key != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		err := s.googleBooks(key).Check(ctx)
		if errors.Is(err, books.ErrGoogleBooksKey) {
			writeError(w, http.StatusBadRequest, "Google Books didn't accept that key. Check it, and that the Books API is switched on for it.")
			return
		}
		if err != nil {
			writeUpstreamError(w, "check the key with Google Books", err)
			return
		}
	}
	if err := s.Settings.Set(settings.KeyGoogleBooksAPIKey, key, true); err != nil {
		writeError(w, http.StatusInternalServerError, "The key couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, googleBooksState{Set: key != ""})
}

// handleSearchGoogleBooks lists Google Books results in Google's order, in the movie and show info language.
// A saved key is sent when there is one. Without a key the same search still runs.
func (s *Server) handleSearchGoogleBooks(w http.ResponseWriter, r *http.Request) {
	key := s.savedGoogleBooksKey()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 200 {
		q = q[:200]
	}
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, googleBooksSearch{Configured: key != "", Results: []books.GoogleHit{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, err := s.googleBooks(key).Search(ctx, q, s.infoLanguage(), 12)
	if err != nil {
		slog.Info("books: Google Books search failed", "err", err)
		writeJSON(w, http.StatusOK, googleBooksSearch{Configured: key != "", Unavailable: true, Results: []books.GoogleHit{}})
		return
	}
	if hits == nil {
		hits = []books.GoogleHit{}
	}
	writeJSON(w, http.StatusOK, googleBooksSearch{Configured: key != "", Results: hits})
}

type adoptGoogleRequest struct {
	ISBN      string `json:"isbn"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Ebook     bool   `json:"ebook"`
	Audiobook bool   `json:"audiobook"`
	SearchNow bool   `json:"searchNow"`
}

// handleAdoptGoogleBook adds a book found through Google Books. Open Library or Hardcover is used when the same book is there; otherwise the Google Books volume is saved, with its cover.
func (s *Server) handleAdoptGoogleBook(w http.ResponseWriter, r *http.Request) {
	var req adoptGoogleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if len(req.Title) > 300 || len(req.Author) > 200 || len(req.ISBN) > 32 {
		writeError(w, http.StatusBadRequest, "Pick a book from the search results to add.")
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
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	found, err := s.bookFromGoogle(ctx, req.ISBN, req.Title, req.Author)
	if errors.Is(err, books.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Google Books doesn't have that book.")
		return
	}
	if err != nil {
		writeUpstreamError(w, "match the book on Open Library", err)
		return
	}
	add := addBookRequest{
		OLKey: found.Key, Title: found.Title, Author: found.Author, AuthorKey: found.AuthorKey,
		Year: found.Year, CoverID: found.CoverID, HardcoverID: found.HardcoverID, GoogleID: found.GoogleID,
		Ebook: req.Ebook, Audiobook: req.Audiobook, SearchNow: req.SearchNow,
	}
	if s.mustRequest(r) {
		raw, err := json.Marshal(add)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "The book couldn't be asked for.")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		s.fileRequest(w, r, "book")
		return
	}
	s.finishAddBook(w, r, add)
}

// bookFromGoogle finds the Open Library edition for an ISBN, then by title
// and author, then the same on Hardcover. When none of those has it, the
// Google Books volume itself is returned so it can be added.
func (s *Server) bookFromGoogle(ctx context.Context, isbn, title, author string) (books.Found, error) {
	isbn = digitsAndX(isbn)
	if len(isbn) == 10 || len(isbn) == 13 {
		found, err := s.OpenLibrary.Search(ctx, "isbn:"+isbn, 5)
		if err != nil {
			return books.Found{}, err
		}
		if len(found) > 0 {
			return found[0], nil
		}
	}
	title = strings.TrimSpace(title)
	author = strings.TrimSpace(author)
	if title != "" {
		found, err := s.OpenLibrary.FindOnOpenLibrary(ctx, title, author)
		if err == nil {
			return found, nil
		}
		if !errors.Is(err, books.ErrNotFound) {
			return books.Found{}, err
		}
	}
	if hc := s.hardcover(); hc != nil && title != "" {
		hits, err := hc.Search(ctx, strings.TrimSpace(title+" "+author), 8)
		if err != nil {
			slog.Info("books: Hardcover lookup after Google Books failed", "err", err)
		} else {
			for _, hit := range hits {
				if books.SameTitle(hit.Title, title, true) && (author == "" || books.SameAuthor(hit.Author, author)) {
					return hit, nil
				}
			}
		}
	}
	if title != "" {
		hits, err := s.googleBooks(s.savedGoogleBooksKey()).Search(ctx, strings.TrimSpace(title+" "+author), s.infoLanguage(), 8)
		if err != nil {
			return books.Found{}, err
		}
		for _, hit := range hits {
			if books.SameTitle(hit.Title, title, true) && (author == "" || books.SameAuthor(hit.Author, author)) {
				if f, ok := hit.Found(); ok {
					return f, nil
				}
			}
		}
	}
	return books.Found{}, books.ErrNotFound
}

func digitsAndX(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' || r == 'X' || r == 'x' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *Server) savedGoogleBooksKey() string {
	key, err := s.Settings.Get(settings.KeyGoogleBooksAPIKey)
	if err != nil {
		return ""
	}
	key, ok := books.CleanGoogleBooksKey(key)
	if !ok {
		return ""
	}
	return key
}

func (s *Server) googleBooks(key string) *books.GoogleBooks {
	g := books.NewGoogleBooks(key, s.OpenLibrary.UserAgent)
	if s.googleBooksBase != "" {
		g.Base = strings.TrimRight(s.googleBooksBase, "/")
	}
	return g
}

// googleVol is one volume remembered from a search or a page, so a cover is
// not fetched again and an id nobody searched for is refused.
type googleVol struct {
	title, author, blurb, cover string
	year                        int
	noCover                     bool
	until                       time.Time
}

type googleQuery struct {
	found []books.Found
	until time.Time
}

// googleMemo remembers Google's answers for a short while.
type googleMemo struct {
	mu      sync.Mutex
	vols    map[string]googleVol
	queries map[string]googleQuery
}

func (m *googleMemo) query(key string) ([]books.Found, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.queries[key]
	if !ok || time.Now().After(q.until) {
		return nil, false
	}
	return append([]books.Found(nil), q.found...), true
}

func (m *googleMemo) rememberQuery(key string, found []books.Found, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.queries == nil {
		m.queries = map[string]googleQuery{}
	}
	m.queries[key] = googleQuery{found: append([]books.Found(nil), found...), until: time.Now().Add(ttl)}
}

func (m *googleMemo) remember(f books.Found, ttl time.Duration) {
	if f.GoogleID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vols == nil {
		m.vols = map[string]googleVol{}
	}
	m.vols[f.GoogleID] = googleVol{
		title: f.Title, author: f.Author, blurb: f.GoogleBlurb, cover: f.GoogleCover,
		year: f.Year, noCover: f.GoogleCover == "", until: time.Now().Add(ttl),
	}
}

func (m *googleMemo) vol(id string) (googleVol, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.vols[id]
	if !ok || time.Now().After(v.until) {
		return googleVol{}, false
	}
	return v, true
}

// googleHits is one Google Books search, limited to a few seconds. The answer,
// including an empty one and a failure, is remembered.
func (s *Server) googleHits(ctx context.Context, q, lang string) []books.Found {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return nil
	}
	key := strings.ToLower(q) + "\n" + books.GoogleBooksLanguage(lang)
	if found, ok := s.gbooks.query(key); ok {
		return found
	}
	ctx, cancel := context.WithTimeout(ctx, googleWait)
	defer cancel()
	found, err := s.googleBooks(s.savedGoogleBooksKey()).Results(ctx, q, lang, 12)
	if err != nil {
		slog.Info("books: Google Books search failed", "err", err)
		s.gbooks.rememberQuery(key, nil, googleRetry)
		return nil
	}
	s.gbooks.rememberQuery(key, found, googleRemember)
	for _, f := range found {
		s.gbooks.remember(f, googleRemember)
	}
	return found
}

// fillGoogleGap copies a Google Books cover or description onto a book from
// another catalogue when that book has none. One short search, remembered.
func (s *Server) fillGoogleGap(ctx context.Context, info *books.WorkInfo) {
	if info == nil {
		return
	}
	needCover := info.CoverID == 0 && info.CoverImage == "" && info.GoogleCover == ""
	needBlurb := strings.TrimSpace(info.Description) == ""
	if !needCover && !needBlurb {
		return
	}
	q := strings.TrimSpace(info.Title + " " + info.Author)
	for _, h := range s.googleHits(ctx, q, s.infoLanguage()) {
		if !books.SameTitle(info.Title, h.Title, true) || !books.SameAuthor(info.Author, h.Author) {
			continue
		}
		if info.Year > 0 && h.Year > 0 {
			d := info.Year - h.Year
			if d < 0 {
				d = -d
			}
			if d > 1 {
				continue
			}
		}
		if info.GoogleID == "" {
			info.GoogleID = h.GoogleID
		}
		if needCover && h.GoogleCover != "" {
			info.GoogleCover = h.GoogleCover
		}
		if needBlurb && h.GoogleBlurb != "" {
			info.Description = h.GoogleBlurb
		}
		return
	}
}

// googleWork is a volume's details from the recent search, or one short lookup.
// A search hit with a title but no blurb is not enough: the list often omits
// the description, and the volume itself still has it.
func (s *Server) googleWork(ctx context.Context, id string) (books.WorkInfo, error) {
	v, has := s.gbooks.vol(id)
	if has && v.title != "" && strings.TrimSpace(v.blurb) != "" {
		f := books.Found{Key: books.GoogleKey(id), Title: v.title, Author: v.author, Year: v.year, GoogleID: id, GoogleCover: v.cover, GoogleBlurb: v.blurb}
		return books.WorkInfo{Found: f, Description: v.blurb, Subjects: []string{}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, googleWait)
	defer cancel()
	info, err := s.googleBooks(s.savedGoogleBooksKey()).Book(ctx, id)
	if err != nil {
		if has && v.title != "" {
			f := books.Found{Key: books.GoogleKey(id), Title: v.title, Author: v.author, Year: v.year, GoogleID: id, GoogleCover: v.cover, GoogleBlurb: v.blurb}
			return books.WorkInfo{Found: f, Description: v.blurb, Subjects: []string{}}, nil
		}
		return books.WorkInfo{}, err
	}
	if info.GoogleCover == "" && has && v.cover != "" {
		info.GoogleCover = v.cover
	}
	s.gbooks.remember(info.Found, googleRemember)
	return info, nil
}
