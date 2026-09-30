package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/web"
)

// frontendHandler serves the embedded React build, falling back to
// index.html for any path that isn't a real static asset — required for
// client-side routing (search/library/settings/onboarding are all
// client-side routes).
func frontendHandler() http.Handler {
	sub, err := fs.Sub(web.DistFS, web.DistDir)
	if err != nil {
		// Should be unreachable: web.DistFS always embeds at least the
		// placeholder dist/index.html checked into the repo.
		panic("web: embedded dist not found: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		// Built assets have content-hashed names, so they can be kept
		// forever; the page that points at them must be re-checked every
		// time so an update is picked up straight away instead of the
		// browser running last version's JavaScript.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func trimLeadingSlash(p string) string {
	if p == "" || p == "/" {
		return "."
	}
	if p[0] == '/' {
		return p[1:]
	}
	return p
}
