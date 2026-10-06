package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// memberRoutes is every route a member (non-administrator) account may call.
// It is spelled out here on purpose: opening a route to members has to be a
// visible, reviewed change to this list, not a side effect.
var memberRoutes = []string{
	"GET /api/auth/me",
	"PUT /api/auth/profile",
	"POST /api/auth/change-password",
	"GET /api/auth/api-keys",
	"POST /api/auth/api-keys",
	"DELETE /api/auth/api-keys/{id}",
	"GET /api/health",
	"GET /api/dashboard",
	"GET /api/calendar",
	"GET /api/stats/library",
	"GET /api/exclusions",
	"POST /api/exclusions",
	"DELETE /api/exclusions/{kind}/{tmdbId}",
	"GET /api/calendar/feed",
	"POST /api/calendar/feed",
	"DELETE /api/calendar/feed",
	"GET /api/wanted",
	"GET /api/activity",
	"GET /api/modules",
	"GET /api/watched",
	"GET /api/requests",
	"DELETE /api/requests/{id}",
	"GET /api/discover/trending",
	"GET /api/discover/popular",
	"GET /api/discover/tv/trending",
	"GET /api/discover/tv/popular",
	"GET /api/discover/search",
	"GET /api/discover/genres",
	"GET /api/discover/browse",
	"GET /api/discover/list",
	"GET /api/discover/import-list",
	"GET /api/discover/for-you",
	"GET /api/tv/search",
	"GET /api/tmdb/search",
	"GET /api/tmdb/movies/{tmdbId}",
	"GET /api/tmdb/movies/{tmdbId}/similar",
	"GET /api/tmdb/tv/{tmdbId}",
	"GET /api/search",
	"POST /api/search/grab",
	"GET /api/quality-profiles",
	"GET /api/movies",
	"POST /api/movies",
	"GET /api/movies/{id}",
	"GET /api/movies/{id}/similar",
	"GET /api/movies/{id}/search",
	"POST /api/movies/{id}/search-now",
	"POST /api/movies/{id}/grab",
	"GET /api/tags",
	"PUT /api/movies/{id}/tags",
	"PUT /api/series/{id}/tags",
	"PUT /api/movies/{id}/monitored",
	"GET /api/series",
	"POST /api/series",
	"GET /api/series/{id}",
	"POST /api/series/{id}/refresh",
	"GET /api/series/{id}/search",
	"POST /api/series/{id}/search-now",
	"POST /api/series/{id}/grab",
	"PUT /api/series/{id}/monitored",
	"PUT /api/series/{id}/type",
	"PUT /api/series/{id}/seasons/{season}/monitored",
	"PUT /api/episodes/{id}/monitored",
	"GET /api/books",
	"POST /api/books",
	"GET /api/books/search",
	"GET /api/books/discover",
	"GET /api/books/subjects",
	"GET /api/books/{id}/links",
	"GET /api/books/progress",
	"GET /api/book-authors",
	"GET /api/book-works/{key}",
	"GET /api/book-authors/{key}/works",
	"PUT /api/book-authors/{key}/follow",
	"DELETE /api/book-authors/{key}/follow",
	"GET /api/book-works/{key}/series",
	"GET /api/book-series",
	"PUT /api/book-series/{source}/{key}/follow",
	"DELETE /api/book-series/{source}/{key}/follow",
	"GET /api/books/{id}/progress",
	"PUT /api/books/{id}/progress",
	"GET /api/books/{id}/read",
	"GET /api/books/{id}/tracks",
	"GET /api/books/{id}/listen/{n}",
	"GET /api/books/{id}",
	"PUT /api/books/{id}/want",
	"POST /api/books/{id}/search",
	"GET /api/books/{id}/releases",
	"POST /api/books/{id}/grab",
	"GET /api/music/profiles",
	"GET /api/music/tiers",
	"GET /api/music/search",
	"GET /api/music/discover",
	"GET /api/music/discover/artists",
	"GET /api/music/covers/release-group/{mbid}",
	"GET /api/music/artists",
	"POST /api/music/artists",
	"GET /api/music/artists/{id}",
	"GET /api/music/albums/{id}",
	"GET /api/music/albums/{id}/files",
	"GET /api/music/albums/{id}/cover",
	"GET /api/music/artists/{id}/cover",
	"PUT /api/music/albums/{id}/monitored",
	"POST /api/music/albums/{id}/search",
	"POST /api/music/albums/{id}/search-now",
	"POST /api/music/albums/{id}/grab",
	"GET /api/music/albums/{id}/events",
	"GET /api/music/wanted",
	"GET /api/movies/{id}/files",
	"GET /api/series/{id}/files",
	"GET /api/movies/{id}/events",
	"GET /api/series/{id}/events",
	"GET /api/files/stream",
	"GET /api/media-servers/links",
	"GET /api/queue",
	"POST /api/queue/{id}/retry",
	"GET /api/subtitles/wanted",
	"POST /api/subtitles/get",
	"POST /api/subtitles/timing",
	"GET /api/movies/{id}/subtitles",
	"POST /api/movies/{id}/subtitles/download",
	"GET /api/movies/{id}/subtitles/status",
	"GET /api/episodes/{id}/subtitles",
	"POST /api/episodes/{id}/subtitles/download",
	"GET /api/episodes/{id}/subtitles/status",
}

// mustBeAdmin are routes that must never be opened to members, whatever
// else changes: they show or change credentials, settings or accounts.
var mustBeAdmin = []string{
	"GET /api/users", "POST /api/users", "PUT /api/users/{id}", "DELETE /api/users/{id}",
	"GET /api/settings", "PUT /api/settings", "POST /api/settings/folder-create",
	"GET /api/indexers", "GET /api/usenet-servers", "GET /api/vpn/configs", "GET /api/notifications", "GET /api/media-servers",
	"GET /api/system/backup", "POST /api/system/restore",
	"GET /api/system/problems", "POST /api/system/problems/read", "GET /api/system/problems/export", "PUT /api/system/problems/notify",
	"DELETE /api/movies/{id}", "DELETE /api/books/{id}", "GET /api/books/import", "POST /api/books/import", "DELETE /api/series/{id}", "DELETE /api/music/artists/{id}", "PUT /api/music/artists/{id}",
	"POST /api/music/profiles", "PUT /api/music/profiles/{id}", "DELETE /api/music/profiles/{id}",
	"POST /api/quality-profiles", "PUT /api/quality-profiles/{id}", "DELETE /api/quality-profiles/{id}",
	"DELETE /api/queue/{id}", "DELETE /api/queue", "POST /api/queue/{id}/blocklist",
	"POST /api/queue/{id}/pause", "POST /api/queue/{id}/resume", "POST /api/queue/{id}/stop", "POST /api/queue/pause-all", "POST /api/queue/resume-all",
	"POST /api/library/import", "GET /api/metrics", "GET /api/usage",
	"PUT /api/library/bulk/monitored", "PUT /api/library/bulk/no-upgrade", "PUT /api/library/bulk/profile", "PUT /api/library/bulk/sources", "PUT /api/library/bulk/tags",
	"POST /api/library/bulk/search-now", "POST /api/library/bulk/remove",
	"PUT /api/music/bulk/follow", "PUT /api/music/bulk/profile", "POST /api/music/bulk/remove",
}

func TestEveryRouteIsClassified(t *testing.T) {
	table := (&Server{}).protectedRoutes()

	var gotMember []string
	for pattern, level := range table.access {
		switch level {
		case accessMember:
			gotMember = append(gotMember, pattern)
		case accessAdmin:
		default:
			t.Errorf("route %q has no access level", pattern)
		}
	}
	want := append([]string(nil), memberRoutes...)
	sort.Strings(want)
	sort.Strings(gotMember)
	if strings.Join(gotMember, "\n") != strings.Join(want, "\n") {
		t.Fatalf("member routes changed.\n got:\n%s\n\nwant:\n%s\n\nIf this is deliberate, update memberRoutes in this test.",
			strings.Join(gotMember, "\n"), strings.Join(want, "\n"))
	}
	for _, p := range mustBeAdmin {
		if table.access[p] != accessAdmin {
			t.Errorf("%s must be admin-only, is %q", p, table.access[p])
		}
	}

	// Walk the package source: every /api route registered anywhere must be
	// either one of the public (signed-out) routes or in the access table,
	// registered through the member or admin group. A route added on some
	// other mux would bypass the role check, so it fails here.
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok {
					return true
				}
				pattern, _ := strconv.Unquote(lit.Value)
				if !strings.Contains(pattern, "/api/") {
					return true
				}
				recv, _ := sel.X.(*ast.Ident)
				// member.Need(...).HandleFunc and the named permission groups
				// (releases, manage, play, subs) are member routes too.
				if c, ok := sel.X.(*ast.CallExpr); ok {
					if fs, ok := c.Fun.(*ast.SelectorExpr); ok && fs.Sel.Name == "Need" {
						if id, ok := fs.X.(*ast.Ident); ok && id.Name == "member" {
							recv = id
						}
					}
				}
				if recv != nil && (recv.Name == "releases" || recv.Name == "manage" || recv.Name == "play" || recv.Name == "subs") {
					recv = &ast.Ident{Name: "member"}
				}
				switch {
				case recv != nil && recv.Name == "public":
					// signed-out routes and the hand-off to the auth middleware
				case recv != nil && recv.Name == "root" && pattern == "/api/":
				case recv != nil && (recv.Name == "member" || recv.Name == "admin"):
					if _, ok := table.access[pattern]; !ok {
						t.Errorf("%s: %s registered but missing from the access table", name, pattern)
					}
					seen[pattern] = true
				default:
					t.Errorf("%s: %s is registered outside the member/admin route groups", name, pattern)
				}
				return true
			})
		}
	}
	if len(seen) != len(table.access) {
		t.Errorf("source has %d member/admin registrations, table has %d", len(seen), len(table.access))
	}
}
