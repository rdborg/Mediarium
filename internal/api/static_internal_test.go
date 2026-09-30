package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/web"
)

// After an update the browser must load the new interface, not the old one it
// kept: the page that points at the scripts is always re-checked, and the
// scripts have names that change with their content, so they can be kept for
// ever.
func TestInterfaceIsNotServedStaleAfterAnUpdate(t *testing.T) {
	h := frontendHandler()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	for _, path := range []string{"/", "/settings/system", "/library"} {
		w := get(path)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("GET %s: %d, Cache-Control %q; want 200 and no-cache", path, w.Code, w.Header().Get("Cache-Control"))
		}
	}

	sub, err := fs.Sub(web.DistFS, web.DistDir)
	if err != nil {
		t.Fatal(err)
	}
	assets, _ := fs.ReadDir(sub, "assets")
	if len(assets) == 0 {
		t.Skip("the interface has not been built here (web/dist/assets is empty)")
	}
	for _, a := range assets {
		w := get("/assets/" + a.Name())
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("GET /assets/%s: %d, Cache-Control %q", a.Name(), w.Code, w.Header().Get("Cache-Control"))
		}
		if !hashedName(a.Name()) {
			t.Errorf("asset %s has no content hash in its name, so an old copy could be kept after an update", a.Name())
		}
	}
}

// hashedName reports whether a built file name carries a content hash
// (index-DGfyRCUI.js), which Vite adds to everything it builds.
func hashedName(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] == '-' {
			rest := name[i+1:]
			n := 0
			for n < len(rest) && rest[n] != '.' {
				n++
			}
			if n >= 6 {
				return true
			}
		}
	}
	return false
}
