package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/fileperms"
	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/ytdlp"
)

// The Channel archiver (V4 D21, #143): follow YouTube channels and playlists
// (or another website an administrator adds) and keep their videos, or just the
// sound, in a folder of their own. It is off until an administrator ticks the
// warning, gives the path of a yt-dlp file and switches the module on.
//
// Mediarium never ships, downloads or updates yt-dlp: it starts the file the
// administrator supplied, with fixed arguments (internal/ytdlp). The followed
// sources and the ids of the videos seen are in archiver_sources and
// archiver_videos; what was downloaded is listed in the settings card instead of
// the Activity page, which keeps the archiver apart from the download line.
//
// A check runs one source at a time: a job looks every ten minutes for the
// source that has waited longest for its check, and does nothing while the
// interval has not passed, so sources are spread out. Nothing is checked in the
// first ten minutes after the app starts.

const (
	archiverTick       = 10 * time.Minute
	archiverFirstRun   = 10 * time.Minute
	maxArchiverSources = 100
	maxArchiverHosts   = 10
	archiverRetries    = 3 // download attempts for one video before it is left as failed
)

type archiverConfig struct {
	YtdlpPath      string   `json:"ytdlpPath"`
	Folder         string   `json:"folder"`
	Format         string   `json:"format"`         // video, mp3 or m4a: the choice for a new source
	MaxPerRun      int      `json:"maxPerRun"`      // videos downloaded per source per check
	PauseSeconds   int      `json:"pauseSeconds"`   // wait between two downloads
	IntervalHours  int      `json:"intervalHours"`  // between two checks of a source
	TimeoutMinutes int      `json:"timeoutMinutes"` // longest one video may take
	ExtraHosts     []string `json:"extraHosts"`     // websites besides YouTube
	Acknowledged   bool     `json:"acknowledged"`   // the warning was read
}

func (s *Server) archiverConfig() archiverConfig {
	c := archiverConfig{PauseSeconds: 30} // what a saved setting without the field gets
	if v, _ := s.Settings.Get(settings.KeyArchiverConfig); v != "" {
		_ = json.Unmarshal([]byte(v), &c)
	}
	return c.withDefaults()
}

func (c archiverConfig) withDefaults() archiverConfig {
	if !ytdlp.ValidFormat(c.Format) {
		c.Format = ytdlp.FormatVideo
	}
	if c.MaxPerRun < 1 {
		c.MaxPerRun = 5
	}
	if c.PauseSeconds < 0 {
		c.PauseSeconds = 0
	}
	if c.IntervalHours < 1 {
		c.IntervalHours = 12
	}
	if c.TimeoutMinutes < 1 {
		c.TimeoutMinutes = 60
	}
	if c.ExtraHosts == nil {
		c.ExtraHosts = []string{}
	}
	return c
}

func (s *Server) saveArchiverConfig(c archiverConfig) error {
	b, _ := json.Marshal(c)
	return s.Settings.Set(settings.KeyArchiverConfig, string(b), false)
}

// archiverEnabled reports whether the Channel archiver is switched on.
func (s *Server) archiverEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyModulesArchiver)
	return v == "1"
}

func (c archiverConfig) runner() ytdlp.Runner {
	return ytdlp.Runner{Path: c.YtdlpPath, Timeout: time.Duration(c.TimeoutMinutes) * time.Minute}
}

// archiverReady checks what the module needs before it can be switched on:
// the ticked warning, a folder, and a yt-dlp file that answers with a version.
func (s *Server) archiverReady(ctx context.Context) error {
	c := s.archiverConfig()
	switch {
	case !c.Acknowledged:
		return errArchiverTick
	case c.Folder == "":
		return errArchiverFolder
	case c.YtdlpPath == "":
		return errArchiverPath
	}
	_, err := c.runner().Version(ctx)
	return err
}

var (
	errArchiverWrite  = "The download folder couldn't be written to."
	errArchiverTick   = errors.New("Tick the box to say you have read the warning first.")
	errArchiverFolder = errors.New("Choose a full path for the download folder, like /archive.")
	errArchiverPath   = errors.New("Give the full path of the yt-dlp file, like /usr/local/bin/yt-dlp.")
)

// ---- state of the running check

type archiverState struct {
	mu      sync.Mutex
	running int64 // the source being checked, 0 when none
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	// notBefore is the first moment the job may start a check: ten minutes after
	// the app starts, so a restart is never also a burst of downloads.
	notBefore time.Time
}

// archiverBegin claims the one place for a check. It returns false when a check
// is already running. done must be called when the check is over.
func (s *Server) archiverBegin(parent context.Context, id int64) (context.Context, func(), bool) {
	a := &s.archiver
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running != 0 {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	a.running, a.cancel = id, cancel
	a.wg.Add(1)
	return ctx, func() {
		a.mu.Lock()
		a.running, a.cancel = 0, nil
		a.mu.Unlock()
		cancel()
		a.wg.Done()
	}, true
}

func (s *Server) archiverRunning() int64 {
	s.archiver.mu.Lock()
	defer s.archiver.mu.Unlock()
	return s.archiver.running
}

// archiverStop ends a running check (the module was switched off, or Mediarium
// is stopping) and waits a little for it to wind down.
func (s *Server) archiverStop(wait time.Duration) {
	a := &s.archiver
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Unlock()
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(wait):
	}
}

// ---- the job

// archiverJob runs every ten minutes. It checks the source that has waited
// longest, when its interval has passed, and then returns: one source at a time.
func (s *Server) archiverJob(ctx context.Context) {
	if !s.archiverEnabled() || time.Now().Before(s.archiver.notBefore) {
		return
	}
	cfg := s.archiverConfig()
	cutoff := time.Now().Add(-time.Duration(cfg.IntervalHours) * time.Hour).UTC().Format(time.RFC3339)
	var id int64
	err := s.db.QueryRow(`SELECT id FROM archiver_sources WHERE last_checked = '' OR last_checked <= ? ORDER BY last_checked, id LIMIT 1`, cutoff).Scan(&id)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Info("archiver: look for the next source failed", "err", err)
		}
		return
	}
	run, done, ok := s.archiverBegin(ctx, id)
	if !ok {
		return
	}
	defer done()
	s.archiverCheck(run, id)
}

// ---- sources

type archiverSource struct {
	ID          int64  `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Format      string `json:"format"`
	Backlog     bool   `json:"backlog"`
	AddedAt     string `json:"addedAt"`
	LastChecked string `json:"lastChecked"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Queued      int    `json:"queued"`
	Done        int    `json:"done"`
	Failed      int    `json:"failed"`
	Checking    bool   `json:"checking"`
}

func (s *Server) archiverSource(id int64) (archiverSource, error) {
	var src archiverSource
	var backlog int
	err := s.db.QueryRow(`SELECT id, url, name, format, backlog, added_at, last_checked, status, message FROM archiver_sources WHERE id = ?`, id).
		Scan(&src.ID, &src.URL, &src.Name, &src.Format, &backlog, &src.AddedAt, &src.LastChecked, &src.Status, &src.Message)
	src.Backlog = backlog == 1
	return src, err
}

func (s *Server) archiverSources() ([]archiverSource, error) {
	rows, err := s.db.Query(`SELECT s.id, s.url, s.name, s.format, s.backlog, s.added_at, s.last_checked, s.status, s.message,
		COALESCE(SUM(v.state = 'queued'), 0), COALESCE(SUM(v.state = 'done'), 0), COALESCE(SUM(v.state = 'failed'), 0)
		FROM archiver_sources s LEFT JOIN archiver_videos v ON v.source_id = s.id GROUP BY s.id ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("archiver: list sources: %w", err)
	}
	defer rows.Close()
	running := s.archiverRunning()
	out := []archiverSource{}
	for rows.Next() {
		var src archiverSource
		var backlog int
		if err := rows.Scan(&src.ID, &src.URL, &src.Name, &src.Format, &backlog, &src.AddedAt, &src.LastChecked, &src.Status, &src.Message, &src.Queued, &src.Done, &src.Failed); err != nil {
			return nil, fmt.Errorf("archiver: read source: %w", err)
		}
		src.Backlog = backlog == 1
		src.Checking = src.ID == running
		out = append(out, src)
	}
	return out, rows.Err()
}

type archiverVideo struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Error  string `json:"error"`
	At     string `json:"at"`
}

// archiverRecent is the last videos that were downloaded or failed.
func (s *Server) archiverRecent() ([]archiverVideo, error) {
	rows, err := s.db.Query(`SELECT CASE WHEN s.name <> '' THEN s.name ELSE s.url END, v.video_id, v.title, v.state, v.error,
		CASE WHEN v.done_at <> '' THEN v.done_at ELSE v.seen_at END AS at
		FROM archiver_videos v JOIN archiver_sources s ON s.id = v.source_id
		WHERE v.state IN ('done', 'failed') ORDER BY at DESC LIMIT 30`)
	if err != nil {
		return nil, fmt.Errorf("archiver: list videos: %w", err)
	}
	defer rows.Close()
	out := []archiverVideo{}
	for rows.Next() {
		var v archiverVideo
		if err := rows.Scan(&v.Source, &v.ID, &v.Title, &v.State, &v.Error, &v.At); err != nil {
			return nil, fmt.Errorf("archiver: read video: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- a check

func (s *Server) archiverNote(id int64, status, message string) {
	if _, err := s.db.Exec(`UPDATE archiver_sources SET status = ?, message = ?, last_checked = ? WHERE id = ?`,
		status, message, time.Now().UTC().Format(time.RFC3339), id); err != nil {
		slog.Info("archiver: couldn't save the result of a check", "err", err)
	}
}

type archiverQueued struct {
	id, title, url string
	attempts       int
}

// archiverCheck lists one source, remembers the videos it has not seen, and
// downloads up to the limit of them, newest first, one after the other.
func (s *Server) archiverCheck(ctx context.Context, id int64) {
	cfg := s.archiverConfig()
	src, err := s.archiverSource(id)
	if err != nil {
		return
	}
	if cfg.YtdlpPath == "" || cfg.Folder == "" || !cfg.Acknowledged {
		s.archiverNote(id, "error", errArchiverPath.Error())
		return
	}
	addr, err := ytdlp.ValidateURL(src.URL, cfg.ExtraHosts)
	if err != nil {
		s.archiverNote(id, "error", err.Error())
		return
	}
	run := cfg.runner()
	videos, _, err := run.List(ctx, addr)
	if ctx.Err() != nil {
		return
	}
	if err != nil && len(videos) == 0 {
		s.archiverNote(id, "error", err.Error())
		return
	}
	first := src.LastChecked == ""
	if src.Name == "" {
		for _, v := range videos {
			if v.Channel != "" {
				src.Name = ytdlp.SafeName(v.Channel, "", 80)
				break
			}
		}
		if src.Name != "" {
			_, _ = s.db.Exec(`UPDATE archiver_sources SET name = ? WHERE id = ?`, src.Name, id)
		}
	}
	seenAt := time.Now().UTC().Format(time.RFC3339)
	for i, v := range videos {
		state := "queued"
		if first && !src.Backlog {
			state = "skipped"
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO archiver_videos (source_id, video_id, title, state, seen_at, rank, url) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, v.ID, v.Title, state, seenAt, i, v.URL); err != nil {
			s.archiverNote(id, "error", "That couldn't be saved.")
			return
		}
	}
	s.archiverCleanLeftovers(cfg.Folder)

	rows, err := s.db.Query(`SELECT video_id, title, url, attempts FROM archiver_videos
		WHERE source_id = ? AND (state = 'queued' OR (state = 'failed' AND attempts < ?)) ORDER BY seen_at DESC, rank LIMIT ?`, id, archiverRetries, cfg.MaxPerRun)
	if err != nil {
		s.archiverNote(id, "error", "That couldn't be saved.")
		return
	}
	var todo []archiverQueued
	for rows.Next() {
		var q archiverQueued
		if err := rows.Scan(&q.id, &q.title, &q.url, &q.attempts); err == nil {
			todo = append(todo, q)
		}
	}
	_ = rows.Close()

	got, failed := 0, 0
	for i, q := range todo {
		if ctx.Err() != nil || !s.archiverEnabled() {
			return // switched off or stopping: what is left stays queued
		}
		if i > 0 && cfg.PauseSeconds > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(cfg.PauseSeconds) * time.Second):
			}
		}
		dest, msg, err := s.archiverDownload(ctx, cfg, run, src, q)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			failed++
			_, _ = s.db.Exec(`UPDATE archiver_videos SET state = 'failed', attempts = attempts + 1, error = ? WHERE source_id = ? AND video_id = ?`, msg, id, q.id)
			slog.Info("archiver: a video failed", "source", id, "video", q.id, "err", msg)
			continue
		}
		got++
		_, _ = s.db.Exec(`UPDATE archiver_videos SET state = 'done', error = '', path = ?, done_at = ? WHERE source_id = ? AND video_id = ?`,
			dest, time.Now().UTC().Format(time.RFC3339), id, q.id)
		slog.Info("archiver: a video was kept", "source", id, "video", q.id)
	}
	status, note := "ok", ""
	if failed > 0 && got == 0 {
		status = "error"
		note = "Some videos couldn't be downloaded. The list below says why."
	}
	s.archiverNote(id, status, note)
}

var fileExt = regexp.MustCompile(`^\.[A-Za-z0-9]{1,5}$`)

// archiverDownload fetches one video into a folder of its own, then moves it to
// <folder>/<channel>/<title> [id].<ext>, with the info file beside it. It returns
// where the video went, or a short reason in plain words.
func (s *Server) archiverDownload(ctx context.Context, cfg archiverConfig, run ytdlp.Runner, src archiverSource, q archiverQueued) (string, string, error) {
	addr, err := ytdlp.VideoAddress(src.URL, ytdlp.Video{ID: q.id, URL: q.url}, cfg.ExtraHosts)
	if err != nil {
		return "", err.Error(), err
	}
	if err := fileperms.MkdirAll(cfg.Folder); err != nil {
		return "", errArchiverWrite, err
	}
	tmp, err := os.MkdirTemp(cfg.Folder, ".archiver-")
	if err != nil {
		return "", errArchiverWrite, err
	}
	defer os.RemoveAll(tmp)
	res, _, err := run.Download(ctx, addr, src.Format, tmp)
	if err != nil {
		return "", shortReason(err), err
	}
	channel := ytdlp.SafeName(src.Name, "Channel", 80)
	dir := filepath.Join(cfg.Folder, channel)
	if err := fileperms.MkdirAll(dir); err != nil {
		return "", errArchiverWrite, err
	}
	base := ytdlp.SafeName(q.title, q.id, 120) + " [" + q.id + "]"
	ext := filepath.Ext(res.Media)
	if !fileExt.MatchString(ext) {
		ext = ".bin"
	}
	dest := filepath.Join(dir, base+ext)
	if _, err := os.Stat(dest); err == nil {
		return dest, "", nil // already there from before: keep that one
	}
	if err := os.Rename(res.Media, dest); err != nil {
		return "", errArchiverWrite, err
	}
	_ = fileperms.ApplyFile(dest)
	if res.Info != "" {
		info := filepath.Join(dir, base+".info.json")
		if err := os.Rename(res.Info, info); err == nil {
			_ = fileperms.ApplyFile(info)
		}
	}
	return dest, "", nil
}

// shortReason is what is kept about a failed download: the error, which holds
// the line yt-dlp said, never more than a few hundred characters.
func shortReason(err error) string {
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

// archiverCleanLeftovers removes the work folders an earlier run left behind
// (a crash, a power cut) when they are over an hour old.
func (s *Server) archiverCleanLeftovers(folder string) {
	old, _ := filepath.Glob(filepath.Join(folder, ".archiver-*"))
	for _, d := range old {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() && time.Since(fi.ModTime()) > time.Hour {
			_ = os.RemoveAll(d)
		}
	}
}

// ---- API

type archiverPayload struct {
	Enabled bool             `json:"enabled"`
	Config  archiverConfig   `json:"config"`
	Hosts   []string         `json:"defaultHosts"`
	Sources []archiverSource `json:"sources"`
	Recent  []archiverVideo  `json:"recent"`
}

func (s *Server) archiverPayload() (archiverPayload, error) {
	srcs, err := s.archiverSources()
	if err != nil {
		return archiverPayload{}, err
	}
	recent, err := s.archiverRecent()
	if err != nil {
		return archiverPayload{}, err
	}
	return archiverPayload{Enabled: s.archiverEnabled(), Config: s.archiverConfig(), Hosts: ytdlp.DefaultHosts, Sources: srcs, Recent: recent}, nil
}

// GET /api/archiver: the settings, the followed sources and the last videos.
func (s *Server) handleGetArchiver(w http.ResponseWriter, r *http.Request) {
	p, err := s.archiverPayload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// PUT /api/archiver saves the settings. They can be saved while the module is
// off, so it can be set up before it is switched on.
func (s *Server) handlePutArchiver(w http.ResponseWriter, r *http.Request) {
	var req archiverConfig
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	req.YtdlpPath = strings.TrimSpace(req.YtdlpPath)
	req.Folder = strings.TrimSpace(req.Folder)
	if req.MaxPerRun < 1 || req.MaxPerRun > 50 || req.PauseSeconds < 0 || req.PauseSeconds > 3600 || req.IntervalHours < 1 || req.IntervalHours > 168 || req.TimeoutMinutes < 5 || req.TimeoutMinutes > 720 {
		writeError(w, http.StatusBadRequest, "Check the numbers: 1 to 50 videos per check, a pause of 0 to 3600 seconds, a check every 1 to 168 hours and 5 to 720 minutes for one video.")
		return
	}
	if !ytdlp.ValidFormat(req.Format) {
		writeError(w, http.StatusBadRequest, "Choose a format: video, mp3 or m4a.")
		return
	}
	if req.YtdlpPath != "" {
		if err := ytdlp.CheckPath(req.YtdlpPath); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Folder != "" {
		if !filepath.IsAbs(req.Folder) || strings.ContainsRune(req.Folder, 0) {
			writeError(w, http.StatusBadRequest, errArchiverFolder.Error())
			return
		}
		req.Folder = filepath.Clean(req.Folder)
		if err := fileperms.MkdirAll(req.Folder); err != nil {
			writeError(w, http.StatusBadRequest, "Mediarium can't write to that folder.")
			return
		}
		if err := fsinfo.CheckWritable(req.Folder); err != nil {
			writeError(w, http.StatusBadRequest, "Mediarium can't write to that folder.")
			return
		}
	}
	hosts := []string{}
	seen := map[string]bool{}
	for _, h := range req.ExtraHosts {
		if strings.TrimSpace(h) == "" {
			continue
		}
		h, err := ytdlp.CleanHost(h)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	if len(hosts) > maxArchiverHosts {
		writeError(w, http.StatusBadRequest, "You can allow up to 10 other websites.")
		return
	}
	req.ExtraHosts = hosts
	if s.archiverEnabled() {
		switch {
		case !req.Acknowledged:
			writeError(w, http.StatusBadRequest, "The warning has to stay ticked while the Channel archiver is on.")
			return
		case req.Folder == "":
			writeError(w, http.StatusBadRequest, errArchiverFolder.Error())
			return
		case req.YtdlpPath == "":
			writeError(w, http.StatusBadRequest, errArchiverPath.Error())
			return
		}
	}
	if err := s.saveArchiverConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	s.handleGetArchiver(w, r)
}

// POST /api/archiver/test runs "yt-dlp --version" on the file at the given
// path (or the saved one) and answers with the version.
func (s *Server) handleArchiverTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	_ = decodeJSON(r, &req)
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = s.archiverConfig().YtdlpPath
	}
	if path == "" {
		writeError(w, http.StatusBadRequest, errArchiverPath.Error())
		return
	}
	v, err := ytdlp.Runner{Path: path}.Version(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": v})
}

func (s *Server) requireArchiver(w http.ResponseWriter) bool {
	if !s.archiverEnabled() {
		writeError(w, http.StatusConflict, "The Channel archiver is switched off. Switch it on in Settings > Media types.")
		return false
	}
	return true
}

// POST /api/archiver/sources follows a channel or a playlist. Body: {"url",
// "name" (optional), "format" (video, mp3 or m4a), "backlog"}.
func (s *Server) handleAddArchiverSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireArchiver(w) {
		return
	}
	var req struct {
		URL     string `json:"url"`
		Name    string `json:"name"`
		Format  string `json:"format"`
		Backlog bool   `json:"backlog"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	cfg := s.archiverConfig()
	addr, err := ytdlp.ValidateURL(req.URL, cfg.ExtraHosts)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Format == "" {
		req.Format = cfg.Format
	}
	if !ytdlp.ValidFormat(req.Format) {
		writeError(w, http.StatusBadRequest, "Choose a format: video, mp3 or m4a.")
		return
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM archiver_sources`).Scan(&n)
	if n >= maxArchiverSources {
		writeError(w, http.StatusBadRequest, "You can follow up to 100 channels and playlists.")
		return
	}
	name := ""
	if strings.TrimSpace(req.Name) != "" {
		name = ytdlp.SafeName(req.Name, "", 80)
	}
	backlog := 0
	if req.Backlog {
		backlog = 1
	}
	if _, err := s.db.Exec(`INSERT INTO archiver_sources (url, name, format, backlog, added_at) VALUES (?, ?, ?, ?, ?)`,
		addr, name, req.Format, backlog, time.Now().UTC().Format(time.RFC3339)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "That channel or playlist is already followed.")
			return
		}
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	s.handleGetArchiver(w, r)
}

func archiverID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// DELETE /api/archiver/sources/{id} stops following a source. The files that
// were kept are left where they are.
func (s *Server) handleDeleteArchiverSource(w http.ResponseWriter, r *http.Request) {
	id := archiverID(r)
	if s.archiverRunning() == id && id != 0 {
		s.archiverStop(5 * time.Second)
	}
	_, _ = s.db.Exec(`DELETE FROM archiver_videos WHERE source_id = ?`, id)
	res, err := s.db.Exec(`DELETE FROM archiver_sources WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "That channel or playlist isn't followed.")
		return
	}
	s.handleGetArchiver(w, r)
}

// POST /api/archiver/sources/{id}/check checks one source now, in the
// background, and tries the videos that failed again.
func (s *Server) handleArchiverCheckNow(w http.ResponseWriter, r *http.Request) {
	if !s.requireArchiver(w) {
		return
	}
	id := archiverID(r)
	if _, err := s.archiverSource(id); err != nil {
		writeError(w, http.StatusNotFound, "That channel or playlist isn't followed.")
		return
	}
	ctx, done, ok := s.archiverBegin(context.Background(), id)
	if !ok {
		writeError(w, http.StatusConflict, "A check is already running. Try again when it is done.")
		return
	}
	_, _ = s.db.Exec(`UPDATE archiver_videos SET state = 'queued', attempts = 0 WHERE source_id = ? AND state = 'failed'`, id)
	go func() {
		defer done()
		s.archiverCheck(ctx, id)
	}()
	s.handleGetArchiver(w, r)
}
