package api

import "net/http"

// robotsTxt asks search engines to stay away. Everything behind the sign-in is
// private anyway; this keeps the sign-in page itself, and the address of a
// server reachable from the internet, out of search results.
func robotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}
