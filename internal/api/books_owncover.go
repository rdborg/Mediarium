package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/netguard"
)

// Change cover. Open Library has no cover for many books, or has one only for
// another edition. A person can then give the book a picture of their own (an
// uploaded file or a link), or pick the cover of another edition Open Library
// does have. The choice is kept apart from the automatic cover (see the
// 0190 migration), so nothing that looks the book up again can undo it, and
// "Remove my cover" brings the automatic one back.

const (
	ownCoverMaxAge     = 30 * 24 * time.Hour
	coverLinkTimeout   = 20 * time.Second
	maxCoverLinkLength = 2048
)

// coverLinkClient is the client that downloads a cover from a link. It may only
// reach the public internet: a link someone typed in must not be able to make
// the server fetch a page from the home network. Tests give their own.
func (s *Server) coverLinkClient() *http.Client {
	if s.coverLink != nil {
		return s.coverLink
	}
	return netguard.PublicClient(coverLinkTimeout)
}

// coverProblem is an error with the sentence a person should read and the
// status to answer with.
type coverProblem struct {
	status int
	text   string
}

func (e *coverProblem) Error() string { return e.text }

// coverProblemOf turns what PrepareOwnCover said into a coverProblem.
func coverProblemOf(err error) *coverProblem {
	var cp *coverProblem
	switch {
	case errors.As(err, &cp):
		return cp
	case errors.Is(err, books.ErrCoverTooBig):
		return &coverProblem{http.StatusRequestEntityTooLarge, fmt.Sprintf("That picture is bigger than %d MB. Use a smaller one.", books.MaxOwnCoverBytes>>20)}
	case errors.Is(err, books.ErrCoverFormat):
		return &coverProblem{http.StatusUnsupportedMediaType, "That isn't a JPEG, PNG or WebP picture. Save it as one of those and try again."}
	case errors.Is(err, books.ErrCoverUnreadable):
		return &coverProblem{http.StatusUnprocessableEntity, "That picture couldn't be read. It may be damaged, so try another one."}
	}
	return nil
}

func (s *Server) writeCoverProblem(w http.ResponseWriter, err error) {
	if cp := coverProblemOf(err); cp != nil {
		writeError(w, cp.status, cp.text)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// handleBookOwnCover serves the picture a person gave a book.
func (s *Server) handleBookOwnCover(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	img, err := s.BookRepo.OwnCoverImage(b.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("X-Content-Type-Options", "nosniff")
	// The address carries the time of the last change, so a new picture has a new address.
	h.Set("Cache-Control", fmt.Sprintf("private, max-age=%d, immutable", int(ownCoverMaxAge.Seconds())))
	_, _ = w.Write(img)
}

// saveOwnCover prepares a picture and keeps it as the book's cover.
func (s *Server) saveOwnCover(w http.ResponseWriter, r *http.Request, b books.Book, raw []byte) {
	jpeg, err := books.PrepareOwnCover(raw)
	if err != nil {
		s.writeCoverProblem(w, err)
		return
	}
	if err := s.BookRepo.SetOwnCover(b.ID, jpeg, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeBookAfterCover(w, r, b.ID)
}

func (s *Server) writeBookAfterCover(w http.ResponseWriter, r *http.Request, id int64) {
	b, err := s.BookRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toBookPayload(b, isAdminRequest(r)))
}

// POST /api/books/{id}/cover: a multipart form with the picture in "file".
func (s *Server) handleUploadBookCover(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	allowLongUpload(w)
	r.Body = http.MaxBytesReader(w, r.Body, books.MaxOwnCoverBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			s.writeCoverProblem(w, books.ErrCoverTooBig)
			return
		}
		writeError(w, http.StatusBadRequest, "Choose a picture to upload.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	var raw []byte
	if r.MultipartForm != nil {
		if fh := r.MultipartForm.File["file"]; len(fh) > 0 {
			f, err := fh[0].Open()
			if err != nil {
				writeError(w, http.StatusBadRequest, "Choose a picture to upload.")
				return
			}
			defer f.Close()
			raw, err = io.ReadAll(io.LimitReader(f, books.MaxOwnCoverBytes+1))
			if err != nil {
				writeError(w, http.StatusBadRequest, "Couldn't read that file. Try choosing it again.")
				return
			}
		}
	}
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "Choose a picture to upload.")
		return
	}
	s.saveOwnCover(w, r, b, raw)
}

type coverLinkRequest struct {
	URL string `json:"url"`
}

// POST /api/books/{id}/cover/link {"url": "https://..."}
func (s *Server) handleBookCoverLink(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	var req coverLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Paste the link of a picture.")
		return
	}
	raw, err := s.fetchCoverLink(r.Context(), req.URL)
	if err != nil {
		s.writeCoverProblem(w, err)
		return
	}
	s.saveOwnCover(w, r, b, raw)
}

// fetchCoverLink downloads the picture at link. Errors are coverProblems, or
// one of books' cover errors for what is wrong with the picture itself.
func (s *Server) fetchCoverLink(ctx context.Context, link string) ([]byte, error) {
	link = strings.TrimSpace(link)
	u, err := url.Parse(link)
	if link == "" || len(link) > maxCoverLinkLength || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, &coverProblem{http.StatusBadRequest, "That doesn't look like a link to a picture. It should start with http or https."}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &coverProblem{http.StatusBadRequest, "That doesn't look like a link to a picture. It should start with http or https."}
	}
	req.Header.Set("User-Agent", s.OpenLibrary.UserAgent)
	req.Header.Set("Accept", "image/jpeg,image/png,image/*;q=0.5")
	if host := strings.ToLower(u.Hostname()); host == "books.google.com" || strings.HasSuffix(host, ".google.com") || strings.HasSuffix(host, ".googleusercontent.com") {
		req.Header.Set("Referer", "https://books.google.com/")
	}
	client := s.coverLinkClient()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, netguard.ErrBlocked) {
			return nil, &coverProblem{http.StatusBadRequest, "That link points to a place on your own network, so Mediarium won't fetch it. Download the picture and upload it instead."}
		}
		slog.Info("books: cover link not reachable", "link", netguard.RedactURL(link), "err", netguard.CleanError(err))
		return nil, &coverProblem{http.StatusBadGateway, "Couldn't download that link. Check it and try again."}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &coverProblem{http.StatusBadGateway, fmt.Sprintf("That link didn't give a picture (the site answered with status %d).", resp.StatusCode)}
	}
	if resp.ContentLength > books.MaxOwnCoverBytes {
		return nil, books.ErrCoverTooBig
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, books.MaxOwnCoverBytes+1))
	if err != nil {
		slog.Info("books: cover link not readable", "link", netguard.RedactURL(link), "err", netguard.CleanError(err))
		return nil, &coverProblem{http.StatusBadGateway, "Couldn't download that link. Check it and try again."}
	}
	return data, nil // PrepareOwnCover checks the size and what it is
}

// ---- Other editions' covers

type coverEdition struct {
	CoverID int    `json:"coverId"`
	URL     string `json:"url"`
}

type coverEditionsPayload struct {
	Editions []coverEdition `json:"editions"`
	// Current is the Open Library cover id the book shows now (0 = none or its own picture).
	Current int `json:"current"`
}

// GET /api/books/{id}/cover/editions: the covers Open Library has for the book.
func (s *Server) handleBookCoverEditions(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ids, err := s.OpenLibrary.WorkCovers(ctx, b.OLKey)
	if errors.Is(err, books.ErrNotFound) {
		ids, err = []int{}, nil
	}
	if err != nil {
		writeUpstreamError(w, "look up this book's other covers", err)
		return
	}
	out := coverEditionsPayload{Editions: make([]coverEdition, 0, len(ids))}
	if b.OwnCover == 0 {
		out.Current = b.OLCoverID()
	}
	for _, id := range ids {
		out.Editions = append(out.Editions, coverEdition{CoverID: id, URL: books.LocalCoverURL(id, "S")})
	}
	s.warmBookCovers("S", ids)
	writeJSON(w, http.StatusOK, out)
}

type coverPickRequest struct {
	CoverID int `json:"coverId"`
}

// PUT /api/books/{id}/cover/edition {"coverId": 123}: show another edition's cover.
func (s *Server) handlePickBookCover(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	var req coverPickRequest
	if err := decodeJSON(r, &req); err != nil || req.CoverID <= 0 {
		writeError(w, http.StatusBadRequest, "Pick one of the covers shown.")
		return
	}
	// Only a cover Open Library lists for this very book can be picked.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ids, err := s.OpenLibrary.WorkCovers(ctx, b.OLKey)
	if err != nil && !errors.Is(err, books.ErrNotFound) {
		writeUpstreamError(w, "look up this book's other covers", err)
		return
	}
	if !slices.Contains(ids, req.CoverID) {
		writeError(w, http.StatusBadRequest, "That cover doesn't belong to this book. Pick one of the covers shown.")
		return
	}
	if err := s.BookRepo.PickCover(b.ID, req.CoverID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.warmBookCovers("M", []int{req.CoverID})
	s.writeBookAfterCover(w, r, b.ID)
}

// DELETE /api/books/{id}/cover: take the chosen cover away.
func (s *Server) handleRemoveBookCover(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	if err := s.BookRepo.ClearOwnCover(b.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "That book isn't in your library.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeBookAfterCover(w, r, b.ID)
}
