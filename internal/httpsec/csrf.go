package httpsec

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// CSRF refuses requests that change something when they were started by
// another web site. The session cookie is SameSite=Strict, which already
// keeps browsers from sending it cross-site; this is the second layer for
// browsers that ignore that and for same-site (sibling subdomain) pages.
//
// A request is let through when:
//   - it only reads (GET, HEAD, OPTIONS), or
//   - it carries an X-API-Key: a page in a browser cannot add that header to
//     a cross-site request without a preflight, and the key is not a cookie, or
//   - the browser says it is same-origin (Sec-Fetch-Site: same-origin or
//     none): a web page cannot forge that header, so it holds even when a
//     proxy rewrites the Host header, or
//   - its Origin (or, without one, Referer) names this server: the host the
//     caller used (Host, or X-Forwarded-Host from a trusted proxy) or one of
//     extraHosts, or
//   - it has neither Origin, Referer nor Sec-Fetch-Site, which is what
//     scripts and tools such as curl send (a browser always sends at least
//     one of them on a request that changes something).
//
// extraHosts are additional accepted host names (ALLOWED_ORIGINS), for a
// proxy that rewrites the Host header and a browser too old to send
// Sec-Fetch-Site.
func (p *Proxies) CSRF(extraHosts []string, next http.Handler) http.Handler {
	extra := make([]string, 0, len(extraHosts))
	for _, h := range extraHosts {
		if h = hostOf(h); h != "" {
			extra = append(extra, h)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.crossSite(r, extra) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Blocked: this request came from another web site. If you use a reverse proxy, make it pass on the Host header (or list the address in ALLOWED_ORIGINS).",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// crossSite reports whether r looks like a forged cross-site request.
func (p *Proxies) crossSite(r *http.Request, extra []string) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	if r.Header.Get("X-API-Key") != "" {
		return false
	}
	site := strings.ToLower(r.Header.Get("Sec-Fetch-Site"))
	if site == "same-origin" || site == "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		// No Origin, no Referer: not a browser form or fetch, unless the
		// browser also admits it came from another site.
		return site == "cross-site" || site == "same-site"
	}
	host := hostOf(origin)
	if host == "" {
		return true // "null" and anything unreadable
	}
	for _, h := range p.PublicHosts(r) {
		if hostsEqual(host, h) {
			return false
		}
	}
	for _, h := range extra {
		if hostsEqual(host, h) {
			return false
		}
	}
	return true
}

// hostOf is the lower-case host[:port] of an Origin or Referer value, or of a
// bare host name.
func hostOf(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.Contains(v, "://") {
		v = "http://" + v
	}
	u, err := url.Parse(v)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// hostsEqual compares two host[:port] values, treating the default ports
// (80, 443) as absent.
func hostsEqual(a, b string) bool {
	return trimDefaultPort(a) == trimDefaultPort(b)
}

func trimDefaultPort(h string) string {
	h = strings.ToLower(h)
	h = strings.TrimSuffix(h, ":443")
	h = strings.TrimSuffix(h, ":80")
	return h
}
