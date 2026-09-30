package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// readDeadline is how long a page's data may take to load before the app
// gives up and says it is busy. The pages behind it read only the database,
// so a wait this long means the database is stuck behind something, not that
// the answer is big. The browser shows "taking longer than usual" and offers
// Retry instead of a loading screen that never ends.
var readDeadline = 10 * time.Second

// busyMessage is what a page gets when its data could not be read in time.
const busyMessage = "Mediarium is busy right now. Try again in a moment."

// deadlinePrefixes are the GET routes that only read the database, and so
// get the deadline. Routes that talk to other services (search, discover,
// subtitles, the media server lookups), stream files or take a long time by
// design (backup, folder checks, clean-up) are left out: they have their own
// time limits and would be cut off for no reason.
var deadlinePrefixes = []string{
	"/api/auth/",
	"/api/onboarding/status",
	"/api/health",
	"/api/dashboard",
	"/api/calendar",
	"/api/wanted",
	"/api/activity",
	"/api/queue",
	"/api/modules",
	"/api/movies",
	"/api/series",
	"/api/settings",
	"/api/users",
	"/api/indexers",
	"/api/usenet-servers",
	"/api/blocklist",
	"/api/notifications",
	"/api/quality-profiles",
	"/api/library/",
	"/api/music/artists",
	"/api/music/wanted",
	"/api/music/profiles",
	"/api/subtitles/wanted",
	"/api/subtitles/quota",
	"/api/usage",
	"/api/vpn/configs",
	"/api/vpn/status",
	"/api/system/info",
	"/api/system/diagnostics",
}

// deadlineExempt are routes under those prefixes that reach outside the
// database anyway (other services, or the disk: a disk-usage count can sit
// behind a hard drive that has to spin up first).
var deadlineExempt = []string{"/search", "/similar", "/subtitles", "/files", "/cover", "-check", "/disk-usage"}

func guarded(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	for _, x := range deadlineExempt {
		if strings.Contains(p, x) {
			return false
		}
	}
	for _, prefix := range deadlinePrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// slowRequest is how long a request may take before it is logged.
const slowRequest = 3 * time.Second

// busyGuard sits in front of every API route. It logs each request that takes
// longer than three seconds (method, path and the state of the database pool,
// never the query string or any body), and gives the database-reading pages a
// deadline: a page that has not been answered in readDeadline gets a clear 503
// instead of hanging.
func (s *Server) busyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			if took := time.Since(start); took >= slowRequest {
				st := s.db.Stats()
				slog.Warn("slow request",
					"method", r.Method, "path", r.URL.Path, "took", took.Round(time.Millisecond).String(),
					"db_in_use", st.InUse, "db_max", st.MaxOpenConnections, "db_waits", st.WaitCount,
					"db_wait_total", st.WaitDuration.Round(time.Millisecond).String())
			}
		}()
		if !guarded(r) {
			next.ServeHTTP(w, r)
			return
		}
		s.serveWithDeadline(w, r, next, readDeadline)
	})
}

// serveWithDeadline runs next, holding back its reply. If it answers within d
// the reply is passed on; if not, the caller gets the busy answer and the
// handler's late reply is thrown away (it carries on in the background until
// it finishes, with its context cancelled). Like http.TimeoutHandler, but
// with a JSON body the pages understand.
func (s *Server) serveWithDeadline(w http.ResponseWriter, r *http.Request, next http.Handler, d time.Duration) {
	ctx, cancel := context.WithTimeout(r.Context(), d)
	defer cancel()
	r = r.WithContext(ctx)

	tw := &deadlineWriter{header: make(http.Header)}
	done := make(chan struct{})
	panicked := make(chan any, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				panicked <- p
			}
		}()
		next.ServeHTTP(tw, r)
		close(done)
	}()

	select {
	case p := <-panicked:
		panic(p)
	case <-done:
		tw.mu.Lock()
		defer tw.mu.Unlock()
		dst := w.Header()
		for k, v := range tw.header {
			dst[k] = v
		}
		code := tw.code
		if code == 0 {
			code = http.StatusOK
		}
		w.WriteHeader(code)
		_, _ = w.Write(tw.body.Bytes())
	case <-ctx.Done():
		tw.mu.Lock()
		tw.timedOut = true
		tw.mu.Unlock()
		if ctx.Err() == context.DeadlineExceeded {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"` + busyMessage + `","busy":true}` + "\n"))
			st := s.db.Stats()
			slog.Warn("request gave up waiting", "method", r.Method, "path", r.URL.Path, "after", d.String(),
				"db_in_use", st.InUse, "db_max", st.MaxOpenConnections, "db_waits", st.WaitCount)
		}
	}
}

type deadlineWriter struct {
	mu       sync.Mutex
	header   http.Header
	body     bytes.Buffer
	code     int
	timedOut bool
}

func (t *deadlineWriter) Header() http.Header { return t.header }

func (t *deadlineWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if t.code == 0 {
		t.code = http.StatusOK
	}
	return t.body.Write(p)
}

func (t *deadlineWriter) WriteHeader(code int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timedOut || t.code != 0 {
		return
	}
	t.code = code
}
