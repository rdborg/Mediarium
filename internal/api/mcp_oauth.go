package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	timerate "golang.org/x/time/rate"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
)

// Sign-in for MCP clients that find it by themselves (Home Assistant, chat
// programs): OAuth 2.1 with PKCE as the MCP specification describes it
// (authorization of 2025-06-18). The pieces:
//
//   - /.well-known/oauth-protected-resource and /.well-known/oauth-authorization-server
//     say where to sign in (RFC 9728, RFC 8414);
//   - POST /oauth/register lets a client register itself (RFC 7591), or an
//     administrator makes one by hand for programs that ask for a client ID and
//     secret;
//   - GET /oauth/authorize checks the request and sends the browser to the
//     consent page of the web app (/connect-app), where the administrator signs
//     in the usual way and says yes or no;
//   - POST /oauth/token swaps the code for tokens and refresh tokens for new ones.
//
// The tokens are kept by internal/auth (hashed). The MCP address takes them as
// Bearer tokens next to API keys.

const (
	oauthScopeRead  = "mcp:read"
	oauthScopeWrite = "mcp:write"

	oauthCodeTTL    = 60 * time.Second
	oauthRequestTTL = 10 * time.Minute
	oauthMaxPending = 1000
	oauthMaxBody    = 16 << 10

	// oauthDeniedText goes to the app, not to the screen.
	oauthDeniedText = "The administrator said no."
)

var oauthScopes = []string{oauthScopeRead, oauthScopeWrite}

// oauthState is what the sign-in keeps in memory: requests waiting for an
// answer on the consent page and codes waiting to be swapped for tokens. All of
// it is short-lived, so a restart only means the person starts again.
type oauthState struct {
	mu       sync.Mutex
	pending  map[string]*oauthPending
	codes    map[string]*oauthCode // by the hash of the code
	used     map[string]oauthUsed  // codes already swapped, to spot a second try
	limiters map[string]*timerate.Limiter
}

type oauthPending struct {
	clientID      string
	redirectURI   string
	scopes        []string
	state         string
	challenge     string
	confidential  bool
	expires       time.Time
	redirectHost  string
	clientDisplay string
}

type oauthCode struct {
	clientID    string
	redirectURI string
	userID      int64
	scope       string
	challenge   string
	expires     time.Time
}

type oauthUsed struct {
	grantID int64
	expires time.Time
}

// allowOAuth is a small per-address limit for the endpoints anyone can reach.
func (s *Server) allowOAuth(kind string, r *http.Request, every time.Duration, burst int) bool {
	st := &s.oauth
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.limiters == nil || len(st.limiters) > 5000 {
		st.limiters = map[string]*timerate.Limiter{}
	}
	key := kind + "|" + httpsec.LimitKey(s.clientIP(r))
	l := st.limiters[key]
	if l == nil {
		l = timerate.NewLimiter(timerate.Every(every), burst)
		st.limiters[key] = l
	}
	return l.Allow()
}

func oauthRandomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("oauth: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func oauthHash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// externalBase is the address the caller used to reach Mediarium, with the
// URL base: "https://home.example.com/mediarium". A trusted proxy's
// X-Forwarded-Proto and -Host are honoured the same way as for the e-reader
// addresses.
func (s *Server) externalBase(r *http.Request) string {
	return s.publicBase(r) + s.cfg.URLBase
}

// ---- Replies ----

func oauthCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
	h.Set("Access-Control-Max-Age", "600")
}

// oauthPreflight answers an OPTIONS request and reports whether it did.
func oauthPreflight(w http.ResponseWriter, r *http.Request) bool {
	oauthCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// oauthFail is the JSON error the OAuth specifications ask for.
func oauthFail(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	body := map[string]string{"error": code}
	if description != "" {
		body["error_description"] = description
	}
	writeJSON(w, status, body)
}

// ---- Discovery ----

// handleWellKnown answers everything under /.well-known/. The two OAuth
// documents are served while MCP is on; anything else (and everything while
// MCP is off) is a plain JSON 404, never the web app's HTML, which a client
// looking for OAuth metadata could not read.
func (s *Server) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	if oauthPreflight(w, r) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		oauthFail(w, http.StatusMethodNotAllowed, "invalid_request", "Use GET.")
		return
	}
	name, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/.well-known/"), "/")
	if !s.mcpEnabled() {
		oauthFail(w, http.StatusNotFound, "not_found", "Nothing here.")
		return
	}
	base := s.externalBase(r)
	w.Header().Set("Cache-Control", "no-store")
	switch name {
	case "oauth-protected-resource":
		resource := base + "/mcp"
		if rest == "api/mcp" || strings.HasSuffix(rest, "/api/mcp") {
			resource = base + "/api/mcp"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"resource":                 resource,
			"authorization_servers":    []string{base},
			"bearer_methods_supported": []string{"header"},
			"scopes_supported":         oauthScopes,
			"resource_name":            "Mediarium",
		})
	case "oauth-authorization-server", "openid-configuration":
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/oauth/authorize",
			"token_endpoint":                        base + "/oauth/token",
			"registration_endpoint":                 base + "/oauth/register",
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
			"scopes_supported":                      oauthScopes,
		})
	default:
		oauthFail(w, http.StatusNotFound, "not_found", "Nothing here.")
	}
}

// resourceMetadataURL is where an unauthorised client finds out how to sign in.
func (s *Server) resourceMetadataURL(r *http.Request) string {
	return s.externalBase(r) + "/.well-known/oauth-protected-resource"
}

// ---- Redirect addresses ----

var customSchemePattern = regexp.MustCompile(`^[a-z][a-z0-9+.\-]*$`)

// Schemes a redirect address may never use: they run code or read files in the browser.
var blockedSchemes = map[string]bool{
	"javascript": true, "data": true, "file": true, "vbscript": true, "about": true,
	"blob": true, "ftp": true, "ws": true, "wss": true, "chrome": true, "view-source": true,
}

func isLoopbackHost(h string) bool {
	h = strings.ToLower(strings.Trim(h, "[]"))
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// validRedirectURI checks an address a client wants the browser sent back to:
// https, http to this machine, or an app's own scheme. An administrator
// registering a client by hand may also use plain http to another host (for
// example Home Assistant at http://homeassistant.local:8123).
func validRedirectURI(raw string, byAdmin bool) error {
	if raw == "" || len(raw) > 512 {
		return errors.New("a redirect address must be between 1 and 512 characters")
	}
	for _, c := range raw {
		if c < 0x21 || c == 0x7f {
			return errors.New("a redirect address can't hold spaces or control characters")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("that redirect address isn't a valid address")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return errors.New("a redirect address can't have a # part")
	}
	if u.User != nil {
		return errors.New("a redirect address can't hold a user name or password")
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme == "https":
		if u.Hostname() == "" {
			return errors.New("that redirect address has no host")
		}
	case scheme == "http":
		if u.Hostname() == "" {
			return errors.New("that redirect address has no host")
		}
		if !byAdmin && !isLoopbackHost(u.Hostname()) {
			return errors.New("http redirect addresses are only allowed for localhost and 127.0.0.1; use https")
		}
	case blockedSchemes[scheme] || !customSchemePattern.MatchString(scheme):
		return errors.New("that redirect address uses a scheme that isn't allowed")
	}
	return nil
}

func cleanClientName(v string) string {
	var b strings.Builder
	for _, c := range strings.TrimSpace(v) {
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteRune(c)
	}
	name := b.String()
	if r := []rune(name); len(r) > 100 {
		name = string(r[:100])
	}
	return strings.TrimSpace(name)
}

// ---- /oauth/ ----

// handleOAuth routes everything under /oauth/.
func (s *Server) handleOAuth(w http.ResponseWriter, r *http.Request) {
	if oauthPreflight(w, r) {
		return
	}
	if !s.mcpEnabled() {
		oauthFail(w, http.StatusNotFound, "not_found", "MCP is switched off.")
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/oauth/") {
	case "register":
		s.handleOAuthRegister(w, r)
	case "authorize":
		s.handleOAuthAuthorize(w, r)
	case "token":
		s.handleOAuthToken(w, r)
	default:
		oauthFail(w, http.StatusNotFound, "not_found", "Nothing here.")
	}
}

// handleOAuthRegister is dynamic client registration (RFC 7591): public
// clients, which then prove themselves with PKCE.
func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		oauthFail(w, http.StatusMethodNotAllowed, "invalid_request", "Use POST.")
		return
	}
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
		GrantTypes   []string `json:"grant_types"`
		ResponseType []string `json:"response_types"`
	}
	if err := decodeLimited(r, &req); err != nil {
		oauthFail(w, http.StatusBadRequest, "invalid_client_metadata", "The registration isn't valid JSON.")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 {
		oauthFail(w, http.StatusBadRequest, "invalid_redirect_uri", "Send between 1 and 10 redirect_uris.")
		return
	}
	seen := map[string]bool{}
	for _, u := range req.RedirectURIs {
		if err := validRedirectURI(u, false); err != nil {
			oauthFail(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error()+".")
			return
		}
		if seen[u] {
			oauthFail(w, http.StatusBadRequest, "invalid_redirect_uri", "A redirect address is listed twice.")
			return
		}
		seen[u] = true
	}
	for _, g := range req.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthFail(w, http.StatusBadRequest, "invalid_client_metadata", "Only the authorization_code and refresh_token grants are supported.")
			return
		}
	}
	for _, t := range req.ResponseType {
		if t != "code" {
			oauthFail(w, http.StatusBadRequest, "invalid_client_metadata", "Only the code response type is supported.")
			return
		}
	}
	method := req.AuthMethod
	switch method {
	case "":
		method = "none"
	case "none", "client_secret_post", "client_secret_basic":
	default:
		oauthFail(w, http.StatusBadRequest, "invalid_client_metadata", "That token_endpoint_auth_method isn't supported.")
		return
	}
	name := cleanClientName(req.ClientName)
	if name == "" {
		name = "An app"
	}
	// Only registrations that would be saved count against the limit.
	if !s.allowOAuth("register", r, 6*time.Minute, 10) {
		w.Header().Set("Retry-After", "360")
		oauthFail(w, http.StatusTooManyRequests, "temporarily_unavailable", "Too many registrations from this address. Try again later.")
		return
	}
	id, err := auth.NewOAuthClientID()
	if err != nil {
		oauthFail(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	secret := ""
	if method != "none" {
		if secret, err = auth.NewOAuthSecret(); err != nil {
			oauthFail(w, http.StatusInternalServerError, "server_error", "")
			return
		}
	}
	if err := s.Auth.CreateOAuthClient(id, secret, name, req.RedirectURIs, false); err != nil {
		if errors.Is(err, auth.ErrOAuthTooManyClients) {
			oauthFail(w, http.StatusServiceUnavailable, "temporarily_unavailable", "Too many apps are registered. An administrator can remove unused ones.")
			return
		}
		slog.Error("mcp oauth: could not save a client", "err", err)
		oauthFail(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	slog.Info("mcp oauth: client registered", "client", name, "from", s.clientIP(r))
	out := map[string]any{
		"client_id":                  id,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                name,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": method,
	}
	if secret != "" {
		out["client_secret"] = secret
		out["client_secret_expires_at"] = 0
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, out)
}

func decodeLimited(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, oauthMaxBody)).Decode(v)
}

// oauthErrorPage is shown when the request can't be trusted enough to send the
// browser back to the app (unknown app, wrong redirect address).
func oauthErrorPage(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Mediarium</title>
<style>body{font-family:system-ui,sans-serif;background:#0f1115;color:#e8eaf0;display:grid;place-items:center;min-height:100vh;margin:0;padding:16px}main{max-width:30rem}h1{font-size:1.25rem}p{line-height:1.5;color:#b8bdc9}@media (prefers-color-scheme:light){body{background:#f4f5f8;color:#1b1e26}p{color:#4a5160}}</style></head>
<body><main><h1>This sign-in link can't be used</h1><p>%s</p><p>Go back to the app and start the connection again.</p></main></body></html>`, html.EscapeString(message))
}

// redirectWith sends the browser back to the app with the given parameters.
func redirectWith(redirectURI string, params map[string]string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func parseScopes(raw string) ([]string, bool) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return append([]string(nil), oauthScopes...), true
	}
	var out []string
	for _, f := range fields {
		ok := false
		for _, known := range oauthScopes {
			if f == known {
				ok = true
			}
		}
		if !ok {
			return nil, false
		}
		dup := false
		for _, o := range out {
			dup = dup || o == f
		}
		if !dup {
			out = append(out, f)
		}
	}
	return out, true
}

var pkceValue = regexp.MustCompile(`^[A-Za-z0-9\-._~]{43,128}$`)
var pkceChallenge = regexp.MustCompile(`^[A-Za-z0-9\-_]{43}$`)

// handleOAuthAuthorize checks an authorization request and hands the browser
// to the consent page. The page itself is the web app's (/connect-app): it
// asks the person to sign in first when they aren't, the same way every other
// page does, including single sign-on.
func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, OPTIONS")
		oauthFail(w, http.StatusMethodNotAllowed, "invalid_request", "Use GET.")
		return
	}
	if !s.allowOAuth("authorize", r, time.Second, 30) {
		oauthErrorPage(w, "Too many sign-in attempts from this address. Wait a moment.")
		return
	}
	q := r.URL.Query()
	client, found, err := s.Auth.OAuthClientByID(q.Get("client_id"))
	if err != nil {
		slog.Error("mcp oauth: client lookup failed", "err", err)
		oauthErrorPage(w, "Something went wrong on the server.")
		return
	}
	if !found {
		oauthErrorPage(w, "The app is not known to this Mediarium. It has to register first.")
		return
	}
	redirect := q.Get("redirect_uri")
	if redirect == "" && len(client.RedirectURIs) == 1 {
		redirect = client.RedirectURIs[0]
	}
	match := false
	for _, u := range client.RedirectURIs {
		if u == redirect {
			match = true
		}
	}
	if !match {
		oauthErrorPage(w, "The address the app wants to return to doesn't match the one it registered.")
		return
	}
	state := q.Get("state")
	fail := func(code, desc string) {
		http.Redirect(w, r, redirectWith(redirect, map[string]string{"error": code, "error_description": desc, "state": state}), http.StatusFound)
	}
	if len(state) > 512 {
		fail("invalid_request", "The state value is too long.")
		return
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "Only response_type=code is supported.")
		return
	}
	scopes, ok := parseScopes(q.Get("scope"))
	if !ok {
		fail("invalid_scope", "Supported scopes are mcp:read and mcp:write.")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge != "" {
		if q.Get("code_challenge_method") != "S256" {
			fail("invalid_request", "Only code_challenge_method=S256 is supported.")
			return
		}
		if !pkceChallenge.MatchString(challenge) {
			fail("invalid_request", "The code_challenge isn't valid.")
			return
		}
	} else if !client.Confidential {
		fail("invalid_request", "PKCE is required: send code_challenge with code_challenge_method=S256.")
		return
	}
	host := ""
	if u, err := url.Parse(redirect); err == nil {
		host = u.Host
		if host == "" {
			host = u.Scheme + ":"
		}
	}
	id := oauthRandomID(24)
	st := &s.oauth
	st.mu.Lock()
	now := time.Now()
	if st.pending == nil {
		st.pending = map[string]*oauthPending{}
	}
	for k, p := range st.pending {
		if now.After(p.expires) {
			delete(st.pending, k)
		}
	}
	if len(st.pending) >= oauthMaxPending {
		st.mu.Unlock()
		oauthErrorPage(w, "Too many sign-ins are waiting. Try again in a few minutes.")
		return
	}
	st.pending[id] = &oauthPending{clientID: client.ID, redirectURI: redirect, scopes: scopes, state: state, challenge: challenge,
		confidential: client.Confidential, expires: now.Add(oauthRequestTTL), redirectHost: host, clientDisplay: client.Name}
	st.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, s.cfg.URLBase+"/connect-app?r="+url.QueryEscape(id), http.StatusFound)
}

// ---- Token endpoint ----

// clientCredentials reads who is calling the token endpoint: Basic, or
// client_id and client_secret in the form.
func clientCredentials(r *http.Request) (id, secret string, basic bool) {
	if u, p, ok := r.BasicAuth(); ok {
		id, _ = url.QueryUnescape(u)
		secret, _ = url.QueryUnescape(p)
		return id, secret, true
	}
	return r.PostFormValue("client_id"), r.PostFormValue("client_secret"), false
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		oauthFail(w, http.StatusMethodNotAllowed, "invalid_request", "Use POST.")
		return
	}
	if !s.allowOAuth("token", r, time.Second, 30) {
		w.Header().Set("Retry-After", "2")
		oauthFail(w, http.StatusTooManyRequests, "slow_down", "Too many requests. Wait a moment.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oauthMaxBody)
	if err := r.ParseForm(); err != nil {
		oauthFail(w, http.StatusBadRequest, "invalid_request", "Send the request as application/x-www-form-urlencoded.")
		return
	}
	clientID, secret, basic := clientCredentials(r)
	client, found, err := s.Auth.OAuthClientByID(clientID)
	if err != nil {
		slog.Error("mcp oauth: client lookup failed", "err", err)
		oauthFail(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	if !found || !s.Auth.CheckOAuthClientSecret(clientID, secret) {
		if basic {
			w.Header().Set("WWW-Authenticate", `Basic realm="mediarium"`)
		}
		oauthFail(w, http.StatusUnauthorized, "invalid_client", "The client_id or client_secret isn't right.")
		return
	}
	var tokens *auth.OAuthTokens
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		tokens, err = s.exchangeCode(client, r)
	case "refresh_token":
		tokens, err = s.Auth.RefreshOAuth(client.ID, r.PostFormValue("refresh_token"))
		if errors.Is(err, auth.ErrOAuthInvalidGrant) {
			err = errGrant("The refresh token isn't valid any more. Sign in again.")
		}
	default:
		oauthFail(w, http.StatusBadRequest, "unsupported_grant_type", "Use authorization_code or refresh_token.")
		return
	}
	if err != nil {
		var ge grantError
		if errors.As(err, &ge) {
			oauthFail(w, http.StatusBadRequest, "invalid_grant", string(ge))
			return
		}
		slog.Error("mcp oauth: token request failed", "err", err)
		oauthFail(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  tokens.Access,
		"token_type":    "Bearer",
		"expires_in":    int(tokens.AccessTTL.Seconds()),
		"refresh_token": tokens.Refresh,
		"scope":         tokens.Scope,
	})
}

type grantError string

func (e grantError) Error() string { return string(e) }
func errGrant(m string) error      { return grantError(m) }

// exchangeCode swaps an authorization code for tokens. A code works once: it
// is taken out whatever happens next, and one presented a second time takes
// the grant it made down with it.
func (s *Server) exchangeCode(client *auth.OAuthClient, r *http.Request) (*auth.OAuthTokens, error) {
	code := r.PostFormValue("code")
	hash := oauthHash(code)
	st := &s.oauth
	st.mu.Lock()
	c := st.codes[hash]
	delete(st.codes, hash)
	reused, wasUsed := st.used[hash]
	st.mu.Unlock()
	if wasUsed {
		if reused.grantID > 0 {
			_ = s.Auth.RevokeOAuthGrant(reused.grantID)
			slog.Warn("mcp oauth: a code was used twice, the grant was revoked", "client", client.Name)
		}
		return nil, errGrant("That code was already used.")
	}
	if c == nil || time.Now().After(c.expires) {
		return nil, errGrant("The code is unknown or has run out.")
	}
	if c.clientID != client.ID {
		return nil, errGrant("The code was issued to another app.")
	}
	if ru := r.PostFormValue("redirect_uri"); ru != "" && ru != c.redirectURI {
		return nil, errGrant("redirect_uri isn't the one used to ask for the code.")
	}
	if c.challenge != "" {
		verifier := r.PostFormValue("code_verifier")
		sum := sha256.Sum256([]byte(verifier))
		want := base64.RawURLEncoding.EncodeToString(sum[:])
		if !pkceValue.MatchString(verifier) || subtle.ConstantTimeCompare([]byte(want), []byte(c.challenge)) != 1 {
			return nil, errGrant("The code_verifier doesn't match.")
		}
	}
	tokens, err := s.Auth.CreateOAuthGrant(c.clientID, c.userID, c.scope)
	if err != nil {
		return nil, fmt.Errorf("create grant: %w", err)
	}
	st.mu.Lock()
	if st.used == nil {
		st.used = map[string]oauthUsed{}
	}
	st.used[hash] = oauthUsed{grantID: tokens.GrantID, expires: time.Now().Add(10 * time.Minute)}
	st.mu.Unlock()
	return tokens, nil
}

// oauthOpenPath reports whether a path is one of the token or registration
// addresses, which programs (not browsers with a session) call. They carry no
// cookie, so the cross-site check, meant for browsers acting on a session,
// has nothing to protect there.
func (s *Server) oauthOpenPath(p string) bool {
	p = strings.TrimPrefix(p, s.cfg.URLBase)
	return p == "/oauth/token" || p == "/oauth/register"
}

// handleMCPOther answers the addresses under /mcp/ that don't exist, in JSON
// (the web app's page would be HTML, which a program can't use).
func handleMCPOther(w http.ResponseWriter, r *http.Request) {
	oauthCORS(w)
	oauthFail(w, http.StatusNotFound, "not_found", "The MCP address is /mcp.")
}
