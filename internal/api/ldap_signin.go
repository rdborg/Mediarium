package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/ldapauth"
	"github.com/rdborg/mediarium/internal/settings"
)

// Sign-in with a directory (LDAP or Active Directory). The user name and
// password go through the same form and the same limits as a local sign-in
// (handleLogin); this file has what is special about a directory: turning what
// the directory knows into an account, and the settings pages for it.

// errNoAccountHere is a good directory sign-in by someone who has no account
// here and whom the setup does not make one for.
var errNoAccountHere = errors.New("no account here for that person")

// directorySignIn checks the password with the directory and finds or makes the
// account. No password is stored anywhere.
func (s *Server) directorySignIn(ctx context.Context, cfg ldapauth.Config, username, password string) (*auth.User, error) {
	entry, err := s.sso().ldap.Authenticate(ctx, cfg, username, password)
	if err != nil {
		return nil, err
	}
	return s.accountForDirectory(cfg, entry)
}

// accountForDirectory is the account of a person the directory vouches for. The
// account is found by the directory's stable id, never by the user name, so a
// renamed or recycled name cannot reach somebody else's account.
func (s *Server) accountForDirectory(cfg ldapauth.Config, e *ldapauth.Entry) (*auth.User, error) {
	cfg = cfg.Defaults()
	u, err := s.Auth.UserByIdentity(auth.SourceLDAP, e.ID)
	switch {
	case err == nil:
		if u.Disabled {
			return nil, auth.ErrAccountDisabled
		}
		// The sign-in name follows the directory when the person is renamed
		// there, so the old name is free for whoever gets it next.
		if _, err := s.Auth.RenameExternal(u.ID, e.Username); err != nil {
			slog.Warn("auth: could not follow a rename in the directory", "err", err)
		}
		s.syncDirectoryRole(cfg, u, e)
		if err := s.Auth.RefreshExternalProfile(u.ID, e.Name, e.Email); err != nil {
			slog.Warn("auth: could not refresh a directory account's details", "err", err)
		}
		return s.Auth.GetUser(u.ID)
	case errors.Is(err, auth.ErrNoIdentity):
		if !cfg.AutoCreate {
			return nil, errNoAccountHere
		}
		role, groupRole := directoryGroups(cfg, e).newAccountRole()
		created, err := s.Auth.CreateExternalUser(auth.ExternalProfile{
			Provider: auth.SourceLDAP, Subject: e.ID, Username: e.Username, Name: e.Name, Email: e.Email, Role: role,
		})
		if err != nil {
			return nil, err
		}
		if groupRole != "" {
			if err := s.Auth.SetGroupRole(created.ID, groupRole); err != nil {
				slog.Error("auth: could not note where an account's role comes from", "user", created.Username, "err", err)
			}
		}
		slog.Info("auth: account made on a first directory sign-in", "user", created.Username, "role", created.Role())
		return created, nil
	}
	return nil, err
}

// directoryGroups is what the directory's groups say about one person.
func directoryGroups(cfg ldapauth.Config, e *ldapauth.Entry) groupRoles {
	return groupRoles{
		adminOn: strings.TrimSpace(cfg.AdminGroupFilter) != "", viewerOn: strings.TrimSpace(cfg.ViewerGroupFilter) != "",
		inAdmin: e.Admin, inViewer: e.Viewer, def: cfg.DefaultRole,
	}
}

// syncDirectoryRole keeps the role in step with the administrator group and
// the viewer group, when the setup has them (see groupRoles). Other roles are
// left as an administrator set them. The last administrator is never demoted.
func (s *Server) syncDirectoryRole(cfg ldapauth.Config, u *auth.User, e *ldapauth.Entry) {
	s.applyGroupRoles(u, directoryGroups(cfg, e))
}

// ---- Settings ----

type ldapPayload struct {
	ldapauth.Config
	HasBindPassword bool `json:"hasBindPassword"`
	DisabledByEnv   bool `json:"disabledByEnv"`
}

func (s *Server) ldapPayload() ldapPayload {
	c := s.ldapSaved()
	has := c.BindPassword != ""
	c.BindPassword = ""
	return ldapPayload{Config: c, HasBindPassword: has, DisabledByEnv: s.cfg.DisableSSO}
}

// GET /api/auth/ldap
func (s *Server) handleGetLDAP(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ldapPayload())
}

const ldapFieldMax = 2000

// ldapFromRequest reads the setup from a request. A blank bind password keeps
// the saved one, but only for the same server address.
func (s *Server) ldapFromRequest(r *http.Request, extra any) (ldapauth.Config, error) {
	body := struct {
		ldapauth.Config
		TestUsername string `json:"testUsername"`
		TestPassword string `json:"testPassword"`
	}{}
	if err := decodeJSON(r, &body); err != nil {
		return ldapauth.Config{}, err
	}
	c := body.Config
	if t, ok := extra.(*[2]string); ok {
		t[0], t[1] = body.TestUsername, body.TestPassword
	}
	for _, p := range []*string{&c.URL, &c.BindDN, &c.BaseDN, &c.UserFilter, &c.UsernameAttr, &c.NameAttr, &c.EmailAttr, &c.IDAttr, &c.AdminGroupFilter, &c.ViewerGroupFilter, &c.GroupBaseDN} {
		*p = strings.TrimSpace(*p)
		if utf8.RuneCountInString(*p) > ldapFieldMax {
			return ldapauth.Config{}, errors.New("a field is too long")
		}
	}
	if len(c.CACert) > 20000 || len(c.BindPassword) > 1024 {
		return ldapauth.Config{}, errors.New("a field is too long")
	}
	c.CACert = strings.TrimSpace(c.CACert)
	if c.BindPassword == "" {
		if old := s.ldapSaved(); secretKept(old.URL, c.URL) {
			c.BindPassword = old.BindPassword
		}
	}
	return c, nil
}

// PUT /api/auth/ldap saves the setup. Switching it on needs a complete setup
// and a bind account the directory accepts; switched off, whatever is there
// is kept as a draft.
func (s *Server) handlePutLDAP(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	c, err := s.ldapFromRequest(r, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if c.CheckGroups() != nil {
		writeError(w, http.StatusBadRequest, "The viewer group filter has to be different from the administrator group filter.")
		return
	}
	if c.Enabled {
		if err := c.Check(); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Can't switch directory sign-in on yet: %s.", err.Error()))
			return
		}
		if err := s.sso().ldap.Probe(c); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Can't switch directory sign-in on yet: %s.", ldapProblem(err)))
			return
		}
	}
	b, _ := json.Marshal(c)
	if err := s.Settings.Set(settings.KeyAuthLDAP, string(b), true); err != nil {
		slog.Error("auth: could not save the directory setup", "err", err)
		writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
		return
	}
	actor := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		actor = u.Username
	}
	slog.Warn("auth: directory sign-in settings saved", "on", c.Enabled, "server", c.HostPort(), "by", actor)
	writeJSON(w, http.StatusOK, s.ldapPayload())
}

// ldapProblem is the sentence for a directory error, without the sentinel's prefix.
func ldapProblem(err error) string {
	text := strings.TrimPrefix(err.Error(), ldapauth.ErrUnavailable.Error()+": ")
	text = strings.TrimSuffix(text, ".")
	if text == "" {
		return "the directory couldn't be used"
	}
	return strings.ToLower(text[:1]) + text[1:]
}

// POST /api/auth/ldap/test {config..., testUsername, testPassword}: connects
// with the details sent (not yet saved), finds the user and shows what was
// found. With a password it also checks it. Nobody is signed in and nothing is
// stored.
func (s *Server) handleTestLDAP(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	var typed [2]string
	c, err := s.ldapFromRequest(r, &typed)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if err := c.Check(); err != nil {
		writeError(w, http.StatusBadRequest, capitalise(err.Error())+".")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	rep, err := s.sso().ldap.Test(ctx, c, strings.TrimSpace(typed[0]), typed[1])
	if err != nil {
		if errors.Is(err, ldapauth.ErrUnavailable) {
			writeError(w, http.StatusBadGateway, capitalise(ldapProblem(err))+".")
			return
		}
		if errors.Is(err, ldapauth.ErrAmbiguous) {
			writeError(w, http.StatusBadRequest, "More than one entry matches that username. Make the user filter more specific.")
			return
		}
		writeError(w, http.StatusBadRequest, capitalise(err.Error())+".")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
