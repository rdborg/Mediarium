package api

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
)

// securityState is what the server derives from TRUSTED_PROXIES and
// ALLOWED_ORIGINS, built on first use so the zero Server works.
type securityState struct {
	once    sync.Once
	proxies *httpsec.Proxies
	origins []string
	// byUser counts failed sign-ins per account name from every address
	// together, so a password guessed from many addresses at once (which the
	// per-address limit cannot see) is slowed down too.
	byUser *auth.LoginLimiter
	// setup is the one-time code for creating the first administrator from
	// outside the home network (setup.go).
	setup setupState
}

// Failed sign-ins tolerated for one account name from all addresses together
// per window. Far above the per-address limit, so a stranger typing wrong
// guesses cannot easily keep the real owner out, yet a distributed guessing
// run stops.
const (
	userLoginAttempts = 25
	userLoginWindow   = 15 * time.Minute
)

func (s *Server) sec() *securityState {
	st := &s.security
	st.once.Do(func() {
		p, err := httpsec.ParseProxies(s.cfg.TrustedProxies)
		if err != nil {
			// main refuses to start on a bad value; this is for tests and
			// embedders. Trusting nobody is the safe reading.
			slog.Error("security: ignoring invalid TRUSTED_PROXIES", "err", err)
			p, _ = httpsec.ParseProxies("none")
		}
		st.proxies = p
		for _, o := range strings.Split(s.cfg.AllowedOrigins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				st.origins = append(st.origins, o)
			}
		}
		st.byUser = auth.NewLoginLimiter(userLoginAttempts, userLoginWindow)
	})
	return st
}

// clientIP is the real address of the caller (see httpsec.Proxies.ClientIP).
func (s *Server) clientIP(r *http.Request) string {
	return s.sec().proxies.ClientIP(r)
}

// isHTTPS reports whether the caller reached Mediarium over HTTPS, directly
// or through a trusted proxy.
func (s *Server) isHTTPS(r *http.Request) bool {
	return s.sec().proxies.IsHTTPS(r)
}

// harden wraps the whole app in the security response headers and the
// cross-site request check.
func (s *Server) harden(next http.Handler) http.Handler {
	st := s.sec()
	return st.proxies.Headers(st.proxies.CSRF(st.origins, next))
}
