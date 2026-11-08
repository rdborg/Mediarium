package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/settings"
)

// Library lifecycle rules (V4 D13), off until an administrator switches the
// module on (Settings > Media types).
//
// The rules are the ones the plain cleanup has (a movie watched a while ago,
// a movie nobody watched, an episode watched a while ago; titles with a keep
// tag are left alone), with a safer way of acting on them:
//
//  1. A dry run lists what the rules would take, and changes nothing.
//  2. "Act for real" can only be switched on after a dry run with these very
//     rules. Changing a rule switches it off again.
//  3. Once it is on, a title that matches goes on the Leaving soon shelf and
//     keeps its files for the grace period. Anyone signed in can press Keep
//     on it, which exempts the title from then on.
//  4. When the grace period ends and the title still matches, its files are
//     removed the same way the plain cleanup removes them (to the recycle bin
//     when it is on) and the removal is written to Activity.
//
// A title that stops matching (it was watched again, or downloaded again)
// leaves the shelf by itself.

const (
	lifecycleEvery    = 24 * time.Hour
	defaultGraceDays  = 14
	maxLifecycleShelf = 200
)

// LifecycleRules are the rules and the switch that makes them act.
type LifecycleRules struct {
	MoviesWatchedDays   int      `json:"moviesWatchedDays"`   // 0 switches a rule off
	MoviesUnwatchedDays int      `json:"moviesUnwatchedDays"` // 0 switches a rule off
	EpisodesWatchedDays int      `json:"episodesWatchedDays"` // 0 switches a rule off
	GraceDays           int      `json:"graceDays"`           // days on the shelf before the files go
	KeepTags            []string `json:"keepTags"`            // titles with one of these tags are never taken
	ActForReal          bool     `json:"actForReal"`          // false: the rules only ever list what would go
}

type lifecycleStatus struct {
	LastRun     string `json:"lastRun,omitempty"`     // RFC 3339
	LastDryRun  string `json:"lastDryRun,omitempty"`  // RFC 3339
	DryRunKey   string `json:"dryRunKey,omitempty"`   // the rules the last dry run was made with
	LastRemoved int    `json:"lastRemoved,omitempty"` // titles removed by the last run
}

// cleanup is the rules in the shape the plain cleanup's search takes.
func (r LifecycleRules) cleanup() CleanupRules {
	return CleanupRules{Enabled: true, MoviesWatchedDays: r.MoviesWatchedDays, MoviesUnwatchedDays: r.MoviesUnwatchedDays, EpisodesWatchedDays: r.EpisodesWatchedDays, KeepTags: r.KeepTags}
}

func (r LifecycleRules) anyRule() bool {
	return r.MoviesWatchedDays > 0 || r.MoviesUnwatchedDays > 0 || r.EpisodesWatchedDays > 0
}

// key names what the rules pick: a dry run counts for these rules only. The
// grace period and the switch itself are not part of it.
func (r LifecycleRules) key() string {
	tags := make([]string, 0, len(r.KeepTags))
	for _, t := range r.KeepTags {
		tags = append(tags, strings.ToLower(strings.TrimSpace(t)))
	}
	sort.Strings(tags)
	return fmt.Sprintf("%d/%d/%d/%s", r.MoviesWatchedDays, r.MoviesUnwatchedDays, r.EpisodesWatchedDays, strings.Join(tags, ","))
}

func (s *Server) lifecycleRules() LifecycleRules {
	var r LifecycleRules
	if v, _ := s.Settings.Get(settings.KeyLifecycleRules); v != "" {
		_ = json.Unmarshal([]byte(v), &r)
	}
	if r.KeepTags == nil {
		r.KeepTags = []string{}
	}
	if r.GraceDays < 1 {
		r.GraceDays = defaultGraceDays
	}
	return r
}

func (s *Server) saveLifecycleRules(r LifecycleRules) error {
	b, _ := json.Marshal(r)
	return s.Settings.Set(settings.KeyLifecycleRules, string(b), false)
}

func (s *Server) lifecycleStatus() lifecycleStatus {
	var st lifecycleStatus
	if v, _ := s.Settings.Get(settings.KeyLifecycleStatus); v != "" {
		_ = json.Unmarshal([]byte(v), &st)
	}
	return st
}

func (s *Server) saveLifecycleStatus(st lifecycleStatus) {
	b, _ := json.Marshal(st)
	_ = s.Settings.Set(settings.KeyLifecycleStatus, string(b), false)
}

// watchDataFresh reports whether what's been watched was read from every
// media server recently enough to act on.
func (s *Server) watchDataFresh() bool {
	last, _ := time.Parse(time.RFC3339, s.watchedStatus().LastFullSync)
	return time.Since(last) <= watchedFreshFor
}

type lifecycleFlag struct {
	Kind     string
	ID       int64
	SeriesID int64
	Title    string
	Reason   string
	DueAt    time.Time
}

func (s *Server) lifecycleFlags() (map[string]lifecycleFlag, error) {
	rows, err := s.db.Query(`SELECT kind, item_id, series_id, title, reason, due_at FROM lifecycle_flags`)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: list flags: %w", err)
	}
	defer rows.Close()
	out := map[string]lifecycleFlag{}
	for rows.Next() {
		var f lifecycleFlag
		var due string
		if err := rows.Scan(&f.Kind, &f.ID, &f.SeriesID, &f.Title, &f.Reason, &due); err != nil {
			return nil, fmt.Errorf("lifecycle: read flag: %w", err)
		}
		f.DueAt, _ = time.Parse(time.RFC3339, due)
		out[cleanupKey(f.Kind, f.ID)] = f
	}
	return out, rows.Err()
}

type lifecycleKeep struct {
	Kind  string `json:"kind"` // movie or series
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

func (s *Server) lifecycleKeeps() ([]lifecycleKeep, error) {
	rows, err := s.db.Query(`SELECT kind, title_id, title FROM lifecycle_keeps ORDER BY title COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: list keeps: %w", err)
	}
	defer rows.Close()
	out := []lifecycleKeep{}
	for rows.Next() {
		var k lifecycleKeep
		if err := rows.Scan(&k.Kind, &k.ID, &k.Title); err != nil {
			return nil, fmt.Errorf("lifecycle: read keep: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// isKept reports whether a candidate is exempt: the movie itself, or the
// show an episode belongs to.
func (s *Server) lifecycleKept(it libraryCleanupItem) bool {
	kind, id := "movie", it.ID
	if it.Kind == "episode" {
		kind, id = "series", it.SeriesID
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM lifecycle_keeps WHERE kind = ? AND title_id = ?`, kind, id).Scan(&n); err != nil {
		return true // when in doubt, keep it
	}
	return n > 0
}

// lifecycleCandidates is what the rules take now, without the kept titles.
func (s *Server) lifecycleCandidates(rules LifecycleRules, now time.Time) ([]libraryCleanupItem, error) {
	items, err := s.libraryCleanupCandidates(rules.cleanup(), now)
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for _, it := range items {
		if !s.lifecycleKept(it) {
			out = append(out, it)
		}
	}
	return out, nil
}

type lifecycleResult struct {
	Flagged int                  `json:"flagged"` // titles on the shelf after the run
	Removed []libraryCleanupItem `json:"removed"`
	Failed  []libraryCleanupItem `json:"failed"`
}

// lifecycleRun puts new matches on the shelf, takes off what no longer
// matches and removes what has waited out the grace period. It only runs
// while "Act for real" is on, and only on fresh watch data.
func (s *Server) lifecycleRun(now time.Time) (lifecycleResult, error) {
	res := lifecycleResult{Removed: []libraryCleanupItem{}, Failed: []libraryCleanupItem{}}
	rules := s.lifecycleRules()
	if !rules.ActForReal || !rules.anyRule() {
		return res, errors.New("lifecycle: act for real is off")
	}
	if !s.watchDataFresh() {
		return res, errors.New("what's been watched hasn't been read from all your media servers in the last two days, so nothing was removed. Press Read now first, and check every server answers")
	}
	watchedMu.Lock()
	defer watchedMu.Unlock()
	items, err := s.lifecycleCandidates(rules, now)
	if err != nil {
		return res, err
	}
	flags, err := s.lifecycleFlags()
	if err != nil {
		return res, err
	}
	due := now.Add(time.Duration(rules.GraceDays) * 24 * time.Hour).UTC().Format(time.RFC3339)
	stamp := now.UTC().Format(time.RFC3339)
	tx, err := s.db.Begin()
	if err != nil {
		return res, fmt.Errorf("lifecycle: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // a no-op once committed
	current := map[string]bool{}
	for _, it := range items {
		k := cleanupKey(it.Kind, it.ID)
		current[k] = true
		if _, ok := flags[k]; ok {
			_, err = tx.Exec(`UPDATE lifecycle_flags SET title = ?, reason = ?, series_id = ? WHERE kind = ? AND item_id = ?`, it.Title, it.Reason, it.SeriesID, it.Kind, it.ID)
		} else {
			_, err = tx.Exec(`INSERT INTO lifecycle_flags (kind, item_id, series_id, title, reason, flagged_at, due_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, it.Kind, it.ID, it.SeriesID, it.Title, it.Reason, stamp, due)
		}
		if err != nil {
			return res, fmt.Errorf("lifecycle: save flag: %w", err)
		}
	}
	for k, f := range flags {
		if !current[k] {
			if _, err := tx.Exec(`DELETE FROM lifecycle_flags WHERE kind = ? AND item_id = ?`, f.Kind, f.ID); err != nil {
				return res, fmt.Errorf("lifecycle: drop flag: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("lifecycle: commit: %w", err)
	}

	// Only titles that were already on the shelf can be due: a title flagged
	// by this very run has the whole grace period ahead of it.
	var ready []libraryCleanupItem
	for _, it := range items {
		if f, ok := flags[cleanupKey(it.Kind, it.ID)]; ok && !f.DueAt.After(now) {
			ready = append(ready, it)
		}
	}
	if len(ready) > maxCleanupPerRun {
		ready = ready[:maxCleanupPerRun]
	}
	for _, it := range ready {
		if s.lifecycleKept(it) { // kept while this run was going
			continue
		}
		if err := s.cleanLibraryItem(it); err != nil {
			slog.Warn("lifecycle: couldn't remove", "title", it.Title, "err", err)
			res.Failed = append(res.Failed, it)
			continue
		}
		_, _ = s.db.Exec(`DELETE FROM lifecycle_flags WHERE kind = ? AND item_id = ?`, it.Kind, it.ID)
		res.Removed = append(res.Removed, it)
		_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Library lifecycle removed %s (%s)", it.Title, it.Reason))
	}
	res.Flagged = len(items) - len(res.Removed)
	st := s.lifecycleStatus()
	st.LastRun = stamp
	st.LastRemoved = len(res.Removed)
	s.saveLifecycleStatus(st)
	if len(res.Removed) > 0 {
		slog.Info("lifecycle: removed titles after the grace period", "removed", len(res.Removed))
	}
	return res, nil
}

// lifecycleTick is called every hour from the watched job and runs the rules
// once a day.
func (s *Server) lifecycleTick(_ context.Context) {
	if !s.lifecycleRules().ActForReal {
		return
	}
	last, _ := time.Parse(time.RFC3339, s.lifecycleStatus().LastRun)
	if time.Since(last) < lifecycleEvery {
		return
	}
	if _, err := s.lifecycleRun(time.Now()); err != nil {
		slog.Info("lifecycle: run failed", "err", err)
	}
}

func (s *Server) clearLifecycleFlags() {
	_, _ = s.db.Exec(`DELETE FROM lifecycle_flags`)
}

// ---- API

type lifecyclePayload struct {
	Rules       LifecycleRules  `json:"rules"`
	Status      lifecycleStatus `json:"status"`
	DryRunDone  bool            `json:"dryRunDone"` // a dry run was made with these rules
	WatchSync   bool            `json:"watchSync"`  // what's been watched is being read
	WatchFresh  bool            `json:"watchFresh"` // ...from every server, recently
	OnShelf     int             `json:"onShelf"`
	Keeps       []lifecycleKeep `json:"keeps"`
	TitlesLimit int             `json:"limit"` // most titles removed in one run
}

func (s *Server) lifecyclePayload() (lifecyclePayload, error) {
	rules, st := s.lifecycleRules(), s.lifecycleStatus()
	keeps, err := s.lifecycleKeeps()
	if err != nil {
		return lifecyclePayload{}, err
	}
	var onShelf int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM lifecycle_flags`).Scan(&onShelf); err != nil {
		return lifecyclePayload{}, fmt.Errorf("lifecycle: count flags: %w", err)
	}
	return lifecyclePayload{
		Rules: rules, Status: st, DryRunDone: st.DryRunKey != "" && st.DryRunKey == rules.key(),
		WatchSync: s.watchedSyncOn(), WatchFresh: s.watchDataFresh(), OnShelf: onShelf, Keeps: keeps, TitlesLimit: maxCleanupPerRun,
	}, nil
}

// GET /api/lifecycle
func (s *Server) handleGetLifecycle(w http.ResponseWriter, r *http.Request) {
	p, err := s.lifecyclePayload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// PUT /api/lifecycle with the rules. "Act for real" can only be switched on
// when a dry run was made with these very rules; saving changed rules with it
// off empties the shelf, so the grace period starts over.
func (s *Server) handlePutLifecycle(w http.ResponseWriter, r *http.Request) {
	if !s.lifecycleEnabled() {
		writeError(w, http.StatusConflict, "Library lifecycle is switched off. Switch it on in Settings > Media types.")
		return
	}
	var req LifecycleRules
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	for _, d := range []int{req.MoviesWatchedDays, req.MoviesUnwatchedDays, req.EpisodesWatchedDays} {
		if d < 0 || d > 3650 {
			writeError(w, http.StatusBadRequest, "Days must be between 1 and 3650, or 0 to switch a rule off.")
			return
		}
	}
	if req.GraceDays < 1 || req.GraceDays > 365 {
		writeError(w, http.StatusBadRequest, "The days on the shelf must be between 1 and 365.")
		return
	}
	tags := []string{}
	for _, t := range req.KeepTags {
		if t = strings.TrimSpace(t); t != "" && len(t) <= 40 {
			tags = append(tags, t)
		}
	}
	req.KeepTags = tags
	old, st := s.lifecycleRules(), s.lifecycleStatus()
	if req.ActForReal {
		switch {
		case !req.anyRule():
			writeError(w, http.StatusBadRequest, "Set at least one rule before acting for real.")
			return
		case st.DryRunKey != req.key():
			writeError(w, http.StatusBadRequest, "Run a dry run with these rules first.")
			return
		}
	}
	if err := s.saveLifecycleRules(req); err != nil {
		writeError(w, http.StatusInternalServerError, "The lifecycle rules couldn't be saved.")
		return
	}
	if req.anyRule() && !s.watchedSyncOn() {
		_ = setBool(s.Settings, settings.KeyWatchedSync, true) // the rules need to know what was watched
	}
	switch {
	case !req.ActForReal && (old.ActForReal || req.key() != old.key()):
		s.clearLifecycleFlags()
	case req.ActForReal && !old.ActForReal:
		// Put the first titles on the shelf now. Nothing can be due yet.
		if _, err := s.lifecycleRun(time.Now()); err != nil {
			slog.Info("lifecycle: first run failed", "err", err)
		}
	}
	p, err := s.lifecyclePayload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// POST /api/lifecycle/dry-run: what the saved rules would put on the shelf
// now. Nothing is changed except the note that this dry run was made.
func (s *Server) handleLifecycleDryRun(w http.ResponseWriter, r *http.Request) {
	if !s.lifecycleEnabled() {
		writeError(w, http.StatusConflict, "Library lifecycle is switched off. Switch it on in Settings > Media types.")
		return
	}
	rules := s.lifecycleRules()
	if !rules.anyRule() {
		writeError(w, http.StatusBadRequest, "Set at least one rule first.")
		return
	}
	now := time.Now()
	items, err := s.lifecycleCandidates(rules, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []libraryCleanupItem{}
	}
	st := s.lifecycleStatus()
	st.DryRunKey = rules.key()
	st.LastDryRun = now.UTC().Format(time.RFC3339)
	s.saveLifecycleStatus(st)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": maxCleanupPerRun, "graceDays": rules.GraceDays, "watchFresh": s.watchDataFresh()})
}

type lifecycleShelfItem struct {
	Kind      string `json:"kind"` // movie or episode
	ID        int64  `json:"id"`
	SeriesID  int64  `json:"seriesId,omitempty"`
	TMDBID    int    `json:"tmdbId,omitempty"` // movies
	Title     string `json:"title"`
	Reason    string `json:"reason"`
	PosterURL string `json:"posterUrl,omitempty"`
	DueAt     string `json:"dueAt"`
	DaysLeft  int    `json:"daysLeft"`
}

// GET /api/lifecycle/shelf: the titles on the Leaving soon shelf, the ones
// due first at the top. Empty while the module or "Act for real" is off.
func (s *Server) handleLifecycleShelf(w http.ResponseWriter, r *http.Request) {
	out := []lifecycleShelfItem{}
	if !s.lifecycleEnabled() || !s.lifecycleRules().ActForReal {
		writeJSON(w, http.StatusOK, map[string]any{"items": out})
		return
	}
	flags, err := s.lifecycleFlags()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	for _, f := range flags {
		it := lifecycleShelfItem{Kind: f.Kind, ID: f.ID, SeriesID: f.SeriesID, Title: f.Title, Reason: f.Reason, DueAt: f.DueAt.UTC().Format(time.RFC3339)}
		left := f.DueAt.Sub(now)
		if left > 0 {
			it.DaysLeft = int((left + 24*time.Hour - 1) / (24 * time.Hour))
		}
		if f.Kind == "movie" {
			m, err := s.MovieRepo.Get(f.ID)
			if err != nil {
				continue
			}
			it.TMDBID, it.PosterURL = m.TMDBID, metadata.PosterURL(m.PosterPath)
		} else {
			sh, err := s.MovieRepo.GetSeries(f.SeriesID)
			if err != nil {
				continue
			}
			it.PosterURL = metadata.PosterURL(sh.PosterPath)
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DueAt != out[j].DueAt {
			return out[i].DueAt < out[j].DueAt
		}
		return out[i].Title < out[j].Title
	})
	if len(out) > maxLifecycleShelf {
		out = out[:maxLifecycleShelf]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// POST /api/lifecycle/keep {"kind": "movie" | "episode", "id": 3}: the title
// on the shelf stays. For an episode that is its whole show.
func (s *Server) handleLifecycleKeep(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
		ID   int64  `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Kind != "movie" && req.Kind != "episode") || req.ID <= 0 {
		writeError(w, http.StatusBadRequest, "Choose a movie or an episode from the shelf.")
		return
	}
	flags, err := s.lifecycleFlags()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	f, ok := flags[cleanupKey(req.Kind, req.ID)]
	if !ok {
		writeError(w, http.StatusNotFound, "That title isn't on the Leaving soon shelf.")
		return
	}
	kind, id, title := "movie", f.ID, f.Title
	if f.Kind == "episode" {
		sh, err := s.MovieRepo.GetSeries(f.SeriesID)
		if err != nil {
			writeError(w, http.StatusNotFound, "That title isn't on the Leaving soon shelf.")
			return
		}
		kind, id, title = "series", sh.ID, sh.Title
	}
	if err := s.keepTitle(kind, id, title); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kept": true})
}

// keepTitle exempts a movie or show and takes its titles off the shelf.
func (s *Server) keepTitle(kind string, id int64, title string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("lifecycle: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // a no-op once committed
	if _, err := tx.Exec(`INSERT INTO lifecycle_keeps (kind, title_id, title, kept_at) VALUES (?, ?, ?, ?) ON CONFLICT(kind, title_id) DO UPDATE SET title = excluded.title`, kind, id, title, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("lifecycle: keep: %w", err)
	}
	if kind == "movie" {
		_, err = tx.Exec(`DELETE FROM lifecycle_flags WHERE kind = 'movie' AND item_id = ?`, id)
	} else {
		_, err = tx.Exec(`DELETE FROM lifecycle_flags WHERE kind = 'episode' AND series_id = ?`, id)
	}
	if err != nil {
		return fmt.Errorf("lifecycle: clear flags: %w", err)
	}
	return tx.Commit()
}

// POST /api/lifecycle/unkeep {"kind": "movie" | "series", "id": 3}: the rules
// may take the title again.
func (s *Server) handleLifecycleUnkeep(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
		ID   int64  `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Kind != "movie" && req.Kind != "series") || req.ID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if _, err := s.db.Exec(`DELETE FROM lifecycle_keeps WHERE kind = ? AND title_id = ?`, req.Kind, req.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	p, err := s.lifecyclePayload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}
