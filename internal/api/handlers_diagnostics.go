package api

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rdborg/mediarium/internal/backup"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/logbuf"
	"github.com/rdborg/mediarium/internal/selfupdate"
	"github.com/rdborg/mediarium/internal/store"
)

// processStart is when this run of the app began.
var processStart = time.Now()

type diagnosticsDB struct {
	JournalMode       string `json:"journalMode"` // "wal", or "delete" when WAL could not be switched on
	Problem           string `json:"problem,omitempty"`
	SizeBytes         int64  `json:"sizeBytes"`
	WALBytes          int64  `json:"walBytes"`
	MaxOpen           int    `json:"maxOpen"`
	Open              int    `json:"open"`
	InUse             int    `json:"inUse"`
	Idle              int    `json:"idle"`
	WaitCount         int64  `json:"waitCount"`
	WaitSeconds       string `json:"waitSeconds"`
	MaxIdleClosed     int64  `json:"maxIdleClosed"`
	MaxLifetimeClosed int64  `json:"maxLifetimeClosed"`
}

type diagnosticsUsenet struct {
	Server     string `json:"server"`
	InUse      int    `json:"inUse"`
	Limit      int    `json:"limit"`      // connections allowed now
	Configured int    `json:"configured"` // connections set for the login
}

type diagnosticsHealth struct {
	ID    string `json:"id"`
	Level string `json:"level"`
	Title string `json:"title"`
}

// diagnosticsUpdate is the update row of the support report: what is running,
// what is installed on top of the image, and how updating is set up. Nothing
// in it is secret.
type diagnosticsUpdate struct {
	Image       string `json:"image,omitempty"`     // version inside the container image
	Installed   string `json:"installed,omitempty"` // version of the update installed on top of it
	Latest      string `json:"latest,omitempty"`    // newest version the last check found
	CheckedAt   string `json:"checkedAt,omitempty"`
	CheckError  string `json:"checkError,omitempty"`
	Check       bool   `json:"check"`
	AutoInstall bool   `json:"autoInstall"`
	AllowPush   bool   `json:"allowPush"`
	AutoRestart bool   `json:"autoRestartWhenStuck"`
	Supervisor  string `json:"supervisor"`
	SelfRestart string `json:"selfRestart,omitempty"` // this run began after Mediarium restarted itself
}

type diagnosticsPayload struct {
	Version       string               `json:"version"`
	GoVersion     string               `json:"goVersion"`
	OS            string               `json:"os"`
	Arch          string               `json:"arch"`
	Now           string               `json:"now"`
	UptimeSeconds int64                `json:"uptimeSeconds"`
	SafeMode      bool                 `json:"safeMode"`
	Goroutines    int                  `json:"goroutines"`
	MemoryBytes   uint64               `json:"memoryBytes"`
	Database      diagnosticsDB        `json:"database"`
	Update        diagnosticsUpdate    `json:"update"`
	Downloads     *int                 `json:"downloadsRunning,omitempty"` // left out when the database did not answer in time
	Usenet        []diagnosticsUsenet  `json:"usenet"`                     // news server logins used since the app started
	Health        []diagnosticsHealth  `json:"health"`
	Log           []string             `json:"log"`      // the last 500 log lines, secrets taken out
	Problems      []diagnosticsProblem `json:"problems"` // the latest entries of the problem log (Logs and errors), secrets taken out
}

// diagnosticsProblem is one row of the problem log in the support report.
type diagnosticsProblem struct {
	At      string `json:"at"`
	Level   string `json:"level"`
	Area    string `json:"area"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count"`
	Detail  string `json:"detail,omitempty"`
}

// within runs fn and waits at most d for it. It reports whether fn finished.
// Diagnostics are for when something is wrong, so a part that hangs (a stuck
// database) is skipped instead of hanging the whole report.
func within(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// handleDiagnostics returns what a person needs to send with a support
// request: version and platform, how the database is doing, the setup
// warnings and the recent log. Nothing secret is in it: log lines are scrubbed
// as they are kept, and no settings, keys or paths to keys are included.
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	mode := store.Mode(s.db)
	st := s.db.Stats()
	dbInfo := diagnosticsDB{
		JournalMode: mode.JournalMode, Problem: mode.WALProblem,
		MaxOpen: st.MaxOpenConnections, Open: st.OpenConnections, InUse: st.InUse, Idle: st.Idle,
		WaitCount: st.WaitCount, WaitSeconds: st.WaitDuration.Round(time.Millisecond).String(),
		MaxIdleClosed: st.MaxIdleClosed, MaxLifetimeClosed: st.MaxLifetimeClosed,
	}
	dbPath := filepath.Join(s.cfg.ConfigDir, backup.DBFile)
	if fi, err := os.Stat(dbPath); err == nil {
		dbInfo.SizeBytes = fi.Size()
	}
	if fi, err := os.Stat(dbPath + "-wal"); err == nil {
		dbInfo.WALBytes = fi.Size()
	}

	out := diagnosticsPayload{
		Version: s.version, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		Now: time.Now().UTC().Format(time.RFC3339), UptimeSeconds: int64(time.Since(processStart).Seconds()),
		SafeMode: s.cfg.PauseAutomation, Goroutines: runtime.NumGoroutine(), MemoryBytes: mem.Alloc,
		Database: dbInfo, Usenet: []diagnosticsUsenet{}, Health: []diagnosticsHealth{}, Log: logbuf.Default.Lines(),
	}
	for _, st := range download.ServerStates() {
		out.Usenet = append(out.Usenet, diagnosticsUsenet{Server: st.Host, InUse: st.InUse, Limit: st.Limit, Configured: st.Configured})
	}
	if out.Log == nil {
		out.Log = []string{}
	}

	out.Update = s.diagnosticsUpdate()
	out.Problems = s.diagnosticsProblems()

	var running int
	var runErr error
	if within(2*time.Second, func() { running, runErr = s.QueueRepo.CountRunning() }) && runErr == nil {
		out.Downloads = &running
	}
	var items []healthItem
	if within(3*time.Second, func() { items = s.collectHealth() }) {
		for _, it := range items {
			out.Health = append(out.Health, diagnosticsHealth{ID: it.ID, Level: it.Level, Title: it.Title})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) diagnosticsUpdate() diagnosticsUpdate {
	pushed, _ := selfupdate.State(s.updateDir())
	n := s.notice()
	d := diagnosticsUpdate{
		Image: s.imageVersion(), Check: n.Enabled, CheckError: n.Error,
		AutoInstall: s.autoInstallEnabled(), AllowPush: s.allowPushEnabled(), AutoRestart: s.autoRestartEnabled(),
		Supervisor: s.control().Kind,
	}
	if pushed != nil {
		d.Installed = pushed.Version
	}
	if n.Latest != nil {
		d.Latest = n.Latest.Version
	}
	if n.CheckedAt != nil {
		d.CheckedAt = n.CheckedAt.UTC().Format(time.RFC3339)
	}
	if st := s.selfRestartNote(); st.Reason != "" {
		d.SelfRestart = st.At.UTC().Format(time.RFC3339) + ": " + st.Reason
	}
	return d
}
