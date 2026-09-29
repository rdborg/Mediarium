package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ryanborg/mediarium/internal/auth"
)

// accountPayload is one row of the account list an administrator manages.
// name is the name as stored (may be empty; the app then shows the username).
type accountPayload struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	Role        string     `json:"role"` // "admin" or "member"
	IsAdmin     bool       `json:"isAdmin"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt"` // null until the first sign-in
}

func toAccountPayload(a *auth.Account) accountPayload {
	return accountPayload{
		ID: a.ID, Username: a.Username, Name: a.Name(), Email: a.Email,
		Role: a.Role(), IsAdmin: a.IsAdmin, CreatedAt: a.CreatedAt, LastLoginAt: a.LastLoginAt,
	}
}

// userRef names the account that did something, e.g. added a title.
type userRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// accountNames returns a lookup from account id to its reference, loading
// the account list at most once (on first use) per call of accountNames. An
// unknown or deleted account resolves to nil.
func (s *Server) accountNames() func(int64) *userRef {
	var names map[int64]string
	return func(id int64) *userRef {
		if id <= 0 {
			return nil
		}
		if names == nil {
			names = map[int64]string{}
			list, err := s.Auth.ListAccounts()
			if err != nil {
				log.Printf("api: list accounts for names: %v", err)
			}
			for i := range list {
				names[list[i].ID] = list[i].DisplayName()
			}
		}
		name, ok := names[id]
		if !ok {
			return nil
		}
		return &userRef{ID: id, Name: name}
	}
}

// requester is the signed-in account behind r: its id (0 if none) and the
// " by Name" suffix activity messages use.
func requester(r *http.Request) (id int64, byline string) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return 0, ""
	}
	return u.ID, " by " + u.DisplayName()
}

// parseRole maps "admin"/"member" to is_admin.
func parseRole(role string) (isAdmin bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case auth.RoleAdmin:
		return true, true
	case auth.RoleMember:
		return false, true
	}
	return false, false
}

// writeAccountError turns the account rules into plain answers.
func writeAccountError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "That account does not exist.")
	case errors.Is(err, auth.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "That username is already taken.")
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(w, http.StatusConflict, "There must always be at least one administrator.")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// handleListUsers lists every account with its role and last sign-in.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.Auth.ListAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]accountPayload, len(list))
	for i := range list {
		out[i] = toAccountPayload(&list[i])
	}
	writeJSON(w, http.StatusOK, out)
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

// handleCreateUser adds an account for someone else, for example a family
// member, with the role the administrator picks.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if len(req.Username) < 3 {
		writeError(w, http.StatusBadRequest, "username must be at least 3 characters")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	isAdmin, ok := parseRole(req.Role)
	if !ok {
		writeError(w, http.StatusBadRequest, `role must be "admin" or "member"`)
		return
	}
	if req.Email != "" && !validEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "that does not look like a valid email address")
		return
	}
	first, last := splitName(req.Name, "", "")
	created, err := s.Auth.CreateAccount(req.Username, req.Password, first, last, req.Email, isAdmin)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAccountPayload(created))
}

type updateUserRequest struct {
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Role     *string `json:"role"`
	Password *string `json:"password"`
}

func accountID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return 0, false
	}
	return id, true
}

// handleUpdateUser changes another account's name, email or role, or sets a
// new password for it (which signs that account out everywhere). An
// administrator cannot take away their own administrator role.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := accountID(w, r)
	if !ok {
		return
	}
	var req updateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var upd auth.AccountUpdate
	if req.Name != nil {
		first, last := splitName(*req.Name, "", "")
		upd.FirstName, upd.LastName = &first, &last
	}
	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		if email != "" && !validEmail(email) {
			writeError(w, http.StatusBadRequest, "that does not look like a valid email address")
			return
		}
		upd.Email = &email
	}
	if req.Role != nil {
		isAdmin, ok := parseRole(*req.Role)
		if !ok {
			writeError(w, http.StatusBadRequest, `role must be "admin" or "member"`)
			return
		}
		if me := auth.UserFromContext(r.Context()); me != nil && me.ID == id && !isAdmin {
			writeError(w, http.StatusBadRequest, "You cannot remove your own administrator role. Ask another administrator to do it.")
			return
		}
		upd.IsAdmin = &isAdmin
	}
	if req.Password != nil {
		if len(*req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		upd.Password = req.Password
	}
	updated, err := s.Auth.UpdateAccount(id, upd)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAccountPayload(updated))
}

// handleDeleteUser removes an account with its sessions and API keys. Titles
// it added stay in the library. An administrator cannot delete themselves.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := accountID(w, r)
	if !ok {
		return
	}
	if me := auth.UserFromContext(r.Context()); me != nil && me.ID == id {
		writeError(w, http.StatusBadRequest, "You cannot delete your own account.")
		return
	}
	if err := s.Auth.DeleteAccount(id); err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
