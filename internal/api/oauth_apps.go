package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
)

// The parts of the MCP sign-in that talk to the screen: the consent page and
// the connected-apps card in Settings > Media servers and apps > Other apps. They live apart from
// mcp_oauth.go because their sentences are translated (see tools/msgextract,
// which leaves out the mcp*.go files).

// ---- The consent page's API ----

type oauthRequestInfo struct {
	ClientName   string   `json:"clientName"`
	RedirectHost string   `json:"redirectHost"`
	Scopes       []string `json:"scopes"`
	Account      string   `json:"account"`
	CanApprove   bool     `json:"canApprove"`
}

func (s *Server) handleOAuthRequestInfo(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	st := &s.oauth
	st.mu.Lock()
	p := st.pending[r.PathValue("id")]
	if p != nil && time.Now().After(p.expires) {
		delete(st.pending, r.PathValue("id"))
		p = nil
	}
	st.mu.Unlock()
	if p == nil {
		writeError(w, http.StatusNotFound, "This sign-in request has run out. Go back to the app and start the connection again.")
		return
	}
	writeJSON(w, http.StatusOK, oauthRequestInfo{ClientName: p.clientDisplay, RedirectHost: p.redirectHost, Scopes: p.scopes,
		Account: user.Username, CanApprove: user.IsAdmin})
}

// handleOAuthDecision is the Allow or Deny of the consent page. Only an
// administrator can allow, and only from a signed-in browser (never with an API
// key). It answers with the address to send the browser to.
func (s *Server) handleOAuthDecision(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	user := auth.UserFromContext(r.Context())
	var req struct {
		Allow bool `json:"allow"`
		Write bool `json:"write"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.Allow && !user.IsAdmin {
		writeError(w, http.StatusForbidden, "Only an administrator can let an app connect to Mediarium. Sign in with an administrator account.")
		return
	}
	id := r.PathValue("id")
	st := &s.oauth
	st.mu.Lock()
	p := st.pending[id]
	delete(st.pending, id)
	st.mu.Unlock()
	if p == nil || time.Now().After(p.expires) {
		writeError(w, http.StatusNotFound, "This sign-in request has run out. Go back to the app and start the connection again.")
		return
	}
	if !req.Allow {
		slog.Info("mcp oauth: an app was refused", "client", p.clientDisplay, "by", user.Username)
		writeJSON(w, http.StatusOK, map[string]string{"redirect": redirectWith(p.redirectURI, map[string]string{"error": "access_denied", "error_description": oauthDeniedText, "state": p.state})})
		return
	}
	scope := oauthScopeRead
	wantsWrite := false
	for _, sc := range p.scopes {
		wantsWrite = wantsWrite || sc == oauthScopeWrite
	}
	if req.Write && wantsWrite {
		scope = oauthScopeRead + " " + oauthScopeWrite
	}
	code := oauthRandomID(32)
	now := time.Now()
	st.mu.Lock()
	if st.codes == nil {
		st.codes = map[string]*oauthCode{}
		st.used = map[string]oauthUsed{}
	}
	for k, c := range st.codes {
		if now.After(c.expires) {
			delete(st.codes, k)
		}
	}
	for k, u := range st.used {
		if now.After(u.expires) {
			delete(st.used, k)
		}
	}
	st.codes[oauthHash(code)] = &oauthCode{clientID: p.clientID, redirectURI: p.redirectURI, userID: user.ID, scope: scope, challenge: p.challenge, expires: now.Add(oauthCodeTTL)}
	st.mu.Unlock()
	slog.Info("mcp oauth: an app was allowed", "client", p.clientDisplay, "by", user.Username, "scope", scope)
	writeJSON(w, http.StatusOK, map[string]string{"redirect": redirectWith(p.redirectURI, map[string]string{"code": code, "state": p.state})})
}

// ---- Settings: connected apps and hand-made clients ----

type mcpAppJSON struct {
	ID         int64  `json:"id"`
	ClientName string `json:"clientName"`
	Account    string `json:"account"`
	Scope      string `json:"scope"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt"`
}

type mcpClientJSON struct {
	ClientID     string   `json:"clientId"`
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirectUris"`
	CreatedAt    string   `json:"createdAt"`
}

func (s *Server) handleListMCPApps(w http.ResponseWriter, r *http.Request) {
	grants, err := s.Auth.ListOAuthGrants()
	if err != nil {
		slog.Error("mcp oauth: could not list the apps", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't load the connected apps. Try again.")
		return
	}
	clients, err := s.Auth.ListOAuthClients()
	if err != nil {
		slog.Error("mcp oauth: could not list the clients", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't load the connected apps. Try again.")
		return
	}
	out := struct {
		Apps    []mcpAppJSON    `json:"apps"`
		Clients []mcpClientJSON `json:"clients"`
	}{Apps: []mcpAppJSON{}, Clients: []mcpClientJSON{}}
	for _, g := range grants {
		a := mcpAppJSON{ID: g.ID, ClientName: g.ClientName, Account: g.Username, Scope: g.Scope, CreatedAt: g.CreatedAt.UTC().Format(time.RFC3339)}
		if g.LastUsedAt != nil {
			a.LastUsedAt = g.LastUsedAt.UTC().Format(time.RFC3339)
		}
		out.Apps = append(out.Apps, a)
	}
	for _, c := range clients {
		out.Clients = append(out.Clients, mcpClientJSON{ClientID: c.ID, Name: c.Name, RedirectURIs: c.RedirectURIs, CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevokeMCPApp(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a connected app.")
		return
	}
	ok, err := s.Auth.RevokeOAuthGrantByID(id)
	if err != nil {
		slog.Error("mcp oauth: could not revoke", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't disconnect that app. Try again.")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "That app is not connected any more.")
		return
	}
	slog.Warn("mcp oauth: an app was disconnected", "by", actorName(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleCreateMCPClient makes a client by hand, for programs that ask for a
// client ID and secret (Home Assistant's application credentials). The secret
// is shown once and kept only as a hash.
func (s *Server) handleCreateMCPClient(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	var req struct {
		Name        string `json:"name"`
		RedirectURI string `json:"redirectUri"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	name := cleanClientName(req.Name)
	uri := strings.TrimSpace(req.RedirectURI)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Give the app a name.")
		return
	}
	if err := validRedirectURI(uri, true); err != nil {
		writeError(w, http.StatusBadRequest, "That redirect address can't be used. Use an https address, or the address the app shows.")
		return
	}
	id, err := auth.NewOAuthClientID()
	if err == nil {
		var secret string
		if secret, err = auth.NewOAuthSecret(); err == nil {
			if err = s.Auth.CreateOAuthClient(id, secret, name, []string{uri}, true); err == nil {
				slog.Warn("mcp oauth: a client was registered by hand", "client", name, "by", actorName(r))
				writeJSON(w, http.StatusCreated, map[string]any{"clientId": id, "clientSecret": secret, "name": name, "redirectUris": []string{uri}})
				return
			}
		}
	}
	if errors.Is(err, auth.ErrOAuthTooManyClients) {
		writeError(w, http.StatusConflict, "There are too many registered apps. Remove some first.")
		return
	}
	slog.Error("mcp oauth: could not make a client", "err", err)
	writeError(w, http.StatusInternalServerError, "Couldn't register that app. Try again.")
}

func (s *Server) handleDeleteMCPClient(w http.ResponseWriter, r *http.Request) {
	ok, err := s.Auth.DeleteOAuthClient(r.PathValue("id"))
	if err != nil {
		slog.Error("mcp oauth: could not remove a client", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't remove that app. Try again.")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "That app is not registered any more.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
