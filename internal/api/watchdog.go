package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/rdborg/mediarium/internal/selfupdate"
)

const (
	// watchdogEvery is how often the app looks at itself.
	watchdogEvery = 30 * time.Second
	// watchdogProbeTimeout is how long one look may take.
	watchdogProbeTimeout = 5 * time.Second
)

// watchdogState remembers since when the app has not been answering.
type watchdogState struct {
	failingSince time.Time
}

// observe records one look at the app and says whether it should now restart.
// ok is whether both the database and the web server answered. busy means a
// restore, an update or a move from another app is running; those can make
// the app slow on purpose, so the count starts over and nothing restarts.
func (w *watchdogState) observe(now time.Time, ok, busy bool) (restart bool) {
	if ok || busy {
		w.failingSince = time.Time{}
		return false
	}
	if w.failingSince.IsZero() {
		w.failingSince = now
		return false
	}
	return now.Sub(w.failingSince) >= watchdogStuckFor
}

// maintenanceBusy reports whether something is running that must not be
// interrupted, or that makes the app slow on purpose: a restore, an install of
// an update, a move from another app, or filling in the details of an import.
func (s *Server) maintenanceBusy() bool {
	if !s.restoreMu.TryLock() {
		return true
	}
	s.restoreMu.Unlock()
	if !s.upd.opMu.TryLock() {
		return true
	}
	s.upd.opMu.Unlock()
	if s.migrator != nil && s.migrator.Status().Running {
		return true
	}
	s.importWork.mu.Lock()
	running := s.importWork.running
	s.importWork.mu.Unlock()
	return running
}

// probeDatabase asks the database a trivial question.
func (s *Server) probeDatabase(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, watchdogProbeTimeout)
	defer cancel()
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one) == nil && one == 1
}

// probeWeb asks the app's own web server, on the loopback address, for its
// version, the way a browser or the container's health check would.
func (s *Server) probeWeb(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, watchdogProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/version", s.cfg.Port), nil)
	if err != nil {
		return false
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// StartWatchdog starts the self-check: every 30 seconds it asks the database
// and the web server whether they answer, and if either has failed for three
// minutes in a row it writes a note and exits with status 1, so the
// container's restart policy (or the service manager) starts a fresh
// process. The next start says so in the log. It also picks up the note a
// previous run left. The returned function stops it.
func (s *Server) StartWatchdog() (stop func()) {
	if reason, at, ok := selfupdate.TakeStuck(s.updateDir()); ok {
		s.upd.mu.Lock()
		s.upd.stuck = stuckRestart{At: at, Reason: reason}
		s.upd.mu.Unlock()
		slog.Warn("Mediarium restarted itself because it stopped answering", "reason", reason, "at", at.Format(time.RFC3339))
		noteRestartedItself(reason, at)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watchdogLoop(ctx, watchdogEvery)
	}()
	return func() {
		cancel()
		<-done
	}
}

func (s *Server) watchdogLoop(ctx context.Context, every time.Duration) {
	var st watchdogState
	tick := time.NewTicker(every)
	defer tick.Stop()
	warned := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		restart, dbOK, webOK := s.watchdogRound(ctx, &st, time.Now())
		if !restart {
			continue
		}
		if !s.control().CanRestart {
			// Exiting would leave Mediarium stopped with nothing to start it.
			if !warned {
				warned = true
				slog.Error("Mediarium has not answered for 3 minutes, but nothing here would start it again, so it is left running")
			}
			continue
		}
		reason := fmt.Sprintf("the database answered: %t, the web server answered: %t", dbOK, webOK)
		slog.Error("Mediarium has not answered for 3 minutes, so it is restarting itself", "database", dbOK, "web", webOK)
		if err := selfupdate.WriteStuck(s.updateDir(), reason); err != nil {
			slog.Warn("watchdog: could not leave a note for the next start", "err", err)
		}
		s.exitWith(1)
		return
	}
}

// watchdogRound is one look. The switch is read each time, with a short wait,
// so a stuck database cannot stop the look itself; an unreadable switch counts
// as on.
func (s *Server) watchdogRound(ctx context.Context, st *watchdogState, now time.Time) (restart, dbOK, webOK bool) {
	enabled := true
	within(3*time.Second, func() { enabled = s.autoRestartEnabled() })
	if !enabled {
		st.failingSince = time.Time{}
		return false, true, true
	}
	dbOK, webOK = s.probeDatabase(ctx), s.probeWeb(ctx)
	return st.observe(now, dbOK && webOK, s.maintenanceBusy()), dbOK, webOK
}

// exitWith ends the process with a status. Tests replace it.
func (s *Server) exitWith(code int) {
	if s.upd.exitWith != nil {
		s.upd.exitWith(code)
		return
	}
	os.Exit(code)
}

// watchdogStuckFor is how long the app must fail to answer, in a row, before
// it restarts itself. A variable so tests can shorten it.
var watchdogStuckFor = 3 * time.Minute

// selfRestartNote is what the previous run left when it restarted itself, empty
// when this run began normally.
func (s *Server) selfRestartNote() stuckRestart {
	s.upd.mu.Lock()
	defer s.upd.mu.Unlock()
	return s.upd.stuck
}
