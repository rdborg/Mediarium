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
	"GET /api/plex-watchlist", "PUT /api/plex-watchlist", "DELETE /api/plex-watchlist", "POST /api/plex-watchlist/pin",
	"GET /api/plex-watchlist/pin/{pinId}", "POST /api/plex-watchlist/check", "GET /api/plex-watchlist/items",
	"POST /api/feedback",
	"GET /api/auth/me",
	"GET /api/auth/two-step",
	"POST /api/auth/two-step/setup",
	"POST /api/auth/two-step/enable",
	"POST /api/auth/two-step/disable",
	"POST /api/auth/two-step/recovery",
	"PUT /api/auth/profile",
	"POST /api/auth/change-password",
	"GET /api/auth/sessions",
	"DELETE /api/auth/sessions",
	"GET /api/auth/api-keys",
	"POST /api/auth/api-keys",
	"DELETE /api/auth/api-keys/{id}",
	"GET /api/oauth/requests/{id}",
	"POST /api/oauth/requests/{id}",
	"GET /api/auth/ereader",
	"POST /api/auth/ereader/opds",
	"DELETE /api/auth/ereader/opds",
	"POST /api/auth/ereader/kobo",
	"DELETE /api/auth/ereader/kobo",
	"POST /api/auth/ereader/kobo/resync",
	"GET /api/auth/passkeys",
	"POST /api/auth/passkeys/register/begin",
	"POST /api/auth/passkeys/register/finish",
	"PUT /api/auth/passkeys/{id}",
	"DELETE /api/auth/passkeys/{id}",
	"POST /api/auth/oidc/link",
	"DELETE /api/auth/oidc/link",
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
	"GET /api/search/waiting",
	"GET /api/activity",
	"GET /api/modules",
	"GET /api/watched",
	"GET /api/lifecycle/shelf", "POST /api/lifecycle/keep",
	"GET /api/requests",
	"DELETE /api/requests/{id}",
	"GET /api/requests/status", "POST /api/requests/removal", "GET /api/issues", "POST /api/issues",
	"GET /api/auth/request-notices", "PUT /api/auth/request-notices", "POST /api/auth/request-notices/devices", "DELETE /api/auth/request-notices/devices/{id}",
	"GET /api/discover/trending",
	"GET /api/discover/popular",
	"GET /api/discover/tv/trending",
	"GET /api/discover/tv/popular",
	"GET /api/discover/search",
	"GET /api/events",
	"GET /api/library/suggest",
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
	"GET /api/tmdb/people/{personId}",
	"GET /api/search",
	"POST /api/search/grab",
	"GET /api/quality-profiles",
	"GET /api/custom-formats",
	"GET /api/stats/history",
	"POST /api/custom-formats/test",
	"GET /api/movies",
	"POST /api/movies",
	"GET /api/movies/{id}",
	"GET /api/movies/{id}/similar",
	"GET /api/movies/{id}/search",
	"POST /api/movies/{id}/search-now",
	"POST /api/movies/{id}/grab",
	"POST /api/movies/{id}/replace-smaller",
	"GET /api/tags",
	"PUT /api/movies/{id}/tags",
	"PUT /api/series/{id}/tags",
	"PUT /api/movies/{id}/monitored",
	"GET /api/series",
	"POST /api/series",
	"GET /api/series/{id}",
	"POST /api/series/{id}/refresh",
	"GET /api/series/{id}/search",
	"POST /api/series/{id}/search-complete",
	"POST /api/series/{id}/search-now",
	"POST /api/series/{id}/grab",
	"PUT /api/series/{id}/monitored",
	"POST /api/series/{id}/monitor-mode",
	"PUT /api/series/{id}/type",
	"GET /api/series/{id}/numbering",
	"POST /api/series/{id}/numbering/refresh",
	"GET /api/series/{id}/numbering/custom",
	"PUT /api/series/{id}/numbering/custom",
	"PUT /api/series/{id}/seasons/{season}/monitored",
	"PUT /api/episodes/{id}/monitored",
	"GET /api/books",
	"POST /api/books",
	"GET /api/books/search",
	"GET /api/books/search/google",
	"POST /api/books/search/google",
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
	"GET /api/books/{id}/own-cover",
	"POST /api/books/{id}/cover",
	"POST /api/books/{id}/cover/link",
	"GET /api/books/{id}/cover/editions",
	"PUT /api/books/{id}/cover/edition",
	"DELETE /api/books/{id}/cover",
	"PUT /api/books/{id}/want",
	"POST /api/books/{id}/search",
	"GET /api/books/{id}/releases",
	"POST /api/books/{id}/grab",
	"GET /api/music/profiles",
	"GET /api/music/tiers",
	"GET /api/music/search",
	"GET /api/music/discover",
	"GET /api/music/discover/artists",
	"GET /api/music/preview/{mbid}",
	"GET /api/collections", "GET /api/collections/{id}",
	"GET /api/library/folders",
	"GET /api/library/counts", "GET /api/library/owned", "GET /api/queue/count",
	"GET /api/library/file-details",
	"PUT /api/movies/{id}/folder",
	"PUT /api/series/{id}/folder",
	"GET /api/music/covers/release-group/{mbid}",
	"GET /api/music/artists",
	"POST /api/music/artists",
	"GET /api/music/artists/{id}",
	"GET /api/music/artists/{id}/discography",
	"GET /api/music/filters",
	"POST /api/music/artists/{id}/albums",
	"GET /api/music/albums/{id}",
	"GET /api/music/albums/{id}/files",
	"GET /api/music/albums/{id}/cover",
	"GET /api/music/artists/{id}/cover",
	"PUT /api/music/albums/{id}/monitored",
	"PUT /api/music/tracks/{id}/monitored",
	"POST /api/music/albums/{id}/search",
	"POST /api/music/albums/{id}/search-now",
	"POST /api/music/albums/{id}/grab",
	"GET /api/music/albums/{id}/events",
	"GET /api/music/wanted",
	"GET /api/music/playlists", "POST /api/music/playlists", "GET /api/music/playlists/{id}", "PUT /api/music/playlists/{id}", "DELETE /api/music/playlists/{id}",
	"POST /api/music/playlists/{id}/tracks", "DELETE /api/music/playlists/{id}/tracks/{entry}", "PUT /api/music/playlists/{id}/order",
	"GET /api/music/tracks/{id}/lyrics", "GET /api/music/tracks/{id}/gain", "GET /api/music/player-settings",
	"GET /api/music/listenbrainz", "PUT /api/music/listenbrainz", "DELETE /api/music/listenbrainz",
	"POST /api/music/listenbrainz/playing", "POST /api/music/listenbrainz/listen",
	"GET /api/movies/{id}/files", "GET /api/movies/{id}/copies", "GET /api/series/{id}/copies",
	"GET /api/series/{id}/files",
	"GET /api/movies/{id}/events",
	"GET /api/series/{id}/events",
	"GET /api/files/stream",
	"GET /api/media-servers/links", "GET /api/apps", "GET /api/books/{id}/comic", "GET /api/books/{id}/comic/{n}", "GET /api/books/{id}/file-cover",
	"GET /api/queue",
	"POST /api/queue/{id}/retry",
	"GET /api/subtitles/wanted", "GET /api/book-covers/{file}", "GET /api/img/{kind}/{size}/{file}", "GET /api/music/tracks/{id}/play",
	"POST /api/subtitles/get",
	"POST /api/subtitles/timing",
	"GET /api/movies/{id}/subtitles",
	"POST /api/movies/{id}/subtitles/download",
	"GET /api/movies/{id}/subtitles/status",
	"GET /api/episodes/{id}/subtitles",
	"POST /api/episodes/{id}/subtitles/download",
	"GET /api/episodes/{id}/subtitles/status",
	"GET /api/scores/{kind}/{tmdbId}",
}

// mustBeAdmin are routes that must never be opened to members, whatever
// else changes: they show or change credentials, settings or accounts.
var mustBeAdmin = []string{
	"GET /api/users", "POST /api/users", "PUT /api/users/{id}", "DELETE /api/users/{id}", "DELETE /api/users/{id}/two-step",
	"GET /api/auth/sign-in", "PUT /api/auth/sign-in", "GET /api/auth/oidc", "PUT /api/auth/oidc", "POST /api/auth/oidc/test",
	"GET /api/auth/ldap", "PUT /api/auth/ldap", "POST /api/auth/ldap/test", "GET /api/auth/proxy-signin", "PUT /api/auth/proxy-signin", "GET /api/auth/idle-signout", "PUT /api/auth/idle-signout",
	"GET /api/settings", "PUT /api/settings", "POST /api/settings/folder-create", "GET /api/settings/file-permissions", "PUT /api/settings/file-permissions",
	"POST /api/collections/{id}/add", "PUT /api/collections/{id}/follow",
	"GET /api/library/browse", "POST /api/movies/{id}/locate-file", "POST /api/movies/{id}/locate-folder", "POST /api/episodes/{id}/locate-file", "POST /api/series/{id}/change-folder",
	"POST /api/library/bulk/folder", "POST /api/library/move-files/preview", "POST /api/library/move-files", "GET /api/settings/metadata-files", "PUT /api/settings/metadata-files", "POST /api/library/metadata-files/run",
	"GET /api/indexers", "GET /api/usenet-servers", "GET /api/vpn/configs", "GET /api/notifications", "GET /api/media-servers",
	"GET /api/system/backup", "POST /api/system/restore",
	"GET /api/system/config-file", "POST /api/system/config-file/check", "POST /api/system/config-file/apply", "GET /api/system/config-file/start", "PUT /api/system/config-file/start",
	"POST /api/movies/{id}/copies", "POST /api/series/{id}/copies",
	"GET /api/import-lists", "POST /api/import-lists", "POST /api/import-lists/preview", "PUT /api/import-lists/{id}", "DELETE /api/import-lists/{id}",
	"PUT /api/import-lists/{id}/enabled", "POST /api/import-lists/{id}/run", "POST /api/import-lists/{id}/forget", "POST /api/import-lists/plex/{userId}/run",
	"GET /api/settings/simkl", "DELETE /api/settings/simkl", "POST /api/settings/simkl/pin", "GET /api/settings/simkl/pin/{code}",
	"GET /api/requests/settings", "PUT /api/requests/settings", "POST /api/issues/{id}/resolve", "POST /api/issues/{id}/reopen", "DELETE /api/issues/{id}",
	"GET /api/system/backups/cloud", "PUT /api/system/backups/cloud", "POST /api/system/backups/cloud/test", "POST /api/system/backups/cloud/upload", "GET /api/system/backups/cloud/list",
	"GET /api/usage-stats", "PUT /api/usage-stats", "POST /api/usage-stats/reset-id",
	"GET /api/system/problems", "POST /api/system/problems/read", "GET /api/system/problems/export", "PUT /api/system/problems/notify", "PUT /api/system/problems/debug",
	"DELETE /api/movies/{id}", "DELETE /api/books/{id}", "GET /api/books/import", "POST /api/books/import", "DELETE /api/series/{id}", "DELETE /api/music/artists/{id}", "PUT /api/music/artists/{id}",
	"POST /api/music/profiles", "PUT /api/music/profiles/{id}", "DELETE /api/music/profiles/{id}",
	"POST /api/quality-profiles", "POST /api/quality-profiles/simulate", "PUT /api/quality-profiles/{id}", "DELETE /api/quality-profiles/{id}",
	"POST /api/custom-formats", "PUT /api/custom-formats/{id}", "DELETE /api/custom-formats/{id}",
	"POST /api/custom-formats/assign", "POST /api/custom-formats/default-scores", "GET /api/custom-formats/trash-update", "POST /api/custom-formats/trash-update",
	"GET /api/trash-guides/{app}", "POST /api/trash-guides/{app}/formats", "POST /api/trash-guides/{app}/profiles",
	"GET /api/proxy", "PUT /api/proxy", "POST /api/proxy/test", "GET /api/download-client/torrents", "GET /api/debrid/torrents", "POST /api/debrid/torrents/{id}/remove", "POST /api/download-client/torrents/{hash}/{action}", "GET /api/notifications/push-key",
	"GET /api/usenet-client", "PUT /api/usenet-client", "POST /api/usenet-client/test", "GET /api/usenet-client/jobs", "POST /api/usenet-client/jobs/{id}/{action}",
	"DELETE /api/queue/{id}", "DELETE /api/queue", "POST /api/queue/{id}/blocklist",
	"POST /api/queue/{id}/pause", "POST /api/queue/{id}/resume", "POST /api/queue/{id}/stop", "POST /api/queue/pause-all", "POST /api/queue/resume-all",
	"GET /api/queue/{id}/torrent", "PUT /api/queue/{id}/torrent", "PUT /api/queue/{id}/torrent/files",
	"GET /api/downloads/torrent-options", "PUT /api/downloads/torrent-options", "GET /api/indexers/seed-rules", "PUT /api/indexers/{id}/seed-rules",
	"POST /api/library/import", "GET /api/metrics", "GET /api/usage",
	"PUT /api/library/bulk/monitored", "PUT /api/library/bulk/no-upgrade", "PUT /api/library/bulk/profile", "PUT /api/library/bulk/sources", "PUT /api/library/bulk/tags",
	"POST /api/library/bulk/search-now", "POST /api/library/bulk/replace-smaller", "POST /api/library/bulk/remove",
	"PUT /api/music/bulk/follow", "PUT /api/music/bulk/profile", "POST /api/music/bulk/remove",
	"GET /api/search/delay", "PUT /api/search/delay", "GET /api/quality/size-limits", "PUT /api/quality/size-limits", "POST /api/quality/size-limits/reset",
	"POST /api/indexers/{id}/resume", "GET /api/stats/indexers",
	"GET /api/irc-announce", "POST /api/irc-announce", "POST /api/irc-announce/test", "PUT /api/irc-announce/{id}", "DELETE /api/irc-announce/{id}", "PUT /api/irc-announce/{id}/enabled", "GET /api/irc-announce/{id}/log",
	"PUT /api/music/player-settings", "PUT /api/music/filters",
	"PUT /api/apps", "GET /api/automation/quiet", "PUT /api/automation/quiet", "POST /api/notifications/digest", "POST /api/books/import/retry", "POST /api/books/import/match",
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
