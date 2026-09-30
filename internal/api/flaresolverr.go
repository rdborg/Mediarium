package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// bundledFlareSolverrURL is where the "-full" image runs its built-in
// FlareSolverr. It listens on the loopback address only.
const bundledFlareSolverrURL = "http://127.0.0.1:8191"

// flareSolverrState is what Settings > Indexers & Search shows about the helper.
type flareSolverrState struct {
	// Bundled is true in the "-full" image (the helper is built in).
	Bundled bool `json:"bundled"`
	// Configured is true when some helper address is in use, built in or saved.
	Configured bool `json:"configured"`
	// Running is true when the helper answered a moment ago.
	Running bool `json:"running"`
	// Version is the helper's version when it reported one.
	Version string `json:"version,omitempty"`
}

// flareSolverrProbe remembers the last answer so the dashboard and the
// Settings page can ask often without hammering the helper.
type flareSolverrProbe struct {
	mu   sync.Mutex
	url  string
	at   time.Time
	last flareSolverrState
}

const flareSolverrProbeTTL = 15 * time.Second

// flareSolverrCheck asks the helper in use whether it is ready. The answer is
// cached for a few seconds.
func (s *Server) flareSolverrCheck() flareSolverrState {
	url := s.flareSolverrURL()
	st := flareSolverrState{Bundled: s.cfg.BundledFlareSolverr, Configured: url != ""}
	if url == "" {
		return st
	}
	p := &s.flareProbe
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.url == url && time.Since(p.at) < flareSolverrProbeTTL {
		cached := p.last
		cached.Bundled = st.Bundled
		return cached
	}
	st.Running, st.Version = probeFlareSolverr(url)
	p.url, p.at, p.last = url, time.Now(), st
	return st
}

// probeFlareSolverr calls the helper's home page, which answers
// {"msg":"FlareSolverr is ready!","version":"..."} once it is up.
func probeFlareSolverr(base string) (running bool, version string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/", nil)
	if err != nil {
		return false, ""
	}
	resp, err := netguard.Client(3 * time.Second).Do(req)
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, ""
	}
	var body struct {
		Msg     string `json:"msg"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return false, ""
	}
	return strings.Contains(strings.ToLower(body.Msg), "ready"), body.Version
}

// handleFlareSolverrStatus reports whether the Cloudflare helper is built in
// and answering (admin only).
func (s *Server) handleFlareSolverrStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.flareSolverrCheck())
}
