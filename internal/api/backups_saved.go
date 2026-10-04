package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/backup"
	"github.com/rdborg/mediarium/internal/settings"
)

// Automatic backups: every night (on unless switched off) and before every
// update, Mediarium saves a backup zip in /config/backups and keeps the newest
// few. Each is a normal backup that Restore accepts.

const (
	defaultBackupKeep = 7
	backupEvery       = 24 * time.Hour
	backupCheck       = time.Hour
)

var savedBackupMu sync.Mutex

func (s *Server) backupAutoEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyBackupAuto)
	return v != "0"
}

func (s *Server) backupKeep() int {
	v, _ := s.Settings.Get(settings.KeyBackupKeep)
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultBackupKeep
	}
	return n
}

// saveBackup makes a backup in the backups folder and trims the folder to
// the number kept. why goes in the activity line ("nightly", "before updating
// to 1.4.0").
func (s *Server) saveBackup(ctx context.Context, why string) (backup.Saved, error) {
	savedBackupMu.Lock()
	defer savedBackupMu.Unlock()
	now := time.Now()
	path, err := backup.Save(ctx, s.db, s.cfg.ConfigDir, s.version, now)
	if err != nil {
		return backup.Saved{}, err
	}
	if _, err := backup.Prune(s.cfg.ConfigDir, s.backupKeep()); err != nil {
		log.Printf("backup: %v", err)
	}
	info, _ := os.Stat(path)
	saved := backup.Saved{Name: backup.FileName(now), CreatedAt: now}
	if info != nil {
		saved.Size = info.Size()
	}
	_ = s.QueueRepo.LogActivity(0, "backup", fmt.Sprintf("Saved a backup (%s, %s)", why, humanBytes(saved.Size)))
	return saved, nil
}

// backupJob is the automation loop's hourly check: a backup a day, at most.
func (s *Server) backupJob(ctx context.Context) {
	if !s.backupAutoEnabled() {
		return
	}
	list, err := backup.ListSaved(s.cfg.ConfigDir)
	if err == nil && len(list) > 0 && time.Since(list[0].CreatedAt) < backupEvery-backupCheck {
		return
	}
	if _, err := s.saveBackup(ctx, "nightly"); err != nil {
		log.Printf("backup: nightly backup failed: %v", err)
		noteBackupFailed(err)
	}
}

type savedBackupsPayload struct {
	Auto   bool           `json:"auto"`
	Keep   int            `json:"keep"`
	Folder string         `json:"folder"`
	Items  []backup.Saved `json:"items"`
}

func (s *Server) handleListSavedBackups(w http.ResponseWriter, r *http.Request) {
	list, err := backup.ListSaved(s.cfg.ConfigDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []backup.Saved{}
	}
	writeJSON(w, http.StatusOK, savedBackupsPayload{Auto: s.backupAutoEnabled(), Keep: s.backupKeep(), Folder: s.cfg.ConfigDir + "/" + backup.SavedDir, Items: list})
}

// handleSaveBackupNow makes a backup in the folder straight away.
func (s *Server) handleSaveBackupNow(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) || !requireSession(w, r) {
		return
	}
	saved, err := s.saveBackup(r.Context(), "made by hand")
	if err != nil {
		log.Printf("backup: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save the backup. Check that the config folder has free space.")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// handleDownloadSavedBackup sends one saved backup. Like a fresh backup, it
// holds every stored secret, so it needs a signed-in administrator, not an
// API key.
func (s *Server) handleDownloadSavedBackup(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) || !requireSession(w, r) {
		return
	}
	name := r.PathValue("name")
	p, err := backup.PathOf(s.cfg.ConfigDir, name)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "That backup isn't there any more.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, p)
}
