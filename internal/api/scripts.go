package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/settings"
)

// A script to run after each import (Settings > System > Scripts). Off until
// an administrator picks one. Only files placed in the scripts folder inside
// the config folder (/config/scripts) can be picked, and only from a
// signed-in browser session, not with an API key: whoever can choose what
// runs can run anything as Mediarium's user. Scripts run one at a time, with
// a time limit, and are told about the import in MEDIARIUM_* environment
// variables.

const (
	scriptDefaultTimeout = 5 * time.Minute
	scriptMaxTimeout     = time.Hour
	scriptOutputKept     = 4 * 1024 // bytes of output kept for Settings when a run fails
	scriptMaxWaiting     = 50       // runs queued behind the current one; more are dropped
)

// scriptConfig is the setting: the script's file name ("" = off) and how
// long it may run.
type scriptConfig struct {
	Script     string `json:"script"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

// scriptRun is what the last run did.
type scriptRun struct {
	At       string `json:"at"`
	Script   string `json:"script"`
	Event    string `json:"event"`
	Title    string `json:"title,omitempty"`
	ExitCode int    `json:"exitCode"`
	TimedOut bool   `json:"timedOut,omitempty"`
	Seconds  int    `json:"seconds"`
	Output   string `json:"output,omitempty"` // the end of what it printed, when it failed
	Problem  string `json:"problem,omitempty"`
}

var (
	scriptMu      sync.Mutex // one script at a time
	scriptWaiting atomic.Int32
)

func (s *Server) scriptsDir() string { return filepath.Join(s.cfg.ConfigDir, "scripts") }

func (s *Server) scriptConfig() scriptConfig {
	var c scriptConfig
	if v, _ := s.Settings.Get(settings.KeyScriptAfterImport); v != "" {
		_ = json.Unmarshal([]byte(v), &c)
	}
	return c
}

func (c scriptConfig) timeout() time.Duration {
	if c.TimeoutSec <= 0 {
		return scriptDefaultTimeout
	}
	return min(time.Duration(c.TimeoutSec)*time.Second, scriptMaxTimeout)
}

// listScripts lists the files in the scripts folder that can be run.
func listScripts(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, e := range entries {
		if _, err := scriptPath(dir, e.Name()); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// scriptPath checks that name is a runnable file directly inside dir (no
// folders, no links leading out of it) and returns its path.
func scriptPath(dir, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return "", errors.New("pick a file from the scripts folder")
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("the scripts folder: %w", err)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(dir, name))
	if err != nil {
		return "", fmt.Errorf("%s isn't in the scripts folder", name)
	}
	if filepath.Dir(real) != realDir {
		return "", fmt.Errorf("%s leads outside the scripts folder", name)
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s isn't a file", name)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s can't be run: make it executable (chmod +x)", name)
	}
	return real, nil
}

// scriptEnv is what a script is told, in MEDIARIUM_* variables, plus the
// little it needs to run (PATH, HOME, TZ, LANG). Nothing else from
// Mediarium's own environment is passed on.
func scriptEnv(event string, it notify.Item) []string {
	env := []string{"MEDIARIUM_EVENT=" + event}
	for _, k := range []string{"PATH", "HOME", "TZ", "LANG"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	media := it.Media
	if media == "" {
		media = "movie"
	}
	add := func(k, v string) {
		if v != "" {
			env = append(env, "MEDIARIUM_"+k+"="+strings.ReplaceAll(v, "\x00", ""))
		}
	}
	add("MEDIA", media)
	add("TITLE", it.Title)
	if it.Year > 0 {
		add("YEAR", strconv.Itoa(it.Year))
	}
	add("EPISODE", it.Episode)
	add("QUALITY", it.Quality)
	add("PATH", it.Path)
	if it.Path != "" {
		folder := filepath.Dir(it.Path)
		if info, err := os.Stat(it.Path); err == nil && info.IsDir() {
			folder = it.Path // a season pack or an album: the folder itself
		}
		add("FOLDER", folder)
	}
	add("RELEASE", it.Release)
	if it.SizeBytes > 0 {
		add("SIZE", strconv.FormatInt(it.SizeBytes, 10))
	}
	add("PAGE", it.LinkPath)
	return env
}

// tailBuffer keeps the last n bytes written to it.
type tailBuffer struct {
	n   int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.n {
		t.buf = t.buf[len(t.buf)-t.n:]
	}
	return len(p), nil
}

// runScript runs the chosen script for one event and waits for it.
func (s *Server) runScript(event string, it notify.Item) scriptRun {
	c := s.scriptConfig()
	run := scriptRun{At: time.Now().UTC().Format(time.RFC3339), Script: c.Script, Event: event, Title: it.Name(), ExitCode: -1}
	path, err := scriptPath(s.scriptsDir(), c.Script)
	if err != nil {
		run.Problem = err.Error()
		return run
	}
	limit := c.timeout()
	if event == "test" {
		limit = min(limit, time.Minute) // someone is waiting on the answer
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.Dir = s.scriptsDir()
	cmd.Env = scriptEnv(event, it)
	out := &tailBuffer{n: scriptOutputKept}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 5 * time.Second
	ownProcessGroup(cmd)
	start := time.Now()
	err = cmd.Run()
	// Anything the script started and left running goes with it.
	killProcessGroup(cmd)
	run.Seconds = int(time.Since(start).Round(time.Second) / time.Second)
	if cmd.ProcessState != nil {
		run.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case ctx.Err() != nil:
		run.TimedOut = true
		run.Problem = fmt.Sprintf("It was stopped after %s.", limit)
	case err != nil && run.ExitCode <= 0:
		run.Problem = "It couldn't be started: " + err.Error() + ". Check that its first line names an interpreter the container has, such as #!/bin/sh."
	case err != nil:
		run.Problem = fmt.Sprintf("It finished with exit code %d.", run.ExitCode)
	}
	if run.Problem != "" {
		run.Output = string(bytes.ToValidUTF8(out.buf, []byte("?")))
	}
	return run
}

func (s *Server) saveScriptRun(run scriptRun) {
	b, _ := json.Marshal(run)
	_ = s.Settings.Set(settings.KeyScriptLastRun, string(b), false)
}

// scriptAfterImport runs the chosen script, in the background, after an
// import. Nothing happens while no script is chosen.
func (s *Server) scriptAfterImport(it notify.Item) {
	if s.scriptConfig().Script == "" {
		return
	}
	if scriptWaiting.Add(1) > scriptMaxWaiting {
		scriptWaiting.Add(-1)
		slog.Warn("scripts: too many runs waiting, skipped one", "title", it.Name())
		return
	}
	s.background(func() {
		scriptMu.Lock()
		defer scriptMu.Unlock()
		scriptWaiting.Add(-1)
		run := s.runScript("imported", it)
		s.saveScriptRun(run)
		if run.Problem != "" {
			slog.Warn("scripts: run after import failed", "script", run.Script, "title", run.Title, "exit", run.ExitCode, "timedOut", run.TimedOut)
			_ = s.QueueRepo.LogActivity(0, "script", fmt.Sprintf("The script %s, run after importing %s, had a problem. %s", run.Script, run.Title, run.Problem))
		}
	})
}

type scriptsPayload struct {
	Folder     string     `json:"folder"`
	Scripts    []string   `json:"scripts"`
	Script     string     `json:"script"`
	TimeoutSec int        `json:"timeoutSec"`
	LastRun    *scriptRun `json:"lastRun,omitempty"`
}

func (s *Server) scriptsPayload() scriptsPayload {
	c := s.scriptConfig()
	out := scriptsPayload{Folder: s.scriptsDir(), Scripts: listScripts(s.scriptsDir()), Script: c.Script, TimeoutSec: int(c.timeout() / time.Second)}
	if v, _ := s.Settings.Get(settings.KeyScriptLastRun); v != "" {
		var run scriptRun
		if json.Unmarshal([]byte(v), &run) == nil {
			out.LastRun = &run
		}
	}
	return out
}

// GET /api/scripts: the scripts folder, what is in it and the choice.
func (s *Server) handleGetScripts(w http.ResponseWriter, r *http.Request) {
	_ = os.MkdirAll(s.scriptsDir(), 0o755)
	writeJSON(w, http.StatusOK, s.scriptsPayload())
}

// refuseScriptsByKey answers 403 for an API key: the script that runs is
// chosen and tried only from a signed-in browser session.
func (s *Server) refuseScriptsByKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-API-Key") == "" {
		return false
	}
	actor := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		actor = u.Username
	}
	slog.Warn("scripts: an API key tried to change or run the script", "by", actor, "from", s.clientIP(r))
	writeError(w, http.StatusForbidden, "Scripts can only be chosen and tried from Settings while signed in, not with an API key.")
	return true
}

// PUT /api/scripts {"script": "notify.sh", "timeoutSec": 300}; an empty
// script switches it off.
func (s *Server) handlePutScripts(w http.ResponseWriter, r *http.Request) {
	if s.refuseScriptsByKey(w, r) {
		return
	}
	var req scriptConfig
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	req.Script = strings.TrimSpace(req.Script)
	if req.TimeoutSec < 0 || req.TimeoutSec > int(scriptMaxTimeout/time.Second) {
		writeError(w, http.StatusBadRequest, "The time limit must be between 1 second and 1 hour.")
		return
	}
	if req.Script != "" {
		if _, err := scriptPath(s.scriptsDir(), req.Script); err != nil {
			writeError(w, http.StatusBadRequest, "That script can't be used: "+err.Error()+".")
			return
		}
	}
	b, _ := json.Marshal(req)
	if err := s.Settings.Set(settings.KeyScriptAfterImport, string(b), false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.scriptsPayload())
}

// POST /api/scripts/test runs the chosen script once, with MEDIARIUM_EVENT
// set to "test" and a made-up movie, and says how it went.
func (s *Server) handleTestScript(w http.ResponseWriter, r *http.Request) {
	if s.refuseScriptsByKey(w, r) {
		return
	}
	if s.scriptConfig().Script == "" {
		writeError(w, http.StatusConflict, "Pick a script and save it first.")
		return
	}
	// Never queue behind import runs: someone is waiting on the answer.
	if !scriptMu.TryLock() {
		writeError(w, http.StatusConflict, "A script is running now, after an import. Try again when it has finished.")
		return
	}
	run := s.runScript("test", notify.Item{Media: "movie", Title: "Test Movie", Year: 2001, Quality: "Bluray-1080p", Path: filepath.Join(s.moviesRoot(), "Test Movie (2001)", "Test Movie (2001).mkv")})
	scriptMu.Unlock()
	s.saveScriptRun(run)
	writeJSON(w, http.StatusOK, run)
}
