package api

import (
	"errors"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
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
		"id": u.ID, "username": u.Username, "isAdmin": u.IsAdmin, "role": u.Role(),
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
			writeError(w, http.StatusForbidden, setupCodeMessage)
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
	ip := s.clientIP(r)
	ipKey := httpsec.LimitKey(ip)
	if !s.LoginLimiter.Allow(ipKey) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a few minutes and try again.")
		return
	}

	var req loginRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicBody)
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	userKey := strings.ToLower(strings.TrimSpace(req.Username))
	if len(userKey) > 128 {
		userKey = userKey[:128]
	}
	byUser := s.sec().byUser
	if !byUser.Allow(userKey) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a few minutes and try again.")
		return
	}
	user, err := s.Auth.Authenticate(req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.LoginLimiter.RecordFailure(ipKey)
		byUser.RecordFailure(userKey)
		slog.Warn("auth: failed sign-in", "user", userKey, "ip", ip)
		writeError(w, http.StatusUnauthorized, "Wrong username or password.")
		return
	}
	if err != nil {
		log.Printf("api: login: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't sign you in. Try again.")
		return
	}
	s.LoginLimiter.RecordSuccess(ipKey)
	if err := s.Auth.RecordLogin(user.ID); err != nil {
		log.Printf("api: login: %v", err) // bookkeeping only; the sign-in itself succeeded
	}
	s.startSession(w, r, user.ID)
	writeJSON(w, http.StatusOK, userPayload(user))
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) {
	token, expiresAt, err := s.Auth.CreateSession(userID)
	if err != nil {
		log.Printf("api: create session: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't sign you in. Try again.")
		return
	}
	auth.SetSessionCookie(w, token, expiresAt, s.isHTTPS(r))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.Auth.DeleteSession(cookie.Value)
	}
	auth.ClearSessionCookie(w, s.isHTTPS(r))
	writeJSON(w, http.StatusOK, nil)
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
	err := s.Auth.ChangePassword(user.ID, req.CurrentPassword, req.NewPassword)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "Your current password isn't right.")
		return
	}
	if err != nil {
		log.Printf("api: change password: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't change the password. Check the Mediarium log for details.")
		return
	}
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
		out[i] = apiKeyPayload{ID: k.ID, Name: k.Name, CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt}
	}
	writeJSON(w, http.StatusOK, out)
}

type createAPIKeyRequest struct {
	Name string `json:"name"`
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
	rawKey, err := s.Auth.CreateAPIKey(user.ID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiKeyPayload{Name: req.Name, Key: rawKey})
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
			writeJSON(w, http.StatusOK, nil)
		}
		return
	}
	if err := s.Auth.RevokeAPIKey(user.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
