package api

import (
	"bytes"
	"io/fs"
	"net/http"
	"regexp"
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
			if r.URL.Path == "/bookshelf" || strings.HasPrefix(r.URL.Path, "/bookshelf/") {
				// Mediarium Books installs as an app of its own: the same
				// page, with its own name and web app manifest.
				if page, err := fs.ReadFile(sub, "index.html"); err == nil {
					w.Header().Set("Cache-Control", "no-cache")
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					_, _ = w.Write(bookshelfPage(page))
					return
				}
			}
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
		if strings.HasSuffix(r.URL.Path, ".webmanifest") {
			// What browsers expect for "Add to home screen".
			w.Header().Set("Content-Type", "application/manifest+json")
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

// bookshelfPage is index.html for Mediarium Books: its own web app manifest
// and title, so a browser offers to install it as a separate app.
func bookshelfPage(page []byte) []byte {
	out := bytes.Replace(page, []byte(`href="/manifest.webmanifest"`), []byte(`href="/bookshelf.webmanifest"`), 1)
	return titleTag.ReplaceAll(out, []byte("<title>Mediarium Books</title>"))
}

var titleTag = regexp.MustCompile(`<title>[^<]*</title>`)
