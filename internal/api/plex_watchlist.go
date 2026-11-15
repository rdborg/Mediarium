package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/origin"
	"github.com/rdborg/mediarium/internal/plainerror"
)

// Plex watchlists. Each account can connect its own Plex account (the same
// PIN sign-in as for servers). From then on, a movie or show it puts on its
// Plex watchlist is added like any other title it adds: straight into the
// library for administrators and accounts that may add titles themselves,
// as a request for an administrator otherwise. What was already on the list
// when it was connected is shown on Discover with Add buttons, not added in
// one go.

const (
	watchlistInterval = 20 * time.Minute
	// watchlistPerRun caps the new titles handled per account per check, so a
	// long list is worked through over a few checks instead of all at once.
	watchlistPerRun = 25
	// watchlistLookupGap spaces out the lookups Plex rate-limits.
	watchlistLookupGap = 300 * time.Millisecond
	watchlistShowMax   = 60
)

type plexWatchPin struct {
	pinID    int64
	clientID string
	userID   int64
}

type plexWatchlistRow struct {
	UserID    int64
	Token     string
	Name      string
	AutoAdd   bool
	CheckedAt string
	LastError string
}

type plexWatchlistStatus struct {
	Connected bool   `json:"connected"`
	Name      string `json:"name,omitempty"`
	AutoAdd   bool   `json:"autoAdd"`
	CheckedAt string `json:"checkedAt,omitempty"`
	LastError string `json:"lastError,omitempty"`
	// Added and Requested count what the watchlist brought in so far.
	Added     int `json:"added"`
	Requested int `json:"requested"`
}

var watchlistRunMu sync.Mutex // one check at a time

func (s *Server) watchlistRow(userID int64) (plexWatchlistRow, bool, error) {
	var r plexWatchlistRow
	var enc string
	var auto int
	err := s.db.QueryRow(`SELECT user_id, token, plex_name, auto_add, checked_at, last_error FROM plex_watchlists WHERE user_id = ?`, userID).
		Scan(&r.UserID, &enc, &r.Name, &auto, &r.CheckedAt, &r.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("read plex watchlist: %w", err)
	}
	r.AutoAdd = auto != 0
	if r.Token, err = s.box.Decrypt(enc); err != nil {
		return r, false, fmt.Errorf("read plex watchlist token: %w", err)
	}
	return r, true, nil
}

func (s *Server) watchlistRows() ([]plexWatchlistRow, error) {
	rows, err := s.db.Query(`SELECT w.user_id FROM plex_watchlists w JOIN users u ON u.id = w.user_id WHERE w.auto_add = 1 AND u.disabled = 0`)
	if err != nil {
		return nil, fmt.Errorf("list plex watchlists: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []plexWatchlistRow
	for _, id := range ids {
		if r, ok, err := s.watchlistRow(id); err == nil && ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Server) watchlistStatus(userID int64) (plexWatchlistStatus, error) {
	r, ok, err := s.watchlistRow(userID)
	if err != nil || !ok {
		return plexWatchlistStatus{AutoAdd: true}, err
	}
	st := plexWatchlistStatus{Connected: true, Name: r.Name, AutoAdd: r.AutoAdd, CheckedAt: r.CheckedAt, LastError: r.LastError}
	_ = s.db.QueryRow(`SELECT COUNT(*) FILTER (WHERE outcome = 'added'), COUNT(*) FILTER (WHERE outcome = 'requested') FROM plex_watchlist_items WHERE user_id = ?`, userID).Scan(&st.Added, &st.Requested)
	return st, nil
}

func (s *Server) watchlistPlexTV() (*mediaservers.PlexTV, error) {
	clientID, err := s.mediaClientID()
	if err != nil {
		return nil, err
	}
	return s.plexTV(clientID), nil
}

// POST /api/plex-watchlist/pin: start connecting the caller's Plex account.
func (s *Server) handleStartWatchlistPin(w http.ResponseWriter, r *http.Request) {
	userID, _ := requester(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	p, err := s.watchlistPlexTV()
	if err != nil {
		writeSignInError(w, "starting Sign in with Plex", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	pin, err := p.CreatePIN(ctx)
	if err != nil {
		writeSignInError(w, "starting Sign in with Plex", err)
		return
	}
	s.signIns().watchPins.Put(strconv.FormatInt(pin.ID, 10), &plexWatchPin{pinID: pin.ID, clientID: p.ClientID, userID: userID})
	writeJSON(w, http.StatusOK, map[string]any{
		"pinId": pin.ID, "code": pin.Code, "authUrl": mediaservers.PlexAuthURL(p.ClientID, pin.Code), "expiresIn": int(plexPinTTL.Seconds()),
	})
}

// GET /api/plex-watchlist/pin/{pinId}: has the person approved it yet?
func (s *Server) handlePollWatchlistPin(w http.ResponseWriter, r *http.Request) {
	userID, _ := requester(r)
	key := r.PathValue("pinId")
	session, ok := s.signIns().watchPins.Get(key)
	if !ok || session.userID != userID {
		writeError(w, http.StatusGone, "That Plex sign-in has expired. Start again.")
		return
	}
	p := s.plexTV(session.clientID)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	pin, err := p.CheckPIN(ctx, session.pinID)
	if errors.Is(err, mediaservers.ErrPlexPINExpired) {
		s.signIns().watchPins.Delete(key)
		writeError(w, http.StatusGone, "That Plex sign-in has expired. Start again.")
		return
	}
	if err != nil {
		writeSignInError(w, "checking Sign in with Plex", err)
		return
	}
	if pin.AuthToken == "" {
		writeJSON(w, http.StatusOK, map[string]any{"done": false})
		return
	}
	s.signIns().watchPins.Delete(key)
	name := ""
	if a, err := p.Account(ctx, pin.AuthToken); err == nil {
		name = a.Username
		if name == "" {
			name = a.Title
		}
	}
	enc, err := s.box.Encrypt(pin.AuthToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.db.Exec(`INSERT INTO plex_watchlists (user_id, token, plex_name) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET token = excluded.token, plex_name = excluded.plex_name, last_error = ''`, userID, enc, name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// What is on the list now is shown with Add buttons rather than added.
	if err := s.watchlistBaseline(ctx, userID, pin.AuthToken); err != nil {
		slog.Warn("plex watchlist: read the list after connecting", "err", err)
	}
	slog.Info("plex watchlist connected", "user", userID)
	st, _ := s.watchlistStatus(userID)
	writeJSON(w, http.StatusOK, map[string]any{"done": true, "status": st})
}

// watchlistBaseline marks what's on the list now as seen, without adding it.
func (s *Server) watchlistBaseline(ctx context.Context, userID int64, token string) error {
	p, err := s.watchlistPlexTV()
	if err != nil {
		return err
	}
	items, err := p.Watchlist(ctx, token)
	if err != nil {
		return err
	}
	for _, it := range items {
		_, _ = s.db.Exec(`INSERT OR IGNORE INTO plex_watchlist_items (user_id, plex_key, kind, title, year, outcome) VALUES (?, ?, ?, ?, ?, 'existing')`,
			userID, it.Key, it.Kind, it.Title, it.Year)
	}
	return nil
}

// GET /api/plex-watchlist
func (s *Server) handleWatchlistStatus(w http.ResponseWriter, r *http.Request) {
	userID, _ := requester(r)
	st, err := s.watchlistStatus(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// PUT /api/plex-watchlist {"autoAdd": bool}
func (s *Server) handleUpdateWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _ := requester(r)
	var req struct {
		AutoAdd *bool `json:"autoAdd"`
	}
	if err := decodeJSON(r, &req); err != nil || req.AutoAdd == nil {
		writeError(w, http.StatusBadRequest, "Say whether new watchlist titles should be added.")
		return
	}
	auto := 0
	if *req.AutoAdd {
		auto = 1
	}
	res, err := s.db.Exec(`UPDATE plex_watchlists SET auto_add = ? WHERE user_id = ?`, auto, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "Connect your Plex account first.")
		return
	}
	st, _ := s.watchlistStatus(userID)
	writeJSON(w, http.StatusOK, st)
}

// DELETE /api/plex-watchlist: forget the Plex account and what was seen.
func (s *Server) handleDisconnectWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _ := requester(r)
	_, _ = s.db.Exec(`DELETE FROM plex_watchlist_items WHERE user_id = ?`, userID)
	if _, err := s.db.Exec(`DELETE FROM plex_watchlists WHERE user_id = ?`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	slog.Info("plex watchlist disconnected", "user", userID)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/plex-watchlist/check: look at the caller's watchlist now.
func (s *Server) handleCheckWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _ := requester(r)
	row, ok, err := s.watchlistRow(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "Connect your Plex account first.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	added, requested, err := s.syncWatchlist(ctx, row)
	if err != nil {
		writeError(w, http.StatusBadGateway, plainerror.Message(err))
		return
	}
	st, _ := s.watchlistStatus(userID)
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "requested": requested, "status": st})
}

// GET /api/plex-watchlist/items: what's on the caller's watchlist, with the
// details Discover shows, so anything can be added by hand.
func (s *Server) handleWatchlistItems(w http.ResponseWriter, r *http.Request) {
	userID, _ := requester(r)
	row, ok, err := s.watchlistRow(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "Connect your Plex account first (Settings > Accounts > Linked services).")
		return
	}
	p, err := s.watchlistPlexTV()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	items, err := p.Watchlist(ctx, row.Token)
	if err != nil {
		writeError(w, http.StatusBadGateway, plainerror.Message(err))
		return
	}
	if len(items) > watchlistShowMax {
		items = items[:watchlistShowMax]
	}
	out := []discoverPayload{}
	for _, it := range items {
		tmdb, err := s.watchlistTMDB(ctx, p, row, it)
		if err != nil || tmdb == 0 {
			continue
		}
		switch it.Kind {
		case "movie":
			if m, err := s.TMDB().GetMovie(ctx, tmdb); err == nil {
				out = append(out, s.movieDiscover(ctx, *m))
			}
		case "tv":
			if sh, err := s.TMDB().GetShow(ctx, tmdb); err == nil {
				out = append(out, s.showDiscover(ctx, sh.Show))
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// watchlistTMDB returns a watchlist title's TMDB id, from what was stored
// or asked from Plex (and stored).
func (s *Server) watchlistTMDB(ctx context.Context, p *mediaservers.PlexTV, row plexWatchlistRow, it mediaservers.WatchlistItem) (int, error) {
	var tmdb int
	err := s.db.QueryRow(`SELECT tmdb_id FROM plex_watchlist_items WHERE user_id = ? AND plex_key = ?`, row.UserID, it.Key).Scan(&tmdb)
	if err == nil && tmdb > 0 {
		return tmdb, nil
	}
	ids, err := p.WatchlistTitleIDs(ctx, row.Token, it.Key)
	if err != nil {
		return 0, err
	}
	time.Sleep(watchlistLookupGap)
	if ids.TMDB > 0 {
		_, _ = s.db.Exec(`UPDATE plex_watchlist_items SET tmdb_id = ? WHERE user_id = ? AND plex_key = ?`, ids.TMDB, row.UserID, it.Key)
	}
	return ids.TMDB, nil
}

// watchlistJob checks every connected watchlist that adds new titles.
func (s *Server) watchlistJob(ctx context.Context) {
	rows, err := s.watchlistRows()
	if err != nil || len(rows) == 0 {
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		if _, _, err := s.syncWatchlist(ctx, row); err != nil {
			slog.Warn("plex watchlist: check failed", "user", row.UserID, "err", err)
		}
	}
}

// syncWatchlist handles the titles new on one watchlist.
func (s *Server) syncWatchlist(ctx context.Context, row plexWatchlistRow) (added, requested int, err error) {
	watchlistRunMu.Lock()
	defer watchlistRunMu.Unlock()
	defer func() {
		msg := ""
		if err != nil {
			msg = plainerror.Message(err)
		}
		_, _ = s.db.Exec(`UPDATE plex_watchlists SET checked_at = ?, last_error = ? WHERE user_id = ?`, time.Now().UTC().Format(time.RFC3339), msg, row.UserID)
	}()
	user, err := s.Auth.GetUser(row.UserID)
	if err != nil {
		return 0, 0, fmt.Errorf("the account that connected this watchlist is gone: %w", err)
	}
	if user.Disabled {
		return 0, 0, errors.New("the account that connected this watchlist is turned off, so nothing is added from it")
	}
	if user.Viewer {
		return 0, 0, errors.New("the account that connected this watchlist is read-only, so nothing is added from it")
	}
	p, err := s.watchlistPlexTV()
	if err != nil {
		return 0, 0, err
	}
	items, err := p.Watchlist(ctx, row.Token)
	if err != nil {
		return 0, 0, err
	}
	seen := map[string]bool{}
	if rs, err := s.db.Query(`SELECT plex_key FROM plex_watchlist_items WHERE user_id = ?`, row.UserID); err == nil {
		for rs.Next() {
			var k string
			if rs.Scan(&k) == nil {
				seen[k] = true
			}
		}
		rs.Close()
	}
	handled := 0
	for _, it := range items {
		if seen[it.Key] || ctx.Err() != nil {
			continue
		}
		if handled >= watchlistPerRun {
			break
		}
		handled++
		_, _ = s.db.Exec(`INSERT OR IGNORE INTO plex_watchlist_items (user_id, plex_key, kind, title, year) VALUES (?, ?, ?, ?, ?)`,
			row.UserID, it.Key, it.Kind, it.Title, it.Year)
		outcome := s.addFromWatchlist(ctx, p, row, user, it)
		_, _ = s.db.Exec(`UPDATE plex_watchlist_items SET outcome = ? WHERE user_id = ? AND plex_key = ?`, outcome, row.UserID, it.Key)
		switch outcome {
		case "added":
			added++
		case "requested":
			requested++
		}
	}
	if added+requested > 0 {
		slog.Info("plex watchlist checked", "user", row.UserID, "added", added, "requested", requested)
	}
	return added, requested, nil
}

// addFromWatchlist adds one title the way the person would from its page,
// and says what happened.
func (s *Server) addFromWatchlist(ctx context.Context, p *mediaservers.PlexTV, row plexWatchlistRow, user *auth.User, it mediaservers.WatchlistItem) string {
	tmdb, err := s.watchlistTMDB(ctx, p, row, it)
	if err != nil {
		return "failed"
	}
	if tmdb == 0 {
		return "no-match"
	}
	var (
		handler http.HandlerFunc
		payload any
	)
	switch it.Kind {
	case "movie":
		if !s.moviesEnabled() || !s.mayUse(user, auth.PermMovies) {
			return "off"
		}
		if _, ok, _ := s.MovieRepo.GetByTMDBID(tmdb); ok {
			return "in-library"
		}
		handler, payload = s.handleAddMovie, map[string]any{"tmdbId": tmdb, "searchNow": true}
	case "tv":
		if !s.tvEnabled() || !s.mayUse(user, auth.PermTV) {
			return "off"
		}
		if _, ok, _ := s.MovieRepo.GetSeriesByTMDBID(tmdb); ok {
			return "in-library"
		}
		handler, payload = s.handleAddSeries, map[string]any{"tmdbId": tmdb, "monitor": "all", "searchNow": true}
	default:
		return "failed"
	}
	body, _ := json.Marshal(payload)
	inner := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(withOrigin(auth.WithUser(ctx, user), origin.Origin{Kind: origin.Watchlist}))
	inner.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, inner)
	switch {
	case rec.Code == http.StatusCreated || rec.Code == http.StatusOK:
		_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("%s came from %s's Plex watchlist", it.Title, user.DisplayName()))
		return "added"
	case rec.Code == http.StatusAccepted:
		return "requested"
	case rec.Code == http.StatusConflict:
		return "in-library"
	}
	slog.Info("plex watchlist: couldn't add a title", "title", it.Title, "status", rec.Code, "answer", rec.Body.String())
	return "failed"
}

// mayUse reports whether an account is allowed a permission (an administrator
// always is). The watchlist adds titles without going through the route that
// checks it, so it asks here.
func (s *Server) mayUse(u *auth.User, perm string) bool {
	p, err := s.Auth.PermissionsOf(u.ID)
	return err == nil && p.Allows(perm)
}
