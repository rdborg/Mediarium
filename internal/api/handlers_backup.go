package api

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ryanborg/mediarium/internal/auth"
	"github.com/ryanborg/mediarium/internal/backup"
)

// defaultRestartDelay gives the restore response time to reach the browser
// before the process goes away.
const defaultRestartDelay = 1500 * time.Millisecond

// SetExitFunc sets what happens after a restore has been staged: the app
// must restart so the staged files are swapped in before the database is
// opened. main wires this to a graceful shutdown (the process exits 0 and
// the container's restart policy, e.g. `restart: unless-stopped`, starts it
// again). The default is a plain os.Exit(0). On a setup with no restart
// policy the user has to start Mediarium again by hand; the restore is
// applied on that next start either way. Tests replace it so they do not
// exit.
func (s *Server) SetExitFunc(fn func()) { s.exitFn = fn }

func (s *Server) exit() {
	if s.exitFn != nil {
		s.exitFn()
		return
	}
	log.Println("Mediarium is exiting to apply a restore; start it again if it is not restarted automatically")
	os.Exit(0)
}

// requireAdmin reports whether the caller is an administrator, writing a 403
// itself if not. Backups contain every stored credential and restoring
// replaces the whole database, so neither is offered to ordinary users.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if u := auth.UserFromContext(r.Context()); u != nil && u.IsAdmin {
		return true
	}
	writeError(w, http.StatusForbidden, "Only an administrator can back up or restore Mediarium.")
	return false
}

// handleSystemInfo returns facts the backup page shows. lastBackupHint is
// reserved for the UI and always empty for now: the server does not track
// when the browser last downloaded a backup.
func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var dbSize int64
	if fi, err := os.Stat(filepath.Join(s.cfg.ConfigDir, backup.DBFile)); err == nil {
		dbSize = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        s.version,
		"configDir":      s.cfg.ConfigDir,
		"dbSizeBytes":    dbSize,
		"lastBackupHint": "",
	})
}

// headerOnFirstWrite sets the download headers only once there is a body to
// send, so a failure before the first byte can still be a normal JSON error.
type headerOnFirstWrite struct {
	w       http.ResponseWriter
	name    string
	started bool
}

func (h *headerOnFirstWrite) Write(p []byte) (int, error) {
	if !h.started {
		h.started = true
		hdr := h.w.Header()
		hdr.Set("Content-Type", "application/zip")
		hdr.Set("Content-Disposition", `attachment; filename="`+h.name+`"`)
		hdr.Set("Cache-Control", "no-store")
	}
	return h.w.Write(p)
}

// handleBackup streams a backup zip (app.db snapshot, secret.key, manifest).
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	now := time.Now()
	out := &headerOnFirstWrite{w: w, name: backup.FileName(now)}
	if err := backup.Create(r.Context(), s.db, s.cfg.ConfigDir, s.version, now, out); err != nil {
		log.Printf("backup: create failed: %v", err)
		if !out.started {
			writeError(w, http.StatusInternalServerError, "Could not create the backup. Check the Mediarium log for details.")
			return
		}
		// Part of the zip is already on its way: cut the connection so the
		// browser reports a failed download rather than saving a broken file.
		panic(http.ErrAbortHandler)
	}
}

// handleRestore accepts a backup zip (multipart field "file", max 1 GB),
// validates it strictly, stages it in <config>/restore-pending/ and restarts
// the app so main applies it before the database is opened.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	if !s.restoreMu.TryLock() {
		writeError(w, http.StatusConflict, "A restore is already in progress.")
		return
	}
	defer s.restoreMu.Unlock()

	// The cap covers the whole request; the file part gets its own exact cap
	// below. A little slack accounts for the multipart framing.
	r.Body = http.MaxBytesReader(w, r.Body, backup.MaxRestoreBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "Upload the backup as a multipart form with the zip in a field named \"file\".")
		return
	}

	upload, err := s.receiveBackupUpload(mr)
	if err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.Is(err, errBackupTooLarge) || errors.As(err, &tooBig):
			writeError(w, http.StatusRequestEntityTooLarge, "That file is too large to be a Mediarium backup (limit 1 GB).")
		case errors.Is(err, errNoBackupFile):
			writeError(w, http.StatusBadRequest, "No backup file was uploaded. Choose the zip in a field named \"file\".")
		default:
			log.Printf("backup: receive restore upload: %v", err)
			writeError(w, http.StatusBadRequest, "The upload could not be read. Try again.")
		}
		return
	}
	defer os.Remove(upload)

	if _, err := backup.Stage(s.cfg.ConfigDir, upload); err != nil {
		var invalid *backup.InvalidError
		if errors.As(err, &invalid) {
			writeError(w, http.StatusBadRequest, invalid.Reason)
			return
		}
		log.Printf("backup: stage restore failed: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not prepare the restore. Your data was not changed. Check the Mediarium log for details.")
		return
	}
	log.Println("backup: restore staged; restarting to apply it")

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restarting": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	delay := s.restartDelay
	if delay == 0 {
		delay = defaultRestartDelay
	}
	go func() {
		time.Sleep(delay)
		s.exit()
	}()
}

var (
	errNoBackupFile   = errors.New("no file part in the upload")
	errBackupTooLarge = errors.New("backup upload is too large")
)

// receiveBackupUpload copies the "file" part into a temporary file in the
// config folder (same disk as the staging area) and returns its path. The
// part is capped at backup.MaxRestoreBytes exactly.
func (s *Server) receiveBackupUpload(mr *multipart.Reader) (string, error) {
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return "", errNoBackupFile
		}
		if err != nil {
			return "", err
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		tmp, err := os.CreateTemp(s.cfg.ConfigDir, ".restore-upload-*.zip")
		if err != nil {
			return "", fmt.Errorf("create upload file: %w", err)
		}
		n, copyErr := io.Copy(tmp, io.LimitReader(part, backup.MaxRestoreBytes+1))
		closeErr := tmp.Close()
		switch {
		case copyErr != nil:
			os.Remove(tmp.Name())
			return "", copyErr
		case closeErr != nil:
			os.Remove(tmp.Name())
			return "", fmt.Errorf("write upload file: %w", closeErr)
		case n > backup.MaxRestoreBytes:
			os.Remove(tmp.Name())
			return "", errBackupTooLarge
		}
		return tmp.Name(), nil
	}
}
