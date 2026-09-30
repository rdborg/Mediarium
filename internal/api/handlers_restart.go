package api

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/rdborg/mediarium/internal/selfupdate"
)

// controlPayload says what the Restart and Shut down buttons can do here.
type controlPayload struct {
	Kind        string `json:"kind"`        // docker | systemd | launchd | custom | none
	CanRestart  bool   `json:"canRestart"`  // something starts Mediarium again after it exits
	CanShutdown bool   `json:"canShutdown"` // Mediarium can stop itself for good (only when nothing would start it again)
	Note        string `json:"note"`
}

// detectSupervisor works out what starts Mediarium again after it exits:
// "docker" (the container's restart policy), "systemd", "launchd", "custom"
// (MEDIARIUM_SUPERVISED is set for a service manager we do not know), or
// "none". getenv and exists are passed in so this can be tested.
func detectSupervisor(supervisedByHand bool, imageVersion string, getenv func(string) string, exists func(string) bool) string {
	switch {
	case imageVersion != "" || exists("/.dockerenv") || getenv("container") != "":
		return "docker"
	case supervisedByHand:
		return "custom"
	case getenv("INVOCATION_ID") != "":
		return "systemd"
	}
	if v := getenv("XPC_SERVICE_NAME"); v != "" && v != "0" {
		return "launchd"
	}
	return "none"
}

func (s *Server) control() controlPayload {
	kind := detectSupervisor(s.cfg.Supervised, s.cfg.ImageVersion, os.Getenv, pathExists)
	if s.upd.supervisorIs != "" {
		kind = s.upd.supervisorIs
	}
	c := controlPayload{Kind: kind, CanRestart: kind != "none", CanShutdown: kind == "none"}
	switch kind {
	case "docker":
		c.Note = "Docker starts Mediarium again by itself when the container's restart policy is \"unless stopped\", which is what the supplied compose file uses. To stop Mediarium, use your container manager."
	case "systemd", "launchd", "custom":
		c.Note = "Your service manager starts Mediarium again by itself. To stop it, use the service manager."
	default:
		c.Note = "Mediarium isn't run by anything that starts it again, so it can't restart itself. Start it again yourself after it stops."
	}
	return c
}

const notRestartableMessage = "Restart is not available here. Start Mediarium again yourself."

// handleRestart restarts Mediarium. With ?safe=true it starts again once with
// automatic searching, downloading and refreshing paused (the same as safe
// mode), so the pages load and settings can be changed.
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !s.control().CanRestart {
		writeError(w, http.StatusConflict, notRestartableMessage)
		return
	}
	if s.maintenanceBusy() {
		writeError(w, http.StatusConflict, "Mediarium is busy with a restore, update or import. Try again in a minute.")
		return
	}
	safe := r.URL.Query().Get("safe") == "true" || r.URL.Query().Get("safe") == "1"
	if safe {
		if err := selfupdate.WriteSafeOnce(s.updateDir()); err != nil {
			slog.Error("restart: could not write the safe-mode marker", "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't prepare the restart. Check the update folder in your config folder.")
			return
		}
	}
	actor := actorName(r)
	slog.Warn("restart requested", "by", actor, "from", s.clientIP(r), "safe", safe)
	msg := "Mediarium was restarted by " + actor + "."
	if safe {
		msg = "Mediarium was restarted by " + actor + " with automation paused."
	}
	_ = s.QueueRepo.LogActivity(0, "update", msg)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "restarting": true, "safe": safe, "restartingIn": int(updateRestartDelay.Seconds())})
	flush(w)
	s.restartSoon(updateRestartDelay)
}

// handleShutdown stops Mediarium for good. Only offered when nothing would
// start it again; in Docker (or under a service manager) it would just come
// back, so those are told to use their own tools.
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	c := s.control()
	if !c.CanShutdown {
		writeError(w, http.StatusConflict, "To stop Mediarium, use your container manager or service manager.")
		return
	}
	actor := actorName(r)
	slog.Warn("shutdown requested", "by", actor, "from", s.clientIP(r))
	_ = s.QueueRepo.LogActivity(0, "update", "Mediarium was shut down by "+actor+".")
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "shuttingDown": true})
	flush(w)
	s.restartSoon(updateRestartDelay)
}
