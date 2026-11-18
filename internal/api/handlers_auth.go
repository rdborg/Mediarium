package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/ldapauth"
)

// handleVersion reports the running build's version (version-pinned
// Docker tags, changelog per release). Public/unauthenticated
// since it's shown on the About page before login and isn't sensitive.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}

// handleOnboardingStatus tells the frontend whether to show the first-run
// wizard or the normal app shell.
func (s *Server) handleOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	needed, err := s.Auth.FirstRunNeeded()
	if err != nil {
		log.Printf("api: onboarding status: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't check whether setup is finished. Try again.")
		return
	}
	// setupCodeRequired tells the sign-up page whether to ask for the setup
	// code: only while no account exists, and only for someone who is not on
	// the home network (setup.go).
	writeJSON(w, http.StatusOK, map[string]bool{"firstRunNeeded": needed, "setupCodeRequired": needed && s.setupNeedsCode(r)})
}

type createAdminRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Name      string `json:"name"`      // one free-form display name; optional
	FirstName string `json:"firstName"` // still accepted (older clients)
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	// SetupCode is the one-time code from the Mediarium log, needed only when
	// the sign-up is not made from the home network (setup.go).
	SetupCode string `json:"setupCode"`
}

// userPayload is the shape every endpoint that describes the logged-in user
// returns (login, onboarding, /me, profile update). role is "admin" or
// "member"; isAdmin is kept for older clients.
func userPayload(u *auth.User) map[string]any {
	return map[string]any{
		"id": u.ID, "username": u.Username, "isAdmin": u.IsAdmin, "role": u.Role(), "source": sourceOf(u.Source),
		"name": u.DisplayName(), "firstName": u.FirstName, "lastName": u.LastName, "email": u.Email,
	}
}

// splitName turns the single "name" the app sends into the stored first/last
// pair: the whole name goes in the first field. Older clients that send
// firstName and lastName keep working. Either may be empty; the app then
// greets the user by username.
func splitName(name, first, last string) (string, string) {
	if n := strings.TrimSpace(name); n != "" {
		return n, ""
	}
	if strings.TrimSpace(first) == "" && strings.TrimSpace(last) == "" {
		return "", ""
	}
	return strings.TrimSpace(first), strings.TrimSpace(last)
}

// handleCreateAdmin is onboarding wizard step 1. Refuses once an
// admin already exists — it's not a general "create user" endpoint.
func (s *Server) handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	needed, err := s.Auth.FirstRunNeeded()
	if err != nil {
		log.Printf("api: create admin: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't check whether setup is finished. Try again.")
		return
	}
	if !needed {
		writeError(w, http.StatusConflict, "An admin account already exists. Sign in instead.")
		return
	}

	var req createAdminRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicBody)
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	// From outside the home network the one-time code from the log is needed,
	// and wrong guesses count against the caller like wrong passwords do.
	if s.setupNeedsCode(r) {
		ip := s.clientIP(r)
		ipKey := httpsec.LimitKey(ip)
		if !s.LoginLimiter.Allow(ipKey) {
			writeError(w, http.StatusTooManyRequests, "Too many attempts. Wait a few minutes and try again.")
			return
		}
		if !s.setupCodeMatches(req.SetupCode) {
			s.LoginLimiter.RecordFailure(ipKey)
			slog.Warn("auth: first-run sign-up refused, the setup code was missing or wrong", "ip", ip)
			// One whole sentence in one literal, so the screen can translate it.
			writeError(w, http.StatusForbidden, "This Mediarium has no account yet, and it can't tell that you are on the home network, so it needs its one-time setup code. You'll find it in the Mediarium log (for example, run: docker logs mediarium). Look for \"setup code\".")
			return
		}
	}
	req.Username = strings.TrimSpace(req.Username)
	req.FirstName, req.LastName = splitName(req.Name, req.FirstName, req.LastName)
	req.Email = strings.TrimSpace(req.Email)
	if rejectBad(w, checkUsername(req.Username), checkPassword(req.Password, "Password"), checkEmail(req.Email), checkMaxLen(req.FirstName, "Name", nameMax)) {
		return
	}

	// One statement checks that no account exists and creates this one, so
	// two requests racing for the first-run screen cannot both win.
	user, err := s.Auth.CreateFirstAdmin(req.Username, req.Password, req.FirstName, req.LastName, req.Email)
	if errors.Is(err, auth.ErrSetupDone) {
		writeError(w, http.StatusConflict, "An admin account already exists. Sign in instead.")
		return
	}
	if err != nil {
		log.Printf("api: create admin: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't create the account. Check the Mediarium log for details.")
		return
	}
	s.startSession(w, r, user.ID)
	writeJSON(w, http.StatusCreated, userPayload(user))
}

// maxPublicBody is the biggest body the two pages that need no sign-in accept.
// A sign-in is a few hundred bytes; anyone on the internet can send one.
const maxPublicBody = 64 << 10

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Failures are counted per caller address (the real one: see
	// httpsec.Proxies.ClientIP) and per account name from all addresses.
	//
	// A try is counted before the password is checked (the check takes a
	// quarter of a second, and a burst of parallel requests would all be let
	// through before the first failure was written down). It is given back
	// unless the credentials turn out to be wrong.
	started := time.Now()
	ip := s.clientIP(r)
	ipKey := httpsec.LimitKey(ip)
	if !s.LoginLimiter.Reserve(ipKey) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a few minutes and try again.")
		return
	}
	byUser := s.sec().byUser
	var userKey string
	ipHeld, userHeld := true, false
	defer func() {
		if ipHeld {
			s.LoginLimiter.Refund(ipKey)
		}
		if userHeld {
			byUser.Refund(userKey)
		}
	}()

	var req loginRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicBody)
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	typedName := strings.ToLower(strings.TrimSpace(req.Username))
	if len(typedName) > 128 {
		typedName = typedName[:128]
	}
	// Typed names have their own prefix, so a name like "code:1" cannot land on
	// the key that guards another account's code step.
	userKey = nameGuessKey(typedName)
	if !byUser.Reserve(userKey) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a few minutes and try again.")
		return
	}
	userHeld = true
	// A name that belongs to an account with a password here, or to no account
	// at all while the directory is off, is a password sign-in. A directory
	// account, or a name nobody here has while the directory is on, goes to the
	// directory. A person with a password here is never handed to the directory.
	var (
		user   *auth.User
		err    error
		method = auth.MethodPassword
	)
	dirCfg, dirOn := s.ldapActive()
	viaDirectory := false
	if dirOn {
		known, knownErr := s.Auth.UserByName(strings.TrimSpace(req.Username))
		viaDirectory = knownErr != nil || known.Source == auth.SourceLDAP
	}
	if viaDirectory {
		method = auth.MethodLDAP
		user, err = s.directorySignIn(r.Context(), dirCfg, strings.TrimSpace(req.Username), req.Password)
	} else {
		if !s.localSignInOn() {
			if dirOn {
				// The directory is on, so names unknown here are answered "wrong username or
				// password". A name with a password here gets the same answer, and the try
				// counts, so the sign-in form can't be used to find out which names exist.
				ipHeld, userHeld = false, false // the tries stay counted
				slog.Warn("auth: failed sign-in, password sign-in is off", "user", typedName, "ip", ip)
				waitOutFailure(r.Context(), started)
				writeError(w, http.StatusUnauthorized, "Wrong username or password.")
				return
			}
			writeError(w, http.StatusForbidden, localOffMessage)
			return
		}
		user, err = s.Auth.Authenticate(req.Username, req.Password)
	}
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, ldapauth.ErrInvalidCredentials), errors.Is(err, ldapauth.ErrAmbiguous):
		ipHeld, userHeld = false, false // the tries stay counted
		slog.Warn("auth: failed sign-in", "user", typedName, "ip", ip, "method", method)
		if dirOn {
			waitOutFailure(r.Context(), started)
		}
		writeError(w, http.StatusUnauthorized, "Wrong username or password.")
		return
	case errors.Is(err, auth.ErrAccountDisabled):
		slog.Warn("auth: sign-in refused, the account is turned off", "user", typedName, "ip", ip)
		writeError(w, http.StatusForbidden, disabledMessage)
		return
	case errors.Is(err, errNoAccountHere):
		slog.Warn("auth: directory sign-in refused, no account here and none is made automatically", "user", typedName, "ip", ip)
		writeError(w, http.StatusForbidden, "That worked, but there is no account for you here yet. Ask an administrator to add one.")
		return
	case errors.Is(err, auth.ErrUsernameTaken):
		slog.Warn("auth: directory sign-in refused, the user name is taken by another account", "user", typedName, "ip", ip)
		writeError(w, http.StatusConflict, "An account with your username already exists here and isn't linked to the directory. Ask an administrator.")
		return
	case errors.Is(err, ldapauth.ErrUnavailable):
		slog.Error("auth: the directory could not be used for a sign-in", "err", err)
		writeError(w, http.StatusBadGateway, "The directory couldn't be reached. Try again in a moment, or ask an administrator.")
		return
	case err != nil:
		log.Printf("api: login: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't sign you in. Try again.")
		return
	}
	// With two-step sign-in on, the password alone isn't enough: the app
	// code comes next (two_step.go). The failures counted against this
	// address are kept until that code is right, so a right password can't be
	// used to wipe the count of wrong codes.
	st, err := s.Auth.TwoStepOf(user.ID)
	if err != nil {
		// Not knowing whether a second step is due is no reason to skip it.
		slog.Error("auth: sign-in refused, could not read the two-step state", "user", typedName, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't sign you in. Try again.")
		return
	}
	if st.Enabled {
		s.askForCode(w, user)
		return
	}
	s.LoginLimiter.RecordSuccess(ipKey)
	s.finishSignInVia(w, r, user, method, nil)
}

// failedSignInFloor is the least time a wrong sign-in takes when the directory
// is on. A name with a password here costs a password check and a name that
// goes to the directory costs a network lookup; waiting out the difference
// keeps the response time from telling which names exist here.
var failedSignInFloor = 750 * time.Millisecond

func waitOutFailure(ctx context.Context, started time.Time) {
	wait := failedSignInFloor - time.Since(started)
	if wait <= 0 {
		return
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) {
	s.startSessionVia(w, r, userID, auth.MethodPassword)
}

func (s *Server) startSessionVia(w http.ResponseWriter, r *http.Request, userID int64, method string) {
	token, expiresAt, err := s.Auth.CreateSessionVia(userID, method)
	if err != nil {
		log.Printf("api: create session: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't sign you in. Try again.")
		return
	}
	auth.SetSessionCookieAt(w, token, expiresAt, s.isHTTPS(r), s.cookiePath())
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.Auth.DeleteSession(cookie.Value)
	}
	auth.ClearSessionCookieAt(w, s.isHTTPS(r), s.cookiePath())
	if s.cfg.URLBase != "" {
		// A cookie from before the prefix was set sits at the root.
		auth.ClearSessionCookie(w, s.isHTTPS(r))
	}
	writeJSON(w, http.StatusOK, nil)
}

type sessionPayload struct {
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Method    string    `json:"method"`
	Current   bool      `json:"current"`
}

// handleListSessions lists the places the caller is signed in (GET
// /api/auth/sessions). It needs a browser session, not an API key.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	cookie, err := r.Cookie(auth.CookieName)
	if user == nil || err != nil {
		writeError(w, http.StatusForbidden, "Signed-in devices can only be seen from your profile while signed in, not with an API key.")
		return
	}
	list, err := s.Auth.SessionsOf(user.ID, cookie.Value)
	if err != nil {
		log.Printf("api: list sessions: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the signed-in devices. Check the Mediarium log for details.")
		return
	}
	out := make([]sessionPayload, len(list))
	for i, x := range list {
		out[i] = sessionPayload{CreatedAt: x.CreatedAt, ExpiresAt: x.ExpiresAt, Method: x.Method, Current: x.Current}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleEndOtherSessions signs the caller out everywhere except this browser
// (DELETE /api/auth/sessions).
func (s *Server) handleEndOtherSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	cookie, err := r.Cookie(auth.CookieName)
	if user == nil || err != nil {
		writeError(w, http.StatusForbidden, "Signed-in devices can only be changed from your profile while signed in, not with an API key.")
		return
	}
	n, err := s.Auth.DeleteOtherSessions(user.ID, cookie.Value)
	if err != nil {
		log.Printf("api: end other sessions: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't sign out the other devices. Check the Mediarium log for details.")
		return
	}
	slog.Warn("auth: signed out all other devices", "user", user.Username, "ended", n, "ip", s.clientIP(r))
	writeJSON(w, http.StatusOK, map[string]int64{"ended": n})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if rejectBad(w, checkPassword(req.NewPassword, "New password")) {
		return
	}
	// Guessing the current password from a stolen session is slowed down like guessing it at sign-in.
	byUser := s.sec().byUser
	guessKey := passwordGuessKey(user.ID)
	if !byUser.Reserve(guessKey) { // counted before the slow check, given back unless the password is wrong
		writeError(w, http.StatusTooManyRequests, "Too many wrong passwords. Wait a few minutes and try again.")
		return
	}
	err := s.Auth.ChangePassword(user.ID, req.CurrentPassword, req.NewPassword)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "Your current password isn't right.")
		return
	}
	byUser.Refund(guessKey)
	if errors.Is(err, auth.ErrNotLocal) {
		writeError(w, http.StatusBadRequest, "Your account signs in through single sign-on or the directory, so its password is changed there, not here.")
		return
	}
	if err != nil {
		log.Printf("api: change password: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't change the password. Check the Mediarium log for details.")
		return
	}
	slog.Warn("auth: password changed by its owner", "user", user.Username, "ip", s.clientIP(r))
	// Changing the password signed every session out; this browser gets a new one.
	if _, err := r.Cookie(auth.CookieName); err == nil {
		s.startSession(w, r, user.ID)
	}
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	out := userPayload(user)
	if p, err := s.Auth.PermissionsOf(user.ID); err == nil {
		out["permissions"] = p // the app hides what the account may not do
	}
	if ids, err := s.Auth.IdentitiesOf(user.ID); err == nil {
		out["oidcLinked"] = hasProvider(ids, oidcProvider) // tied to a single sign-on account
	}
	writeJSON(w, http.StatusOK, out)
}

type updateProfileRequest struct {
	Username  string `json:"username"`
	Name      string `json:"name"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

// handleUpdateUserProfile lets the logged-in user change their username, name and
// email, returning the updated account.
func (s *Server) handleUpdateUserProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	var req updateProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.FirstName, req.LastName = splitName(req.Name, req.FirstName, req.LastName)
	req.Email = strings.TrimSpace(req.Email)
	// Only a changed username has to meet today's rules; one an older version
	// saved can stay as it is.
	nameProblem := ""
	if req.Username != user.Username {
		nameProblem = checkUsername(req.Username)
	}
	if rejectBad(w, nameProblem, checkEmail(req.Email), checkMaxLen(req.FirstName, "Name", nameMax)) {
		return
	}
	updated, err := s.Auth.UpdateProfile(user.ID, req.Username, req.FirstName, req.LastName, req.Email)
	if errors.Is(err, auth.ErrUsernameTaken) {
		writeError(w, http.StatusConflict, "That username is already taken.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, userPayload(updated))
}

type apiKeyPayload struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	ReadOnly  bool       `json:"readOnly"` // may only be used for GET requests
	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	Key       string     `json:"key,omitempty"` // only ever set on creation — see handleCreateAPIKey
}

// handleListAPIKeys lists the account's API keys without the keys themselves
// (only a hash is stored; see internal/auth.CreateAPIKey), just enough to
// recognise and revoke one later.
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	keys, err := s.Auth.ListAPIKeys(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]apiKeyPayload, len(keys))
	for i, k := range keys {
		out[i] = apiKeyPayload{ID: k.ID, Name: k.Name, ReadOnly: k.ReadOnly, CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt}
	}
	writeJSON(w, http.StatusOK, out)
}

type createAPIKeyRequest struct {
	Name string `json:"name"`
	// ReadOnly makes a key that can only read: any request that is not a GET
	// is refused. Good for a dashboard or a monitoring script.
	ReadOnly bool `json:"readOnly"`
}

// handleCreateAPIKey makes a new API key and returns the key itself this one
// time. Only a hash is kept, so there is no way to view it again later, the
// same as GitHub's personal access tokens.
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	var req createAPIKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if rejectBad(w, checkRequired(req.Name, "Give the key a name, for example: my script."), checkMaxLen(req.Name, "The key name", 60), checkNoControl(req.Name, "The key name")) {
		return
	}
	rawKey, err := s.Auth.CreateAPIKeyScoped(user.ID, req.Name, req.ReadOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditAccount(r, fmt.Sprintf("An API key named %q was made for account %q", req.Name, user.Username))
	writeJSON(w, http.StatusCreated, apiKeyPayload{Name: req.Name, ReadOnly: req.ReadOnly, Key: rawKey})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That API key doesn't exist.")
		return
	}
	if r.URL.Query().Get("remove") == "true" {
		removed, err := s.Auth.DeleteRevokedAPIKey(user.ID, id)
		switch {
		case err != nil:
			writeError(w, http.StatusInternalServerError, err.Error())
		case !removed:
			writeError(w, http.StatusConflict, "Only a key that has been revoked can be deleted. Revoke it first.")
		default:
			s.auditAccount(r, fmt.Sprintf("A revoked API key (number %d) of account %q was deleted", id, user.Username))
			writeJSON(w, http.StatusOK, nil)
		}
		return
	}
	if err := s.Auth.RevokeAPIKey(user.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditAccount(r, fmt.Sprintf("An API key (number %d) of account %q was revoked", id, user.Username))
	writeJSON(w, http.StatusOK, nil)
}
