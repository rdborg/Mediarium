package api

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/opds"
)

// E-reader access: the OPDS catalogue (opds_api.go) and Kobo sync
// (kobo_api.go). Neither uses the web sign-in. The catalogue takes HTTP Basic
// with the account's user name and its e-reader password; the Kobo address has
// the account's secret token in it. Both secrets belong to one account, can
// only read books, and are made and removed on the profile page
// (ereader_profile.go).

// ereaderRuntime is what the e-reader routes keep in memory.
type ereaderRuntime struct {
	once sync.Once
	// Wrong e-reader passwords and unknown Kobo tokens, counted per caller
	// address, and wrong passwords per account name from every address. They
	// are kept apart from the web sign-in counters so a stranger guessing at
	// the catalogue can never lock the owner out of the web page.
	failIP   *auth.LoginLimiter
	failUser *auth.LoginLimiter
	// kepub limits conversions to one at a time (the work is heavy and a
	// Kobo asks for a whole shelf at once).
	kepubMu sync.Mutex
	// lastPrune is when old covers were last cleared out (unix seconds).
	lastPrune atomic.Int64

	// feeds holds the catalogue pages already built, and the ebook list is
	// kept for the Kobo sync and for building them. Both are thrown away when
	// the book list's change marker moves (ereader_cache.go).
	feeds      *opds.Cache
	listMu     sync.Mutex
	listVer    int64
	listBooks  []books.Book
	listLoaded bool
	// listBuilds and feedBuilds count the times the database was read for the
	// list and a page was built, so tests can see a cache hit.
	listBuilds atomic.Int64
	feedBuilds atomic.Int64

	storeMu   sync.RWMutex
	storeBase string // the Kobo store, replaced in tests
	storeHTTP *http.Client
}

const (
	ereaderIPAttempts   = 20
	ereaderUserAttempts = 30
	ereaderWindow       = 15 * time.Minute
	// defaultKoboStore is where Kobo's own servers are.
	defaultKoboStore = "https://storeapi.kobo.com"
)

func (s *Server) ereaderState() *ereaderRuntime {
	rt := &s.ereader
	rt.once.Do(func() {
		rt.failIP = auth.NewLoginLimiter(ereaderIPAttempts, ereaderWindow)
		rt.failUser = auth.NewLoginLimiter(ereaderUserAttempts, ereaderWindow)
		rt.feeds = opds.NewCache(opdsCacheEntries, opdsCacheBytes)
		rt.storeBase = defaultKoboStore
		rt.storeHTTP = &http.Client{
			Timeout: 25 * time.Second,
			// A redirect from the store is passed on to the Kobo, not followed here.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	})
	return rt
}

// ereaderLimitKey is the key the failure counters use for the caller.
func (s *Server) ereaderLimitKey(r *http.Request) string {
	return httpsec.LimitKey(s.clientIP(r))
}

// rateLimited answers 429 and reports true when the caller has failed too
// often lately.
func (s *Server) ereaderRateLimited(w http.ResponseWriter, r *http.Request) bool {
	if s.ereaderState().failIP.Allow(s.ereaderLimitKey(r)) {
		return false
	}
	w.Header().Set("Retry-After", "900")
	http.Error(w, "Too many wrong tries. Wait a few minutes and try again.", http.StatusTooManyRequests)
	return true
}

// ereaderUserAllowed reports whether the account may read books through an
// e-reader: ebooks are on and the account may read in Mediarium Books.
func (s *Server) ereaderUserAllowed(userID int64) (bool, string) {
	if !s.ebooksEnabled() {
		return false, "Ebooks are switched off in Mediarium."
	}
	// An account an administrator turned off loses its e-reader access too.
	if u, err := s.Auth.GetUser(userID); err != nil || u.Disabled {
		return false, "This account is turned off."
	}
	perms, err := s.Auth.PermissionsOf(userID)
	if err != nil {
		return false, "This account can't be found."
	}
	if !perms.Play {
		return false, "This account isn't allowed to read books."
	}
	return true, ""
}

// opdsUser signs a catalogue request in with HTTP Basic. On failure it has
// already answered, and ok is false.
func (s *Server) opdsUser(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	if s.ereaderRateLimited(w, r) {
		return nil, false
	}
	name, password, given := r.BasicAuth()
	if !given {
		challenge(w)
		return nil, false
	}
	rt := s.ereaderState()
	ipKey := s.ereaderLimitKey(r)
	userKey := strings.ToLower(strings.TrimSpace(name))
	if len(userKey) > 128 {
		userKey = userKey[:128]
	}
	if !rt.failUser.Allow(userKey) {
		w.Header().Set("Retry-After", "900")
		http.Error(w, "Too many wrong tries for this account. Wait a few minutes and try again.", http.StatusTooManyRequests)
		return nil, false
	}
	user, err := s.Auth.UserByName(name)
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			slog.Error("opds: look up account", "err", err)
			http.Error(w, "Couldn't check the account. Try again.", http.StatusInternalServerError)
			return nil, false
		}
		// Do the same work as for a real account so the time says nothing.
		s.EReader.CheckOPDS(0, password)
		s.opdsFailed(r, rt, ipKey, userKey)
		challenge(w)
		return nil, false
	}
	if !s.EReader.CheckOPDS(user.ID, password) {
		s.opdsFailed(r, rt, ipKey, userKey)
		challenge(w)
		return nil, false
	}
	if ok, why := s.ereaderUserAllowed(user.ID); !ok {
		http.Error(w, why, http.StatusForbidden)
		return nil, false
	}
	rt.failIP.RecordSuccess(ipKey)
	return user, true
}

func (s *Server) opdsFailed(r *http.Request, rt *ereaderRuntime, ipKey, userKey string) {
	rt.failIP.RecordFailure(ipKey)
	rt.failUser.RecordFailure(userKey)
	// The name is logged, never the password.
	slog.Warn("opds: wrong e-reader password", "user", userKey, "ip", s.clientIP(r))
}

func challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Mediarium books", charset="UTF-8"`)
	http.Error(w, "Sign in with your username and your e-reader password.", http.StatusUnauthorized)
}

var hostPattern = regexp.MustCompile(`^[a-z0-9.\-]+(:[0-9]{1,5})?$|^\[[0-9a-f:.]+\](:[0-9]{1,5})?$`)

// publicBase is the address the caller used to reach Mediarium, such as
// "https://books.example:8264": the scheme and host (with the port when it is
// not the usual one), taking a trusted proxy's word for them.
func (s *Server) publicBase(r *http.Request) string {
	scheme := "http"
	if s.isHTTPS(r) {
		scheme = "https"
	}
	hosts := s.sec().proxies.PublicHosts(r)
	host := hosts[0]
	if len(hosts) > 1 {
		host = hosts[1] // the host the trusted proxy says the caller used
	}
	if !hostPattern.MatchString(host) {
		host = strings.ToLower(r.Host)
	}
	return scheme + "://" + host
}
