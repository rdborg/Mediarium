package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/omdb"
	"github.com/rdborg/mediarium/internal/settings"
)

// Scores from outside TMDB (#169): IMDb, Rotten Tomatoes and Metacritic,
// read from OMDb with the person's own key. OMDb's free key allows 1,000
// lookups a day, so:
//   - scores are kept for a week (outside_scores), also "OMDb doesn't know it";
//   - a title page asks when it opens, within the day's lookups;
//   - Discover cards show the scores that are kept, and the ones they don't
//     have yet are looked up in the background, one every few seconds and at
//     most scoreWarmPerDay a day, so browsing can't use up the key.

const (
	scoresKeep        = 7 * 24 * time.Hour
	scoreLookupsDay   = 950 // what a free key allows, less a little room
	scoreWarmPerDay   = 400 // of those, for Discover cards in the background
	scoreWarmEvery    = 3 * time.Second
	scoreWarmQueueMax = 300
)

// scoreView is what a title page or a card gets.
type scoreView struct {
	IMDb           float64 `json:"imdb,omitempty"`
	IMDbVotes      int     `json:"imdbVotes,omitempty"`
	RottenTomatoes int     `json:"rottenTomatoes,omitempty"`
	Metacritic     int     `json:"metacritic,omitempty"`
}

func (v scoreView) empty() bool { return v == scoreView{} }

type keptScore struct {
	view    scoreView
	found   bool
	fetched time.Time
}

type scoreState struct {
	mu      sync.Mutex
	loaded  bool
	byTitle map[string]keptScore // "movie:603" -> scores
	byIMDb  map[string]keptScore

	day         string // UTC day the counts are for
	used, warm  int
	spentUntil  time.Time // the key said its lookups are used up
	queue       []scoreJob
	queued      map[string]bool
	workerAlive bool

	client    *omdb.Client
	clientKey string
	testBase  string
}

type scoreJob struct {
	kind string
	tmdb int
}

func titleKey(kind string, tmdb int) string { return kind + ":" + strconv.Itoa(tmdb) }

func (s *Server) scores() *scoreState {
	s.scoresOnce.Do(func() {
		s.scoreState = &scoreState{byTitle: map[string]keptScore{}, byIMDb: map[string]keptScore{}, queued: map[string]bool{}}
	})
	return s.scoreState
}

// omdbClient is the client for the saved key, or nil without one.
func (s *Server) omdbClient() *omdb.Client {
	if s.Settings == nil {
		return nil
	}
	key, _ := s.Settings.Get(settings.KeyOMDbAPIKey)
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	st := s.scores()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.client == nil || st.clientKey != key {
		base := omdb.DefaultBaseURL
		if st.testBase != "" {
			base = st.testBase
		}
		st.client, st.clientKey = omdb.NewAt(base, key), key
	}
	return st.client
}

// loadScores reads the kept scores once.
func (s *Server) loadScores() {
	st := s.scores()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.loaded || s.db == nil {
		return
	}
	st.loaded = true
	rows, err := s.db.Query(`SELECT imdb_id, kind, tmdb_id, imdb, imdb_votes, rotten_tomatoes, metacritic, found, fetched_at FROM outside_scores`)
	if err != nil {
		slog.Warn("scores: read the kept scores", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, kind, at string
			tmdb         int
			k            keptScore
		)
		if err := rows.Scan(&id, &kind, &tmdb, &k.view.IMDb, &k.view.IMDbVotes, &k.view.RottenTomatoes, &k.view.Metacritic, &k.found, &at); err != nil {
			slog.Warn("scores: read the kept scores", "err", err)
			return
		}
		k.fetched, _ = time.Parse(time.RFC3339, at)
		st.byIMDb[id] = k
		if tmdb > 0 {
			st.byTitle[titleKey(kind, tmdb)] = k
		}
	}
}

// keptScoresFor is what is kept for a title, for a card; nil when nothing
// (or nothing found) is kept.
func (s *Server) keptScoresFor(kind string, tmdb int) *scoreView {
	s.loadScores()
	st := s.scores()
	st.mu.Lock()
	k, ok := st.byTitle[titleKey(kind, tmdb)]
	st.mu.Unlock()
	if !ok || !k.found || k.view.empty() {
		return nil
	}
	v := k.view
	return &v
}

// takeLookup counts one OMDb lookup against today's allowance; warm is a
// background one, which has its own smaller share.
func (st *scoreState) takeLookup(warm bool, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if day := now.UTC().Format("2006-01-02"); day != st.day {
		st.day, st.used, st.warm = day, 0, 0
	}
	if now.Before(st.spentUntil) || st.used >= scoreLookupsDay || (warm && st.warm >= scoreWarmPerDay) {
		return false
	}
	st.used++
	if warm {
		st.warm++
	}
	return true
}

// lookupScores asks OMDb for one title and keeps the answer.
func (s *Server) lookupScores(ctx context.Context, c *omdb.Client, kind string, tmdb int, imdbID string, warm bool) (keptScore, error) {
	st := s.scores()
	now := time.Now()
	if !st.takeLookup(warm, now) {
		return keptScore{}, omdb.ErrLimit
	}
	got, err := c.Get(ctx, imdbID)
	k := keptScore{fetched: now}
	switch {
	case errors.Is(err, omdb.ErrNotFound):
	case errors.Is(err, omdb.ErrLimit):
		st.mu.Lock()
		st.spentUntil = now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		st.mu.Unlock()
		return keptScore{}, err
	case err != nil:
		return keptScore{}, err
	default:
		k.found = true
		k.view = scoreView{IMDb: got.IMDb, IMDbVotes: got.IMDbVotes, RottenTomatoes: got.RottenTomatoes, Metacritic: got.Metacritic}
	}
	if _, err := s.db.Exec(`INSERT INTO outside_scores (imdb_id, kind, tmdb_id, imdb, imdb_votes, rotten_tomatoes, metacritic, found, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(imdb_id) DO UPDATE SET kind = excluded.kind, tmdb_id = excluded.tmdb_id, imdb = excluded.imdb, imdb_votes = excluded.imdb_votes,
			rotten_tomatoes = excluded.rotten_tomatoes, metacritic = excluded.metacritic, found = excluded.found, fetched_at = excluded.fetched_at`,
		imdbID, kind, tmdb, k.view.IMDb, k.view.IMDbVotes, k.view.RottenTomatoes, k.view.Metacritic, k.found, now.UTC().Format(time.RFC3339)); err != nil {
		slog.Warn("scores: keep the scores", "imdb", imdbID, "err", err)
	}
	st.mu.Lock()
	st.byIMDb[imdbID] = k
	if tmdb > 0 {
		st.byTitle[titleKey(kind, tmdb)] = k
	}
	st.mu.Unlock()
	return k, nil
}

// handleTitleScores answers a title page: GET /api/scores/{kind}/{tmdbId}?imdb=tt...
// It looks the title up when nothing fresh is kept.
func (s *Server) handleTitleScores(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	tmdb, err := strconv.Atoi(r.PathValue("tmdbId"))
	imdbID := strings.TrimSpace(r.URL.Query().Get("imdb"))
	if (kind != "movie" && kind != "tv") || err != nil || tmdb <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a film or show.")
		return
	}
	out := struct {
		Set    bool       `json:"set"` // an OMDb key is saved
		Scores *scoreView `json:"scores"`
	}{}
	c := s.omdbClient()
	out.Set = c != nil
	if c == nil || !omdb.ValidID(imdbID) {
		writeJSON(w, http.StatusOK, out)
		return
	}
	s.loadScores()
	st := s.scores()
	st.mu.Lock()
	k, ok := st.byIMDb[imdbID]
	st.mu.Unlock()
	if !ok || time.Since(k.fetched) > scoresKeep {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		fresh, err := s.lookupScores(ctx, c, kind, tmdb, imdbID, false)
		if err == nil {
			k, ok = fresh, true
		} else if !errors.Is(err, omdb.ErrLimit) {
			slog.Info("scores: OMDb lookup failed", "imdb", imdbID, "err", err)
		}
	}
	if ok && k.found && !k.view.empty() {
		v := k.view
		out.Scores = &v
	}
	writeJSON(w, http.StatusOK, out)
}

// warmScores puts the Discover titles without kept scores in line for a
// background lookup. It does nothing without a key.
func (s *Server) warmScores(items []discoverPayload) {
	if s.omdbClient() == nil {
		return
	}
	s.loadScores()
	st := s.scores()
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, it := range items {
		key := titleKey(it.MediaType, it.TMDBID)
		if k, ok := st.byTitle[key]; ok && time.Since(k.fetched) < scoresKeep {
			continue
		}
		if st.queued[key] || len(st.queue) >= scoreWarmQueueMax {
			continue
		}
		st.queued[key] = true
		st.queue = append(st.queue, scoreJob{kind: it.MediaType, tmdb: it.TMDBID})
	}
	if len(st.queue) > 0 && !st.workerAlive {
		st.workerAlive = true
		go s.scoreWorker()
	}
}

// scoreWorker looks up the waiting titles one at a time and stops when the
// line is empty or the day's background share is used.
func (s *Server) scoreWorker() {
	st := s.scores()
	defer func() {
		st.mu.Lock()
		st.workerAlive = false
		st.mu.Unlock()
	}()
	for {
		st.mu.Lock()
		if len(st.queue) == 0 {
			st.mu.Unlock()
			return
		}
		job := st.queue[0]
		st.queue = st.queue[1:]
		delete(st.queued, titleKey(job.kind, job.tmdb))
		st.mu.Unlock()

		c := s.omdbClient()
		if c == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		imdbID, err := s.imdbIDFor(ctx, job.kind, job.tmdb)
		if err == nil && imdbID != "" {
			_, err = s.lookupScores(ctx, c, job.kind, job.tmdb, imdbID, true)
		}
		cancel()
		if errors.Is(err, omdb.ErrLimit) {
			st.mu.Lock()
			st.queue, st.queued = nil, map[string]bool{}
			st.mu.Unlock()
			return
		}
		time.Sleep(scoreWarmEvery)
	}
}

// imdbIDFor asks TMDB for a title's IMDb id.
func (s *Server) imdbIDFor(ctx context.Context, kind string, tmdb int) (string, error) {
	if kind == "movie" {
		d, err := s.TMDB().GetMovieDetail(ctx, tmdb)
		if err != nil {
			return "", fmt.Errorf("movie %d: %w", tmdb, err)
		}
		return d.IMDBID, nil
	}
	d, err := s.TMDB().GetShow(ctx, tmdb)
	if err != nil {
		return "", fmt.Errorf("show %d: %w", tmdb, err)
	}
	return d.ExternalIDs.IMDBID, nil
}

// ---- Settings > Info, lists and subtitles: the OMDb key ----

type omdbStatePayload struct {
	Set bool `json:"set"`
}

func (s *Server) handleGetOMDb(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, omdbStatePayload{Set: s.omdbClient() != nil})
}

// handlePutOMDb saves a key after checking it with OMDb, or removes it.
func (s *Server) handlePutOMDb(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key    string `json:"key"`
		Remove bool   `json:"remove"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	key := strings.TrimSpace(req.Key)
	switch {
	case req.Remove:
		if err := s.Settings.Set(settings.KeyOMDbAPIKey, "", true); err != nil {
			writeError(w, http.StatusInternalServerError, "The key couldn't be removed.")
			return
		}
	case key != "":
		if len(key) > 64 || strings.ContainsAny(key, " \r\n\t/?&=#") {
			writeError(w, http.StatusBadRequest, "That doesn't look like an OMDb key. Copy it again from the email OMDb sent you.")
			return
		}
		base := omdb.DefaultBaseURL
		if tb := s.scores().testBase; tb != "" {
			base = tb
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := omdb.NewAt(base, key).Check(ctx); err != nil {
			switch {
			case errors.Is(err, omdb.ErrBadKey):
				writeError(w, http.StatusBadRequest, "OMDb didn't accept that key. Open the link in the email OMDb sent you to activate it, then try again.")
			case errors.Is(err, omdb.ErrLimit):
				writeError(w, http.StatusBadRequest, "That key has used its lookups for today. Try again tomorrow.")
			default:
				writeUpstreamError(w, "check the key with OMDb", err)
			}
			return
		}
		if err := s.Settings.Set(settings.KeyOMDbAPIKey, key, true); err != nil {
			writeError(w, http.StatusInternalServerError, "The key couldn't be saved.")
			return
		}
	}
	writeJSON(w, http.StatusOK, omdbStatePayload{Set: s.omdbClient() != nil})
}

// withScores gives a Discover card the scores kept for it, and puts it in line
// for a lookup when none are kept.
func (s *Server) withScores(p discoverPayload) discoverPayload {
	if s.omdbClient() == nil {
		return p
	}
	if p.Scores = s.keptScoresFor(p.MediaType, p.TMDBID); p.Scores == nil {
		s.warmScores([]discoverPayload{p})
	}
	return p
}
