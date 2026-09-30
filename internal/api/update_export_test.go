package api

import (
	"context"
	"time"

	"github.com/rdborg/mediarium/internal/updatecheck"
)

// Hooks for the update, restart and watchdog tests (package api_test).

// TestSetUpdateEnv sets the running version and the version the container
// image is said to hold. An empty image version means "not started by the
// image's entrypoint".
func (s *Server) TestSetUpdateEnv(version, imageVersion string) {
	s.version = version
	s.cfg.ImageVersion = imageVersion
}

// TestSetGitHub points the new-version check at a fake release server.
func (s *Server) TestSetGitHub(baseURL string) {
	s.initUpdates()
	s.upd.checker = &updatecheck.Checker{Repo: "rdborg/Mediarium", Version: s.version, BaseURL: baseURL}
}

// TestSetFetcher replaces the release downloader.
func (s *Server) TestSetFetcher(f *updatecheck.Fetcher) {
	s.initUpdates()
	s.upd.fetcher = f
}

func (s *Server) TestSetPushLimit(n int64)   { s.upd.pushMax = n }
func (s *Server) TestSetSupervisor(k string) { s.upd.supervisorIs = k }
func (s *Server) TestSetExitWith(fn func(code int)) {
	s.upd.exitWith = fn
}

// TestHoldUpdateLock pretends an update is in progress until release is called.
func (s *Server) TestHoldUpdateLock() (release func()) {
	s.upd.opMu.Lock()
	return s.upd.opMu.Unlock
}

// TestUpdateTick runs one look of the update loop as if it were now.
func (s *Server) TestUpdateTick(ctx context.Context, now time.Time) { s.updateTick(ctx, now) }

// TestWatchdog runs the watchdog with a short look interval and a short
// "stuck" time, and returns a function that stops it.
func (s *Server) TestWatchdog(every, stuckFor time.Duration) (stop func()) {
	old := watchdogStuckFor
	watchdogStuckFor = stuckFor
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watchdogLoop(ctx, every)
	}()
	return func() {
		cancel()
		<-done
		watchdogStuckFor = old
	}
}

// TestNoteSelfRestart reads the note a watchdog restart left, as the start of
// the app does.
func (s *Server) TestNoteSelfRestart() {
	stop := s.StartWatchdog()
	stop()
}

func (s *Server) TestUpdateDir() string { return s.updateDir() }
