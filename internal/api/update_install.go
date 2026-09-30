package api

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/selfupdate"
	"github.com/rdborg/mediarium/internal/updatecheck"
)

// updateError is a refusal with the sentence to show and the HTTP status.
type updateError struct {
	status int
	msg    string
	err    error // what went wrong underneath, for the log only
}

func (e *updateError) Error() string {
	if e.err != nil {
		return e.msg + ": " + e.err.Error()
	}
	return e.msg
}

func refuse(status int, msg string) *updateError { return &updateError{status: status, msg: msg} }

func failed(msg string, err error) *updateError {
	return &updateError{status: http.StatusInternalServerError, msg: msg, err: err}
}

func writeUpdateError(w http.ResponseWriter, e *updateError) {
	if e.err != nil {
		slog.Error("update failed", "message", e.msg, "err", e.err)
	}
	writeError(w, e.status, e.msg)
}

const (
	// updateRestartDelay gives the reply time to reach the browser before the
	// app exits to start the new program.
	updateRestartDelay = 2 * time.Second

	notInstallableMessage = "This Mediarium wasn't started by its Docker image, so it can't restart into an update by itself. Update it the way you installed it."
)

// commitProgram checks a program file that has been written to tmp (and whose
// SHA-256 is sum) and puts it in place as the installed update. It does not
// restart. wantVersion, when set, is the version the file must report. On any
// refusal the file is left for the caller to delete.
func (s *Server) commitProgram(ctx context.Context, tmp, sum, wantVersion string, force bool) (version string, _ *updateError) {
	probe, err := selfupdate.Inspect(ctx, tmp)
	switch {
	case errors.Is(err, selfupdate.ErrWrongPlatform):
		return "", &updateError{status: http.StatusUnprocessableEntity, msg: fmt.Sprintf("That file is for a different kind of system. This Mediarium runs on %s/%s.", runtime.GOOS, runtime.GOARCH), err: err}
	case errors.Is(err, selfupdate.ErrNotMediarium):
		return "", &updateError{status: http.StatusUnprocessableEntity, msg: "That file is not a Mediarium program for this system.", err: err}
	case err != nil:
		return "", failed("Couldn't run the new program to check it. Make sure the config folder allows programs to run.", err)
	}
	if wantVersion != "" && probe.Version != wantVersion {
		return "", &updateError{status: http.StatusUnprocessableEntity, msg: fmt.Sprintf("The program says it is version %s, not %s, so it was not installed.", probe.Version, wantVersion)}
	}
	if err := selfupdate.Decide(s.version, s.imageVersion(), probe.Version, force); err != nil {
		switch {
		case errors.Is(err, selfupdate.ErrNotNewer):
			return "", &updateError{status: http.StatusConflict, msg: fmt.Sprintf("Version %s is not newer than the version running now (%s). To install it anyway, for example to go back, add ?force=true.", probe.Version, s.version)}
		case errors.Is(err, selfupdate.ErrOlderThanImage):
			return "", &updateError{status: http.StatusConflict, msg: fmt.Sprintf("Version %s is older than the version inside the Docker image (%s), so the container would not use it. Remove the installed update instead to go back to the image's version.", probe.Version, s.imageVersion())}
		default:
			return "", &updateError{status: http.StatusConflict, msg: "Mediarium can't tell whether that version is newer. Add ?force=true if you are sure.", err: err}
		}
	}
	if err := selfupdate.Install(s.updateDir(), tmp, probe.Version, sum); err != nil {
		return "", failed("Couldn't put the new program in place. Your current version has not been changed.", err)
	}
	return probe.Version, nil
}

// auditUpdate records an installed update in the log and on the Activity page:
// who did it, what version, and (shortened on the page) the checksum. The
// login, its key and any other secret are never part of it.
func (s *Server) auditUpdate(actor, how, version, sum string) {
	slog.Warn("update installed", "by", actor, "how", how, "version", version, "from", s.version, "sha256", sum)
	short := sum
	if len(short) > 12 {
		short = short[:12]
	}
	msg := fmt.Sprintf("Mediarium %s was installed by %s (%s, checksum %s). It restarts to use it.", version, actor, how, short)
	if err := s.QueueRepo.LogActivity(0, "update", msg); err != nil {
		slog.Warn("update: could not write the Activity entry", "err", err)
	}
}

// actorName is who is making the request, for the record.
func actorName(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return u.Username
	}
	return "an unknown account"
}

// restartSoon exits the app after a short pause, so the reply that was just
// sent gets through. The container's restart policy (or the service manager)
// starts it again; the entrypoint then picks the newest program.
func (s *Server) restartSoon(after time.Duration) {
	if s.restartDelay != 0 {
		after = s.restartDelay
	}
	go func() {
		time.Sleep(after)
		s.exit()
	}()
}

// flush pushes a written reply to the client before the app goes away.
func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// ---- "Update now": installing a signed release from GitHub ----

// installJob is the state of the download-and-install started by the button.
type installJob struct {
	State   string // "", "downloading", "checking", "installing", "restarting", "failed"
	Version string
	Error   string
	Auto    bool
}

type jobPayload struct {
	State   string `json:"state"`
	Version string `json:"version"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
	Active  bool   `json:"active"`
	Auto    bool   `json:"auto,omitempty"`
}

func (j installJob) payload() *jobPayload {
	if j.State == "" {
		return nil
	}
	p := &jobPayload{State: j.State, Version: j.Version, Error: j.Error, Auto: j.Auto, Active: j.State != "failed"}
	switch j.State {
	case "downloading":
		p.Message = "Downloading Mediarium " + j.Version + "…"
	case "checking":
		p.Message = "Checking the download…"
	case "installing":
		p.Message = "Installing…"
	case "restarting":
		p.Message = "Restarting to use Mediarium " + j.Version + "…"
	case "failed":
		p.Message = "The update didn't work."
	}
	return p
}

func (s *Server) setJob(state, errText string) {
	u := &s.upd
	u.mu.Lock()
	u.job.State, u.job.Error = state, errText
	u.mu.Unlock()
}

// startInstall begins downloading and installing the newest release, in the
// background. It refuses when nothing newer is known, when the release is not
// signed, when this install cannot restart into an update, or when another
// update is already going.
func (s *Server) startInstall(actor string, auto bool) *updateError {
	s.initUpdates()
	if !s.canInstallUpdates() {
		return refuse(http.StatusConflict, notInstallableMessage)
	}
	n := s.notice()
	if !n.Available || n.Latest == nil {
		return refuse(http.StatusConflict, "There is no newer version to install. Press Check now first.")
	}
	if !n.CanInstall {
		return refuse(http.StatusConflict, n.InstallNote)
	}
	u := &s.upd
	u.mu.Lock()
	latestNow, pub := u.latest, u.pubKey
	u.mu.Unlock()
	if latestNow == nil {
		return refuse(http.StatusConflict, "There is no newer version to install. Press Check now first.")
	}
	rel := *latestNow

	if !u.opMu.TryLock() {
		return refuse(http.StatusConflict, "Another update is already in progress.")
	}
	u.mu.Lock()
	u.job = installJob{State: "downloading", Version: rel.Version, Auto: auto}
	u.mu.Unlock()

	go func() {
		defer u.opMu.Unlock()
		if err := s.runInstall(rel, pub, actor); err != nil {
			slog.Error("update: installing the release failed", "version", rel.Version, "err", err)
			noteUpdateInstallFailed(rel.Version, err.msg, err.err)
			s.setJob("failed", err.msg)
			return
		}
		s.setJob("restarting", "")
		s.restartSoon(updateRestartDelay)
	}()
	return nil
}

func (s *Server) runInstall(rel updatecheck.Release, pub ed25519.PublicKey, actor string) *updateError {
	dir := s.updateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return failed("Couldn't create the update folder inside your config folder.", err)
	}
	selfupdate.CleanTemp(dir)
	ctx, cancel := context.WithTimeout(context.Background(), updatecheck.DownloadTimeout)
	defer cancel()

	dl, err := s.upd.fetcher.Fetch(ctx, rel, runtime.GOOS, runtime.GOARCH, pub, dir)
	if err != nil {
		switch {
		case errors.Is(err, updatecheck.ErrBadSignature):
			return &updateError{status: http.StatusBadGateway, msg: "The download's signature isn't valid, so it wasn't installed.", err: err}
		case errors.Is(err, updatecheck.ErrBadChecksum):
			return &updateError{status: http.StatusBadGateway, msg: "The download doesn't match its signed checksum, so it wasn't installed.", err: err}
		case errors.Is(err, updatecheck.ErrNoSignature):
			return &updateError{status: http.StatusConflict, msg: "This release isn't signed, so it can't be installed automatically.", err: err}
		case errors.Is(err, updatecheck.ErrNoProgram):
			return &updateError{status: http.StatusBadGateway, msg: "The download doesn't contain Mediarium, so it wasn't installed.", err: err}
		case errors.Is(err, updatecheck.ErrHostNotAllowed):
			return &updateError{status: http.StatusBadGateway, msg: "The download was redirected somewhere unsafe, so it was stopped.", err: err}
		}
		return &updateError{status: http.StatusBadGateway, msg: "Couldn't download the update. Try again later.", err: err}
	}
	s.setJob("installing", "")
	defer os.Remove(dl.Path) // gone already when the install moved it
	version, uerr := s.commitProgram(ctx, dl.Path, dl.SHA256, rel.Version, false)
	if uerr != nil {
		return uerr
	}
	s.auditUpdate(actor, "signed release from GitHub", version, dl.SHA256)
	return nil
}

// handleInstallUpdate starts "Update now".
func (s *Server) handleInstallUpdate(w http.ResponseWriter, r *http.Request) {
	if uerr := s.startInstall(actorName(r), false); uerr != nil {
		writeUpdateError(w, uerr)
		return
	}
	writeJSON(w, http.StatusAccepted, s.notice())
}

// handleInstallStatus reports how "Update now" is going.
func (s *Server) handleInstallStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.notice())
}

// ---- Installing by itself overnight ----

// autoWindow is the local time of day the overnight install may start.
const (
	autoWindowStart = 2 // 02:00
	autoWindowEnd   = 5 // 05:00
)

// maybeAutoInstall installs a new signed release without being asked, when the
// switch is on, it is night, and nothing is downloading. It tries each version
// once per run of the app, so a release that keeps failing does not loop.
func (s *Server) maybeAutoInstall(ctx context.Context, now time.Time) {
	if !s.autoInstallEnabled() || s.cfg.PauseAutomation || !s.canInstallUpdates() {
		return
	}
	if h := now.Hour(); h < autoWindowStart || h >= autoWindowEnd {
		return
	}
	n := s.notice()
	if !n.Available || !n.CanInstall || n.Latest == nil {
		return
	}
	u := &s.upd
	u.mu.Lock()
	tried, active := u.autoTried, u.job.State != "" && u.job.State != "failed"
	u.mu.Unlock()
	if tried == n.Latest.Version || active {
		return
	}
	if s.downloadsBusy() || s.maintenanceBusy() {
		return
	}
	u.mu.Lock()
	u.autoTried = n.Latest.Version
	u.mu.Unlock()
	slog.Info("update: installing the new version overnight", "version", n.Latest.Version)
	if uerr := s.startInstall("the automatic update", true); uerr != nil {
		slog.Info("update: the overnight install did not start", "reason", uerr.msg)
	}
}

// downloadsBusy reports whether any download is running.
func (s *Server) downloadsBusy() bool {
	n, err := s.QueueRepo.CountRunning()
	return err != nil || n > 0
}
