package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/settings"
)

// The problem log (Settings > System > Logs and errors). Every route here is
// administrator-only; see protectedRoutes.

// startProblems opens the problem log and makes it the place every part of the
// app reports to (problems.Record).
func (s *Server) startProblems() {
	s.Problems = problems.Open(s.db)
	s.Problems.OnNew = s.problemAlert
	problems.SetDefault(s.Problems)
	s.Usage.OnLimitHit = noteServiceLimit
}

// notifyOnProblems reports the "tell me about errors" switch (off unless set).
func (s *Server) notifyOnProblems() bool {
	v, _ := s.Settings.Get(settings.KeyNotifyOnProblems)
	return v == "1"
}

// problemAlert sends a new error to the notification targets that listen for
// Health, when the person switched that on. It runs after the problem is saved.
func (s *Server) problemAlert(e problems.Entry) {
	if !s.notifyOnProblems() {
		return
	}
	title, message := e.Message, ""
	if h, ok := problems.Lookup(e.Code); ok {
		title = h.Title
		message = h.Explain + " " + h.Try
		if e.Message != "" && e.Message != h.Title {
			message = e.Message + "\n" + message
		}
	}
	if e.Title != "" {
		title += ": " + e.Title
	}
	s.notifyEvent("health", title, message)
}

// problemView is one problem as the page shows it, with its help.
type problemView struct {
	ID           int64  `json:"id"`
	Level        string `json:"level"`
	Area         string `json:"area"`
	AreaLabel    string `json:"areaLabel"`
	Code         string `json:"code"`
	Title        string `json:"title"`   // the plain heading (the message itself for a code without help)
	Message      string `json:"message"` // what was recorded for this one
	Explain      string `json:"explain,omitempty"`
	Try          string `json:"try,omitempty"`
	LinkLabel    string `json:"linkLabel,omitempty"`
	LinkPath     string `json:"linkPath,omitempty"`
	Detail       string `json:"detail,omitempty"`
	ForTitle     string `json:"forTitle,omitempty"` // the movie, show or album
	ForLink      string `json:"forLink,omitempty"`
	DownloadID   int64  `json:"downloadId,omitempty"`
	Count        int    `json:"count"`
	FirstAt      string `json:"firstAt"`
	LastAt       string `json:"lastAt"`
	Read         bool   `json:"read"`
	KnownProblem bool   `json:"known"`
}

func viewOf(e problems.Entry) problemView {
	v := problemView{
		ID: e.ID, Level: string(e.Level), Area: string(e.Area), AreaLabel: problems.AreaLabel(e.Area), Code: e.Code,
		Title: e.Message, Message: e.Message, Detail: e.Detail, ForTitle: e.Title, ForLink: e.Link, DownloadID: e.DownloadID,
		Count: e.Count, FirstAt: e.FirstAt.UTC().Format(time.RFC3339), LastAt: e.LastAt.UTC().Format(time.RFC3339), Read: e.Read,
	}
	if h, ok := problems.Lookup(e.Code); ok {
		v.KnownProblem = true
		v.Title, v.Explain, v.Try, v.LinkLabel, v.LinkPath = h.Title, h.Explain, h.Try, h.LinkLabel, h.LinkPath
	}
	return v
}

// parseDay reads a date filter: a full time (RFC 3339) or a plain day
// (2026-09-30) in the server's time zone. endOfDay makes a plain day mean the
// start of the next one, so "to 30 September" includes the 30th.
func parseDay(v string, endOfDay bool) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation("2006-01-02", v, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a date", v)
	}
	if endOfDay {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}

// problemFilter reads the filters of the page from the query string.
func problemFilter(r *http.Request) (problems.Filter, error) {
	q := r.URL.Query()
	f := problems.Filter{Text: q.Get("q"), Code: q.Get("code")}
	switch lv := q.Get("level"); lv {
	case "", "all":
	case "error", "warning":
		f.Level = problems.Level(lv)
	default:
		return f, fmt.Errorf("the level must be error or warning")
	}
	if a := q.Get("area"); a != "" && a != "all" {
		f.Area = problems.Area(a)
	}
	var err error
	if f.Since, err = parseDay(q.Get("from"), false); err != nil {
		return f, err
	}
	if f.Until, err = parseDay(q.Get("to"), true); err != nil {
		return f, err
	}
	f.UnreadOnly = q.Get("unread") == "1" || q.Get("unread") == "true"
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Offset, _ = strconv.Atoi(q.Get("offset"))
	return f, nil
}

type problemsPayload struct {
	Items            []problemView       `json:"items"`
	Total            int                 `json:"total"`
	Counts           problems.Counts     `json:"counts"`
	Areas            []problems.AreaInfo `json:"areas"`
	NotifyOnProblems bool                `json:"notifyOnProblems"`
	KeepDays         int                 `json:"keepDays"`
}

// handleProblems lists the problems that match the filters, with the counts for
// the cards at the top of the page. With summary=1 it returns only the counts.
func (s *Server) handleProblems(w http.ResponseWriter, r *http.Request) {
	f, err := problemFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	counts, err := s.Problems.Counts(time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := problemsPayload{Items: []problemView{}, Counts: counts, Areas: problems.Areas(), NotifyOnProblems: s.notifyOnProblems(), KeepDays: s.problemKeepDays()}
	if r.URL.Query().Get("summary") != "1" {
		entries, total, err := s.Problems.List(f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out.Total = total
		for _, e := range entries {
			out.Items = append(out.Items, viewOf(e))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type problemsReadRequest struct {
	IDs []int64 `json:"ids"`
	All bool    `json:"all"`
}

// handleProblemsRead marks problems as read: the ones listed, or all of them.
func (s *Server) handleProblemsRead(w http.ResponseWriter, r *http.Request) {
	var req problemsReadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if !req.All && len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "Choose which problems to mark as read.")
		return
	}
	var (
		n   int64
		err error
	)
	if req.All {
		n, err = s.Problems.MarkAllRead(problems.Filter{})
	} else {
		n, err = s.Problems.MarkRead(req.IDs)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts, _ := s.Problems.Counts(time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"marked": n, "counts": counts})
}

// handleProblemsExport sends the problems that match the filters as a plain
// text file, newest first, to attach to a support request.
func (s *Server) handleProblemsExport(w http.ResponseWriter, r *http.Request) {
	f, err := problemFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f.Limit, f.Offset = 200, 0
	var all []problems.Entry
	for {
		page, total, err := s.Problems.List(f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		all = append(all, page...)
		f.Offset += len(page)
		if len(page) == 0 || f.Offset >= total {
			break
		}
	}
	now := time.Now()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mediarium-problems-%s.txt"`, now.Format("2006-01-02")))
	_, _ = w.Write([]byte(problems.Text(all, now)))
}

// handleProblemsNotify switches the notification for new errors on or off.
func (s *Server) handleProblemsNotify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	value := "0"
	if req.Enabled {
		value = "1"
	}
	if err := s.Settings.Set(settings.KeyNotifyOnProblems, value, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifyOnProblems": req.Enabled})
}

// problemKeepDays is how many days problems are kept: 30, or fewer when the
// history setting keeps finished downloads for a shorter time.
func (s *Server) problemKeepDays() int {
	days := problems.KeepDays
	if h := s.historyRetentionDays(); h > 0 && h < days {
		days = h
	}
	return days
}

// pruneProblems removes old problems. The daily clean-up calls it.
func (s *Server) pruneProblems(now time.Time) {
	if s.Problems == nil {
		return
	}
	if _, err := s.Problems.Prune(now, s.problemKeepDays()); err != nil {
		slog.Warn("problems: prune", "err", err) // not worth a row in the problem log itself
	}
}
