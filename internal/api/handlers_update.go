package api

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/selfupdate"
	"github.com/rdborg/mediarium/internal/settings"
)

// updateStatePayload is the answer of GET /api/system/update: what is running,
// what the container image holds, what has been installed on top of it, and
// whether installing is switched on.
type updateStatePayload struct {
	Running     string             `json:"running"`
	Image       string             `json:"image"`  // version inside the container image; empty when not started by the image
	Pushed      *selfupdate.Pushed `json:"pushed"` // the installed update, null when there is none
	Failed      *selfupdate.Failed `json:"failed,omitempty"`
	AllowPush   bool               `json:"allowPush"`
	CanInstall  bool               `json:"canInstall"` // this install can restart into an installed update
	Platform    string             `json:"platform"`
	Options     optionsPayload     `json:"options"`
	Control     controlPayload     `json:"control"`
	SelfRestart *stuckRestart      `json:"selfRestart,omitempty"` // this run began after the app restarted itself
}

// handleUpdateState reports the update state (administrators only).
func (s *Server) handleUpdateState(w http.ResponseWriter, r *http.Request) {
	pushed, failed := selfupdate.State(s.updateDir())
	var self *stuckRestart
	if st := s.selfRestartNote(); st.Reason != "" {
		self = &st
	}
	writeJSON(w, http.StatusOK, updateStatePayload{
		Running: s.version, Image: s.imageVersion(), Pushed: pushed, Failed: failed,
		AllowPush: s.allowPushEnabled(), CanInstall: s.canInstallUpdates(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Options:  s.options(), Control: s.control(), SelfRestart: self,
	})
}

// ---- The switches ----

type optionsPayload struct {
	UpdateCheck          bool `json:"updateCheck"`          // ask GitHub once a day for a new version
	AutoInstall          bool `json:"autoInstall"`          // install a new signed release overnight
	AllowPush            bool `json:"allowPush"`            // let an administrator upload a program file
	AutoRestartWhenStuck bool `json:"autoRestartWhenStuck"` // restart when nothing has answered for three minutes
}

func (s *Server) options() optionsPayload {
	return optionsPayload{
		UpdateCheck: s.updateCheckEnabled(), AutoInstall: s.autoInstallEnabled(),
		AllowPush: s.allowPushEnabled(), AutoRestartWhenStuck: s.autoRestartEnabled(),
	}
}

type optionsRequest struct {
	UpdateCheck          *bool `json:"updateCheck"`
	AutoInstall          *bool `json:"autoInstall"`
	AllowPush            *bool `json:"allowPush"`
	AutoRestartWhenStuck *bool `json:"autoRestartWhenStuck"`
}

// handleGetOptions reports the switches for checking for updates, installing them, pushed updates and restarting when stuck.
func (s *Server) handleGetOptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.options())
}

// handlePutOptions changes the switches named in the body and leaves the rest.
// Allowing pushed updates can only be switched ON from a signed-in browser
// session: a request that authenticates with an API key may switch it off but
// not on, so a leaked key cannot open the door it would then walk through.
func (s *Server) handlePutOptions(w http.ResponseWriter, r *http.Request) {
	var req optionsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	actor := actorName(r)
	if req.AllowPush != nil && *req.AllowPush && r.Header.Get("X-API-Key") != "" {
		slog.Warn("update: an API key tried to switch on pushed updates", "by", actor, "from", s.clientIP(r))
		writeError(w, http.StatusForbidden, "Pushed updates can only be turned on from Settings while signed in, not with an API key.")
		return
	}
	for _, c := range []struct {
		val *bool
		key string
	}{
		{req.UpdateCheck, settings.KeyUpdatesCheck},
		{req.AutoInstall, settings.KeyUpdatesAutoInstall},
		{req.AutoRestartWhenStuck, settings.KeyAutoRestartStuck},
	} {
		if c.val == nil {
			continue
		}
		if err := setBool(s.Settings, c.key, *c.val); err != nil {
			slog.Error("update: could not save a setting", "key", c.key, "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't save that. Try again.")
			return
		}
	}
	if req.AllowPush != nil && *req.AllowPush != s.allowPushEnabled() {
		if err := setBool(s.Settings, settings.KeyUpdatesAllowPush, *req.AllowPush); err != nil {
			slog.Error("update: could not save a setting", "key", settings.KeyUpdatesAllowPush, "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't save that. Try again.")
			return
		}
		state := "off"
		if *req.AllowPush {
			state = "on"
		}
		slog.Warn("update: pushed updates switched "+state, "by", actor, "from", s.clientIP(r))
		_ = s.QueueRepo.LogActivity(0, "update", "Updates pushed through the API were switched "+state+" by "+actor+".")
	}
	writeJSON(w, http.StatusOK, s.options())
}

// ---- Pushing a program file ----

// pushUploadTime is the longest a program file may take to arrive.
const pushUploadTime = 20 * time.Minute

const pushOffMessage = "Pushed updates are switched off. An administrator can allow them in Settings > System, under Allow updates pushed through the API."

// handlePushUpdate takes the raw program file as the request body, and its
// SHA-256 in the X-Update-SHA256 header, checks both, and restarts into it.
func (s *Server) handlePushUpdate(w http.ResponseWriter, r *http.Request) {
	actor, from := actorName(r), s.clientIP(r)
	if !s.allowPushEnabled() {
		slog.Warn("update: a pushed update was refused because pushing is switched off", "by", actor, "from", from)
		writeError(w, http.StatusForbidden, pushOffMessage)
		return
	}
	if !s.canInstallUpdates() {
		writeError(w, http.StatusConflict, notInstallableMessage)
		return
	}
	want, err := selfupdate.ParseChecksum(r.Header.Get("X-Update-SHA256"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Send the file's SHA-256 in the X-Update-SHA256 header, as 64 hex digits.")
		return
	}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/") {
		writeError(w, http.StatusUnsupportedMediaType, "Send the program file itself as the request body, not as a form.")
		return
	}
	if r.ContentLength > s.pushLimit() {
		writeError(w, http.StatusRequestEntityTooLarge, "That file is too large to be Mediarium (limit 200 MB).")
		return
	}
	if !s.upd.opMu.TryLock() {
		writeError(w, http.StatusConflict, "Another update is already in progress.")
		return
	}
	defer s.upd.opMu.Unlock()

	dir := s.updateDir()
	selfupdate.CleanTemp(dir)
	// A slow or stalled upload must not hold the update lock for ever.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(pushUploadTime))
	r.Body = http.MaxBytesReader(w, r.Body, s.pushLimit())
	tmp, got, n, err := selfupdate.Receive(dir, r.Body, s.pushLimit())
	if err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.Is(err, selfupdate.ErrTooLarge) || errors.As(err, &tooBig):
			writeError(w, http.StatusRequestEntityTooLarge, "That file is too large to be Mediarium (limit 200 MB).")
		case errors.Is(err, selfupdate.ErrEmpty):
			writeError(w, http.StatusBadRequest, "The upload was empty. Send the program file as the request body.")
		default:
			slog.Warn("update: the upload could not be read", "by", actor, "err", err)
			writeError(w, http.StatusBadRequest, "The upload was cut off or couldn't be stored. Try again.")
		}
		return
	}
	defer os.Remove(tmp) // gone already once it has been installed
	if got != want {
		slog.Warn("update: a pushed file did not match its checksum", "by", actor, "from", from, "sent", want, "actual", got, "bytes", n)
		writeError(w, http.StatusBadRequest, "The file doesn't match the SHA-256 you sent, so it wasn't installed.")
		return
	}

	force := r.URL.Query().Get("force") == "true" || r.URL.Query().Get("force") == "1"
	version, uerr := s.commitProgram(r.Context(), tmp, got, "", force)
	if uerr != nil {
		slog.Warn("update: a pushed file was refused", "by", actor, "from", from, "reason", uerr.msg)
		writeUpdateError(w, uerr)
		return
	}
	s.auditUpdate(actor, "pushed through the API from "+from, version, got)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"version":      version,
		"sha256":       got,
		"restartingIn": int(updateRestartDelay.Seconds()),
	})
	flush(w)
	s.restartSoon(updateRestartDelay)
}

// handleRemoveUpdate removes the installed update, so the next start uses the
// program inside the container image. It is not held back by the "allow
// pushed updates" switch: removing an update only ever goes back to the
// version the image was built with, and it is what a person needs after
// switching pushing off. ?restart=true restarts straight away.
func (s *Server) handleRemoveUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.upd.opMu.TryLock() {
		writeError(w, http.StatusConflict, "Another update is in progress.")
		return
	}
	defer s.upd.opMu.Unlock()

	dir := s.updateDir()
	pushed, failed := selfupdate.State(dir)
	if pushed == nil && failed == nil {
		writeError(w, http.StatusNotFound, "There is no installed update to remove.")
		return
	}
	if err := selfupdate.Remove(dir); err != nil {
		slog.Error("update: could not remove the installed update", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't remove the installed update. Check the update folder in your config folder.")
		return
	}
	actor := actorName(r)
	slog.Warn("update: the installed update was removed", "by", actor, "from", s.clientIP(r))
	removed := ""
	if pushed != nil {
		removed = pushed.Version
	}
	_ = s.QueueRepo.LogActivity(0, "update", "The installed update"+versionSuffix(removed)+" was removed by "+actor+". Mediarium goes back to the version in its Docker image the next time it starts.")

	restart := r.URL.Query().Get("restart") == "true" || r.URL.Query().Get("restart") == "1"
	restart = restart && s.control().CanRestart
	writeJSON(w, http.StatusOK, map[string]any{
		"removed":       removed,
		"image":         s.imageVersion(),
		"runningPushed": pushed != nil && pushed.Running,
		"restarting":    restart,
		"restartingIn":  int(updateRestartDelay.Seconds()),
		"restartNeeded": pushed != nil && pushed.Running && !restart,
	})
	if restart {
		flush(w)
		s.restartSoon(updateRestartDelay)
	}
}

func versionSuffix(v string) string {
	if v == "" {
		return ""
	}
	return " (" + v + ")"
}
