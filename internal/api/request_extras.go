package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/requests"
)

// Request extras: asking for one episode, an age limit per account, and asking
// for a title to be removed. They sit on top of the request flow in
// requests.go: the add an account sent is kept as it was, and an
// administrator approves it or declines it.

// ---- One episode ----

// episodeRef names one episode of a show in an add request.
type episodeRef struct {
	Season int `json:"season"`
	Number int `json:"number"`
}

func (e episodeRef) valid() bool {
	return e.Season >= 0 && e.Season <= 1000 && e.Number >= 1 && e.Number <= 10000
}

// code is "S02E05".
func (e episodeRef) code() string { return fmt.Sprintf("S%02dE%02d", e.Season, e.Number) }

// payloadEpisode returns the episode an add request asks for, if it asks for
// one and not for the whole show.
func payloadEpisode(payload string) (episodeRef, bool) {
	var v struct {
		Episode *episodeRef `json:"episode"`
	}
	if json.Unmarshal([]byte(payload), &v) != nil || v.Episode == nil {
		return episodeRef{}, false
	}
	return *v.Episode, true
}

// describeEpisodeRequest names a request for one episode. The show may be in
// the library already (only the episode is missing then) or not.
func (s *Server) describeEpisodeRequest(ctx context.Context, tmdbID int, ref episodeRef) (requestInfo, error) {
	if !ref.valid() {
		return requestInfo{}, errors.New("Pick an episode from the show's seasons.")
	}
	if sr, ok, _ := s.MovieRepo.GetSeriesByTMDBID(tmdbID); ok {
		ep, err := s.MovieRepo.GetEpisode(sr.ID, ref.Season, ref.Number)
		if errors.Is(err, sql.ErrNoRows) {
			return requestInfo{}, errors.New("That show doesn't have that episode.")
		}
		if err != nil {
			return requestInfo{}, err
		}
		title, poster := s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
		return requestInfo{title: title + " " + ref.code(), poster: poster, inLibrary: ep.Status == library.StatusDownloaded || ep.Monitored}, nil
	}
	sh, err := s.displayTMDB().GetShow(ctx, tmdbID)
	if err != nil {
		return requestInfo{}, err
	}
	eps, err := s.displayTMDB().GetSeason(ctx, tmdbID, ref.Season)
	if err != nil {
		return requestInfo{}, errors.New("That show doesn't have that episode.")
	}
	found := false
	for _, e := range eps {
		if e.Episode == ref.Number {
			found = true
			break
		}
	}
	if !found {
		return requestInfo{}, errors.New("That show doesn't have that episode.")
	}
	return requestInfo{title: sh.Name + " " + ref.code(), poster: metadata.PosterURL(sh.PosterPath)}, nil
}

// monitorOneEpisode makes an episode of a show in the library wanted and
// starts looking for it, as the episode's own monitor switch and Search button
// do. An episode that has its file, or is wanted already, is refused.
func (s *Server) monitorOneEpisode(sr library.Series, ref episodeRef) error {
	ep, err := s.MovieRepo.GetEpisode(sr.ID, ref.Season, ref.Number)
	if errors.Is(err, sql.ErrNoRows) {
		return &removeProblem{http.StatusNotFound, "That show doesn't have that episode."}
	}
	if err != nil {
		return &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	if ep.Status == library.StatusDownloaded {
		return &removeProblem{http.StatusConflict, "That episode is already in the library."}
	}
	if err := s.MovieRepo.SetEpisodeMonitored(ep.ID, true); err != nil {
		return &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	if err := s.MovieRepo.EnableSeriesOfEpisode(ep.ID); err != nil {
		return &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	s.background(func() {
		s.searchEpisodeAgain(tvScope{seriesID: sr.ID, season: ref.Season, episode: ref.Number, specials: ref.Season == 0, force: true})
	})
	return nil
}

// ---- Age limits ----

// ageLimitOf is an account's age limit ("" = none).
func (s *Server) ageLimitOf(userID int64) (limit string, blockUnrated bool) {
	var block int
	if err := s.db.QueryRow(`SELECT max_rating, block_unrated FROM users WHERE id = ?`, userID).Scan(&limit, &block); err != nil {
		return "", false
	}
	return limit, block != 0
}

// certificationOf reads a movie's or show's age rating from TMDB. The client
// keeps what it reads for a few minutes, so asking again for a title that was
// just looked at costs nothing.
func (s *Server) certificationOf(ctx context.Context, kind string, tmdbID int) (string, error) {
	switch kind {
	case "movie":
		d, err := s.displayTMDB().GetMovieDetail(ctx, tmdbID)
		if err != nil {
			return "", err
		}
		return d.Certification(), nil
	case "tv":
		d, err := s.displayTMDB().GetShowFull(ctx, tmdbID)
		if err != nil {
			return "", err
		}
		return d.ContentRating(), nil
	}
	return "", nil
}

// ageRefusal is what an account with an age limit is told when it asks for
// a title: "" when the title is fine.
func (s *Server) ageRefusal(ctx context.Context, userID int64, kind string, tmdbID int) (status int, msg string) {
	if kind != "movie" && kind != "tv" || tmdbID <= 0 {
		return 0, ""
	}
	limit, block := s.ageLimitOf(userID)
	if _, limited := requests.LimitLevel(limit); !limited {
		return 0, ""
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cert, err := s.certificationOf(ctx, kind, tmdbID)
	if err != nil {
		slog.Info("requests: age rating lookup failed", "kind", kind, "tmdb", tmdbID, "err", err)
		return http.StatusBadGateway, "The age rating couldn't be checked right now. Try again in a moment."
	}
	if requests.AgeAllowed(limit, block, cert) {
		return 0, ""
	}
	if _, known := requests.RatingLevel(cert); !known {
		return http.StatusForbidden, "This title has no age rating, and your account may only ask for titles that have one. An administrator can change that in Settings > Account & Access > Requests and reports."
	}
	return http.StatusForbidden, fmt.Sprintf("This title is rated %s, which is above the age limit set for your account. An administrator can change that in Settings > Account & Access > Requests and reports.", strings.TrimSpace(cert))
}

// mayAskForAge answers 403 (or 502 when the rating can't be read) and returns
// false when the caller's age limit stops them adding a title. Administrators,
// and an administrator approving a request, are never stopped.
func (s *Server) mayAskForAge(w http.ResponseWriter, r *http.Request, kind string, tmdbID int) bool {
	u := auth.UserFromContext(r.Context())
	if u == nil || u.IsAdmin || r.Context().Value(approvingKey{}) != nil {
		return true
	}
	if status, msg := s.ageRefusal(r.Context(), u.ID, kind, tmdbID); status != 0 {
		writeError(w, status, msg)
		return false
	}
	return true
}

// ---- Removal requests ----

// removalPayload is what a removal request keeps.
type removalPayload struct {
	LibraryID int64  `json:"libraryId"`
	TMDBID    int    `json:"tmdbId"`
	Reason    string `json:"reason"`
}

func payloadRemoval(payload string) removalPayload {
	var p removalPayload
	_ = json.Unmarshal([]byte(payload), &p)
	return p
}

const maxRemovalReason = 300

// POST /api/requests/removal {"kind": "movie"|"tv", "itemId": 12, "reason": "..."}:
// a member asks for a title in the library to be removed.
func (s *Server) handleCreateRemovalRequest(w http.ResponseWriter, r *http.Request) {
	if isAdminRequest(r) {
		writeError(w, http.StatusBadRequest, "You can remove it yourself from its page.")
		return
	}
	var body struct {
		Kind   string `json:"kind"`
		ItemID int64  `json:"itemId"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &body); err != nil || body.ItemID <= 0 {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	uid, byline := requester(r)
	perm := map[string]string{"movie": auth.PermMovies, "tv": auth.PermTV}[body.Kind]
	p, err := s.Auth.PermissionsOf(uid)
	if err != nil || !p.Allows(perm) {
		writeError(w, http.StatusForbidden, notAllowedMessage)
		return
	}
	var (
		title, poster string
		year, tmdbID  int
	)
	switch body.Kind {
	case "movie":
		m, err := s.MovieRepo.Get(body.ItemID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "That movie isn't in the library.")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if m.CopyOf != 0 {
			writeError(w, http.StatusBadRequest, "Ask for the main movie to be removed, not its second copy.")
			return
		}
		title, poster = s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
		year, tmdbID = m.Year, m.TMDBID
	case "tv":
		sr, err := s.MovieRepo.GetSeries(body.ItemID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "That show isn't in the library.")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sr.CopyOf != 0 {
			writeError(w, http.StatusBadRequest, "Ask for the main show to be removed, not its second copy.")
			return
		}
		title, poster = s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
		year, tmdbID = sr.Year, sr.TMDBID
	default:
		writeError(w, http.StatusBadRequest, "Only movies and shows can be asked to be removed.")
		return
	}
	payload, _ := json.Marshal(removalPayload{LibraryID: body.ItemID, TMDBID: tmdbID, Reason: clip(body.Reason, maxRemovalReason)})

	fileRequestMu.Lock()
	defer fileRequestMu.Unlock()
	open, err := s.Requests.List(0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, x := range open {
		if x.Status == requests.Pending && x.Action == requests.ActionRemove && x.Kind == body.Kind && payloadRemoval(x.Payload).LibraryID == body.ItemID {
			writeError(w, http.StatusConflict, "Someone already asked for that to be removed. An administrator will look at it.")
			return
		}
	}
	req, err := s.Requests.Create(requests.Request{Kind: body.Kind, Title: title, Year: year, Poster: poster, Payload: string(payload), RequestedBy: uid, Action: requests.ActionRemove})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := requestName(req)
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("Asked for %s to be removed%s", name, byline))
	s.notifyEvent("request", "Removal request: "+name, fmt.Sprintf("%s asked for %s to be removed from the library. Approve or decline it under Activity > Requests.", strings.TrimPrefix(byline, " by "), name))
	writeJSON(w, http.StatusAccepted, map[string]any{"requested": true, "request": req, "message": "Sent to an administrator. You'll see the answer under Activity > Requests."})
}

// approveRemoval removes the title a removal request is about, through the
// same path as the Remove button. The files stay unless the administrator
// chose to delete them.
func (s *Server) approveRemoval(w http.ResponseWriter, r *http.Request, req requests.Request) {
	var body struct {
		DeleteFiles bool `json:"deleteFiles"`
	}
	_ = decodeJSON(r, &body)
	p := payloadRemoval(req.Payload)
	if !s.removalTargetExists(req.Kind, p) {
		s.finishRemoval(w, r, req, "It was already gone from the library.")
		return
	}
	var err error
	if req.Kind == "movie" {
		_, err = s.removeMovie(p.LibraryID, body.DeleteFiles)
	} else {
		_, err = s.removeSeries(p.LibraryID, body.DeleteFiles)
	}
	if err != nil {
		writeRemoveError(w, err)
		return
	}
	note := "The files were kept."
	if body.DeleteFiles {
		note = "The files were deleted too."
	}
	s.finishRemoval(w, r, req, note)
}

// removalTargetExists reports whether the title a removal request names is
// still in the library (and is still that title, not a new one with the same id).
func (s *Server) removalTargetExists(kind string, p removalPayload) bool {
	switch kind {
	case "movie":
		m, err := s.MovieRepo.Get(p.LibraryID)
		return err == nil && (p.TMDBID == 0 || m.TMDBID == p.TMDBID)
	case "tv":
		sr, err := s.MovieRepo.GetSeries(p.LibraryID)
		return err == nil && (p.TMDBID == 0 || sr.TMDBID == p.TMDBID)
	}
	return false
}

func (s *Server) finishRemoval(w http.ResponseWriter, r *http.Request, req requests.Request, note string) {
	adminID, byline := requester(r)
	if err := s.Requests.Decide(req.ID, requests.Approved, note, adminID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := requestName(req)
	_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Request to remove %s approved%s", name, byline))
	s.tellPerson(req.RequestedBy, "Your removal request was approved: "+name, name+" was removed from the library.", "/queue")
	out, _ := s.Requests.Get(req.ID)
	writeJSON(w, http.StatusOK, out)
}

// declineRemoval tells the person a removal request was declined.
func (s *Server) declineRemoval(req requests.Request, note string, byline string) {
	name := requestName(req)
	_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Request to remove %s declined%s", name, byline))
	why := ""
	if note != "" {
		why = " Reason: " + sentence(note)
	}
	s.tellPerson(req.RequestedBy, "Your removal request was declined: "+name, name+" stays in the library."+why, "/queue")
}

// ---- Settings ----

// ageLimitBody is one account's age limit in the request settings.
type ageLimitBody struct {
	MaxRating    string `json:"maxRating"`
	BlockUnrated bool   `json:"blockUnrated"`
}

// saveAgeLimits stores age limits {"<account id>": {...}}. Anything the scale
// doesn't know as a limit is refused with a plain message.
func (s *Server) saveAgeLimits(limits map[string]ageLimitBody) (int, string) {
	type row struct {
		id    int64
		limit ageLimitBody
	}
	var rows []row
	for id, l := range limits {
		uid, err := strconv.ParseInt(id, 10, 64)
		if err != nil || uid <= 0 {
			return http.StatusBadRequest, "That isn't an account."
		}
		l.MaxRating = strings.ToUpper(strings.TrimSpace(l.MaxRating))
		if l.MaxRating != "" {
			if _, ok := requests.LimitLevel(l.MaxRating); !ok {
				return http.StatusBadRequest, "Choose an age limit from the list, such as PG-13 or TV-14, or an age such as 12."
			}
		}
		rows = append(rows, row{uid, l})
	}
	for _, x := range rows {
		if _, err := s.db.Exec(`UPDATE users SET max_rating = ?, block_unrated = ? WHERE id = ?`, x.limit.MaxRating, b2i(x.limit.BlockUnrated), x.id); err != nil {
			return http.StatusInternalServerError, err.Error()
		}
	}
	return 0, ""
}
