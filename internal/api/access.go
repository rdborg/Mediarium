package api

import (
	"net/http"

	"github.com/ryanborg/mediarium/internal/auth"
)

// access is who may call a signed-in route.
type access string

const (
	accessMember access = "member" // any signed-in account
	accessAdmin  access = "admin"  // administrators only
)

// forbiddenMessage is the 403 body for a member calling an admin route.
const forbiddenMessage = "Only an administrator can do this."

// routeTable is the signed-in half of the API. A route can only be added
// through one of its two groups (member or admin), so every route carries an
// explicit access level and Routes reads as the single, auditable list of
// who may call what.
type routeTable struct {
	mux    *http.ServeMux
	access map[string]access // pattern -> level
}

func newRouteTable() *routeTable {
	return &routeTable{mux: http.NewServeMux(), access: map[string]access{}}
}

// routeGroup registers routes at one access level.
type routeGroup struct {
	t     *routeTable
	level access
}

func (t *routeTable) group(level access) routeGroup { return routeGroup{t: t, level: level} }

// HandleFunc registers pattern at the group's access level.
func (g routeGroup) HandleFunc(pattern string, h http.HandlerFunc) {
	if _, dup := g.t.access[pattern]; dup {
		panic("api: route registered twice: " + pattern)
	}
	g.t.mux.HandleFunc(pattern, h)
	g.t.access[pattern] = g.level
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
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		case !ok:
			writeError(w, http.StatusForbidden, forbiddenMessage)
			return
		case level == accessAdmin && !user.IsAdmin:
			writeError(w, http.StatusForbidden, forbiddenMessage)
			return
		}
	}
	t.mux.ServeHTTP(w, r)
}

// isAdminRequest reports whether the signed-in caller is an administrator,
// for member routes that show members a trimmed view (health, dashboard).
func isAdminRequest(r *http.Request) bool {
	u := auth.UserFromContext(r.Context())
	return u != nil && u.IsAdmin
}
