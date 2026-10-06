package api

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/auth"
)

// access is who may call a signed-in route.
type access string

const (
	accessMember access = "member" // any signed-in account
	accessAdmin  access = "admin"  // administrators only
)

// forbiddenMessage is the 403 body for a member calling an admin route.
const forbiddenMessage = "Only an administrator can do this."

// notAllowedMessage is the 403 body for a basic account calling a route its
// permissions leave out.
const notAllowedMessage = "Your account isn't allowed to do this. An administrator can change that in Settings > Accounts."

// routeTable is the signed-in half of the API. A route can only be added
// through one of its two groups (member or admin), so every route carries an
// explicit access level and Routes reads as the single, auditable list of
// who may call what.
type routeTable struct {
	mux    *http.ServeMux
	access map[string]access // pattern -> level
	// gate, when set, runs after the role check on every matched route; it
	// answers the request itself and returns false to stop it (a module
	// that is switched off).
	gate func(http.ResponseWriter, *http.Request) bool
	// perms lists, for member routes, the permissions a basic account needs
	// (see auth.Permissions); permsOf reads an account's.
	perms   map[string][]string
	permsOf func(userID int64) (auth.Permissions, error)
}

func newRouteTable() *routeTable {
	return &routeTable{mux: http.NewServeMux(), access: map[string]access{}, perms: map[string][]string{}}
}

// routeGroup registers routes at one access level.
type routeGroup struct {
	t     *routeTable
	level access
	need  []string
}

// Need returns the group with permissions a basic account must have for the
// routes registered through it. Administrators always pass.
func (g routeGroup) Need(perms ...string) routeGroup {
	g.need = append(append([]string(nil), g.need...), perms...)
	return g
}

func (t *routeTable) group(level access) routeGroup { return routeGroup{t: t, level: level} }

// HandleFunc registers pattern at the group's access level.
func (g routeGroup) HandleFunc(pattern string, h http.HandlerFunc) {
	if _, dup := g.t.access[pattern]; dup {
		panic("api: route registered twice: " + pattern)
	}
	g.t.mux.HandleFunc(pattern, h)
	g.t.access[pattern] = g.level
	if len(g.need) > 0 {
		g.t.perms[pattern] = g.need
	}
}

// ServeHTTP checks the caller's role against the matched route before the
// handler runs. It sits behind the auth middleware, so there is always a
// user. A request that matches no route falls through to the mux for its
// usual 404/405; a matched route with no access level (impossible through
// routeGroup, but checked anyway) is refused, so the default is deny.
func (t *routeTable) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, pattern := t.mux.Handler(r)
	if pattern != "" {
		level, ok := t.access[pattern]
		user := auth.UserFromContext(r.Context())
		switch {
		case user == nil:
			writeError(w, http.StatusUnauthorized, "You need to sign in first.")
			return
		case !ok:
			writeError(w, http.StatusForbidden, forbiddenMessage)
			return
		case level == accessAdmin && !user.IsAdmin:
			writeError(w, http.StatusForbidden, forbiddenMessage)
			return
		}
		if need := t.perms[pattern]; len(need) > 0 && !user.IsAdmin {
			if t.permsOf == nil {
				writeError(w, http.StatusForbidden, notAllowedMessage)
				return
			}
			p, err := t.permsOf(user.ID)
			if err != nil {
				writeError(w, http.StatusForbidden, notAllowedMessage)
				return
			}
			for _, n := range need {
				if !p.Allows(n) {
					writeError(w, http.StatusForbidden, notAllowedMessage)
					return
				}
			}
		}
		if t.gate != nil && !t.gate(w, r) {
			return
		}
		if !user.IsAdmin {
			w = &errorScrubber{ResponseWriter: w, r: r}
		}
	}
	t.mux.ServeHTTP(w, r)
}

// genericServerError is what a member sees instead of a server-side error.
const genericServerError = "Something went wrong on the server. An administrator can find the details in the log."

// errorScrubber replaces the message of a 5xx JSON error with a generic one
// for accounts that are not administrators. Handlers put the text of the
// underlying error in those responses (SQL, folder paths, URLs of upstream
// services), which is useful when setting Mediarium up and not something a
// family member, or anyone who got hold of one account, should see. The
// original goes to the log.
type errorScrubber struct {
	http.ResponseWriter
	r     *http.Request
	scrub bool
}

func (e *errorScrubber) WriteHeader(code int) {
	e.scrub = code >= 500 && strings.HasPrefix(e.Header().Get("Content-Type"), "application/json")
	e.ResponseWriter.WriteHeader(code)
}

func (e *errorScrubber) Write(p []byte) (int, error) {
	if !e.scrub {
		return e.ResponseWriter.Write(p)
	}
	e.scrub = false // the whole error body is one write
	body := strings.TrimSpace(string(p))
	if len(body) > 500 {
		body = body[:500]
	}
	slog.Warn("api: server error hidden from a non-administrator", "method", e.r.Method, "path", e.r.URL.Path, "response", body)
	if _, err := e.ResponseWriter.Write([]byte(`{"error":"` + genericServerError + `"}` + "\n")); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (e *errorScrubber) Unwrap() http.ResponseWriter { return e.ResponseWriter }

// Flush and ReadFrom keep streaming (video, downloads) as fast as without
// the wrapper.
func (e *errorScrubber) Flush() {
	if f, ok := e.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (e *errorScrubber) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := e.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(struct{ io.Writer }{e.ResponseWriter}, r)
}

// sessionOnlyMessage is the 403 body when an API key is used for something that
// needs a signed-in browser.
const sessionOnlyMessage = "This can only be done from the Mediarium web page while you are signed in. It can't be done with an API key."

// requireSession refuses a request made with an API key (X-API-Key), writing
// the 403 itself. It guards the few things that would let a stolen key outlive
// its own revocation or take over the server: making new accounts and API
// keys, changing accounts, restoring a backup (which replaces every account,
// key and setting) and switching on pushed updates. A person doing them signs
// in first; scripts never need to.
func requireSession(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-API-Key") == "" {
		return true
	}
	slog.Warn("api: refused an API key for something that needs a signed-in session", "method", r.Method, "path", r.URL.Path)
	writeError(w, http.StatusForbidden, sessionOnlyMessage)
	return false
}

// isAdminRequest reports whether the signed-in caller is an administrator,
// for member routes that show members a trimmed view (health, dashboard).
func isAdminRequest(r *http.Request) bool {
	u := auth.UserFromContext(r.Context())
	return u != nil && u.IsAdmin
}
