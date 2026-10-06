package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/requests"
)

// Requests: a basic account that may not add titles itself (Settings >
// Accounts, "Add titles themselves" off) asks instead. The add it sent is
// kept as it was; an administrator approves it, which runs that same add on
// the person's behalf, or declines it with a note.

type approvingKey struct{}

// mustRequest reports whether the caller's adds become requests.
func (s *Server) mustRequest(r *http.Request) bool {
	if r.Context().Value(approvingKey{}) != nil {
		return false
	}
	u := auth.UserFromContext(r.Context())
	if u == nil || u.IsAdmin {
		return false
	}
	p, err := s.Auth.PermissionsOf(u.ID)
	return err == nil && !p.AddDirect
}

type requestInfo struct {
	title, poster string
	year          int
	inLibrary     bool
}

// describeRequest works out what a request is for, from the add it carries.
func (s *Server) describeRequest(ctx context.Context, kind string, body []byte) (requestInfo, error) {
	switch kind {
	case "movie":
		var req addMovieRequest
		if json.Unmarshal(body, &req) != nil || req.TMDBID <= 0 {
			return requestInfo{}, errors.New("Pick a movie from the search results.")
		}
		if _, ok, _ := s.MovieRepo.GetByTMDBID(req.TMDBID); ok {
			return requestInfo{inLibrary: true}, nil
		}
		m, err := s.TMDB().GetMovie(ctx, req.TMDBID)
		if err != nil {
			return requestInfo{}, err
		}
		return requestInfo{title: m.Title, year: m.Year(), poster: metadata.PosterURL(m.PosterPath)}, nil
	case "tv":
		var req addSeriesRequest
		if json.Unmarshal(body, &req) != nil || req.TMDBID <= 0 {
			return requestInfo{}, errors.New("Pick a show from the search results.")
		}
		if _, ok, _ := s.MovieRepo.GetSeriesByTMDBID(req.TMDBID); ok {
			return requestInfo{inLibrary: true}, nil
		}
		sh, err := s.TMDB().GetShow(ctx, req.TMDBID)
		if err != nil {
			return requestInfo{}, err
		}
		return requestInfo{title: sh.Name, year: sh.Year(), poster: metadata.PosterURL(sh.PosterPath)}, nil
	case "music":
		var req addArtistRequest
		if json.Unmarshal(body, &req) != nil || !artistID.MatchString(strings.TrimSpace(req.MBID)) {
			return requestInfo{}, errors.New("Pick an artist from the search results.")
		}
		a, err := s.MusicBrainz().GetArtist(ctx, strings.TrimSpace(req.MBID))
		if err != nil {
			return requestInfo{}, err
		}
		return requestInfo{title: a.Name}, nil
	case "book":
		var req addBookRequest
		if json.Unmarshal(body, &req) != nil || strings.TrimSpace(req.OLKey) == "" || strings.TrimSpace(req.Title) == "" {
			return requestInfo{}, errors.New("Pick a book from the search results.")
		}
		if _, ok, _ := s.BookRepo.GetByKey(req.OLKey); ok {
			return requestInfo{inLibrary: true}, nil
		}
		title := req.Title
		if req.Author != "" {
			title += " by " + req.Author
		}
		return requestInfo{title: title, year: req.Year, poster: books.CoverURL(req.CoverID, "M")}, nil
	}
	return requestInfo{}, fmt.Errorf("unknown kind %q", kind)
}

// fileRequest answers an add from an account that has to ask: it is kept as
// a request for an administrator.
func (s *Server) fileRequest(w http.ResponseWriter, r *http.Request, kind string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	info, err := s.describeRequest(ctx, kind, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if info.inLibrary {
		writeError(w, http.StatusConflict, "That's already in the library.")
		return
	}
	by, byline := requester(r)
	if dup, _ := s.Requests.PendingFor(by, kind, info.title); dup {
		writeError(w, http.StatusConflict, "You already asked for "+info.title+". An administrator will look at it.")
		return
	}
	req, err := s.Requests.Create(requests.Request{Kind: kind, Title: info.title, Year: info.year, Poster: info.poster, Payload: string(body), RequestedBy: by})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := requestName(req)
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("Asked for %s%s", name, byline))
	s.notifyEvent("request", "New request: "+name, fmt.Sprintf("%s asked for %s. Approve or decline it under Activity > Requests.", strings.TrimPrefix(byline, " by "), name))
	writeJSON(w, http.StatusAccepted, map[string]any{"requested": true, "request": req, "message": "Sent to an administrator. You'll see the answer under Activity > Requests."})
}

func requestName(r requests.Request) string {
	if r.Year > 0 && r.Kind != "book" {
		return fmt.Sprintf("%s (%d)", r.Title, r.Year)
	}
	return r.Title
}

// GET /api/requests: an administrator sees every request, anyone else their own.
func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	var by int64
	if !isAdminRequest(r) {
		by, _ = requester(r)
	}
	list, err := s.Requests.List(by)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pending := 0
	for _, x := range list {
		if x.Status == requests.Pending {
			pending++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": list, "pending": pending})
}

func (s *Server) requestFromPath(w http.ResponseWriter, r *http.Request) (requests.Request, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a request.")
		return requests.Request{}, false
	}
	req, err := s.Requests.Get(id)
	if errors.Is(err, requests.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That request doesn't exist any more.")
		return requests.Request{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return requests.Request{}, false
	}
	return req, true
}

// POST /api/requests/{id}/approve: add the title as the person asked.
func (s *Server) handleApproveRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := s.requestFromPath(w, r)
	if !ok {
		return
	}
	if req.Status != requests.Pending {
		writeError(w, http.StatusConflict, "This request has already been answered.")
		return
	}
	handler := map[string]http.HandlerFunc{"movie": s.handleAddMovie, "tv": s.handleAddSeries, "music": s.handleAddArtist, "book": s.handleAddBook}[req.Kind]
	if handler == nil {
		writeError(w, http.StatusBadRequest, "This request can't be approved.")
		return
	}
	// The add runs on the person's behalf, so the library says who asked.
	who := auth.UserFromContext(r.Context())
	if req.RequestedBy > 0 {
		if u, err := s.Auth.GetUser(req.RequestedBy); err == nil {
			who = u
		}
	}
	ctx := context.WithValue(auth.WithUser(r.Context(), who), approvingKey{}, true)
	inner := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(req.Payload))).WithContext(ctx)
	inner.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, inner)
	note := ""
	switch {
	case rec.Code < 300:
	case rec.Code == http.StatusConflict:
		note = "It was already in the library."
	default:
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if e.Error == "" {
			e.Error = "The title couldn't be added."
		}
		slog.Info("requests: approve failed", "request", req.ID, "status", rec.Code, "err", e.Error)
		writeError(w, http.StatusBadGateway, e.Error)
		return
	}
	adminID, byline := requester(r)
	if err := s.Requests.Decide(req.ID, requests.Approved, note, adminID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("Request for %s approved%s", requestName(req), byline))
	out, _ := s.Requests.Get(req.ID)
	writeJSON(w, http.StatusOK, out)
}

// POST /api/requests/{id}/decline {"note": "..."}
func (s *Server) handleDeclineRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := s.requestFromPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = decodeJSON(r, &body)
	note := strings.TrimSpace(body.Note)
	if len(note) > 300 {
		note = note[:300]
	}
	if req.Status != requests.Pending {
		writeError(w, http.StatusConflict, "This request has already been answered.")
		return
	}
	adminID, byline := requester(r)
	if err := s.Requests.Decide(req.ID, requests.Declined, note, adminID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Request for %s declined%s", requestName(req), byline))
	out, _ := s.Requests.Get(req.ID)
	writeJSON(w, http.StatusOK, out)
}

// DELETE /api/requests/{id}: someone takes back their own waiting request;
// an administrator can remove any.
func (s *Server) handleDeleteRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := s.requestFromPath(w, r)
	if !ok {
		return
	}
	if !isAdminRequest(r) {
		by, _ := requester(r)
		if req.RequestedBy != by || req.Status != requests.Pending {
			writeError(w, http.StatusForbidden, "You can only take back your own requests that are still waiting.")
			return
		}
	}
	if err := s.Requests.Delete(req.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
