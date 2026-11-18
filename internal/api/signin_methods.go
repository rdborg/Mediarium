package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/ldapauth"
	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/oidcauth"
	"github.com/rdborg/mediarium/internal/settings"
)

// Other ways to sign in: an OpenID Connect provider (single sign-on), a
// directory (LDAP or Active Directory) and passkeys. All three are off until an
// administrator switches them on, and none of them replaces the local
// password: that stays on unless an administrator who has already signed in
// another way turns it off, and MEDIARIUM_DISABLE_SSO=1 brings it back whatever
// the settings say.

// ssoState holds what the sign-in methods keep in memory. It is made on first
// use so a zero Server (the route tests build one) still works.
type ssoState struct {
	once       sync.Once
	oidc       *oidcauth.Manager
	ldap       *ldapauth.Client
	ceremonies *ceremonyStore
	// starts limits how often one address may start a sign-in or a passkey
	// ceremony, each of which takes a little memory until it ends.
	starts *auth.LoginLimiter
}

func (s *Server) sso() *ssoState {
	st := &s.ssoS
	st.once.Do(func() {
		if st.oidc == nil {
			st.oidc = oidcauth.New(netguard.Client(20 * time.Second))
		}
		if st.ldap == nil {
			st.ldap = ldapauth.NewClient(netguard.Dialer(15 * time.Second))
		}
		st.ceremonies = newCeremonyStore()
		st.starts = auth.NewLoginLimiter(40, 15*time.Minute)
	})
	return st
}

// readJSONSetting loads a JSON setting into v; an unset or unreadable one leaves v alone.
func (s *Server) readJSONSetting(key string, v any) {
	raw, err := s.Settings.Get(key)
	if err != nil {
		slog.Error("auth: could not read a sign-in setting", "key", key, "err", err)
		return
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), v); err != nil {
			slog.Error("auth: a sign-in setting is not readable", "key", key, "err", err)
		}
	}
}

func (s *Server) oidcSaved() oidcauth.Config {
	var c oidcauth.Config
	s.readJSONSetting(settings.KeyAuthOIDC, &c)
	return c.Defaults()
}

func (s *Server) ldapSaved() ldapauth.Config {
	var c ldapauth.Config
	s.readJSONSetting(settings.KeyAuthLDAP, &c)
	return c.Defaults()
}

// configuredPublicURL is the address Mediarium is opened at ("" when not set).
func (s *Server) configuredPublicURL() string {
	v, _ := s.Settings.Get(settings.KeyPublicURL)
	return strings.TrimRight(strings.TrimSpace(v), "/")
}

// oidcActive is the single sign-on setup when it is switched on, complete, and
// not overridden by the emergency switch.
func (s *Server) oidcActive() (oidcauth.Config, bool) {
	if s.cfg.DisableSSO {
		return oidcauth.Config{}, false
	}
	c := s.oidcSaved()
	if !c.Enabled || c.Check() != nil {
		return oidcauth.Config{}, false
	}
	if _, err := oidcauth.RedirectURL(s.configuredPublicURL()); err != nil {
		return oidcauth.Config{}, false
	}
	return c, true
}

// ldapActive is the directory setup when it is switched on and complete.
func (s *Server) ldapActive() (ldapauth.Config, bool) {
	if s.cfg.DisableSSO {
		return ldapauth.Config{}, false
	}
	c := s.ldapSaved()
	if !c.Enabled || c.Check() != nil {
		return ldapauth.Config{}, false
	}
	return c, true
}

// webauthnHost is the host name passkeys are tied to, from the public address:
// "" when passkeys cannot be used (no address, not https and not localhost, or
// an IP address, which WebAuthn does not accept).
func (s *Server) passkeyOrigin() (origin, rpID string) {
	return passkeyOriginOf(s.configuredPublicURL())
}

// passkeyOriginOf derives the WebAuthn origin and relying party id from the
// public address. WebAuthn needs a secure origin: https, or http on localhost.
// The id is the host name without the port, and an IP address is not allowed.
func passkeyOriginOf(base string) (origin, rpID string) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", ""
	}
	host := strings.ToLower(u.Hostname())
	if isIPLiteral(host) {
		return "", ""
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && (host == "localhost" || strings.HasSuffix(host, ".localhost")):
	default:
		return "", ""
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), host
}

func isIPLiteral(host string) bool {
	if strings.Contains(host, ":") { // an IPv6 address
		return true
	}
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// passkeysActive is true when passkeys are switched on and the public address
// allows them.
func (s *Server) passkeysActive() bool {
	if on, _ := s.Settings.GetBool(settings.KeyAuthPasskeys); !on {
		return false
	}
	origin, _ := s.passkeyOrigin()
	return origin != ""
}

// externalSignIn reports whether some way other than the local password can
// sign people in right now.
func (s *Server) externalSignIn() bool {
	if _, ok := s.oidcActive(); ok {
		return true
	}
	if _, ok := s.ldapActive(); ok {
		return true
	}
	return s.passkeysActive()
}

// localSignInOn reports whether the user name and password form works. It does
// unless an administrator turned it off, there is another way in, and the
// emergency switch is not set.
func (s *Server) localSignInOn() bool {
	if s.cfg.DisableSSO {
		return true
	}
	if v, _ := s.Settings.Get(settings.KeyAuthLocalSignIn); v != "0" {
		return true
	}
	return !s.externalSignIn()
}

const localOffMessage = "Signing in with a password is turned off here. Use one of the other ways to sign in."

// signInMethodsPayload is what the sign-in page needs to know, without signing in.
type signInMethodsPayload struct {
	Local    bool        `json:"local"`
	OIDC     *oidcButton `json:"oidc"`
	LDAP     bool        `json:"ldap"`     // the user name and password form also works for directory accounts
	Passkeys bool        `json:"passkeys"` // "Sign in with a passkey" works
	// PasskeyHost is where passkeys work. Opened at another address, the browser refuses them.
	PasskeyHost string `json:"passkeyHost,omitempty"`
	// AutoSignIn is the method the sign-in page sends visitors to by itself
	// ("oidc"), or empty. It is only set while that method works.
	AutoSignIn string `json:"autoSignIn,omitempty"`
}

// autoSignInOIDC is the one value the automatic sign-in setting can hold besides off.
const autoSignInOIDC = "oidc"

// autoSignIn is the method visitors are sent to by themselves: the saved
// choice while that method works, otherwise nothing, so a provider that was
// switched off never leaves people bounced to a dead end.
func (s *Server) autoSignIn() string {
	v, _ := s.Settings.Get(settings.KeyAuthAutoSignIn)
	if v == autoSignInOIDC {
		if _, ok := s.oidcActive(); ok {
			return autoSignInOIDC
		}
	}
	return ""
}

// autoSignInChoice is one entry of the "sign in automatically with" list.
type autoSignInChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// validAutoSignIn says whether v can be saved: off, or a method that works now.
func validAutoSignIn(v string, oidcWorks bool) (ok bool, problem string) {
	switch v {
	case "":
		return true, ""
	case autoSignInOIDC:
		if oidcWorks {
			return true, ""
		}
		return false, "Single sign-on has to be switched on and set up before visitors can be sent to it."
	}
	return false, "That isn't a way to sign in automatically."
}

type oidcButton struct {
	Name string `json:"name"`
}

// GET /api/auth/methods: public, so the sign-in page can show the buttons.
func (s *Server) handleSignInMethods(w http.ResponseWriter, r *http.Request) {
	out := signInMethodsPayload{Local: s.localSignInOn()}
	if c, ok := s.oidcActive(); ok {
		out.OIDC = &oidcButton{Name: c.Name}
	}
	_, out.LDAP = s.ldapActive()
	if s.passkeysActive() {
		out.Passkeys = true
		_, out.PasskeyHost = s.passkeyOrigin()
	}
	out.AutoSignIn = s.autoSignIn()
	writeJSON(w, http.StatusOK, out)
}

// signInSettings is the administrator's view of the general switches.
type signInSettings struct {
	LocalSignIn       bool   `json:"localSignIn"`       // the saved choice
	LocalEffective    bool   `json:"localEffective"`    // what applies right now
	Passkeys          bool   `json:"passkeys"`          // the saved choice
	PasskeysAvailable bool   `json:"passkeysAvailable"` // the public address allows them
	PublicURL         string `json:"publicUrl"`
	SSODisabledByEnv  bool   `json:"ssoDisabledByEnv"`
	ExternalOn        bool   `json:"externalOn"` // another way to sign in works
	// CanTurnOffLocal says whether the password form can be switched off now:
	// the administrator is signed in through another method.
	CanTurnOffLocal bool   `json:"canTurnOffLocal"`
	SessionMethod   string `json:"sessionMethod"`
	// AutoSignIn is the method visitors are sent to by themselves ("oidc") or
	// empty; AutoSignInChoices are the ones that can be picked right now.
	AutoSignIn        string             `json:"autoSignIn"`
	AutoSignInChoices []autoSignInChoice `json:"autoSignInChoices"`
}

func (s *Server) sessionMethod(r *http.Request) string {
	if r.Header.Get("X-API-Key") != "" {
		return "apikey"
	}
	if c, err := r.Cookie(auth.CookieName); err == nil {
		return s.Auth.SessionMethod(c.Value)
	}
	return ""
}

func (s *Server) signInSettings(r *http.Request) signInSettings {
	saved, _ := s.Settings.Get(settings.KeyAuthLocalSignIn)
	pk, _ := s.Settings.GetBool(settings.KeyAuthPasskeys)
	origin, _ := s.passkeyOrigin()
	m := s.sessionMethod(r)
	choices := []autoSignInChoice{}
	if c, ok := s.oidcActive(); ok {
		choices = append(choices, autoSignInChoice{ID: autoSignInOIDC, Name: c.Name})
	}
	return signInSettings{
		AutoSignIn: s.autoSignIn(), AutoSignInChoices: choices,
		LocalSignIn: saved != "0", LocalEffective: s.localSignInOn(), Passkeys: pk, PasskeysAvailable: origin != "",
		PublicURL: s.configuredPublicURL(), SSODisabledByEnv: s.cfg.DisableSSO, ExternalOn: s.externalSignIn(),
		CanTurnOffLocal: s.externalSignIn() && !s.cfg.DisableSSO && (m == auth.MethodOIDC || m == auth.MethodLDAP || m == auth.MethodPasskey),
		SessionMethod:   m,
	}
}

// GET /api/auth/sign-in
func (s *Server) handleGetSignIn(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.signInSettings(r))
}

// PUT /api/auth/sign-in {"localSignIn"?: bool, "passkeys"?: bool}
func (s *Server) handlePutSignIn(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	var req struct {
		LocalSignIn *bool   `json:"localSignIn"`
		Passkeys    *bool   `json:"passkeys"`
		AutoSignIn  *string `json:"autoSignIn"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	actor := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		actor = u.Username
	}
	if req.Passkeys != nil {
		if *req.Passkeys {
			if origin, _ := s.passkeyOrigin(); origin == "" {
				writeError(w, http.StatusBadRequest, "Passkeys need the address you open Mediarium at, starting with https:// (or http://localhost). Set it under Settings > Basics > General > Address of Mediarium first, and it can't be an IP address.")
				return
			}
		}
		v := "0"
		if *req.Passkeys {
			v = "1"
		}
		if err := s.Settings.Set(settings.KeyAuthPasskeys, v, false); err != nil {
			slog.Error("auth: could not save the passkeys switch", "err", err)
			writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
			return
		}
		slog.Warn("auth: passkeys switched", "on", *req.Passkeys, "by", actor)
	}
	if req.AutoSignIn != nil {
		_, oidcWorks := s.oidcActive()
		if ok, problem := validAutoSignIn(*req.AutoSignIn, oidcWorks); !ok {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
		if err := s.Settings.Set(settings.KeyAuthAutoSignIn, *req.AutoSignIn, false); err != nil {
			slog.Error("auth: could not save the automatic sign-in choice", "err", err)
			writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
			return
		}
		slog.Warn("auth: automatic sign-in changed", "to", *req.AutoSignIn, "by", actor)
	}
	if req.LocalSignIn != nil {
		if !*req.LocalSignIn {
			st := s.signInSettings(r)
			if !st.CanTurnOffLocal {
				writeError(w, http.StatusConflict, "To turn password sign-in off, sign in with single sign-on, the directory or a passkey first, so you know that way works. Otherwise you could lock yourself out.")
				return
			}
		}
		v := "1"
		if !*req.LocalSignIn {
			v = "0"
		}
		if err := s.Settings.Set(settings.KeyAuthLocalSignIn, v, false); err != nil {
			slog.Error("auth: could not save the password sign-in switch", "err", err)
			writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
			return
		}
		slog.Warn("auth: password sign-in switched", "on", *req.LocalSignIn, "by", actor)
	}
	writeJSON(w, http.StatusOK, s.signInSettings(r))
}

// secretKept says whether a saved secret may be kept when the field is left
// empty: not when the address next to it changed, so nobody can point the
// setup at another machine and have the saved secret sent there.
func secretKept(oldAddr, newAddr string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(oldAddr), "/"), strings.TrimRight(strings.TrimSpace(newAddr), "/"))
}
