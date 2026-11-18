package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/settings"
)

// Signing out browsers that sit unused. Settings > People and access > Security
// has a number of days; a browser session not used for that long ends. 0 (the
// default) leaves it off. API keys, calendar feeds and the e-reader passwords
// are not browser sessions and are not touched.

// maxIdleDays is the longest limit that can be set: a browser session ends
// after 7 days in any case, so a longer limit would never apply.
const maxIdleDays = 7

// idleDays is the saved limit in days, 0 when it is off or not a sensible number.
func (s *Server) idleDays() int {
	v, err := s.Settings.Get(settings.KeyAuthIdleDays)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 || n > maxIdleDays {
		return 0
	}
	return n
}

// idleTimeout is idleDays as the duration the sign-in service asks for.
func (s *Server) idleTimeout() time.Duration {
	return time.Duration(s.idleDays()) * 24 * time.Hour
}

// registerIdleSignOut adds the routes and tells the sign-in service where to
// read the limit.
func (s *Server) registerIdleSignOut(admin routeGroup) {
	if s.Auth != nil { // some route tests build a bare server
		s.Auth.SetIdleTimeout(s.idleTimeout)
	}
	admin.HandleFunc("GET /api/auth/idle-signout", s.handleGetIdleSignOut)
	admin.HandleFunc("PUT /api/auth/idle-signout", s.handlePutIdleSignOut)
}

type idleSignOutState struct {
	Days    int `json:"days"`    // 0 = off
	MaxDays int `json:"maxDays"` // the longest limit that can be set
}

func (s *Server) handleGetIdleSignOut(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, idleSignOutState{Days: s.idleDays(), MaxDays: maxIdleDays})
}

func (s *Server) handlePutIdleSignOut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Days *int `json:"days"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Days == nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if *req.Days < 0 || *req.Days > maxIdleDays {
		writeError(w, http.StatusBadRequest, "Choose from 1 to 7 days, or 0 to leave it off.")
		return
	}
	if err := s.Settings.Set(settings.KeyAuthIdleDays, strconv.Itoa(*req.Days), false); err != nil {
		slog.Error("auth: could not save the idle sign-out limit", "err", err)
		writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
		return
	}
	by := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		by = u.Username
	}
	slog.Info("auth: idle sign-out changed", "days", *req.Days, "by", by)
	writeJSON(w, http.StatusOK, idleSignOutState{Days: *req.Days, MaxDays: maxIdleDays})
}

// signInRequired answers a request that has no valid session. A session that
// ended for being idle says so, so the sign-in page can tell the person.
func signInRequired(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrSessionIdle) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"You need to sign in first.","reason":"idle"}` + "\n"))
		return
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// purgeSessions removes the sessions that have run out.
func (s *Server) purgeSessions() {
	if n, err := s.Auth.PurgeSessions(); err != nil {
		slog.Warn("auth: could not remove old sessions", "err", err)
	} else if n > 0 {
		slog.Debug("auth: removed old sessions", "count", n)
	}
}
