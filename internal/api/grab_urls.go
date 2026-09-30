package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// A grab makes the server fetch the release's downloadUrl. Member accounts
// may grab, so without a check a member could make the server request any
// address it can reach (cloud metadata endpoints, the router, other
// containers). A member's downloadUrl must therefore be one this server
// handed out in search results recently. (Members only ever see an opaque
// reference to it, so no address on an indexer's host is accepted either:
// that would let a member make the server call any path of that host, such as
// a local Prowlarr or Jackett.) Administrators are not limited: they can
// already point an indexer at any address, so the check would protect nothing
// there.
const (
	offeredURLTTL = 24 * time.Hour // a search page left open over a day needs a new search
	offeredURLMax = 20000          // bounds memory: about one URL per result, a few MB at most
)

// offeredURLs remembers the download URLs of search results shown to anyone,
// for a while. The zero value is ready to use.
//
// The browser never sees those URLs: a Usenet indexer's download link
// carries the indexer's API key, which must not reach every account that
// can search. Each URL is handed out as an opaque reference ("rel_" plus
// random hex) that only this server can turn back into the URL.
type offeredURLs struct {
	mu    sync.Mutex
	seen  map[string]time.Time // URL -> when it stops counting
	refs  map[string]string    // reference -> URL
	byURL map[string]string    // URL -> its reference, so one URL keeps one reference
	now   func() time.Time
}

const refPrefix = "rel_"

// ref records a URL handed out in search results and returns the opaque
// reference to send instead of it.
func (o *offeredURLs) ref(u string) string {
	if u == "" {
		return ""
	}
	o.add(u)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.refs == nil {
		o.refs, o.byURL = map[string]string{}, map[string]string{}
	}
	if r, ok := o.byURL[u]; ok {
		return r
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return u // no randomness: fall back to the URL rather than breaking grabs
	}
	r := refPrefix + hex.EncodeToString(b)
	o.refs[r] = u
	o.byURL[u] = r
	return r
}

// resolve turns a reference back into its URL. Anything else (an older
// client sending a real URL) is returned unchanged.
func (o *offeredURLs) resolve(v string) string {
	if !strings.HasPrefix(v, refPrefix) {
		return v
	}
	o.mu.Lock()
	u, ok := o.refs[v]
	o.mu.Unlock()
	if !ok || !o.has(u) {
		return v // unknown or expired: the grab check refuses it
	}
	return u
}

func (o *offeredURLs) clock() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}

// add records a URL handed out in search results.
func (o *offeredURLs) add(u string) {
	if u == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.clock()
	if o.seen == nil {
		o.seen = map[string]time.Time{}
	}
	if _, ok := o.seen[u]; !ok && len(o.seen) >= offeredURLMax {
		// Drop what has expired; if still full, drop the oldest entry.
		oldestURL, oldest := "", time.Time{}
		for k, exp := range o.seen {
			if !now.Before(exp) {
				delete(o.seen, k)
				continue
			}
			if oldestURL == "" || exp.Before(oldest) {
				oldestURL, oldest = k, exp
			}
		}
		if len(o.seen) >= offeredURLMax {
			delete(o.seen, oldestURL)
		}
	}
	o.seen[u] = now.Add(offeredURLTTL)
	o.forgetDropped()
}

// forgetDropped removes references whose URL is no longer remembered, so
// the reference maps stay as small as the URL map. Called with o.mu held.
func (o *offeredURLs) forgetDropped() {
	if len(o.byURL) <= len(o.seen) {
		return
	}
	for u, r := range o.byURL {
		if _, ok := o.seen[u]; !ok {
			delete(o.byURL, u)
			delete(o.refs, r)
		}
	}
}

// has reports whether u was handed out recently.
func (o *offeredURLs) has(u string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	exp, ok := o.seen[u]
	if !ok {
		return false
	}
	if !o.clock().Before(exp) {
		delete(o.seen, u)
		return false
	}
	return true
}

// grabURLAllowed decides whether the caller may have the server fetch
// downloadURL (see offeredURLs).
func (s *Server) grabURLAllowed(r *http.Request, downloadURL string) bool {
	if isAdminRequest(r) {
		return true
	}
	return s.offered.has(downloadURL)
}

// checkGrabURL resolves a search result reference in *downloadURL to the
// real URL, then answers 403 and returns false when the caller may not grab
// it.
func (s *Server) checkGrabURL(w http.ResponseWriter, r *http.Request, downloadURL *string) bool {
	*downloadURL = s.offered.resolve(*downloadURL)
	if strings.HasPrefix(*downloadURL, refPrefix) {
		writeError(w, http.StatusForbidden, "This search result has expired. Search again and pick from the new results.")
		return false
	}
	if s.grabURLAllowed(r, *downloadURL) {
		return true
	}
	writeError(w, http.StatusForbidden, "This download link didn't come from a search here. Search again and pick from the results.")
	return false
}
