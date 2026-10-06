package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/settings"
)

// Watched status and cleanup rules (Settings > Connections > Media servers).
// Both are off until an administrator switches them on.
//
// With "Read what's been watched" on, Mediarium asks Plex, Jellyfin and Emby
// every six hours what has been played, and shows it in the library. With
// cleanup rules on, once a day it deletes the files of titles that match
// them (watched a while ago, or never watched long after being added), stops
// looking for them again, and writes each one to Activity. Titles with a
// "keep" tag are never touched, and the recycle bin applies when it is on.

const (
	watchedSyncEvery  = 6 * time.Hour
	cleanupRunEvery   = 24 * time.Hour
	watchedFreshFor   = 48 * time.Hour // cleanup only runs on recent watch data
	maxCleanupPerRun  = 50             // a safety net against a rule set too wide
	watchedJobTick    = time.Hour
	watchedSyncBudget = 3 * time.Minute
)

// CleanupRules are the cleanup settings. Days of 0 switch a rule off.
type CleanupRules struct {
	Enabled             bool     `json:"enabled"`
	MoviesWatchedDays   int      `json:"moviesWatchedDays"`   // a movie watched this many days ago
	MoviesUnwatchedDays int      `json:"moviesUnwatchedDays"` // a movie never watched, added this many days ago
	EpisodesWatchedDays int      `json:"episodesWatchedDays"` // an episode watched this many days ago
	KeepTags            []string `json:"keepTags"`            // titles with one of these tags are never removed
}

type watchedStatus struct {
	LastSync string `json:"lastSync,omitempty"` // RFC 3339
	// LastFullSync is the last read in which every server answered. Cleanup
	// only trusts that: a server that failed would make its plays look like
	// nobody watched.
	LastFullSync string `json:"lastFullSync,omitempty"`
	LastError    string `json:"lastError,omitempty"` // the last sync's problem, in plain words
	Servers      int    `json:"servers"`             // how many servers were read
	Movies       int    `json:"movies"`              // watched movies found in the library
	Episodes     int    `json:"episodes"`            // watched episodes found in the library
	LastClean    string `json:"lastCleanup,omitempty"`
}

var watchedMu sync.Mutex // one sync or cleanup at a time

func (s *Server) watchedSyncOn() bool { return s.settingOn(settings.KeyWatchedSync, false) }

func (s *Server) watchedStatus() watchedStatus {
	var st watchedStatus
	if v, _ := s.Settings.Get(settings.KeyWatchedStatus); v != "" {
		_ = json.Unmarshal([]byte(v), &st)
	}
	return st
}

func (s *Server) saveWatchedStatus(st watchedStatus) {
	b, _ := json.Marshal(st)
	_ = s.Settings.Set(settings.KeyWatchedStatus, string(b), false)
}

func (s *Server) cleanupRules() CleanupRules {
	var r CleanupRules
	if v, _ := s.Settings.Get(settings.KeyCleanupRules); v != "" {
		_ = json.Unmarshal([]byte(v), &r)
	}
	if r.KeepTags == nil {
		r.KeepTags = []string{}
	}
	return r
}

// syncWatched reads every Plex, Jellyfin and Emby server and stores what
// has been watched in the library.
func (s *Server) syncWatched(ctx context.Context) (watchedStatus, error) {
	watchedMu.Lock()
	defer watchedMu.Unlock()
	st := s.watchedStatus()
	servers, err := s.MediaServers.List()
	if err != nil {
		return st, err
	}
	movies := map[int]mediaservers.Play{}
	episodes := map[mediaservers.EpisodeKey]mediaservers.Play{}
	read := 0
	var problems []string
	for _, srv := range servers {
		if !srv.Enabled || (srv.Kind != mediaservers.KindPlex && srv.Kind != mediaservers.KindJellyfin && srv.Kind != mediaservers.KindEmby) {
			continue
		}
		state, err := s.mediaClient.Watched(ctx, srv)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", srv.Name, err))
			continue
		}
		read++
		for id, p := range state.Movies {
			movies[id] = mergePlay(movies[id], p)
		}
		for k, p := range state.Episodes {
			episodes[k] = mergePlay(episodes[k], p)
		}
	}
	if read == 0 {
		st.LastError = strings.Join(problems, "; ")
		if st.LastError == "" {
			st.LastError = "No Plex, Jellyfin or Emby server is switched on under Connections > Media servers."
		}
		s.saveWatchedStatus(st)
		return st, errors.New(st.LastError)
	}
	var rows []library.Watched
	all, err := s.MovieRepo.List()
	if err != nil {
		return st, err
	}
	nMovies := 0
	for _, m := range all {
		if p, ok := movies[m.TMDBID]; ok {
			rows = append(rows, library.Watched{Kind: "movie", TitleID: m.ID, Plays: p.Plays, LastPlayed: p.LastPlayed})
			nMovies++
		}
	}
	shows, err := s.MovieRepo.ListSeries()
	if err != nil {
		return st, err
	}
	byTMDB := map[int]int64{}
	for _, sh := range shows {
		byTMDB[sh.TMDBID] = sh.ID
	}
	nEpisodes := 0
	for k, p := range episodes {
		if id, ok := byTMDB[k.ShowTMDB]; ok {
			rows = append(rows, library.Watched{Kind: "episode", TitleID: id, Season: k.Season, Episode: k.Episode, Plays: p.Plays, LastPlayed: p.LastPlayed})
			nEpisodes++
		}
	}
	if err := s.MovieRepo.ReplaceWatched(rows); err != nil {
		return st, err
	}
	st.LastSync = time.Now().UTC().Format(time.RFC3339)
	if len(problems) == 0 {
		st.LastFullSync = st.LastSync
	} else {
		st.LastFullSync = "" // the list is missing what the failed server knows
	}
	st.LastError = strings.Join(problems, "; ")
	st.Servers, st.Movies, st.Episodes = read, nMovies, nEpisodes
	s.saveWatchedStatus(st)
	slog.Info("watched: read from media servers", "servers", read, "movies", nMovies, "episodes", nEpisodes)
	return st, nil
}

func mergePlay(a, b mediaservers.Play) mediaservers.Play {
	a.Plays += b.Plays
	if b.LastPlayed.After(a.LastPlayed) {
		a.LastPlayed = b.LastPlayed
	}
	return a
}

// watchedJob runs every hour: a sync when one is due, then the cleanup when
// that is due and the watch data is fresh.
func (s *Server) watchedJob(ctx context.Context) {
	if !s.watchedSyncOn() {
		return
	}
	st := s.watchedStatus()
	last, _ := time.Parse(time.RFC3339, st.LastSync)
	if time.Since(last) >= watchedSyncEvery {
		sctx, cancel := context.WithTimeout(ctx, watchedSyncBudget)
		var err error
		st, err = s.syncWatched(sctx)
		cancel()
		if err != nil {
			slog.Info("watched: sync failed", "err", err)
			return
		}
	}
	rules := s.cleanupRules()
	lastClean, _ := time.Parse(time.RFC3339, st.LastClean)
	if rules.Enabled && time.Since(lastClean) >= cleanupRunEvery {
		if _, err := s.runLibraryCleanup(rules, false); err != nil {
			slog.Info("cleanup: run failed", "err", err)
		}
	}
}

// libraryCleanupItem is one title the rules would remove (or did).
type libraryCleanupItem struct {
	Kind     string `json:"kind"` // "movie" or "episode"
	ID       int64  `json:"id"`   // movie id, or the episode's id
	SeriesID int64  `json:"seriesId,omitempty"`
	Title    string `json:"title"`
	Reason   string `json:"reason"`
	Path     string `json:"-"`

	episodeIDs []int64   // every episode in the file (a multi-episode file)
	since      time.Time // last watched, or when it arrived for the unwatched rule
}

func hasTag(tags []string, keep []string) bool {
	for _, t := range tags {
		for _, k := range keep {
			if strings.EqualFold(strings.TrimSpace(t), strings.TrimSpace(k)) && k != "" {
				return true
			}
		}
	}
	return false
}

func ago(t time.Time, now time.Time) string {
	d := int(now.Sub(t).Hours() / 24)
	if d == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", d)
}

// fileTime is a file's modification time, or zero.
func fileTime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func latest(ts ...time.Time) time.Time {
	var out time.Time
	for _, t := range ts {
		if t.After(out) {
			out = t
		}
	}
	return out
}

// movieDownloadTimes maps each movie to its last finished download (as far
// back as the history goes).
func (s *Server) movieDownloadTimes() (map[int64]time.Time, error) {
	rows, err := s.db.Query(`SELECT movie_id, MAX(completed_at) FROM download_queue WHERE status = 'completed' AND movie_id IS NOT NULL AND completed_at IS NOT NULL GROUP BY movie_id`)
	if err != nil {
		return nil, fmt.Errorf("movie download times: %w", err)
	}
	defer rows.Close()
	out := map[int64]time.Time{}
	for rows.Next() {
		var id int64
		var at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("scan movie download time: %w", err)
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, at); err == nil {
				out[id] = t
				break
			}
		}
	}
	return out, rows.Err()
}

// libraryCleanupCandidates lists what the rules remove now, oldest first.
//
// A title watched a while ago but downloaded again since (its last download,
// or its file, is newer than the last play) is kept: someone wants to watch
// it again. A title never watched counts from when it arrived (added,
// downloaded or its file written, whichever is latest), not from when it was
// first wanted.
func (s *Server) libraryCleanupCandidates(rules CleanupRules, now time.Time) ([]libraryCleanupItem, error) {
	watched, err := s.MovieRepo.ListWatched()
	if err != nil {
		return nil, err
	}
	movieTags, err := s.MovieRepo.AllTitleTags(library.TagMovie)
	if err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	seriesTags, err := s.MovieRepo.AllTitleTags(library.TagSeries)
	if err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	movieWatch := map[int64]library.Watched{}
	epWatch := map[[3]int64]library.Watched{}
	for _, w := range watched {
		if w.Kind == "movie" {
			movieWatch[w.TitleID] = w
		} else {
			epWatch[[3]int64{w.TitleID, int64(w.Season), int64(w.Episode)}] = w
		}
	}
	var out []libraryCleanupItem
	if s.moviesEnabled() && (rules.MoviesWatchedDays > 0 || rules.MoviesUnwatchedDays > 0) {
		movies, err := s.MovieRepo.List()
		if err != nil {
			return nil, err
		}
		added, err := s.MovieRepo.MovieAddedDates()
		if err != nil {
			return nil, err
		}
		downloaded, err := s.movieDownloadTimes()
		if err != nil {
			return nil, err
		}
		for _, m := range movies {
			if m.Status != library.StatusDownloaded || m.FilePath == "" || hasTag(movieTags[m.ID], rules.KeepTags) {
				continue
			}
			fresh := latest(downloaded[m.ID], fileTime(m.FilePath)) // when this copy arrived
			w, seen := movieWatch[m.ID]
			switch {
			case seen && rules.MoviesWatchedDays > 0 && !w.LastPlayed.IsZero() && fresh.Before(w.LastPlayed) &&
				now.Sub(w.LastPlayed) >= time.Duration(rules.MoviesWatchedDays)*24*time.Hour:
				out = append(out, libraryCleanupItem{Kind: "movie", ID: m.ID, Title: movieLabel(m), Reason: "watched " + ago(w.LastPlayed, now) + " ago", Path: m.FilePath, since: w.LastPlayed})
			case !seen && rules.MoviesUnwatchedDays > 0:
				arrived := latest(added[m.ID], fresh)
				if !arrived.IsZero() && now.Sub(arrived) >= time.Duration(rules.MoviesUnwatchedDays)*24*time.Hour {
					out = append(out, libraryCleanupItem{Kind: "movie", ID: m.ID, Title: movieLabel(m), Reason: "not watched in the " + ago(arrived, now) + " since it arrived", Path: m.FilePath, since: arrived})
				}
			}
		}
	}
	if s.tvEnabled() && rules.EpisodesWatchedDays > 0 {
		shows, err := s.MovieRepo.ListSeries()
		if err != nil {
			return nil, err
		}
		for _, sh := range shows {
			if hasTag(seriesTags[sh.ID], rules.KeepTags) {
				continue
			}
			eps, err := s.MovieRepo.ListEpisodes(sh.ID)
			if err != nil {
				return nil, err
			}
			// A file can hold several episodes: it goes only when every one
			// of them qualifies, and then all of them are cleared.
			var files []string
			byFile := map[string][]library.Episode{}
			for _, e := range eps {
				if e.Status != library.StatusDownloaded || e.FilePath == "" {
					continue
				}
				if _, ok := byFile[e.FilePath]; !ok {
					files = append(files, e.FilePath)
				}
				byFile[e.FilePath] = append(byFile[e.FilePath], e)
			}
			for _, f := range files {
				group := byFile[f]
				fresh := fileTime(f)
				var lastPlayed time.Time
				ok := true
				for _, e := range group {
					w, seen := epWatch[[3]int64{sh.ID, int64(e.Season), int64(e.Episode)}]
					if !seen || w.LastPlayed.IsZero() || !fresh.Before(w.LastPlayed) ||
						now.Sub(w.LastPlayed) < time.Duration(rules.EpisodesWatchedDays)*24*time.Hour {
						ok = false
						break
					}
					lastPlayed = latest(lastPlayed, w.LastPlayed)
				}
				if !ok {
					continue
				}
				first, last := group[0], group[len(group)-1]
				title := fmt.Sprintf("%s S%02dE%02d", sh.Title, first.Season, first.Episode)
				if len(group) > 1 {
					title += fmt.Sprintf("-E%02d", last.Episode)
				}
				ids := make([]int64, 0, len(group))
				for _, e := range group {
					ids = append(ids, e.ID)
				}
				out = append(out, libraryCleanupItem{Kind: "episode", ID: first.ID, SeriesID: sh.ID, Title: title, Reason: "watched " + ago(lastPlayed, now) + " ago", Path: f, episodeIDs: ids, since: lastPlayed})
			}
		}
	}
	// Oldest first, so the per-run limit takes what has waited longest.
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].since.Equal(out[j].since) {
			return out[i].since.Before(out[j].since)
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

type libraryCleanupResult struct {
	Removed []libraryCleanupItem `json:"removed"`
	Failed  []libraryCleanupItem `json:"failed"`
	More    int                  `json:"more"` // left for the next run (over the per-run limit)
}

// runLibraryCleanup removes what the rules match: the files go (to the recycle bin
// when it is on) and the title is no longer looked for. manual runs (Run now)
// still need the watch data to be fresh.
func (s *Server) runLibraryCleanup(rules CleanupRules, manual bool) (libraryCleanupResult, error) {
	var res libraryCleanupResult
	st := s.watchedStatus()
	last, _ := time.Parse(time.RFC3339, st.LastFullSync)
	if time.Since(last) > watchedFreshFor {
		return res, errors.New("what's been watched hasn't been read from all your media servers in the last two days, so nothing was removed. Press Read now first, and check every server answers")
	}
	watchedMu.Lock()
	defer watchedMu.Unlock()
	items, err := s.libraryCleanupCandidates(rules, time.Now())
	if err != nil {
		return res, err
	}
	if len(items) > maxCleanupPerRun {
		res.More = len(items) - maxCleanupPerRun
		items = items[:maxCleanupPerRun]
	}
	for _, it := range items {
		if err := s.cleanLibraryItem(it); err != nil {
			slog.Warn("cleanup: couldn't remove", "title", it.Title, "err", err)
			res.Failed = append(res.Failed, it)
			continue
		}
		res.Removed = append(res.Removed, it)
		_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Cleanup removed %s (%s)", it.Title, it.Reason))
	}
	st = s.watchedStatus()
	st.LastClean = time.Now().UTC().Format(time.RFC3339)
	s.saveWatchedStatus(st)
	if len(res.Removed) > 0 {
		slog.Info("cleanup: removed watched titles", "removed", len(res.Removed), "manual", manual)
	}
	return res, nil
}

func (s *Server) cleanLibraryItem(it libraryCleanupItem) error {
	switch it.Kind {
	case "movie":
		m, err := s.MovieRepo.Get(it.ID)
		if err != nil {
			return err
		}
		if _, err := s.removeMovieFiles(m); err != nil {
			return err
		}
		if err := s.MovieRepo.ClearMovieFile(m.ID); err != nil {
			return err
		}
		return s.MovieRepo.SetMonitored(m.ID, false)
	case "episode":
		sh, err := s.MovieRepo.GetSeries(it.SeriesID)
		if err != nil {
			return err
		}
		// Only the episode's own file and the files named after it: never
		// the show's folder, even when it is the last tracked file there.
		if _, _, err := s.discardFileWithSidecars(s.tvRoot(), it.Path, seriesLabel(sh)); err != nil {
			return err
		}
		ids := it.episodeIDs
		if len(ids) == 0 {
			ids = []int64{it.ID}
		}
		for _, id := range ids {
			if err := s.MovieRepo.ClearEpisodeFile(id); err != nil {
				return err
			}
			if err := s.MovieRepo.SetEpisodeMonitored(id, false); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unknown kind %q", it.Kind)
}

// ---- API

type watchedSettings struct {
	Sync    bool          `json:"sync"`
	Status  watchedStatus `json:"status"`
	Cleanup CleanupRules  `json:"cleanup"`
}

func (s *Server) watchedSettingsPayload() watchedSettings {
	return watchedSettings{Sync: s.watchedSyncOn(), Status: s.watchedStatus(), Cleanup: s.cleanupRules()}
}

// GET /api/watched/settings
func (s *Server) handleGetWatchedSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.watchedSettingsPayload())
}

// PUT /api/watched/settings {"sync": true, "cleanup": {...}}
func (s *Server) handlePutWatchedSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sync    *bool         `json:"sync"`
		Cleanup *CleanupRules `json:"cleanup"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Cleanup != nil {
		c := *req.Cleanup
		for _, d := range []int{c.MoviesWatchedDays, c.MoviesUnwatchedDays, c.EpisodesWatchedDays} {
			if d < 0 || d > 3650 {
				writeError(w, http.StatusBadRequest, "Days must be between 1 and 3650, or 0 to switch a rule off.")
				return
			}
		}
		if c.Enabled && c.MoviesWatchedDays == 0 && c.MoviesUnwatchedDays == 0 && c.EpisodesWatchedDays == 0 {
			writeError(w, http.StatusBadRequest, "Set at least one rule before switching cleanup on.")
			return
		}
		tags := []string{}
		for _, t := range c.KeepTags {
			if t = strings.TrimSpace(t); t != "" && len(t) <= 40 {
				tags = append(tags, t)
			}
		}
		c.KeepTags = tags
		b, _ := json.Marshal(c)
		if err := s.Settings.Set(settings.KeyCleanupRules, string(b), false); err != nil {
			writeError(w, http.StatusInternalServerError, "The cleanup rules couldn't be saved.")
			return
		}
		if c.Enabled && !s.watchedSyncOn() {
			_ = setBool(s.Settings, settings.KeyWatchedSync, true) // cleanup needs the watch data
		}
	}
	if req.Sync != nil {
		if err := setBool(s.Settings, settings.KeyWatchedSync, *req.Sync); err != nil {
			writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
			return
		}
		if !*req.Sync {
			// No watch data, no cleanup.
			c := s.cleanupRules()
			if c.Enabled {
				c.Enabled = false
				b, _ := json.Marshal(c)
				_ = s.Settings.Set(settings.KeyCleanupRules, string(b), false)
			}
		}
	}
	writeJSON(w, http.StatusOK, s.watchedSettingsPayload())
}

// POST /api/watched/sync: read what's been watched now.
func (s *Server) handleSyncWatched(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), watchedSyncBudget)
	defer cancel()
	st, err := s.syncWatched(ctx)
	if err != nil {
		writeUpstreamError(w, "read what's been watched from your media servers", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// GET /api/watched/cleanup/preview: what the saved rules (or the rules sent
// as ?rules=JSON) would remove now. Nothing is changed.
func (s *Server) handleLibraryCleanupPreview(w http.ResponseWriter, r *http.Request) {
	rules := s.cleanupRules()
	if raw := r.URL.Query().Get("rules"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &rules); err != nil {
			writeError(w, http.StatusBadRequest, "Those rules couldn't be read.")
			return
		}
	}
	items, err := s.libraryCleanupCandidates(rules, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []libraryCleanupItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": maxCleanupPerRun})
}

// POST /api/watched/cleanup/run: apply the saved rules once, now.
func (s *Server) handleLibraryCleanupRun(w http.ResponseWriter, r *http.Request) {
	rules := s.cleanupRules()
	if rules.MoviesWatchedDays == 0 && rules.MoviesUnwatchedDays == 0 && rules.EpisodesWatchedDays == 0 {
		writeError(w, http.StatusBadRequest, "Set at least one rule first.")
		return
	}
	res, err := s.runLibraryCleanup(rules, true)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if res.Removed == nil {
		res.Removed = []libraryCleanupItem{}
	}
	if res.Failed == nil {
		res.Failed = []libraryCleanupItem{}
	}
	writeJSON(w, http.StatusOK, res)
}

type watchedTitle struct {
	Plays      int    `json:"plays"`
	LastPlayed string `json:"lastPlayed,omitempty"`
	Episodes   int    `json:"episodes,omitempty"` // shows: how many episodes were watched
}

// GET /api/watched: what's been watched, by movie and by show, for the
// library pages. Empty while reading it is off.
func (s *Server) handleWatched(w http.ResponseWriter, r *http.Request) {
	out := map[string]map[int64]watchedTitle{"movies": {}, "series": {}}
	if !s.watchedSyncOn() {
		writeJSON(w, http.StatusOK, out)
		return
	}
	list, err := s.MovieRepo.ListWatched()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, x := range list {
		last := ""
		if !x.LastPlayed.IsZero() {
			last = x.LastPlayed.Format(time.RFC3339)
		}
		if x.Kind == "movie" {
			out["movies"][x.TitleID] = watchedTitle{Plays: x.Plays, LastPlayed: last}
			continue
		}
		cur := out["series"][x.TitleID]
		cur.Episodes++
		cur.Plays += x.Plays
		if last > cur.LastPlayed {
			cur.LastPlayed = last
		}
		out["series"][x.TitleID] = cur
	}
	writeJSON(w, http.StatusOK, out)
}
