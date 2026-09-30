package api

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/selfupdate"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/updatecheck"
)

// updateRuntime is everything the Server keeps for updating itself: the
// checker for the newest release, what the last check found, and the one
// installation that may be running. See update_check.go (the notice),
// handlers_update_push.go (a file pushed through the API) and
// update_install.go (installing a signed release from GitHub).
type updateRuntime struct {
	once    sync.Once
	checker *updatecheck.Checker
	fetcher *updatecheck.Fetcher
	pubKey  ed25519.PublicKey // the key release signatures are checked against; nil disables "Update now"

	mu          sync.Mutex
	loaded      bool // latest and checkedAt were read from the settings
	latest      *updatecheck.Release
	checkedAt   time.Time
	lastErr     string // a plain sentence, empty when the last check worked
	checking    bool
	attemptedAt time.Time // when a check last started, whatever came of it
	autoTried   string    // the version an automatic install was last tried for

	opMu sync.Mutex // one upload, install or removal at a time
	job  installJob // the last "Update now" (guarded by mu)

	stuck    stuckRestart   // set at start when the app had restarted itself
	exitWith func(code int) // replaces os.Exit in tests

	// Test hooks (see update_export_test.go).
	pushMax      int64  // the biggest program file accepted; zero means selfupdate.MaxBinaryBytes
	supervisorIs string // pretends this is what starts Mediarium again
}

// stuckRestart is what the previous run left behind when the watchdog
// restarted the app.
type stuckRestart struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

// SetUpdatePublicKey sets the key that signed releases are checked against
// (base64 of a raw ed25519 public key). An empty or unusable key leaves
// "Update now" switched off.
func (s *Server) SetUpdatePublicKey(b64 string) error {
	if strings.TrimSpace(b64) == "" {
		return nil
	}
	key, err := updatecheck.ParsePublicKey(b64)
	if err != nil {
		return fmt.Errorf("update key: %w", err)
	}
	s.upd.mu.Lock()
	s.upd.pubKey = key
	s.upd.mu.Unlock()
	return nil
}

// initUpdates builds the checker and fetcher on first use, so a zero Server
// (as tests build) works.
func (s *Server) initUpdates() {
	s.upd.once.Do(func() {
		if s.upd.checker == nil {
			s.upd.checker = updatecheck.New(s.version)
		}
		if s.upd.fetcher == nil {
			s.upd.fetcher = updatecheck.NewFetcher(s.version)
		}
	})
}

func (s *Server) updateDir() string { return selfupdate.Dir(s.cfg.ConfigDir) }

// imageVersion is the version of the program inside the container image, ""
// when unknown (not started by the image's entrypoint).
func (s *Server) imageVersion() string { return s.cfg.ImageVersion }

// canInstallUpdates reports whether this install restarts into an installed
// update: it must have been started by the container image's entrypoint (which
// sets the image version and picks the newest program), on Linux.
func (s *Server) canInstallUpdates() bool {
	return s.cfg.ImageVersion != "" && runtime.GOOS == "linux"
}

// installKind is "docker" or "native", for the steps the update card shows.
func (s *Server) installKind() string {
	if s.cfg.ImageVersion != "" || pathExists("/.dockerenv") {
		return "docker"
	}
	return "native"
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *Server) settingOn(key string, defaultOn bool) bool {
	v, err := s.Settings.Get(key)
	if err != nil || v == "" {
		return defaultOn
	}
	return v == "1"
}

// updateCheckEnabled is the "check for new versions" switch (on by default).
func (s *Server) updateCheckEnabled() bool { return s.settingOn(settings.KeyUpdatesCheck, true) }

// autoInstallEnabled is the "install updates overnight" switch (off by default).
func (s *Server) autoInstallEnabled() bool { return s.settingOn(settings.KeyUpdatesAutoInstall, false) }

// allowPushEnabled is the "allow updates pushed through the API" switch (off
// by default).
func (s *Server) allowPushEnabled() bool { return s.settingOn(settings.KeyUpdatesAllowPush, false) }

// autoRestartEnabled is the "restart when stuck" switch (on by default).
func (s *Server) autoRestartEnabled() bool { return s.settingOn(settings.KeyAutoRestartStuck, true) }

func setBool(st *settings.Store, key string, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return st.Set(key, v, false)
}

// loadUpdateState reads the last check's result from the settings once.
func (s *Server) loadUpdateState() {
	u := &s.upd
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.loaded {
		return
	}
	u.loaded = true
	if raw, _ := s.Settings.Get(settings.KeyUpdatesLatest); raw != "" {
		var rel updatecheck.Release
		if err := json.Unmarshal([]byte(raw), &rel); err == nil && rel.Version != "" {
			u.latest = &rel
		}
	}
	if raw, _ := s.Settings.Get(settings.KeyUpdatesCheckedAt); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			u.checkedAt = t
		}
	}
}

// storeUpdateResult remembers a successful check, in memory and in the
// settings so it survives a restart.
func (s *Server) storeUpdateResult(rel *updatecheck.Release, at time.Time) {
	u := &s.upd
	u.mu.Lock()
	u.latest, u.checkedAt, u.lastErr = rel, at, ""
	u.mu.Unlock()
	if rel != nil {
		if b, err := json.Marshal(rel); err == nil {
			if err := s.Settings.Set(settings.KeyUpdatesLatest, string(b), false); err != nil {
				slog.Warn("update: could not save the latest version", "err", err)
			}
		}
	}
	if err := s.Settings.Set(settings.KeyUpdatesCheckedAt, at.UTC().Format(time.RFC3339), false); err != nil {
		slog.Warn("update: could not save the check time", "err", err)
	}
}

// pushLimit is the biggest program file a push may carry.
func (s *Server) pushLimit() int64 {
	if s.upd.pushMax > 0 {
		return s.upd.pushMax
	}
	return selfupdate.MaxBinaryBytes
}
