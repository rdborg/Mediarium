package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The interface handler only ever serves the embedded build: odd paths end in
// the app's own page or a redirect, never a folder listing or anything
// outside the embedded files, and HEAD and Range work on real files.
func TestInterfaceHandlerPathsAreConfined(t *testing.T) {
	h := frontendHandler()
	do := func(method, target string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/", nil)
		req.URL.Path = target
		req.URL.RawPath = ""
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	for _, p := range []string{
		"/../../etc/passwd", "/..", "/assets/../../go.mod", "/%2e%2e/secret", "/assets/", "/assets", "/fonts", "/fonts/",
		"/.git/config", "/.env", "//", "/assets//", "/index.html/", "/\\..\\x", "/a\x00b", "/assets/..%2f..%2fgo.mod",
		strings.Repeat("/x", 3000),
	} {
		w := do(http.MethodGet, p, nil)
		body := w.Body.String()
		switch w.Code {
		case http.StatusOK, http.StatusMovedPermanently, http.StatusNotFound:
		default:
			t.Errorf("GET %q: status %d", p, w.Code)
		}
		if strings.Contains(body, "<pre>") || strings.Contains(body, "root:") || strings.Contains(body, "module github.com") {
			t.Errorf("GET %q leaked something: %.80q", p, body)
		}
		if w.Code == http.StatusOK && !strings.Contains(strings.ToLower(body), "<!doctype html") && !strings.Contains(strings.ToLower(body), "<html") {
			t.Errorf("GET %q: 200 but not the app page: %.80q", p, body)
		}
	}

	// A real file: HEAD has no body but the same headers, and a Range works.
	get := do(http.MethodGet, "/favicon.svg", nil)
	// Without the built web files (a clean checkout) the app's own page answers
	// every unknown path with a 200, so look at the type, not only the status.
	if get.Code != http.StatusOK || strings.HasPrefix(get.Header().Get("Content-Type"), "text/html") {
		t.Skip("favicon.svg is not embedded here")
	}
	head := do(http.MethodHead, "/favicon.svg", nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != get.Header().Get("Content-Type") {
		t.Errorf("HEAD: %d, %d body bytes, type %q", head.Code, head.Body.Len(), head.Header().Get("Content-Type"))
	}
	part := do(http.MethodGet, "/favicon.svg", map[string]string{"Range": "bytes=0-3"})
	if part.Code != http.StatusPartialContent || part.Body.Len() != 4 {
		t.Errorf("Range: %d, %d bytes", part.Code, part.Body.Len())
	}
	if !strings.HasPrefix(get.Header().Get("Content-Type"), "image/svg+xml") {
		t.Errorf("svg served as %q", get.Header().Get("Content-Type"))
	}
	// A missing file under /assets must not be cached for a year as the page.
	miss := do(http.MethodGet, "/assets/does-not-exist-12345.js", nil)
	if strings.Contains(miss.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("a missing asset answered with Cache-Control %q", miss.Header().Get("Cache-Control"))
	}
}
