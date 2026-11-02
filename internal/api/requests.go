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
	"sync"
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

// mayAddNew reports whether the caller may put a title that isn't in the
// library yet there through a side door, such as picking a release for it in
// Search: administrators always, everyone else only with the permission for
// that kind of media and "add titles themselves". Otherwise it answers 403.
func (s *Server) mayAddNew(w http.ResponseWriter, r *http.Request, kind string) bool {
	u := auth.UserFromContext(r.Context())
	if u == nil || u.IsAdmin || r.Context().Value(approvingKey{}) != nil {
		return true
	}
	p, err := s.Auth.PermissionsOf(u.ID)
	if err != nil || !p.Allows(kind) {
		writeError(w, http.StatusForbidden, notAllowedMessage)
		return false
	}
	if !p.AddDirect {
		writeError(w, http.StatusForbidden, "That title isn't in the library yet. Ask for it from its page first; once an administrator approves it, you can pick a release.")
		return false
	}
	return true
}

type requestInfo struct {
	title, poster string
	year          int
	inLibrary     bool
}

// requestsMu makes approving, declining and taking back a request take
// turns: an approval can take seconds while the title is looked up, and a
// second click must not answer the same request again.
var requestsMu sync.Mutex

// fileRequestMu makes new requests take turns between the duplicate and
// quota checks and saving the request. It is separate from requestsMu so a
// slow approval doesn't hold up people asking for titles.
var fileRequestMu sync.Mutex

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
		m, err := s.displayTMDB().GetMovie(ctx, req.TMDBID)
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
		sh, err := s.displayTMDB().GetShow(ctx, req.TMDBID)
		if err != nil {
			return requestInfo{}, err
		}
		return requestInfo{title: sh.Name, year: sh.Year(), poster: metadata.PosterURL(sh.PosterPath)}, nil
	case "music":
		var req addArtistRequest
		if json.Unmarshal(body, &req) != nil || !artistID.MatchString(strings.TrimSpace(req.MBID)) {
			return requestInfo{}, errors.New("Pick an artist from the search results.")
		}
		mbid := strings.TrimSpace(req.MBID)
		if _, ok, _ := s.MusicRepo.GetArtistByMBID(mbid); ok {
			return requestInfo{inLibrary: true}, nil
		}
		a, err := s.MusicBrainz().GetArtist(ctx, mbid)
		if err != nil {
			return requestInfo{}, err
		}
		title := a.Name
		if t := strings.TrimSpace(req.AlbumTitle); t != "" && req.AlbumMBID != "" {
			title += " – " + t
		}
		return requestInfo{title: title}, nil
	case "book":
		var req addBookRequest
		if json.Unmarshal(body, &req) != nil || strings.TrimSpace(req.OLKey) == "" || strings.TrimSpace(req.Title) == "" {
			return requestInfo{}, errors.New("Pick a book from the search results.")
		}
		if !req.Ebook && !req.Audiobook {
			return requestInfo{}, errors.New("Choose ebook, audiobook or both.")
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

// requestKey is what a request is for, from the add it carries: the title's
// id where it comes from ("movie:tmdb:603", "music:<mbid>/<album mbid>",
// "book:OL27448W"). Two requests with the same key ask for the same thing.
func requestKey(kind, payload string) string {
	var v struct {
		TMDBID    int    `json:"tmdbId"`
		MBID      string `json:"mbid"`
		AlbumMBID string `json:"albumMbid"`
		OLKey     string `json:"olKey"`
		FourK     bool   `json:"fourK"`
	}
	if json.Unmarshal([]byte(payload), &v) != nil {
		return ""
	}
	switch kind {
	case "movie", "tv":
		if v.TMDBID > 0 {
			if v.FourK { // the 4K copy is its own request
				return fmt.Sprintf("%s:tmdb:%d:4k", kind, v.TMDBID)
			}
			return fmt.Sprintf("%s:tmdb:%d", kind, v.TMDBID)
		}
	case "music":
		if mbid := strings.ToLower(strings.TrimSpace(v.MBID)); mbid != "" {
			return "music:" + mbid + "/" + strings.ToLower(strings.TrimSpace(v.AlbumMBID))
		}
	case "book":
		if k := strings.TrimSpace(v.OLKey); k != "" {
			return "book:" + k
		}
	}
	return ""
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
	fourK := payloadFourK(body)
	if fourK && !s.fourKReady(kind) {
		writeError(w, http.StatusBadRequest, "Asking for a 4K copy isn't available here.")
		return
	}
	info, err := s.describeRequest(ctx, kind, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if info.inLibrary && fourK {
		// A title that is in the library can still be asked for in 4K.
		tmdbID := payloadTMDBID(string(body))
		if s.hasFourKCopy(kind, tmdbID) {
			writeError(w, http.StatusConflict, "That already has a 4K copy.")
			return
		}
		info, _ = s.describeLibraryTitle(kind, tmdbID)
	} else if info.inLibrary {
		writeError(w, http.StatusConflict, "That's already in the library.")
		return
	}
	by, byline := requester(r)
	// The duplicate check, the quota and the new request go together, so many
	// requests sent at once can't all slip past the checks. The slow lookup
	// above stays outside.
	fileRequestMu.Lock()
	req, ok := s.checkAndFileRequest(w, kind, string(body), info, by, fourK)
	fileRequestMu.Unlock()
	if !ok {
		return
	}
	name := requestName(req)
	if fourK {
		name += " in 4K"
	}
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("Asked for %s%s", name, byline))
	s.notifyEvent("request", "New request: "+name, fmt.Sprintf("%s asked for %s. Approve or decline it under Activity > Requests.", strings.TrimPrefix(byline, " by "), name))
	writeJSON(w, http.StatusAccepted, map[string]any{"requested": true, "request": req, "message": "Sent to an administrator. You'll see the answer under Activity > Requests."})
}

// checkAndFileRequest refuses a request the person already sent, or one over
// their quota, and otherwise saves it. The caller holds fileRequestMu.
func (s *Server) checkAndFileRequest(w http.ResponseWriter, kind, body string, info requestInfo, by int64, fourK bool) (requests.Request, bool) {
	mine, err := s.Requests.List(by)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return requests.Request{}, false
	}
	key := requestKey(kind, body)
	for _, x := range mine {
		if x.Status == requests.Pending && x.Kind == kind && key != "" && requestKey(x.Kind, x.Payload) == key {
			writeError(w, http.StatusConflict, fmt.Sprintf("You already asked for %s. An administrator will look at it.", info.title))
			return requests.Request{}, false
		}
	}
	if q := s.requestQuota(by); q.Limit > 0 && q.Left == 0 {
		writeError(w, http.StatusTooManyRequests, quotaRefusal(q, time.Now()))
		return requests.Request{}, false
	}
	req, err := s.Requests.Create(requests.Request{Kind: kind, Title: info.title, Year: info.year, Poster: info.poster, Payload: body, RequestedBy: by, FourK: fourK})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return requests.Request{}, false
	}
	return req, true
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

// requestKindOn reports whether the media type a request is for is switched on.
func (s *Server) requestKindOn(kind string) bool {
	switch kind {
	case "movie":
		return s.moviesEnabled()
	case "tv":
		return s.tvEnabled()
	case "music":
		return s.musicEnabled()
	case "book":
		return s.booksEnabled()
	}
	return false
}

// POST /api/requests/{id}/approve: add the title as the person asked.
func (s *Server) handleApproveRequest(w http.ResponseWriter, r *http.Request) {
	requestsMu.Lock()
	defer requestsMu.Unlock()
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
	if !s.requestKindOn(req.Kind) {
		writeError(w, http.StatusConflict, "That kind of media is switched off under Settings > Media types. Switch it on to approve this request.")
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
	note := ""
	if req.FourK && s.requestInLibrary(r.Context(), req) {
		// The title is in the library already: only its 4K copy is left to add.
		rec.Code = http.StatusOK
	} else {
		handler(rec, inner)
	}
	switch {
	case rec.Code < 300:
	case rec.Code == http.StatusConflict && s.requestInLibrary(r.Context(), req):
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
		status := http.StatusBadGateway
		if rec.Code < 500 {
			status = http.StatusConflict
		}
		writeError(w, status, e.Error)
		return
	}
	if req.FourK {
		copyNote, err := s.addFourKCopy(req)
		if err != nil {
			slog.Info("requests: 4K copy failed", "request", req.ID, "err", err)
			writeError(w, http.StatusConflict, sentence(err.Error()))
			return
		}
		if copyNote != "" {
			note = strings.TrimSpace(note + " " + copyNote)
		}
	}
	adminID, byline := requester(r)
	if err := s.Requests.Decide(req.ID, requests.Approved, note, adminID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	label := requestLabel(req)
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("Request for %s approved%s", label, byline))
	s.tellPerson(req.RequestedBy, "Your request was approved: "+label, label+" was approved. You'll get another message when it's ready to watch.", requestPath(req))
	out, _ := s.Requests.Get(req.ID)
	writeJSON(w, http.StatusOK, out)
}

// requestInLibrary reports whether what req asks for is in the library now.
func (s *Server) requestInLibrary(ctx context.Context, req requests.Request) bool {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	info, err := s.describeRequest(ctx, req.Kind, []byte(req.Payload))
	return err == nil && info.inLibrary
}

// POST /api/requests/{id}/decline {"note": "..."}
func (s *Server) handleDeclineRequest(w http.ResponseWriter, r *http.Request) {
	requestsMu.Lock()
	defer requestsMu.Unlock()
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
	_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Request for %s declined%s", requestLabel(req), byline))
	why := ""
	if note != "" {
		why = " Reason: " + sentence(note)
	}
	s.tellPerson(req.RequestedBy, "Your request was declined: "+requestLabel(req), requestLabel(req)+" was declined."+why, "/queue")
	out, _ := s.Requests.Get(req.ID)
	writeJSON(w, http.StatusOK, out)
}

// DELETE /api/requests/{id}: someone takes back their own waiting request;
// an administrator can remove any.
func (s *Server) handleDeleteRequest(w http.ResponseWriter, r *http.Request) {
	requestsMu.Lock()
	defer requestsMu.Unlock()
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
