package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/oidcauth"
	"github.com/rdborg/mediarium/internal/settings"
)

// Single sign-on with an OpenID Connect provider. The browser goes to the
// provider (start), the provider sends it back (callback), and Mediarium
// checks the answer (internal/oidcauth) before it turns it into a session.
//
// The callback address is made from the public address in the settings, never
// from the request, so a forged Host header cannot make the provider send a
// code anywhere else. A sign-in that fails sends the browser back to the
// sign-in page with a short code (?signin_error=...), never text from the
// provider.

// oidcProvider is the name accounts use for their link to a provider.
const oidcProvider = auth.SourceOIDC

// Sign-in problems the page knows how to word. Anything else is "failed".
const (
	errOff           = "off"         // single sign-on is not switched on
	errNoAccount     = "no_account"  // no account, and none is made automatically
	errNotAllowed    = "not_allowed" // the group or email domain is not allowed
	errDisabled      = "disabled"    // the account is turned off
	errEmailUnver    = "email_unverified"
	errLinkAdmin     = "link_admin"
	errAmbiguous     = "ambiguous"
	errLinkedAlready = "linked_already"
	errLimit         = "limit"
	errFailed        = "failed"
)

// oidcSubject is what an account is tied to: the issuer and the subject, so a
// second provider (or the same address after a change of provider) can never
// land on the same account.
func oidcSubject(issuer, sub string) string { return strings.TrimRight(issuer, "/") + "|" + sub }

func (s *Server) oidcRedirect() (string, error) {
	return oidcauth.RedirectURL(s.configuredPublicURL())
}

// bindingCookie holds the proof, for one started sign-in, that the browser
// which finishes it is the one that started it. Lax, not Strict: the browser
// returns from the provider on a link from another site, and a Strict cookie is
// not sent then.
func (s *Server) setBindingCookie(w http.ResponseWriter, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: oidcauth.CookieName, Value: value, Path: s.oidcCookiePath(), HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(oidcauth.StateTTL.Seconds()),
	})
}

func (s *Server) clearBindingCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: oidcauth.CookieName, Value: "", Path: s.oidcCookiePath(), HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// back sends the browser to the sign-in page with a problem code.
func (s *Server) back(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, s.cfg.URLBase+"/?signin_error="+url.QueryEscape(code), http.StatusSeeOther)
}

// oidcCookiePath is where the browser sends the binding cookie: the sign-in
// routes, under the URL base when there is one.
func (s *Server) oidcCookiePath() string {
	return strings.TrimRight(s.cookiePath(), "/") + "/api/auth/oidc"
}

// GET /api/auth/oidc/start?next=/path: begins a sign-in.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.oidcActive()
	if !ok {
		s.back(w, r, errOff)
		return
	}
	ip := s.clientIP(r)
	if !s.sso().starts.Allow(httpsec.LimitKey(ip)) {
		s.back(w, r, errLimit)
		return
	}
	s.sso().starts.RecordFailure(httpsec.LimitKey(ip)) // every start counts; the limit is generous
	redirect, err := s.oidcRedirect()
	if err != nil {
		s.back(w, r, errOff)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	flow, err := s.sso().oidc.Begin(ctx, cfg, redirect, oidcauth.ModeSignIn, 0, r.URL.Query().Get("next"))
	if err != nil {
		slog.Error("auth: could not start a single sign-on", "err", err)
		s.back(w, r, errFailed)
		return
	}
	s.setBindingCookie(w, flow.Binding, s.isHTTPS(r))
	http.Redirect(w, r, flow.URL, http.StatusSeeOther)
}

// GET /api/auth/oidc/callback: the provider sends the browser back here.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	secure := s.isHTTPS(r)
	binding := ""
	if c, err := r.Cookie(oidcauth.CookieName); err == nil {
		binding = c.Value
	}
	s.clearBindingCookie(w, secure) // one use, whatever happens

	cfg, ok := s.oidcActive()
	if !ok {
		s.back(w, r, errOff)
		return
	}
	ip := s.clientIP(r)
	ipKey := httpsec.LimitKey(ip)
	if !s.LoginLimiter.Allow(ipKey) {
		s.back(w, r, errLimit)
		return
	}
	redirect, err := s.oidcRedirect()
	if err != nil {
		s.back(w, r, errOff)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	res, err := s.sso().oidc.Finish(ctx, cfg, redirect, r.URL.Query(), binding)
	if err != nil {
		var oe *oidcauth.Error
		code := errFailed
		if errors.As(err, &oe) {
			code = oe.Code
			// A person saying no at the provider is not an attack; everything else counts like a wrong password.
			if !(oe.Code == oidcauth.CodeProvider && r.URL.Query().Get("error") == "access_denied") {
				s.LoginLimiter.RecordFailure(ipKey)
			}
		}
		slog.Warn("auth: single sign-on failed", "code", code, "ip", ip, "reason", logReason(err))
		s.back(w, r, code)
		return
	}

	if res.Mode == oidcauth.ModeLink {
		s.finishOIDCLink(w, r, cfg, res)
		return
	}
	user, code := s.accountForOIDC(cfg, res)
	if code != "" {
		slog.Warn("auth: single sign-on refused", "code", code, "ip", ip)
		s.back(w, r, code)
		return
	}
	s.LoginLimiter.RecordSuccess(ipKey)
	if err := s.Auth.RecordLogin(user.ID); err != nil {
		slog.Warn("auth: record sign-in", "err", err)
	}
	s.noteSignIn(r)
	token, expiresAt, err := s.Auth.CreateSessionVia(user.ID, auth.MethodOIDC)
	if err != nil {
		slog.Error("auth: create session", "err", err)
		s.back(w, r, errFailed)
		return
	}
	auth.SetSessionCookieAt(w, token, expiresAt, secure, s.cookiePath())
	slog.Info("auth: signed in through single sign-on", "user", user.Username, "ip", ip)
	http.Redirect(w, r, s.cfg.URLBase+oidcauth.SafeNext(res.Next), http.StatusSeeOther)
}

// logReason keeps what is useful in a log line and leaves out anything that
// could carry a code or token.
func logReason(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
}

// accountForOIDC finds or makes the account for a verified sign-in. A
// non-empty second value is the problem code.
func (s *Server) accountForOIDC(cfg oidcauth.Config, res *oidcauth.Result) (*auth.User, string) {
	cfg = cfg.Defaults()
	if cfg.AllowedGroup != "" && !oidcauth.InGroup(res.Groups, cfg.AllowedGroup) {
		return nil, errNotAllowed
	}
	if cfg.AllowedDomains != "" && !(res.EmailVerified && cfg.EmailAllowed(res.Email)) {
		return nil, errNotAllowed
	}
	subject := oidcSubject(cfg.Issuer, res.Subject)
	u, err := s.Auth.UserByIdentity(oidcProvider, subject)
	switch {
	case err == nil:
		if u.Disabled {
			return nil, errDisabled
		}
		s.applyGroupRoles(u, oidcGroups(cfg, res))
		if err := s.Auth.RefreshExternalProfile(u.ID, res.Name, res.Email); err != nil {
			slog.Warn("auth: could not refresh an account's details", "err", err)
		}
		fresh, err := s.Auth.GetUser(u.ID)
		if err != nil {
			slog.Error("auth: load account", "err", err)
			return nil, errFailed
		}
		return fresh, ""
	case !errors.Is(err, auth.ErrNoIdentity):
		slog.Error("auth: look up a single sign-on identity", "err", err)
		return nil, errFailed
	}

	// Nobody is tied to this identity yet. First, an existing account with the
	// same email address, if the setup allows it and the provider vouches for it.
	if cfg.LinkByEmail && res.Email != "" {
		if !res.EmailVerified {
			return nil, errEmailUnver
		}
		matches, err := s.Auth.LocalUsersByEmail(res.Email)
		if err != nil {
			slog.Error("auth: look up an email address", "err", err)
			return nil, errFailed
		}
		switch {
		case len(matches) > 1:
			return nil, errAmbiguous
		case len(matches) == 1:
			m := matches[0]
			switch {
			case m.IsAdmin:
				return nil, errLinkAdmin
			case m.Disabled:
				return nil, errDisabled
			}
			if err := s.Auth.LinkIdentity(m.ID, oidcProvider, subject); err != nil {
				if errors.Is(err, auth.ErrIdentityTaken) {
					return nil, errLinkedAlready
				}
				slog.Error("auth: link an account by email", "err", err)
				return nil, errFailed
			}
			slog.Warn("auth: an existing account was linked to single sign-on by its verified email address", "user", m.Username)
			linked, err := s.Auth.GetUser(m.ID)
			if err != nil {
				return nil, errFailed
			}
			return linked, ""
		}
	}
	if !cfg.AutoCreate {
		return nil, errNoAccount
	}
	role, groupRole := oidcGroups(cfg, res).newAccountRole()
	name := res.Username
	if name == "" {
		name = strings.SplitN(res.Email, "@", 2)[0]
	}
	if name == "" {
		name = "user"
	}
	created, err := s.Auth.CreateExternalUser(auth.ExternalProfile{
		Provider: oidcProvider, Subject: subject, Username: name, Name: res.Name, Email: res.Email, Role: role, AllowSuffix: true,
	})
	if err != nil {
		slog.Error("auth: create an account for a first single sign-on", "err", err)
		return nil, errFailed
	}
	if groupRole != "" {
		if err := s.Auth.SetGroupRole(created.ID, groupRole); err != nil {
			slog.Error("auth: could not note where an account's role comes from", "user", created.Username, "err", err)
		}
	}
	slog.Info("auth: account made on a first single sign-on", "user", created.Username, "role", created.Role())
	return created, ""
}

// oidcGroups is what the groups claim says about one person.
func oidcGroups(cfg oidcauth.Config, res *oidcauth.Result) groupRoles {
	return groupRoles{
		adminOn: cfg.AdminGroup != "", viewerOn: cfg.ViewerGroup != "",
		inAdmin: oidcauth.InGroup(res.Groups, cfg.AdminGroup), inViewer: oidcauth.InGroup(res.Groups, cfg.ViewerGroup),
		def: cfg.DefaultRole,
	}
}

// finishOIDCLink ties the provider to the account that asked for it.
func (s *Server) finishOIDCLink(w http.ResponseWriter, r *http.Request, cfg oidcauth.Config, res *oidcauth.Result) {
	user, err := s.Auth.GetUser(res.UserID)
	if err != nil || user.Disabled {
		s.back(w, r, errFailed)
		return
	}
	if cfg.AllowedGroup != "" && !oidcauth.InGroup(res.Groups, cfg.AllowedGroup) || cfg.AllowedDomains != "" && !(res.EmailVerified && cfg.EmailAllowed(res.Email)) {
		s.back(w, r, errNotAllowed)
		return
	}
	if err := s.Auth.LinkIdentity(user.ID, oidcProvider, oidcSubject(cfg.Issuer, res.Subject)); err != nil {
		if errors.Is(err, auth.ErrIdentityTaken) {
			http.Redirect(w, r, s.cfg.URLBase+"/settings/profile?linked=taken", http.StatusSeeOther)
			return
		}
		slog.Error("auth: link an account to single sign-on", "err", err)
		s.back(w, r, errFailed)
		return
	}
	slog.Warn("auth: an account was linked to single sign-on", "user", user.Username)
	http.Redirect(w, r, s.cfg.URLBase+"/settings/profile?linked=ok", http.StatusSeeOther)
}

// POST /api/auth/oidc/link {"password"}: starts tying the signed-in account to the provider.
// The answer carries the address to send the browser to.
func (s *Server) handleOIDCLinkStart(w http.ResponseWriter, r *http.Request) {
	if refuseAPIKeyFor(w, r, "Linking single sign-on") {
		return
	}
	user := auth.UserFromContext(r.Context())
	cfg, ok := s.oidcActive()
	if !ok {
		writeError(w, http.StatusConflict, "Single sign-on isn't switched on.")
		return
	}
	if !user.IsLocal() {
		writeError(w, http.StatusConflict, "This account is already made through single sign-on or the directory.")
		return
	}
	if ids, _ := s.Auth.IdentitiesOf(user.ID); hasProvider(ids, oidcProvider) {
		writeError(w, http.StatusConflict, "Your account is already linked.")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	_ = decodeJSON(r, &req)
	// A link is a way back into the account, so a stolen session alone can't add one.
	if !s.checkPassword(w, r, user, req.Password) {
		return
	}
	if !s.sso().starts.Allow(httpsec.LimitKey(s.clientIP(r))) {
		writeError(w, http.StatusTooManyRequests, "Too many tries. Wait a few minutes.")
		return
	}
	s.sso().starts.RecordFailure(httpsec.LimitKey(s.clientIP(r)))
	redirect, err := s.oidcRedirect()
	if err != nil {
		writeError(w, http.StatusConflict, "Set the address you open Mediarium at first (Settings > Basics > General > Address of Mediarium).")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	flow, err := s.sso().oidc.Begin(ctx, cfg, redirect, oidcauth.ModeLink, user.ID, "/settings/profile")
	if err != nil {
		slog.Error("auth: could not start linking single sign-on", "err", err)
		writeError(w, http.StatusBadGateway, "The sign-in provider couldn't be reached. Try again in a moment.")
		return
	}
	s.setBindingCookie(w, flow.Binding, s.isHTTPS(r))
	writeJSON(w, http.StatusOK, map[string]string{"url": flow.URL})
}

func hasProvider(ids []auth.Identity, p string) bool {
	for _, i := range ids {
		if i.Provider == p {
			return true
		}
	}
	return false
}

// DELETE /api/auth/oidc/link: unties the account from the provider. Only an
// account with a password here can do it; one made through the provider has
// nothing else to sign in with.
func (s *Server) handleOIDCUnlink(w http.ResponseWriter, r *http.Request) {
	if refuseAPIKeyFor(w, r, "Linking single sign-on") {
		return
	}
	user := auth.UserFromContext(r.Context())
	if !user.IsLocal() {
		writeError(w, http.StatusConflict, "This account was made through single sign-on, so it has no other way to sign in.")
		return
	}
	if err := s.Auth.UnlinkIdentity(user.ID, oidcProvider); err != nil {
		slog.Error("auth: unlink single sign-on", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't remove the link.")
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// ---- Settings ----

type oidcPayload struct {
	oidcauth.Config
	HasClientSecret bool   `json:"hasClientSecret"`
	RedirectURL     string `json:"redirectUrl"` // what to enter at the provider
	PublicURLSet    bool   `json:"publicUrlSet"`
	DisabledByEnv   bool   `json:"disabledByEnv"`
}

func (s *Server) oidcPayload() oidcPayload {
	c := s.oidcSaved()
	has := c.ClientSecret != ""
	c.ClientSecret = ""
	redirect, err := s.oidcRedirect()
	return oidcPayload{Config: c, HasClientSecret: has, RedirectURL: redirect, PublicURLSet: err == nil, DisabledByEnv: s.cfg.DisableSSO}
}

// GET /api/auth/oidc
func (s *Server) handleGetOIDC(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.oidcPayload())
}

// oidcFromRequest reads the setup. A blank client secret keeps the saved one,
// but only for the same provider address.
func (s *Server) oidcFromRequest(r *http.Request) (oidcauth.Config, error) {
	var c oidcauth.Config
	if err := decodeJSON(r, &c); err != nil {
		return c, err
	}
	for _, p := range []*string{&c.Name, &c.Issuer, &c.ClientID, &c.Scopes, &c.UsernameClaim, &c.EmailClaim, &c.NameClaim, &c.GroupsClaim, &c.AdminGroup, &c.ViewerGroup, &c.AllowedGroup, &c.AllowedDomains} {
		*p = strings.TrimSpace(*p)
		if len(*p) > 1000 {
			return c, errors.New("a field is too long")
		}
	}
	if len(c.ClientSecret) > 2000 {
		return c, errors.New("a field is too long")
	}
	// The issuer is kept exactly as typed: the provider reports it with or
	// without a closing slash (authentik has one) and the two have to agree.
	if c.DefaultRole != "viewer" {
		c.DefaultRole = "member"
	}
	if c.ClientSecret == "" {
		if old := s.oidcSaved(); secretKept(old.Issuer, c.Issuer) {
			c.ClientSecret = old.ClientSecret
		}
	}
	return c, nil
}

// PUT /api/auth/oidc saves the setup. Switching it on needs a complete setup,
// the public address, and a provider that answers its discovery document.
func (s *Server) handlePutOIDC(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	c, err := s.oidcFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if c.CheckGroups() != nil {
		writeError(w, http.StatusBadRequest, "The viewer group has to be a different group than the administrator group.")
		return
	}
	if c.Enabled {
		if err := c.Check(); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Can't switch single sign-on on yet: %s.", err.Error()))
			return
		}
		if _, err := oidcauth.RedirectURL(s.configuredPublicURL()); err != nil {
			writeError(w, http.StatusBadRequest, "Can't switch single sign-on on yet: set the address you open Mediarium at first, under Settings > Basics > General > Address of Mediarium, for example https://mediarium.example.com.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		if _, err := s.sso().oidc.Discover(ctx, c.Issuer); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Can't switch single sign-on on yet: %s.", discoverProblem(err)))
			return
		}
	}
	b, _ := json.Marshal(c)
	if err := s.Settings.Set(settings.KeyAuthOIDC, string(b), true); err != nil {
		slog.Error("auth: could not save the single sign-on setup", "err", err)
		writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
		return
	}
	s.sso().oidc.Forget()
	actor := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		actor = u.Username
	}
	slog.Warn("auth: single sign-on settings saved", "on", c.Enabled, "issuer", c.Issuer, "by", actor)
	writeJSON(w, http.StatusOK, s.oidcPayload())
}

// discoverProblem words a failed discovery.
func discoverProblem(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "issuer did not match") || strings.Contains(msg, "did not match the issuer"):
		return "the provider reports a different issuer address than the one you typed. Copy the issuer from the provider exactly, including any trailing slash"
	case strings.Contains(msg, "404"):
		return "that address has no OpenID Connect setup (there's nothing at /.well-known/openid-configuration). Check the issuer address"
	}
	return "couldn't read the provider's setup at that address (" + logReason(err) + ")"
}

// POST /api/auth/oidc/test {config...}: reads the provider's setup from the
// address sent (nothing is saved) and reports what Mediarium found, with the
// address to enter at the provider.
func (s *Server) handleTestOIDC(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	c, err := s.oidcFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if c.Issuer == "" {
		writeError(w, http.StatusBadRequest, "Enter the provider's address (the issuer) first.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	ep, err := s.sso().oidc.Discover(ctx, c.Issuer)
	if err != nil {
		writeError(w, http.StatusBadGateway, capitalise(discoverProblem(err))+".")
		return
	}
	redirect, rerr := s.oidcRedirect()
	var notes []string
	if rerr != nil {
		notes = append(notes, "Set the address you open Mediarium at (Settings > Basics > General > Address of Mediarium) before switching this on.")
	}
	if u, e := url.Parse(c.Issuer); e == nil && u.Scheme == "http" {
		notes = append(notes, "This provider is reached over http, so the sign-in travels unencrypted on your network.")
	}
	if len(ep.PKCE) > 0 {
		has := false
		for _, m := range ep.PKCE {
			has = has || m == "S256"
		}
		if !has {
			notes = append(notes, "The provider doesn't list S256 for PKCE. Sign-in may fail.")
		}
	}
	if (c.AdminGroup != "" || c.ViewerGroup != "" || c.AllowedGroup != "") && len(ep.ScopesSupported) > 0 {
		found := false
		for _, sc := range strings.Fields(c.Defaults().Scopes) {
			if sc == "groups" || sc == "roles" {
				found = true
			}
		}
		if !found {
			notes = append(notes, `You use a group, but the scopes don't include "groups". Most providers only send groups when it is asked for.`)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "issuer": ep.Issuer, "authorizationEndpoint": ep.Authorization, "tokenEndpoint": ep.Token,
		"userinfoEndpoint": ep.UserInfo, "scopesSupported": ep.ScopesSupported, "redirectUrl": redirect, "notes": notes,
		"summary": fmt.Sprintf("Found the provider. Add %s as the redirect address there.", redirect),
	})
}
