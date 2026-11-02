package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// "Report a problem with this title": anyone signed in can tell the
// administrators that a movie or show in the library has a wrong file, no
// sound, bad subtitles or another problem. The reports are listed under
// Activity > Reports. An administrator marks one resolved with a comment, and
// the person who reported it is told (when messages to people are on). Off
// until an administrator switches it on in Settings > Accounts > All accounts.

type issue struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"` // movie or tv
	ItemID     int64  `json:"itemId"`
	TMDBID     int    `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	Poster     string `json:"poster,omitempty"`
	Category   string `json:"category"`
	Episode    string `json:"episode,omitempty"`
	Message    string `json:"message,omitempty"`
	ReportedBy int64  `json:"reportedBy,omitempty"`
	Reporter   string `json:"reporter,omitempty"`
	Status     string `json:"status"` // open or resolved
	Comment    string `json:"comment,omitempty"`
	CreatedAt  string `json:"createdAt"`
	ResolvedAt string `json:"resolvedAt,omitempty"`
}

// issueCategories are what a report can be about, with the words shown for each.
var issueCategories = map[string]string{
	"wrong-file":    "Wrong file",
	"no-sound":      "No sound or wrong audio",
	"bad-subtitles": "Bad subtitles",
	"bad-picture":   "Bad picture",
	"other":         "Something else",
}

const (
	maxIssueMessage  = 500
	maxOpenPerPerson = 20
)

const issueColumns = `i.id, i.kind, i.item_id, i.tmdb_id, i.title, i.year, i.poster, i.category, i.episode, i.message, COALESCE(i.reported_by, 0),
	COALESCE(NULLIF(TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), u.username, ''),
	i.status, i.comment, i.created_at, i.resolved_at`

func scanIssue(sc interface{ Scan(...any) error }) (issue, error) {
	var i issue
	err := sc.Scan(&i.ID, &i.Kind, &i.ItemID, &i.TMDBID, &i.Title, &i.Year, &i.Poster, &i.Category, &i.Episode, &i.Message, &i.ReportedBy, &i.Reporter,
		&i.Status, &i.Comment, &i.CreatedAt, &i.ResolvedAt)
	return i, err
}

func (s *Server) getIssue(id int64) (issue, error) {
	return scanIssue(s.db.QueryRow(`SELECT `+issueColumns+` FROM issues i LEFT JOIN users u ON u.id = i.reported_by WHERE i.id = ?`, id))
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// GET /api/issues: an administrator sees every report, anyone else their own.
func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	q := `SELECT ` + issueColumns + ` FROM issues i LEFT JOIN users u ON u.id = i.reported_by`
	var args []any
	if !isAdminRequest(r) {
		by, _ := requester(r)
		q += ` WHERE i.reported_by = ?`
		args = append(args, by)
	}
	q += ` ORDER BY CASE i.status WHEN 'open' THEN 0 ELSE 1 END, i.created_at DESC, i.id DESC LIMIT 500`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []issue{}
	open := 0
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if i.Status == "open" {
			open++
		}
		out = append(out, i)
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": out, "open": open, "enabled": s.requestOptions().Reports})
}

// POST /api/issues {"kind": "movie", "itemId": 12, "category": "no-sound", "episode": "S01E02", "message": "..."}
func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	if !s.requestOptions().Reports {
		writeError(w, http.StatusConflict, "Reporting problems is switched off. An administrator can switch it on in Settings > Accounts > Requests and reports.")
		return
	}
	var body struct {
		Kind     string `json:"kind"`
		ItemID   int64  `json:"itemId"`
		Category string `json:"category"`
		Episode  string `json:"episode"`
		Message  string `json:"message"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	if _, ok := issueCategories[body.Category]; !ok {
		writeError(w, http.StatusBadRequest, "Choose what is wrong.")
		return
	}
	message := clip(body.Message, maxIssueMessage)
	if body.Category == "other" && message == "" {
		writeError(w, http.StatusBadRequest, "Say what is wrong.")
		return
	}
	in := issue{Kind: body.Kind, ItemID: body.ItemID, Category: body.Category, Episode: clip(body.Episode, 40), Message: message}
	switch body.Kind {
	case "movie":
		m, err := s.MovieRepo.Get(body.ItemID)
		if errors.Is(err, sql.ErrNoRows) || err != nil {
			writeError(w, http.StatusNotFound, "That movie isn't in the library.")
			return
		}
		title, poster := s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
		in.TMDBID, in.Title, in.Year, in.Poster = m.TMDBID, title, m.Year, poster
	case "tv":
		sr, err := s.MovieRepo.GetSeries(body.ItemID)
		if err != nil {
			writeError(w, http.StatusNotFound, "That show isn't in the library.")
			return
		}
		title, poster := s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
		in.TMDBID, in.Title, in.Year, in.Poster = sr.TMDBID, title, sr.Year, poster
	default:
		writeError(w, http.StatusBadRequest, "Reports are for movies and shows.")
		return
	}
	by, _ := requester(r)
	var open, same int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM issues WHERE COALESCE(reported_by, 0) = ? AND status = 'open'`, by).Scan(&open)
	if open >= maxOpenPerPerson {
		writeError(w, http.StatusTooManyRequests, "You have a lot of open reports. Wait until some are answered.")
		return
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM issues WHERE COALESCE(reported_by, 0) = ? AND status = 'open' AND kind = ? AND item_id = ? AND category = ? AND episode = ?`,
		by, in.Kind, in.ItemID, in.Category, in.Episode).Scan(&same)
	if same > 0 {
		writeError(w, http.StatusConflict, "You already reported that. An administrator will look at it.")
		return
	}
	var byArg any
	if by > 0 {
		byArg = by
	}
	res, err := s.db.Exec(`INSERT INTO issues (kind, item_id, tmdb_id, title, year, poster, category, episode, message, reported_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Kind, in.ItemID, in.TMDBID, in.Title, in.Year, in.Poster, in.Category, in.Episode, in.Message, byArg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	out, _ := s.getIssue(id)
	_, byline := requester(r)
	name := in.Title
	if in.Year > 0 {
		name = fmt.Sprintf("%s (%d)", in.Title, in.Year)
	}
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("Problem reported with %s: %s%s", name, strings.ToLower(issueCategories[in.Category]), byline))
	text := fmt.Sprintf("%s: %s.", strings.TrimPrefix(byline, " by "), issueCategories[in.Category])
	if in.Episode != "" {
		text += " Episode: " + in.Episode + "."
	}
	if in.Message != "" {
		text += " " + in.Message
	}
	s.notifyEvent("request", "Problem reported: "+name, text+" See it under Activity > Reports.")
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) issueFromPath(w http.ResponseWriter, r *http.Request) (issue, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a report.")
		return issue{}, false
	}
	i, err := s.getIssue(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That report doesn't exist any more.")
		return i, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return i, false
	}
	return i, true
}

// POST /api/issues/{id}/resolve {"comment": "..."}
func (s *Server) handleResolveIssue(w http.ResponseWriter, r *http.Request) {
	i, ok := s.issueFromPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Comment string `json:"comment"`
	}
	_ = decodeJSON(r, &body)
	comment := clip(body.Comment, maxIssueMessage)
	by, byline := requester(r)
	var byArg any
	if by > 0 {
		byArg = by
	}
	if _, err := s.db.Exec(`UPDATE issues SET status = 'resolved', comment = ?, resolved_at = ?, resolved_by = ? WHERE id = ?`,
		comment, time.Now().UTC().Format(time.RFC3339), byArg, i.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("Report about %s marked resolved%s", i.Title, byline))
	if i.Status != "resolved" {
		msg := "An administrator marked your report about " + i.Title + " as resolved."
		if comment != "" {
			msg += " They wrote: " + comment
		}
		path := "/title/" + strconv.Itoa(i.TMDBID)
		if i.Kind == "tv" {
			path = "/show/" + strconv.Itoa(i.TMDBID)
		}
		s.tellPerson(i.ReportedBy, "Your report about "+i.Title+" was answered", msg, path)
	}
	out, _ := s.getIssue(i.ID)
	writeJSON(w, http.StatusOK, out)
}

// POST /api/issues/{id}/reopen
func (s *Server) handleReopenIssue(w http.ResponseWriter, r *http.Request) {
	i, ok := s.issueFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.db.Exec(`UPDATE issues SET status = 'open', resolved_at = '', resolved_by = NULL WHERE id = ?`, i.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, _ := s.getIssue(i.ID)
	writeJSON(w, http.StatusOK, out)
}

// DELETE /api/issues/{id}
func (s *Server) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	i, ok := s.issueFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.db.Exec(`DELETE FROM issues WHERE id = ?`, i.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
