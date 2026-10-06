package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/settings"
)

// Sign-in through a reverse proxy. A proxy that does the login itself
// (Authelia, Authentik, Cloudflare Access...) can pass the user name on in a
// header, and Mediarium signs that account in. It is off until an
// administrator names the header, it is only believed from a proxy listed in
// TRUSTED_PROXIES, and it only ever signs in an account that already exists.

// proxyHeader is the header name set under Settings > Accounts ("" = off).
func (s *Server) proxyHeader() string {
	v, err := s.Settings.Get(settings.KeyAuthProxyHeader)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// proxyUser is the account a trusted proxy says is signed in, or nil.
func (s *Server) proxyUser(r *http.Request) *auth.User {
	header := s.proxyHeader()
	if header == "" || !s.sec().proxies.FromTrustedProxy(r) {
		return nil
	}
	name := strings.TrimSpace(r.Header.Get(header))
	if name == "" {
		return nil
	}
	u, err := s.Auth.UserByName(name)
	if err != nil {
		slog.Warn("auth: the proxy named an account that does not exist", "header", header, "from", s.clientIP(r))
		return nil
	}
	return u
}

// signedIn puts the caller's account in the request context: from an API key
// or session cookie first, then from a trusted proxy's header. Anyone else
// gets 401.
func (s *Server) signedIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.Auth.UserFromRequest(r)
		if err != nil && r.Header.Get("X-API-Key") == "" {
			user = s.proxyUser(r)
		}
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
	})
}

type proxySignInState struct {
	Header string `json:"header"` // "" = off
	// For setting it up: whether this request came through a trusted proxy,
	// and what it put in the header (empty when the header is not set or not
	// sent).
	ViaTrustedProxy bool   `json:"viaTrustedProxy"`
	Seen            string `json:"seen"`
}

func (s *Server) proxySignInState(r *http.Request) proxySignInState {
	st := proxySignInState{Header: s.proxyHeader(), ViaTrustedProxy: s.sec().proxies.FromTrustedProxy(r)}
	if st.Header != "" && st.ViaTrustedProxy {
		st.Seen = strings.TrimSpace(r.Header.Get(st.Header))
	}
	return st
}

func (s *Server) handleGetProxySignIn(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.proxySignInState(r))
}

// validProxyHeader reports whether name can be an HTTP header name, and is not
// one a browser or Mediarium itself sets.
func validProxyHeader(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	switch strings.ToLower(name) {
	case "host", "cookie", "authorization", "x-api-key", "origin", "referer", "user-agent", "x-forwarded-for", "x-forwarded-proto", "x-forwarded-host":
		return false
	}
	return true
}

func (s *Server) handlePutProxySignIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Header string `json:"header"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	header := strings.TrimSpace(req.Header)
	actor := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		actor = u.Username
	}
	if header != "" && r.Header.Get("X-API-Key") != "" {
		slog.Warn("auth: an API key tried to switch on sign-in through the proxy", "by", actor, "from", s.clientIP(r))
		writeError(w, http.StatusForbidden, "Sign-in through your proxy can only be turned on from Settings while signed in, not with an API key.")
		return
	}
	if header != "" && !validProxyHeader(header) {
		writeError(w, http.StatusBadRequest, "That isn't a header name Mediarium can use. It looks like Remote-User: letters, numbers and dashes.")
		return
	}
	if err := s.Settings.Set(settings.KeyAuthProxyHeader, header, false); err != nil {
		slog.Error("auth: could not save the proxy header", "err", err)
		writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
		return
	}
	if header == "" {
		slog.Info("auth: sign-in through the proxy switched off", "by", actor)
	} else {
		slog.Warn("auth: sign-in through the proxy switched on", "by", actor, "header", header, "from", s.clientIP(r))
	}
	writeJSON(w, http.StatusOK, s.proxySignInState(r))
}
