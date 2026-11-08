package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/backup"
	"github.com/rdborg/mediarium/internal/selfupdate"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
)

// Going back after an update (Settings > System > Updates). Before an update
// changes the database's tables, a copy of the database is kept next to it
// (store.snapshotBeforeMigrating). Putting that copy back is what an older
// version needs to run again; an installed update also keeps the program it
// replaced, which can be put back at the same time.

type rollbackCopy struct {
	Name    string    `json:"name"`
	At      time.Time `json:"at"`
	Size    int64     `json:"size"`
	Version string    `json:"version,omitempty"` // the version that used the database then, when the copy says
}

type rollbackPayload struct {
	Running  string         `json:"running"`
	Image    string         `json:"image,omitempty"`
	Copies   []rollbackCopy `json:"copies"`
	Previous string         `json:"previous,omitempty"` // a kept program that can be started again
}

// rollbackPrevious is the version of the program kept from before the
// installed update, when the container would start it ("" when there is none).
func (s *Server) rollbackPrevious() string {
	pushed, _ := selfupdate.State(s.updateDir())
	if pushed == nil || !pushed.HasPrevious || pushed.PreviousVersion == "" {
		return ""
	}
	if selfupdate.Decide(s.version, s.imageVersion(), pushed.PreviousVersion, true) != nil {
		return ""
	}
	return pushed.PreviousVersion
}

func (s *Server) handleGetRollback(w http.ResponseWriter, r *http.Request) {
	p := rollbackPayload{Running: s.version, Image: s.imageVersion(), Copies: []rollbackCopy{}, Previous: s.rollbackPrevious()}
	for _, c := range store.SafetyCopies(s.cfg.DBPath) {
		p.Copies = append(p.Copies, rollbackCopy{Name: c.Name, At: c.At, Size: c.Size, Version: store.CopySetting(c.Path, settings.KeyLastVersion)})
	}
	writeJSON(w, http.StatusOK, p)
}

type rollbackRequest struct {
	Copy    string `json:"copy"`    // the copy to put back; "" to keep the database
	Program bool   `json:"program"` // also start the program kept from before the installed update
}

// handleRollback puts a copy back (on the next start, like a restore: the
// database in use now is moved aside, not deleted) and, when asked, the
// previous program, then restarts. Only from a signed-in browser: it throws
// away everything since the copy was made.
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	var req rollbackRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.Copy == "" && !req.Program {
		writeError(w, http.StatusBadRequest, "Choose a copy of the database or the earlier program to go back to.")
		return
	}
	if !s.restoreMu.TryLock() {
		writeError(w, http.StatusConflict, "A restore is already in progress.")
		return
	}
	defer s.restoreMu.Unlock()
	if !s.upd.opMu.TryLock() {
		writeError(w, http.StatusConflict, "Another update is in progress.")
		return
	}
	defer s.upd.opMu.Unlock()

	actor := actorName(r)
	var copyAt time.Time
	if req.Copy != "" {
		path, ok := store.SafetyCopyPath(s.cfg.DBPath, req.Copy)
		if !ok {
			writeError(w, http.StatusNotFound, "That copy of the database isn't there any more.")
			return
		}
		for _, c := range store.SafetyCopies(s.cfg.DBPath) {
			if c.Name == req.Copy {
				copyAt = c.At
			}
		}
		if _, err := backup.StageCopy(s.cfg.ConfigDir, path); err != nil {
			var invalid *backup.InvalidError
			if errors.As(err, &invalid) {
				writeError(w, http.StatusBadRequest, invalid.Reason) // the same sentences as for a backup that can't be restored
				return
			}
			slog.Error("rollback: couldn't stage the copy", "copy", req.Copy, "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't get the copy ready. Your data hasn't been changed. Check the Mediarium log for details.")
			return
		}
	}
	version := ""
	if req.Program {
		if s.rollbackPrevious() == "" {
			_ = backup.Unstage(s.cfg.ConfigDir)
			writeError(w, http.StatusConflict, "There is no earlier program to go back to.")
			return
		}
		v, err := selfupdate.Revert(s.updateDir())
		if err != nil {
			_ = backup.Unstage(s.cfg.ConfigDir)
			slog.Error("rollback: couldn't put the earlier program back", "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't put the earlier program back. Nothing was changed. Check the update folder in your config folder.")
			return
		}
		version = v
	}

	slog.Warn("rollback: going back", "by", actor, "copy", req.Copy, "program", version, "from", s.clientIP(r))
	msg := "Going back after an update, asked for by " + actor + "."
	if req.Copy != "" {
		msg += " The database is put back as it was on " + copyAt.Format("2 Jan 2006 15:04") + "."
	}
	if version != "" {
		msg += " Version " + version + " starts again."
	}
	_ = s.QueueRepo.LogActivity(0, "update", msg)

	writeJSON(w, http.StatusAccepted, map[string]any{"version": version, "restartingIn": int(updateRestartDelay.Seconds())})
	flush(w)
	s.restartInPlace.Store(true)
	s.restartSoon(updateRestartDelay)
}
