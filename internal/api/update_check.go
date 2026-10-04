package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/updatecheck"
)

const (
	// checkEvery is how long the app waits between checks for a new version.
	checkEvery = 24 * time.Hour
	// retryAfterFailure is how soon it tries again when a check did not work.
	retryAfterFailure = time.Hour
	// firstCheckDelay is the wait after start before the first check, so the
	// app is fully up and a crash loop never turns into a request loop.
	firstCheckDelay = 2 * time.Minute
	// manualCheckGap is the least time between two checks that people ask for.
	manualCheckGap = 15 * time.Second
	// schedulerTick is how often the update loop looks at the clock.
	schedulerTick = 10 * time.Minute
	// checkTimeout bounds one check, however slow GitHub is.
	checkTimeout = 30 * time.Second

	couldNotCheck = "Couldn't check just now."
)

// couldNotCheckBecause is the line the Updates box shows after a failed check:
// that it failed, and why, in plain words.
func couldNotCheckBecause(err error) string {
	if errors.Is(err, updatecheck.ErrRateLimited) {
		return couldNotCheck + " GitHub is limiting requests for now. Mediarium tries again later."
	}
	return couldNotCheck + " " + plainerror.Message(err)
}

// latestPayload is the newest release as the page shows it.
type latestPayload struct {
	Version     string    `json:"version"`
	Name        string    `json:"name,omitempty"`
	Notes       string    `json:"notes,omitempty"`
	MoreNotes   bool      `json:"moreNotes,omitempty"`
	URL         string    `json:"url"`
	Prerelease  bool      `json:"prerelease,omitempty"`
	PublishedAt time.Time `json:"publishedAt,omitempty"`
}

type installInfo struct {
	Kind string `json:"kind"` // "docker" or "native"
	Full bool   `json:"full"` // the -full image (Cloudflare helper built in)
}

// noticePayload is the answer of the update notice endpoints.
type noticePayload struct {
	Enabled     bool           `json:"enabled"` // the automatic daily check is on
	Running     string         `json:"running"`
	Available   bool           `json:"available"` // Latest is newer than Running
	Latest      *latestPayload `json:"latest,omitempty"`
	CheckedAt   *time.Time     `json:"checkedAt,omitempty"`
	Error       string         `json:"error,omitempty"`
	Install     installInfo    `json:"install"`
	CanInstall  bool           `json:"canInstall"`            // "Update now" works for Latest
	InstallNote string         `json:"installNote,omitempty"` // why it does not, in a sentence
	Job         *jobPayload    `json:"job,omitempty"`
}

// notice builds the current answer from what is known; it never contacts
// GitHub.
func (s *Server) notice() noticePayload {
	s.loadUpdateState()
	u := &s.upd
	u.mu.Lock()
	latest, checkedAt, lastErr := u.latest, u.checkedAt, u.lastErr
	job := u.job.payload()
	pub := u.pubKey
	u.mu.Unlock()

	n := noticePayload{
		Enabled: s.updateCheckEnabled(),
		Running: s.version,
		Error:   lastErr,
		Install: installInfo{Kind: s.installKind(), Full: s.cfg.BundledFlareSolverr},
		Job:     job,
	}
	if !checkedAt.IsZero() {
		t := checkedAt
		n.CheckedAt = &t
	}
	if latest != nil {
		n.Latest = &latestPayload{
			Version: latest.Version, Name: latest.Name, Notes: latest.Notes, MoreNotes: latest.MoreNotes,
			URL: latest.URL, Prerelease: latest.Prerelease, PublishedAt: latest.PublishedAt,
		}
		n.Available = updatecheck.Newer(s.version, *latest)
	}
	if n.Available {
		ok, why := updatecheck.Installable(*latest, runtime.GOOS, runtime.GOARCH)
		switch {
		case !s.canInstallUpdates():
			n.InstallNote = "Update the way you installed Mediarium, using the steps below."
		case len(pub) == 0:
			n.InstallNote = "This build of Mediarium has no update key, so it can't install updates by itself."
		case !ok:
			n.InstallNote = why
		default:
			n.CanInstall = true
		}
	}
	return n
}

// runUpdateCheck asks GitHub for the newest release and remembers the answer.
// force is for a check somebody asked for: it still waits out manualCheckGap
// after the last one. A failure is quiet: it is kept as a short sentence for
// the page and a line in the log, never an error for the caller, and the
// earlier answer stays.
func (s *Server) runUpdateCheck(ctx context.Context, force bool) {
	s.initUpdates()
	s.loadUpdateState()
	u := &s.upd
	u.mu.Lock()
	if u.checking || (force && time.Since(u.attemptedAt) < manualCheckGap) {
		u.mu.Unlock()
		return
	}
	u.checking = true
	u.attemptedAt = time.Now()
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.checking = false
		u.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	rel, ok, err := u.checker.Latest(ctx, s.version)
	if err != nil {
		u.mu.Lock()
		u.lastErr = couldNotCheckBecause(err)
		u.mu.Unlock()
		if errors.Is(err, updatecheck.ErrRateLimited) {
			slog.Info("update: GitHub is limiting requests; will try again later")
		} else {
			slog.Info("update: could not check for a new version", "err", err)
			noteUpdateCheckFailed(err)
		}
		return
	}
	var latest *updatecheck.Release
	if ok {
		latest = &rel
	}
	s.storeUpdateResult(latest, time.Now())
	if latest != nil && updatecheck.Newer(s.version, *latest) {
		s.announceUpdate(*latest)
	}
}

// announceUpdate sends one message through the notification targets for each
// new version, however many times it is found.
func (s *Server) announceUpdate(rel updatecheck.Release) {
	if last, _ := s.Settings.Get(settings.KeyUpdatesNotified); last == rel.Version {
		return
	}
	if err := s.Settings.Set(settings.KeyUpdatesNotified, rel.Version, false); err != nil {
		slog.Warn("update: could not remember the notification", "err", err)
		return // better silent than the same message every day
	}
	s.notifyEvent("update",
		fmt.Sprintf("Mediarium %s is available", rel.Version),
		fmt.Sprintf("You are running %s. See what's new and how to update: %s", s.version, rel.URL))
}

// handleUpdateLatest returns what is known about the newest release.
func (s *Server) handleUpdateLatest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.notice())
}

// handleUpdateCheck asks GitHub right now (the Check now button) and returns
// the result. It works even when the daily check is switched off.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	s.runUpdateCheck(r.Context(), true)
	writeJSON(w, http.StatusOK, s.notice())
}

// StartUpdates starts the daily check for a new version, and the overnight
// install if it is switched on. It does nothing in safe mode. The returned
// function stops it.
func (s *Server) StartUpdates() (stop func()) {
	if s.cfg.PauseAutomation {
		return func() {}
	}
	s.initUpdates()
	s.loadUpdateState()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.updateLoop(ctx, firstCheckDelay, schedulerTick)
	}()
	return func() {
		cancel()
		<-done
	}
}

// updateLoop looks at the clock every tick: it checks for a new version when
// one is due, and then considers an automatic install. first is the wait
// before the first look.
func (s *Server) updateLoop(ctx context.Context, first, tick time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.updateTick(ctx, time.Now())
		timer.Reset(tick)
	}
}

// updateTick is one look: check if due, then install if allowed.
func (s *Server) updateTick(ctx context.Context, now time.Time) {
	if s.updateCheckEnabled() && s.checkDue(now) {
		s.runUpdateCheck(ctx, false)
	}
	s.maybeAutoInstall(ctx, now)
}

// checkDue reports whether the next check is due: a day after the last
// successful one, or an hour after a failed one.
func (s *Server) checkDue(now time.Time) bool {
	s.loadUpdateState()
	u := &s.upd
	u.mu.Lock()
	defer u.mu.Unlock()
	switch {
	case u.lastErr != "" && !u.attemptedAt.IsZero():
		return now.Sub(u.attemptedAt) >= retryAfterFailure
	case u.checkedAt.IsZero():
		return true
	}
	return now.Sub(u.checkedAt) >= checkEvery
}
