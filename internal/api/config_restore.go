package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/configfile"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
)

// Settings in a file, with a check first (Settings > System > Settings file).
// An administrator downloads everything that isn't a secret as one file, edits
// or keeps it, and later chooses it again: it is checked (nothing written),
// the difference is shown in plain words, and only then applied. An apply
// makes a copy of the database first and is all or nothing. The file is read
// and written through the same routes the Settings pages use, so every value
// is checked by the rules of the page it belongs to.

const configFileMax = 2 << 20 // the biggest file the page takes

// configFileNames are the file names looked for in the config folder.
var configFileNames = []string{"mediarium-config.yaml", "mediarium-config.yml", "mediarium-config.json"}

// inProcess answers a request by calling the signed-in routes directly, as
// user. Nothing goes over the network, so it works whatever address or proxy
// Mediarium is behind.
type inProcess struct {
	h      http.Handler
	user   *auth.User
	viaKey bool // act like an API key, so the few things only a signed-in browser may do are refused
}

func (t inProcess) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(auth.WithUser(req.Context(), t.user))
	r.RemoteAddr = "127.0.0.1:0"
	if t.viaKey {
		r.Header.Set("X-API-Key", "settings-file")
	}
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

// configClient is a client of this Mediarium that acts as user.
func (s *Server) configClient(user *auth.User, viaKey bool) *configfile.Client {
	return &configfile.Client{
		BaseURL: "http://mediarium.internal",
		HTTP:    &http.Client{Transport: inProcess{h: s.selfAPI, user: user, viaKey: viaKey}, Timeout: 5 * time.Minute},
	}
}

func configHash(data []byte, replace bool) string {
	h := sha256.New()
	h.Write(data)
	if replace {
		h.Write([]byte("\nreplace"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// noEnv is how a file sent from the page reads ${NAME}: it doesn't. Only a
// file in the config folder, put there by whoever runs the server, may read
// the environment.
func noEnv(string) (string, bool) { return "", false }

// parseConfigFile reads a file from the page. The problem is a sentence for
// the person, "" when the file reads.
func parseConfigFile(data []byte, env func(string) (string, bool)) (configfile.File, string) {
	f, err := configfile.Parse(data, env)
	if err == nil {
		return f, ""
	}
	msg := sentence(err.Error())
	if strings.Contains(err.Error(), "isn't set") {
		msg += " A file chosen on this page can't read environment variables; write the value in, or leave the line out and set it on the Settings page."
	}
	return configfile.File{}, msg
}

func (s *Server) handleConfigFileExport(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	data, err := configfile.Export(r.Context(), s.configClient(user, false))
	if err != nil {
		slog.Error("settings file: export failed", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't write the settings file. Check the Mediarium log for details.")
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="mediarium-config.yaml"`)
	_, _ = w.Write(data)
}

// readConfigBody takes the file from the request body.
func readConfigBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, configFileMax))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "That file is too big to be a settings file.")
		return nil, false
	}
	return data, true
}

type configCheckResponse struct {
	Review *configfile.Review `json:"review"`
	Hash   string             `json:"hash"` // what Apply needs, so it applies the file that was checked
}

// POST /api/system/config-file/check?replace=1: the file is the body.
func (s *Server) handleConfigFileCheck(w http.ResponseWriter, r *http.Request) {
	data, ok := readConfigBody(w, r)
	if !ok {
		return
	}
	replace := r.URL.Query().Get("replace") == "1"
	f, problem := parseConfigFile(data, noEnv)
	rv := &configfile.Review{FormatVersion: configfile.FormatVersion, Replace: replace, Sections: []configfile.ReviewSection{}, MissingSecrets: []string{}, Problems: []string{}}
	if problem != "" {
		rv.Problems = []string{problem}
	} else if len(f.Blocks) == 0 && len(f.Lists) == 0 {
		rv.Problems = []string{"There is nothing in this file. Download a settings file from this page to see what one looks like."}
	} else {
		rv = configfile.Check(r.Context(), s.configClient(auth.UserFromContext(r.Context()), false), f, configfile.Options{Replace: replace})
	}
	writeJSON(w, http.StatusOK, configCheckResponse{Review: rv, Hash: configHash(data, replace)})
}

type configApplyResponse struct {
	*configfile.Outcome
	Copy string `json:"copy,omitempty"` // the copy of the database made first
}

// POST /api/system/config-file/apply?replace=1&hash=...: the file is the body.
// Only from a signed-in browser: it can switch on things a key may not.
func (s *Server) handleConfigFileApply(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	data, ok := readConfigBody(w, r)
	if !ok {
		return
	}
	replace := r.URL.Query().Get("replace") == "1"
	if configHash(data, replace) != r.URL.Query().Get("hash") {
		writeError(w, http.StatusConflict, "That isn't the file that was checked. Check it again, then apply.")
		return
	}
	f, problem := parseConfigFile(data, noEnv)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	if !s.configMu.TryLock() {
		writeError(w, http.StatusConflict, "A settings file is being applied right now.")
		return
	}
	defer s.configMu.Unlock()
	user := auth.UserFromContext(r.Context())
	res, status, msg := s.applyConfigFile(r.Context(), f, configfile.Options{Replace: replace}, user, false)
	if msg != "" {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// applyConfigFile makes the copy of the database and applies the file. msg is
// a sentence for the person when nothing was done; status goes with it.
func (s *Server) applyConfigFile(ctx context.Context, f configfile.File, opts configfile.Options, user *auth.User, viaKey bool) (res *configApplyResponse, status int, msg string) {
	c := s.configClient(user, viaKey)
	rv := configfile.Check(ctx, c, f, opts)
	if len(rv.Problems) > 0 {
		return nil, http.StatusBadRequest, "The file can't be applied: " + rv.Problems[0]
	}
	if !rv.CanApply {
		return nil, http.StatusBadRequest, "The file doesn't change anything: Mediarium already matches it."
	}
	copyPath, err := store.SnapshotBeforeConfig(s.db, s.cfg.DBPath)
	if err != nil {
		slog.Error("settings file: couldn't copy the database first", "err", err)
		return nil, http.StatusInternalServerError, "Couldn't make a copy of the database first, so nothing was changed. Check the Mediarium log for details."
	}
	out, err := configfile.ApplyAll(ctx, c, f, opts)
	if err != nil {
		var pe *configfile.ProblemsError
		if errors.As(err, &pe) {
			return nil, http.StatusBadRequest, "The file can't be applied: " + pe.Problems()[0]
		}
		return nil, http.StatusInternalServerError, sentence(err.Error())
	}
	res = &configApplyResponse{Outcome: out}
	if copyPath != "" {
		res.Copy = filepath.Base(copyPath)
	}
	by := "an unknown account"
	if user != nil {
		by = user.Username
	}
	if out.Applied {
		slog.Info("settings file applied", "by", by, "changes", out.Changes, "replace", opts.Replace, "copy", res.Copy)
	} else {
		slog.Warn("settings file not applied; what was done was put back", "by", by, "reason", out.Error, "notUndone", len(out.NotUndone), "copy", res.Copy)
	}
	return res, http.StatusOK, ""
}

// ---- Apply the file in the config folder at start ----

type configStartResult struct {
	At      string `json:"at"`
	File    string `json:"file"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type configStartState struct {
	Enabled bool               `json:"enabled"`
	File    string             `json:"file"` // the file found in the config folder, "" when there is none
	Folder  string             `json:"folder"`
	Last    *configStartResult `json:"last,omitempty"`
}

// configFileOnDisk is the settings file in the config folder: its name and contents.
func (s *Server) configFileOnDisk() (string, []byte) {
	for _, n := range configFileNames {
		if data, err := os.ReadFile(filepath.Join(s.cfg.ConfigDir, n)); err == nil {
			return n, data
		}
	}
	return "", nil
}

func (s *Server) configStartState() configStartState {
	st := configStartState{Enabled: s.settingOn(settings.KeyConfigFileAtStart, false), Folder: s.cfg.ConfigDir}
	for _, n := range configFileNames {
		if _, err := os.Stat(filepath.Join(s.cfg.ConfigDir, n)); err == nil {
			st.File = n
			break
		}
	}
	if v, _ := s.Settings.Get(settings.KeyConfigFileResult); v != "" {
		var last configStartResult
		if json.Unmarshal([]byte(v), &last) == nil {
			st.Last = &last
		}
	}
	return st
}

func (s *Server) handleGetConfigFileStart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.configStartState())
}

// PUT /api/system/config-file/start {"enabled": true}. Only a signed-in
// browser may switch it on: a file in a folder would then change settings.
func (s *Server) handlePutConfigFileStart(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "Say whether the file should be applied at start.")
		return
	}
	if err := setBool(s.Settings, settings.KeyConfigFileAtStart, *req.Enabled); err != nil {
		slog.Error("settings file: could not save the switch", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save that. Try again.")
		return
	}
	slog.Info("settings file at start switched", "on", *req.Enabled, "by", actorName(r))
	writeJSON(w, http.StatusOK, s.configStartState())
}

func (s *Server) saveConfigStartResult(file string, ok bool, message string) {
	b, _ := json.Marshal(configStartResult{At: time.Now().Format(time.RFC3339), File: file, OK: ok, Message: message})
	if err := s.Settings.Set(settings.KeyConfigFileResult, string(b), false); err != nil {
		slog.Warn("settings file: could not save what the apply at start did", "err", err)
	}
}

// ApplyConfigFileAtStart applies mediarium-config.yaml (or .yml, .json) from
// the config folder when the switch is on and the file changed since it was
// last applied (or tried). It never stops the start: whatever goes wrong is
// logged and shown on the Settings page, and the file is tried again only
// when it changes. Environment variables (${NAME}) are read here, since the
// file was put there by whoever runs the server. Call it once the routes are
// built (Routes).
func (s *Server) ApplyConfigFileAtStart() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("settings file: the apply at start failed unexpectedly", "err", r)
		}
	}()
	if !s.settingOn(settings.KeyConfigFileAtStart, false) {
		return
	}
	name, data := s.configFileOnDisk()
	if name == "" {
		return
	}
	hash := configHash(data, false)
	if last, _ := s.Settings.Get(settings.KeyConfigFileHash); last == hash {
		return
	}
	accounts, err := s.Auth.ListAccounts()
	var admin *auth.User
	if err == nil {
		for i := range accounts {
			if accounts[i].IsAdmin && !accounts[i].Disabled {
				u := accounts[i].User
				admin = &u
				break
			}
		}
	}
	if admin == nil {
		slog.Info("settings file: not applied yet, there is no administrator account", "file", name)
		return
	}
	if !s.configMu.TryLock() {
		return
	}
	defer s.configMu.Unlock()
	remember := func(ok bool, msg string) {
		_ = s.Settings.Set(settings.KeyConfigFileHash, hash, false)
		s.saveConfigStartResult(name, ok, msg)
	}
	f, err := configfile.Parse(data, os.LookupEnv)
	if err != nil {
		slog.Warn("settings file: "+name+" can't be read, nothing was changed", "err", err)
		remember(false, sentence(err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res, _, msg := s.applyConfigFile(ctx, f, configfile.Options{}, admin, true)
	switch {
	case msg != "" && strings.HasPrefix(msg, "The file doesn't change anything"):
		slog.Info("settings file: already matches Mediarium, nothing to do", "file", name)
		remember(true, "Mediarium already matched the file.")
	case msg != "":
		slog.Warn("settings file: not applied, nothing was changed", "file", name, "reason", msg)
		remember(false, msg)
	case res.Applied:
		remember(true, fmt.Sprintf("Applied the settings file (changed: %d).", res.Changes))
	default:
		remember(false, "Not applied, and what was done was put back: "+res.Error)
	}
}
