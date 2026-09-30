package httpsec

import "net/http"

// contentSecurityPolicy fits the single-page app: everything comes from
// Mediarium itself except poster and cover images (TMDB, the Cover Art
// Archive and the Internet Archive it redirects to). Inline styles are
// allowed because React sets style attributes; scripts are never inline.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: https://image.tmdb.org https://coverartarchive.org https://*.archive.org; " +
	"font-src 'self'; " +
	"media-src 'self' blob:; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// hstsValue is six months. No includeSubDomains or preload: those affect
// sites that share the domain, which is the owner's call, not ours.
const hstsValue = "max-age=15552000"

// Headers sets the security response headers on every response.
// Strict-Transport-Security is added only when the caller is on HTTPS
// (directly or as reported by a trusted proxy): sent over plain HTTP it
// would be ignored anyway, and a LAN visit over http:// must keep working.
func (p *Proxies) Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), interest-cohort=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		// A media manager has no business in a search engine's index.
		h.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		if p.IsHTTPS(r) {
			h.Set("Strict-Transport-Security", hstsValue)
		}
		next.ServeHTTP(w, r)
	})
}
