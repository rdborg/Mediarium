// Package oidcauth signs people in through an OpenID Connect provider
// (Authelia, Authentik, Keycloak, Pocket ID, Google and the like) with the
// authorization code flow.
//
// It leans on two maintained libraries for the parts that are easy to get
// wrong: github.com/coreos/go-oidc for discovery, the key set and the ID token
// signature, issuer, audience and expiry checks, and golang.org/x/oauth2 for the
// code exchange. This package adds what they leave to the caller: a state and a
// nonce that are random, single use and expire, PKCE (S256), a cookie that ties
// the callback to the browser that started it, the authorized party check, and
// strict handling of the claims.
//
// Nothing here knows about accounts. The caller turns a verified Result into a
// session (see internal/api/oidc_signin.go).
package oidcauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config is the saved setup for one provider.
type Config struct {
	Enabled      bool   `json:"enabled"`
	Name         string `json:"name"`   // shown on the button: "Sign in with <name>"
	Issuer       string `json:"issuer"` // the provider's address; /.well-known/openid-configuration is read from it
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	Scopes       string `json:"scopes"` // space separated; "openid" is always added

	// Which claims hold what.
	UsernameClaim string `json:"usernameClaim"` // default preferred_username
	EmailClaim    string `json:"emailClaim"`    // default email
	NameClaim     string `json:"nameClaim"`     // default name
	GroupsClaim   string `json:"groupsClaim"`   // default groups

	// What happens to a person with no account yet.
	AutoCreate  bool   `json:"autoCreate"`  // make the account on the first sign-in
	DefaultRole string `json:"defaultRole"` // "member" (default) or "viewer"
	AdminGroup  string `json:"adminGroup"`  // people in this group are administrators ("" = nobody is, by group)
	// ViewerGroup, when set, makes the people in it viewers (unless they are in
	// the administrator group too). "" = nobody is a viewer by group.
	ViewerGroup string `json:"viewerGroup"`
	// AllowedGroup, when set, limits who may sign in at all to this group.
	AllowedGroup string `json:"allowedGroup"`
	// AllowedDomains, when set, limits who may sign in or be linked to the
	// email domains listed (comma separated, for example "example.com").
	AllowedDomains string `json:"allowedDomains"`
	// LinkByEmail ties a first sign-in to an existing account with the same
	// email address, but only when the provider says the address is verified,
	// and never to an administrator account.
	LinkByEmail bool `json:"linkByEmail"`
}

// Defaults fills the claim names and scopes that were left empty.
func (c Config) Defaults() Config {
	c.Issuer = strings.TrimSpace(c.Issuer)
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		c.Name = "single sign-on"
	}
	if c.UsernameClaim = strings.TrimSpace(c.UsernameClaim); c.UsernameClaim == "" {
		c.UsernameClaim = "preferred_username"
	}
	if c.EmailClaim = strings.TrimSpace(c.EmailClaim); c.EmailClaim == "" {
		c.EmailClaim = "email"
	}
	if c.NameClaim = strings.TrimSpace(c.NameClaim); c.NameClaim == "" {
		c.NameClaim = "name"
	}
	if c.GroupsClaim = strings.TrimSpace(c.GroupsClaim); c.GroupsClaim == "" {
		c.GroupsClaim = "groups"
	}
	if strings.TrimSpace(c.Scopes) == "" {
		c.Scopes = "openid profile email"
	}
	if c.DefaultRole != "viewer" {
		c.DefaultRole = "member"
	}
	return c
}

// ScopeList is the scopes to ask for, "openid" first, once each.
func (c Config) ScopeList() []string {
	out := []string{"openid"}
	for _, s := range strings.Fields(c.Scopes) {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// CheckGroups says what is wrong with the group names: the viewer group has
// to be another group than the administrator group.
func (c Config) CheckGroups() error {
	if v := strings.TrimSpace(c.ViewerGroup); v != "" && v == strings.TrimSpace(c.AdminGroup) {
		return errors.New("the viewer group has to be a different group than the administrator group")
	}
	return nil
}

// Check says what is missing, in words for the settings page.
func (c Config) Check() error {
	c = c.Defaults()
	if err := c.CheckGroups(); err != nil {
		return err
	}
	u, err := url.Parse(c.Issuer)
	switch {
	case c.Issuer == "":
		return errors.New("enter the provider's address (the issuer)")
	case err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http"):
		return errors.New("the provider's address should look like https://auth.example.com")
	case u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return errors.New("the provider's address has to be just the address, without a user name, query or fragment")
	case c.ClientID == "":
		return errors.New("enter the client ID")
	case c.ClientSecret == "":
		return errors.New("enter the client secret")
	}
	return nil
}

// Result is what a verified sign-in tells us.
type Result struct {
	Subject       string
	Username      string
	Email         string
	EmailVerified bool
	Name          string
	Groups        []string
	// Mode, UserID and Next come back from Begin.
	Mode   Mode
	UserID int64
	Next   string
}

// Mode is what a flow is for.
type Mode string

const (
	ModeSignIn Mode = "signin" // sign a person in
	ModeLink   Mode = "link"   // tie the provider to the account that is signed in
)

// Error carries a short code for the sign-in page (never the provider's own
// text, which could say anything) and the underlying cause for the log.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("oidc %s: %v", e.Code, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// Codes of Error.
const (
	CodeState    = "state"    // the state was missing, wrong, used before or too old, or the browser did not match
	CodeProvider = "provider" // the provider could not be reached or said no
	CodeToken    = "token"    // the code could not be exchanged
	CodeIDToken  = "id_token" // the ID token failed a check
	CodeNonce    = "nonce"
	CodeClaims   = "claims" // no usable subject
)

func fail(code string, err error) *Error { return &Error{Code: code, Err: err} }

const (
	// StateTTL is how long a started sign-in may take.
	StateTTL = 10 * time.Minute
	// maxPending bounds the memory a flood of sign-in starts can use.
	maxPending = 1000
	// providerTTL is how long discovery is remembered.
	providerTTL = time.Hour
	// CookieName holds the browser binding for a started sign-in.
	CookieName = "mediarium_oidc"
)

type pending struct {
	nonce    string
	verifier string // PKCE
	binding  [32]byte
	mode     Mode
	userID   int64
	next     string
	issuer   string
	clientID string
	redirect string
	expires  time.Time
}

type cachedProvider struct {
	issuer string
	p      *oidc.Provider
	at     time.Time
}

// Manager runs the flows. It is safe for concurrent use.
type Manager struct {
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	pend     map[string]*pending
	provider cachedProvider
}

// New makes a Manager whose requests to the provider go through client
// (Mediarium passes one that refuses the cloud metadata addresses).
func New(client *http.Client) *Manager {
	return &Manager{client: client, now: time.Now, pend: map[string]*pending{}}
}

func (m *Manager) ctx(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, m.client)
}

// providerFor does discovery for the issuer, remembering it for an hour. The
// provider's own check that the issuer it reports is the address asked for
// (go-oidc does this) is what stops a provider from claiming to be another.
func (m *Manager) providerFor(ctx context.Context, issuer string) (*oidc.Provider, error) {
	m.mu.Lock()
	c := m.provider
	m.mu.Unlock()
	if c.p != nil && c.issuer == issuer && m.now().Sub(c.at) < providerTTL {
		return c.p, nil
	}
	// The key set keeps using this context's client later, so it must not die
	// with the request: go-oidc detaches its own cancellation from it.
	p, err := oidc.NewProvider(m.ctx(ctx), issuer)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.provider = cachedProvider{issuer: issuer, p: p, at: m.now()}
	m.mu.Unlock()
	return p, nil
}

// Forget drops the remembered discovery (after the settings changed).
func (m *Manager) Forget() {
	m.mu.Lock()
	m.provider = cachedProvider{}
	m.mu.Unlock()
}

// Endpoints is what discovery found, for the test button.
type Endpoints struct {
	Issuer, Authorization, Token, UserInfo string
	ScopesSupported                        []string
	ClaimsSupported                        []string
	PKCE                                   []string
}

// Discover reads the provider's setup and reports the endpoints. It does not
// use the client secret.
func (m *Manager) Discover(ctx context.Context, issuer string) (*Endpoints, error) {
	m.Forget()
	p, err := m.providerFor(ctx, issuer)
	if err != nil {
		return nil, fail(CodeProvider, err)
	}
	var extra struct {
		Scopes []string `json:"scopes_supported"`
		Claims []string `json:"claims_supported"`
		PKCE   []string `json:"code_challenge_methods_supported"`
		UserIn string   `json:"userinfo_endpoint"`
	}
	_ = p.Claims(&extra)
	ep := p.Endpoint()
	return &Endpoints{Issuer: issuer, Authorization: ep.AuthURL, Token: ep.TokenURL, UserInfo: extra.UserIn,
		ScopesSupported: extra.Scopes, ClaimsSupported: extra.Claims, PKCE: extra.PKCE}, nil
}

func random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (m *Manager) oauth(cfg Config, p *oidc.Provider, redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  redirect,
		Scopes:       cfg.ScopeList(),
	}
}

// Flow is where to send the browser and the cookie value that goes with it.
type Flow struct {
	URL     string
	Binding string
}

// SafeNext keeps only a path inside this site: it starts with one slash and
// is not "//host" or "/\host", which browsers treat as another site. Anything
// else becomes "/".
func SafeNext(next string) string {
	if next == "" || next[0] != '/' || len(next) > 512 {
		return "/"
	}
	if len(next) > 1 && (next[1] == '/' || next[1] == '\\') {
		return "/"
	}
	for _, r := range next {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return "/"
		}
	}
	return next
}

// Begin starts a sign-in (or a link for the signed-in account userID) and says
// where to send the browser. redirect is the callback address, made from the
// configured public address, never from the request.
func (m *Manager) Begin(ctx context.Context, cfg Config, redirect string, mode Mode, userID int64, next string) (Flow, error) {
	cfg = cfg.Defaults()
	if err := cfg.Check(); err != nil {
		return Flow{}, fail(CodeProvider, err)
	}
	p, err := m.providerFor(ctx, cfg.Issuer)
	if err != nil {
		return Flow{}, fail(CodeProvider, err)
	}
	state, err := random(24)
	if err != nil {
		return Flow{}, fail(CodeState, err)
	}
	nonce, err := random(24)
	if err != nil {
		return Flow{}, fail(CodeState, err)
	}
	binding, err := random(24)
	if err != nil {
		return Flow{}, fail(CodeState, err)
	}
	verifier := oauth2.GenerateVerifier()
	now := m.now()

	m.mu.Lock()
	for k, v := range m.pend {
		if now.After(v.expires) {
			delete(m.pend, k)
		}
	}
	if len(m.pend) >= maxPending {
		// A flood of starts must not lock real people out: the oldest one waiting makes room.
		var oldest string
		var oldestAt time.Time
		for k, v := range m.pend {
			if oldest == "" || v.expires.Before(oldestAt) {
				oldest, oldestAt = k, v.expires
			}
		}
		delete(m.pend, oldest)
	}
	m.pend[state] = &pending{nonce: nonce, verifier: verifier, binding: sha256.Sum256([]byte(binding)), mode: mode, userID: userID,
		next: SafeNext(next), issuer: cfg.Issuer, clientID: cfg.ClientID, redirect: redirect, expires: now.Add(StateTTL)}
	m.mu.Unlock()

	authURL := m.oauth(cfg, p, redirect).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))
	return Flow{URL: authURL, Binding: binding}, nil
}

// take removes and returns the pending sign-in for state: a state works once.
func (m *Manager) take(state string) *pending {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pend[state]
	delete(m.pend, state)
	if p == nil || m.now().After(p.expires) {
		return nil
	}
	return p
}

// Finish completes a sign-in from the callback's query and the binding cookie:
// it checks the state, exchanges the code, verifies the ID token and reads the
// claims. Only a Result that passed every check comes back.
func (m *Manager) Finish(ctx context.Context, cfg Config, redirect string, q url.Values, binding string) (*Result, error) {
	cfg = cfg.Defaults()
	state := q.Get("state")
	pend := m.take(state) // consumed whatever happens next, so a state is never tried twice
	if state == "" || pend == nil {
		return nil, fail(CodeState, errors.New("unknown, used or expired state"))
	}
	bound := sha256.Sum256([]byte(binding))
	if binding == "" || subtle.ConstantTimeCompare(bound[:], pend.binding[:]) != 1 {
		return nil, fail(CodeState, errors.New("the browser that finished the sign-in is not the one that started it"))
	}
	if pend.issuer != cfg.Issuer || pend.clientID != cfg.ClientID || pend.redirect != redirect {
		return nil, fail(CodeState, errors.New("the setup changed during the sign-in"))
	}
	if e := q.Get("error"); e != "" {
		return nil, fail(CodeProvider, fmt.Errorf("the provider answered with error %q", truncate(e, 60)))
	}
	code := q.Get("code")
	if code == "" {
		return nil, fail(CodeProvider, errors.New("no code in the answer"))
	}
	p, err := m.providerFor(ctx, cfg.Issuer)
	if err != nil {
		return nil, fail(CodeProvider, err)
	}
	conf := m.oauth(cfg, p, redirect)
	cctx, cancel := context.WithTimeout(m.ctx(ctx), 20*time.Second)
	defer cancel()
	tok, err := conf.Exchange(cctx, code, oauth2.VerifierOption(pend.verifier))
	if err != nil {
		return nil, fail(CodeToken, err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return nil, fail(CodeIDToken, errors.New("the provider sent no ID token"))
	}
	verifier := p.Verifier(&oidc.Config{ClientID: cfg.ClientID, Now: m.now})
	idt, err := verifier.Verify(cctx, raw)
	if err != nil {
		return nil, fail(CodeIDToken, err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(pend.nonce)) != 1 || idt.Nonce == "" {
		return nil, fail(CodeNonce, errors.New("the nonce does not match"))
	}
	var azp struct {
		AZP string `json:"azp"`
	}
	if err := idt.Claims(&azp); err != nil {
		return nil, fail(CodeIDToken, err)
	}
	if (len(idt.Audience) > 1 && azp.AZP == "") || (azp.AZP != "" && azp.AZP != cfg.ClientID) {
		return nil, fail(CodeIDToken, errors.New("the ID token was issued to another party"))
	}
	if idt.Subject == "" {
		return nil, fail(CodeClaims, errors.New("the ID token has no subject"))
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return nil, fail(CodeIDToken, err)
	}
	// Claims the ID token does not carry may be in the user info answer. It is
	// only believed if it is about the same person.
	need := []string{cfg.UsernameClaim, cfg.EmailClaim, cfg.NameClaim}
	if cfg.AdminGroup != "" || cfg.ViewerGroup != "" || cfg.AllowedGroup != "" {
		need = append(need, cfg.GroupsClaim)
	}
	if missing(claims, need) {
		if ui, err := p.UserInfo(cctx, oauth2.StaticTokenSource(tok)); err == nil && ui.Subject == idt.Subject {
			var extra map[string]any
			if ui.Claims(&extra) == nil {
				for k, v := range extra {
					if _, have := claims[k]; !have {
						claims[k] = v
					}
				}
			}
		}
	}
	res := &Result{
		Subject:       idt.Subject,
		Username:      claimString(claims, cfg.UsernameClaim),
		Email:         claimString(claims, cfg.EmailClaim),
		EmailVerified: claimBool(claims, "email_verified"),
		Name:          claimString(claims, cfg.NameClaim),
		Groups:        claimStrings(claims, cfg.GroupsClaim),
		Mode:          pend.mode,
		UserID:        pend.userID,
		Next:          pend.next,
	}
	// The subject is only unique within one provider; the caller stores the
	// issuer next to it.
	return res, nil
}

func missing(claims map[string]any, names []string) bool {
	for _, n := range names {
		if _, ok := claims[n]; !ok {
			return true
		}
	}
	return false
}

func claimString(c map[string]any, name string) string {
	if s, ok := c[name].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// claimBool reads email_verified, which some providers send as the text "true".
func claimBool(c map[string]any, name string) bool {
	switch v := c[name].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	return false
}

// claimStrings reads a list of text values, or one text value.
func claimStrings(c map[string]any, name string) []string {
	switch v := c[name].(type) {
	case []any:
		var out []string
		for _, x := range v {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v != "" {
			return []string{v}
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// InGroup reports whether group is one of the groups (exact match).
func InGroup(groups []string, group string) bool {
	return group != "" && slices.Contains(groups, group)
}

// EmailAllowed reports whether the address is in one of AllowedDomains (any
// address is when none are listed).
func (c Config) EmailAllowed(email string) bool {
	list := strings.Split(strings.ToLower(c.AllowedDomains), ",")
	listed := false
	for _, d := range list {
		d = strings.TrimPrefix(strings.TrimSpace(d), "@")
		if d == "" {
			continue
		}
		listed = true
		at := strings.LastIndex(email, "@")
		if at >= 0 && strings.EqualFold(email[at+1:], d) {
			return true
		}
	}
	return !listed
}

// RedirectURL is the callback address for a public address like
// https://mediarium.example.com (a path in it is kept).
func RedirectURL(publicURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("set the address you open Mediarium at (Settings > Basics > General > Address of Mediarium) first, for example https://mediarium.example.com")
	}
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return strings.TrimRight(u.String(), "/") + "/api/auth/oidc/callback", nil
}

// Pending reports how many sign-ins are waiting (for tests).
func (m *Manager) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pend)
}
