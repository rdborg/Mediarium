package api

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ryanborg/mediarium/internal/auth"
)

// handleVersion reports the running build's version (PRD §8 — "version-
// pinned Docker tags, changelog per release"). Public/unauthenticated
// since it's shown on the About page before login and isn't sensitive.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}

// handleOnboardingStatus tells the frontend whether to show the first-run
// wizard (PRD §5.2) or the normal app shell.
func (s *Server) handleOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	needed, err := s.Auth.FirstRunNeeded()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"firstRunNeeded": needed})
}

type createAdminRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Name      string `json:"name"`      // one free-form display name; optional
	FirstName string `json:"firstName"` // still accepted (older clients)
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

// userPayload is the shape every endpoint that describes the logged-in user
// returns (login, onboarding, /me, profile update).
func userPayload(u *auth.User) map[string]any {
	return map[string]any{
		"id": u.ID, "username": u.Username, "isAdmin": u.IsAdmin,
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

// validEmail is deliberately loose (one @, something on both sides, a dot in
// the domain): it catches typos, not every RFC quirk.
func validEmail(email string) bool {
	if email == "" || strings.ContainsAny(email, " ,;<>") || strings.IndexFunc(email, unicode.IsSpace) >= 0 {
		return false
	}
	at := strings.IndexByte(email, '@')
	if at < 1 || at != strings.LastIndexByte(email, '@') {
		return false
	}
	domain := email[at+1:]
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

// handleCreateAdmin is onboarding wizard step 1 (PRD §5.2). Refuses once an
// admin already exists — it's not a general "create user" endpoint.
func (s *Server) handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	needed, err := s.Auth.FirstRunNeeded()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !needed {
		writeError(w, http.StatusConflict, "an admin account already exists")
		return
	}

	var req createAdminRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Username) < 3 || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "username must be at least 3 characters and password at least 8")
		return
	}

	req.FirstName, req.LastName = splitName(req.Name, req.FirstName, req.LastName)
	req.Email = strings.TrimSpace(req.Email)
	if req.Email != "" && !validEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "that does not look like a valid email address")
		return
	}

	user, err := s.Auth.CreateUserWithProfile(req.Username, req.Password, req.FirstName, req.LastName, req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.startSession(w, user.ID)
	writeJSON(w, http.StatusCreated, userPayload(user))
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.LoginLimiter.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts, try again later")
		return
	}

	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := s.Auth.Authenticate(req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.LoginLimiter.RecordFailure(ip)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.LoginLimiter.RecordSuccess(ip)
	s.startSession(w, user.ID)
	writeJSON(w, http.StatusOK, userPayload(user))
}

// clientIP prefers X-Forwarded-For (PRD deployments sit behind a reverse
// proxy per PRD §5.1) and falls back to the direct connection's address.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i != -1 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) startSession(w http.ResponseWriter, userID int64) {
	token, expiresAt, err := s.Auth.CreateSession(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	auth.SetSessionCookie(w, token, expiresAt)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.Auth.DeleteSession(cookie.Value)
	}
	auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, nil)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	err := s.Auth.ChangePassword(user.ID, req.CurrentPassword, req.NewPassword)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, userPayload(user))
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
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req updateProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.FirstName, req.LastName = splitName(req.Name, req.FirstName, req.LastName)
	req.Email = strings.TrimSpace(req.Email)
	if len(req.Username) < 3 {
		writeError(w, http.StatusBadRequest, "username must be at least 3 characters")
		return
	}
	if req.Email != "" && !validEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "that does not look like a valid email address")
		return
	}
	updated, err := s.Auth.UpdateProfile(user.ID, req.Username, req.FirstName, req.LastName, req.Email)
	if errors.Is(err, auth.ErrUsernameTaken) {
		writeError(w, http.StatusConflict, "that username is already taken")
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

// handleListAPIKeys never returns the raw key (it's only stored as a hash
// — see internal/auth.CreateAPIKey) — just enough to identify and revoke
// one later.
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
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

// handleCreateAPIKey is the only place the raw key is ever returned — PRD
// §11's "credentials never logged/exposed beyond what's needed" means
// there's no way to view it again later, same as e.g. GitHub's own
// personal-access-token flow.
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req createAPIKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
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
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid api key id")
		return
	}
	if err := s.Auth.RevokeAPIKey(user.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
