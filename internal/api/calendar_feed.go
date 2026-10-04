package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
)

// The calendar feed: an .ics address calendar apps subscribe to, so releases
// and episode airs show up in Google, Apple or Outlook calendars. It needs no
// sign-in, so the address itself is the secret: each account has its own,
// can see it again, make a new one (the old one stops working) or turn it off.

type calendarFeedPayload struct {
	On   bool   `json:"on"`
	Path string `json:"path,omitempty"` // "/api/calendar/feed/<token>.ics"; the page adds the address it is opened at
}

func feedPath(token string) string { return "/api/calendar/feed/" + token + ".ics" }

func (s *Server) handleGetCalendarFeed(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	token, err := s.Auth.CalendarFeed(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if token == "" {
		writeJSON(w, http.StatusOK, calendarFeedPayload{})
		return
	}
	writeJSON(w, http.StatusOK, calendarFeedPayload{On: true, Path: feedPath(token)})
}

func (s *Server) handleNewCalendarFeed(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	token, err := s.Auth.NewCalendarFeed(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, calendarFeedPayload{On: true, Path: feedPath(token)})
}

func (s *Server) handleRemoveCalendarFeed(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	if err := s.Auth.RemoveCalendarFeed(u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, calendarFeedPayload{})
}

// handleCalendarFeed serves the .ics file. It is outside sign-in: the token in
// the address is what lets it in.
func (s *Server) handleCalendarFeed(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(r.PathValue("file"), ".ics")
	if _, err := s.Auth.UserForCalendarFeed(token); err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	entries, err := s.calendarEntries()
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	if albums, err := s.albumCalendarEntries(); err == nil && len(albums) > 0 {
		entries = append(entries, albums...)
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].ReleaseDate < entries[j].ReleaseDate })
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(renderICS(entries, time.Now())))
}

// renderICS writes the entries as an iCalendar file: one all-day event per
// release or air date.
func renderICS(entries []calendarEntryPayload, now time.Time) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(foldICS(s)) }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Mediarium//Calendar//EN")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	line("X-WR-CALNAME:Mediarium")
	stamp := now.UTC().Format("20060102T150405Z")
	for _, e := range entries {
		day, err := time.Parse("2006-01-02", e.ReleaseDate)
		if err != nil {
			continue
		}
		line("BEGIN:VEVENT")
		line(fmt.Sprintf("UID:%s-%d@mediarium", e.Kind, e.ID))
		line("DTSTAMP:" + stamp)
		line("DTSTART;VALUE=DATE:" + day.Format("20060102"))
		line("DTEND;VALUE=DATE:" + day.AddDate(0, 0, 1).Format("20060102"))
		line("SUMMARY:" + escapeICS(icsSummary(e)))
		line("DESCRIPTION:" + escapeICS(icsStatus(e.Status)))
		line("TRANSP:TRANSPARENT")
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return b.String()
}

func icsSummary(e calendarEntryPayload) string {
	switch e.Kind {
	case "episode":
		if e.Subtitle != "" {
			return e.Title + " " + strings.ReplaceAll(e.Subtitle, " · ", " - ")
		}
		return fmt.Sprintf("%s S%02dE%02d", e.Title, e.Season, e.Episode)
	case "album":
		if e.Subtitle != "" {
			return e.Subtitle + " - " + e.Title
		}
	}
	return e.Title
}

func icsStatus(status string) string {
	switch status {
	case "downloaded":
		return "Downloaded"
	case "downloading":
		return "Downloading"
	}
	return "Not downloaded yet"
}

// escapeICS escapes text for an iCalendar value.
func escapeICS(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", "").Replace(s)
}

// foldICS ends a content line with CRLF, folding it at 75 bytes as the format
// asks (without cutting a character in half).
func foldICS(s string) string {
	var b strings.Builder
	for len(s) > 75 {
		cut := 75
		for cut > 0 && (s[cut]&0xC0) == 0x80 {
			cut--
		}
		b.WriteString(s[:cut] + "\r\n ")
		s = s[cut:]
	}
	b.WriteString(s + "\r\n")
	return b.String()
}
