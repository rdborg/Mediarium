package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/settings"
)

// Passkeys (WebAuthn). A passkey is a key pair kept in the person's phone,
// computer or security key; Mediarium holds only the public half. They are off
// until an administrator switches them on, and need the public address of
// Mediarium: the browser ties every passkey to that host name (the "relying
// party id") and only offers it on pages opened at that address.
//
// Sign-in uses discoverable credentials, so the person does not type a user
// name: the browser lists the passkeys it has for this site and the one chosen
// names the account. The ceremony requires user verification (PIN, fingerprint
// or face), so a passkey is a password and a second step in one, and a passkey
// sign-in skips the code of two-step sign-in.
//
// Only accounts with a password here can have passkeys: a passkey would
// outlive the account being turned off in the directory or the provider.
//
// What the library checks for us (go-webauthn): the challenge, the origin, the
// relying party id hash, the signature, user presence and verification, and the
// backup flags. What this file adds: a challenge store with a short life, where
// each challenge works once; the sign counter check, which stops a passkey
// whose counter did not move (a sign that it was copied); and the rules above.

const (
	ceremonyTTL = 5 * time.Minute
	maxCeremony = 1000
	// maxCeremonyPerOwner is how many sign-ins one network (or registrations
	// one account) can have waiting at once.
	maxCeremonyPerOwner = 5
	passkeyName         = 60
	waDisplay           = "Mediarium"
	kindRegister        = "register"
	kindLogin           = "login"
)

type ceremony struct {
	owner   string // the network that started it, or the account registering
	kind    string
	userID  int64
	session webauthn.SessionData
	expires time.Time
}

// ceremonyStore keeps the challenges in memory between the two steps. A
// restart forgets them, which only means the person tries again.
type ceremonyStore struct {
	mu sync.Mutex
	m  map[string]*ceremony
}

func newCeremonyStore() *ceremonyStore { return &ceremonyStore{m: map[string]*ceremony{}} }

func (c *ceremonyStore) put(kind string, userID int64, sd webauthn.SessionData, now time.Time) (string, error) {
	return c.putFrom(kind, userID, "", sd, now)
}

// putFrom is put for a ceremony started by owner. One owner never holds more
// than maxCeremonyPerOwner, and when the store is full the owner with the most
// waiting gives up its oldest, so a flood from many networks mostly evicts
// its own starts rather than the one real person signing in.
func (c *ceremonyStore) putFrom(kind string, userID int64, owner string, sd webauthn.SessionData, now time.Time) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.m {
		if now.After(v.expires) {
			delete(c.m, k)
		}
	}
	counts := map[string]int{}
	for _, v := range c.m {
		counts[v.owner]++
	}
	if owner != "" && counts[owner] >= maxCeremonyPerOwner {
		c.dropOldest(func(v *ceremony) bool { return v.owner == owner })
	} else if len(c.m) >= maxCeremony {
		// A flood of starts must not lock real people out: the owner with the
		// most waiting makes room (the oldest overall among equals).
		most := 0
		for _, n := range counts {
			most = max(most, n)
		}
		c.dropOldest(func(v *ceremony) bool { return counts[v.owner] == most })
	}
	c.m[id] = &ceremony{owner: owner, kind: kind, userID: userID, session: sd, expires: now.Add(ceremonyTTL)}
	return id, nil
}

// dropOldest removes the ceremony that expires first among those that match.
// The caller holds c.mu.
func (c *ceremonyStore) dropOldest(match func(*ceremony) bool) {
	var oldest string
	var oldestAt time.Time
	for k, v := range c.m {
		if match(v) && (oldest == "" || v.expires.Before(oldestAt)) {
			oldest, oldestAt = k, v.expires
		}
	}
	if oldest != "" {
		delete(c.m, oldest)
	}
}

// take removes and returns the ceremony: a challenge works once, whatever the
// outcome, and only for the kind it was made for.
func (c *ceremonyStore) take(id, kind string, now time.Time) (*ceremony, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[id]
	delete(c.m, id)
	if !ok || v.kind != kind || now.After(v.expires) {
		return nil, false
	}
	return v, true
}

// waUser adapts an account to the library.
type waUser struct {
	id      []byte
	name    string
	display string
	creds   []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return u.id }
func (u *waUser) WebAuthnName() string                       { return u.name }
func (u *waUser) WebAuthnDisplayName() string                { return u.display }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (s *Server) newWebAuthn() (*webauthn.WebAuthn, error) {
	origin, rpID := s.passkeyOrigin()
	if origin == "" {
		return nil, errors.New("passkeys need the public address")
	}
	timeouts := webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL}
	return webauthn.New(&webauthn.Config{
		RPID: rpID, RPDisplayName: waDisplay, RPOrigins: []string{origin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{Login: timeouts, Registration: timeouts},
	})
}

// loadWAUser loads an account with its passkeys. Credentials that no longer
// parse are skipped (and logged), never trusted.
func (s *Server) loadWAUser(u *auth.User) (*waUser, error) {
	handle, err := s.Auth.WebAuthnHandle(u.ID)
	if err != nil {
		return nil, err
	}
	keys, err := s.Auth.ListPasskeys(u.ID)
	if err != nil {
		return nil, err
	}
	w := &waUser{id: handle, name: u.Username, display: u.DisplayName()}
	for _, k := range keys {
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(k.Credential), &c); err != nil {
			slog.Error("auth: a stored passkey could not be read", "user", u.Username, "passkey", k.ID, "err", err)
			continue
		}
		w.creds = append(w.creds, c)
	}
	return w, nil
}

const passkeysOffMessage = "Passkeys aren't switched on."

// passkeyGate answers 4xx and returns false when passkeys can't be used.
func (s *Server) passkeyGate(w http.ResponseWriter) bool {
	if !s.passkeysActive() {
		writeError(w, http.StatusConflict, passkeysOffMessage)
		return false
	}
	return true
}

type passkeyView struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	BackedUp   bool       `json:"backedUp"` // synced to the person's cloud account (can be used on their other devices)
}

// GET /api/auth/passkeys: the signed-in person's passkeys, and whether they can add one.
func (s *Server) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	keys, err := s.Auth.ListPasskeys(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]passkeyView, 0, len(keys))
	for _, k := range keys {
		v := passkeyView{ID: k.ID, Name: k.Name, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt}
		var c webauthn.Credential
		if json.Unmarshal([]byte(k.Credential), &c) == nil {
			v.BackedUp = c.Flags.BackupState
		}
		out = append(out, v)
	}
	origin, host := s.passkeyOrigin()
	switchedOn, _ := s.Settings.GetBool(settings.KeyAuthPasskeys)
	reason := ""
	switch {
	case switchedOn && origin == "":
		reason = "Passkeys need the address you open Mediarium at, starting with https:// and with a name (not an IP address). An administrator can set it under Settings > General > Address of Mediarium."
	case !s.passkeysActive():
		reason = "Passkeys aren't switched on. An administrator can switch them on under Settings > Accounts > Ways to sign in."
	case !user.IsLocal():
		reason = "Your account signs in through single sign-on or the directory, so it can't have passkeys here."
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": reason == "", "enabled": switchedOn, "reason": reason, "host": host, "max": auth.MaxPasskeys, "keys": out,
	})
}

// POST /api/auth/passkeys/register/begin {"password"}
func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if refuseAPIKeyFor(w, r, "Passkeys") || !s.passkeyGate(w) {
		return
	}
	user := auth.UserFromContext(r.Context())
	if !user.IsLocal() {
		writeError(w, http.StatusBadRequest, "Your account signs in through single sign-on or the directory, so it can't have passkeys here.")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	_ = decodeJSON(r, &req)
	// A passkey is a way back into the account, so a stolen session alone can't add one.
	if !s.checkPassword(w, r, user, req.Password) {
		return
	}
	if keys, _ := s.Auth.ListPasskeys(user.ID); len(keys) >= auth.MaxPasskeys {
		writeError(w, http.StatusConflict, fmt.Sprintf("An account can have at most %d passkeys. Remove one first.", auth.MaxPasskeys))
		return
	}
	wa, err := s.newWebAuthn()
	if err != nil {
		writeError(w, http.StatusConflict, passkeysOffMessage)
		return
	}
	wu, err := s.loadWAUser(user)
	if err != nil {
		slog.Error("auth: load an account for a passkey", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't start. Try again.")
		return
	}
	creation, session, err := wa.BeginRegistration(wu,
		webauthn.WithExclusions(webauthn.Credentials(wu.creds).CredentialDescriptors()))
	if err != nil {
		slog.Error("auth: begin a passkey registration", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't start. Try again.")
		return
	}
	id, err := s.sso().ceremonies.putFrom(kindRegister, user.ID, fmt.Sprintf("user:%d", user.ID), *session, time.Now())
	if err != nil {
		writeError(w, http.StatusTooManyRequests, "Too many passkey sign-ins are waiting. Try again in a few minutes.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ceremony": id, "options": creation})
}

// POST /api/auth/passkeys/register/finish?c=<ceremony>&name=<label>  (body: the browser's answer)
func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if refuseAPIKeyFor(w, r, "Passkeys") || !s.passkeyGate(w) {
		return
	}
	user := auth.UserFromContext(r.Context())
	if !user.IsLocal() {
		writeError(w, http.StatusBadRequest, "Your account signs in through single sign-on or the directory, so it can't have passkeys here.")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if utf8.RuneCountInString(name) > passkeyName || strings.ContainsAny(name, "\x00\r\n") {
		writeError(w, http.StatusBadRequest, "Give the passkey a shorter name.")
		return
	}
	cer, ok := s.sso().ceremonies.take(r.URL.Query().Get("c"), kindRegister, time.Now())
	if !ok || cer.userID != user.ID {
		writeError(w, http.StatusBadRequest, "That took too long. Start again.")
		return
	}
	wa, err := s.newWebAuthn()
	if err != nil {
		writeError(w, http.StatusConflict, passkeysOffMessage)
		return
	}
	wu, err := s.loadWAUser(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't save the passkey. Try again.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	cred, err := wa.FinishRegistration(wu, cer.session, r)
	if err != nil {
		slog.Warn("auth: a passkey registration failed", "user", user.Username, "reason", waReason(err))
		writeError(w, http.StatusBadRequest, "The passkey couldn't be added. Check that you opened Mediarium at its public address and try again.")
		return
	}
	if name == "" {
		name = "Passkey " + time.Now().Format("2 Jan 2006")
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't save the passkey. Try again.")
		return
	}
	id, err := s.Auth.AddPasskey(user.ID, name, cred.ID, string(raw))
	if err != nil {
		writeError(w, http.StatusConflict, capitalise(err.Error())+".")
		return
	}
	slog.Warn("auth: a passkey was added", "user", user.Username, "passkey", id)
	writeJSON(w, http.StatusCreated, passkeyView{ID: id, Name: name, CreatedAt: time.Now(), BackedUp: cred.Flags.BackupState})
}

func waReason(err error) string {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		return pe.Type + ": " + pe.Details
	}
	return logReason(err)
}

func passkeyID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid passkey.")
		return 0, false
	}
	return id, true
}

// PUT /api/auth/passkeys/{id} {"name"}
func (s *Server) handleRenamePasskey(w http.ResponseWriter, r *http.Request) {
	if refuseAPIKeyFor(w, r, "Passkeys") {
		return
	}
	id, ok := passkeyID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > passkeyName || strings.ContainsAny(req.Name, "\x00\r\n") {
		writeError(w, http.StatusBadRequest, "Give the passkey a name of 1 to 60 characters.")
		return
	}
	user := auth.UserFromContext(r.Context())
	if err := s.Auth.RenamePasskey(user.ID, id, req.Name); errors.Is(err, auth.ErrPasskeyNotFound) {
		writeError(w, http.StatusNotFound, "That passkey doesn't exist.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't rename it.")
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// DELETE /api/auth/passkeys/{id}
func (s *Server) handleDeletePasskey(w http.ResponseWriter, r *http.Request) {
	if refuseAPIKeyFor(w, r, "Passkeys") {
		return
	}
	id, ok := passkeyID(w, r)
	if !ok {
		return
	}
	user := auth.UserFromContext(r.Context())
	if err := s.Auth.DeletePasskey(user.ID, id); errors.Is(err, auth.ErrPasskeyNotFound) {
		writeError(w, http.StatusNotFound, "That passkey doesn't exist.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't remove it.")
		return
	}
	slog.Warn("auth: a passkey was removed", "user", user.Username, "passkey", id)
	writeJSON(w, http.StatusOK, nil)
}

// POST /api/auth/passkey/login/begin: public. Answers the challenge and the
// ceremony id the browser sends back with its answer.
func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !s.passkeyGate(w) {
		return
	}
	ipKey := httpsec.LimitKey(s.clientIP(r))
	if !s.sso().starts.Allow(ipKey) {
		writeError(w, http.StatusTooManyRequests, "Too many tries. Wait a few minutes and try again.")
		return
	}
	s.sso().starts.RecordFailure(ipKey)
	wa, err := s.newWebAuthn()
	if err != nil {
		writeError(w, http.StatusConflict, passkeysOffMessage)
		return
	}
	assertion, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		slog.Error("auth: begin a passkey sign-in", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't start. Try again.")
		return
	}
	id, err := s.sso().ceremonies.putFrom(kindLogin, 0, "net:"+ipKey, *session, time.Now())
	if err != nil {
		writeError(w, http.StatusTooManyRequests, "Too many passkey sign-ins are waiting. Try again in a few minutes.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ceremony": id, "options": assertion})
}

const passkeyFailedMessage = "That passkey didn't work. Try again, or sign in another way."

// POST /api/auth/passkey/login/finish?c=<ceremony>  (body: the browser's answer)
func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !s.passkeyGate(w) {
		return
	}
	ip := s.clientIP(r)
	ipKey := httpsec.LimitKey(ip)
	if !s.LoginLimiter.Allow(ipKey) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a few minutes and try again.")
		return
	}
	cer, ok := s.sso().ceremonies.take(r.URL.Query().Get("c"), kindLogin, time.Now())
	if !ok {
		writeError(w, http.StatusBadRequest, "That took too long. Try again.")
		return
	}
	wa, err := s.newWebAuthn()
	if err != nil {
		writeError(w, http.StatusConflict, passkeysOffMessage)
		return
	}
	var found *auth.User
	var foundKey *auth.Passkey
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		key, err := s.Auth.PasskeyByCredentialID(rawID)
		if err != nil {
			return nil, err
		}
		u, err := s.Auth.UserByWebAuthnHandle(userHandle)
		if err != nil {
			return nil, err
		}
		if key.UserID != u.ID {
			return nil, errors.New("the passkey belongs to another account")
		}
		if !u.IsLocal() {
			return nil, errors.New("the account doesn't use passwords here")
		}
		wu, err := s.loadWAUser(u)
		if err != nil {
			return nil, err
		}
		found, foundKey = u, key
		return wu, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	_, cred, err := wa.FinishPasskeyLogin(handler, cer.session, r)
	if err != nil {
		s.LoginLimiter.RecordFailure(ipKey)
		slog.Warn("auth: failed passkey sign-in", "ip", ip, "reason", waReason(err))
		writeError(w, http.StatusUnauthorized, passkeyFailedMessage)
		return
	}
	if found == nil || foundKey == nil || cred == nil {
		s.LoginLimiter.RecordFailure(ipKey)
		writeError(w, http.StatusUnauthorized, passkeyFailedMessage)
		return
	}
	// A passkey whose counter did not go up may have been copied: two copies
	// would each count from the same number. Refuse it and keep the stored
	// counter, so the real one keeps being compared with what it was.
	if cred.Authenticator.CloneWarning {
		s.LoginLimiter.RecordFailure(ipKey)
		slog.Warn("auth: a passkey sign-in was refused, its counter did not go up (it may have been copied)", "user", found.Username, "passkey", foundKey.ID, "ip", ip)
		writeError(w, http.StatusUnauthorized, "This passkey looks like it has been copied, so it was refused. Remove it in your profile and add it again.")
		return
	}
	if found.Disabled {
		writeError(w, http.StatusForbidden, disabledMessage)
		return
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't sign you in. Try again.")
		return
	}
	if err := s.Auth.SavePasskeyUse(foundKey.ID, string(raw)); err != nil {
		slog.Error("auth: could not save a passkey's new counter", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't sign you in. Try again.")
		return
	}
	s.LoginLimiter.RecordSuccess(ipKey)
	s.finishSignInVia(w, r, found, auth.MethodPasskey, nil)
}
