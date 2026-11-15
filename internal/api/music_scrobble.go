package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/listenbrainz"
)

// Scrobbling to ListenBrainz. Each account can connect its own ListenBrainz
// user token; the player then tells the server when a track starts ("playing
// now") and when it has been played long enough to count. The server looks the
// track up itself, so the browser can't send made-up titles, and sends it with
// the account's own token. The token is stored encrypted and is never sent
// back to the browser or written to the log.

const (
	// A listen older than this, or from the future, is refused.
	listenMaxAge   = 14 * 24 * time.Hour
	listenMaxAhead = 5 * time.Minute
	// listensKept is how many unsent listens are kept per account.
	listensKept = 500
	// listenMinTime is the oldest time ListenBrainz accepts.
	listenMinTime = 1033430400
)

type scrobbleRow struct {
	Token     string
	LBUser    string
	Enabled   bool
	LastError string
	Submitted int
}

func (s *Server) scrobbler() *listenbrainz.Submitter {
	s.music.playerMu.Lock()
	defer s.music.playerMu.Unlock()
	if s.music.scrobble == nil {
		s.music.scrobble = listenbrainz.NewSubmitter(s.version)
	}
	return s.music.scrobble
}

func (s *Server) scrobbleRow(userID int64) (scrobbleRow, bool, error) {
	var r scrobbleRow
	var enc string
	var enabled int
	err := s.db.QueryRow(`SELECT token, lb_user, enabled, last_error, submitted FROM music_listenbrainz WHERE user_id = ?`, userID).
		Scan(&enc, &r.LBUser, &enabled, &r.LastError, &r.Submitted)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("read ListenBrainz connection: %w", err)
	}
	r.Enabled = enabled != 0
	if r.Token, err = s.box.Decrypt(enc); err != nil {
		return r, false, fmt.Errorf("read ListenBrainz token: %w", err)
	}
	return r, true, nil
}

type scrobbleStatus struct {
	Connected bool   `json:"connected"`
	User      string `json:"user,omitempty"`
	Enabled   bool   `json:"enabled"`
	LastError string `json:"lastError,omitempty"`
	Submitted int    `json:"submitted"`
	Pending   int    `json:"pending"`
}

func (s *Server) scrobbleStatus(userID int64) (scrobbleStatus, error) {
	r, ok, err := s.scrobbleRow(userID)
	if err != nil || !ok {
		return scrobbleStatus{}, err
	}
	st := scrobbleStatus{Connected: true, User: r.LBUser, Enabled: r.Enabled, LastError: r.LastError, Submitted: r.Submitted}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM music_listens_pending WHERE user_id = ?`, userID).Scan(&st.Pending)
	return st, nil
}

// GET /api/music/listenbrainz
func (s *Server) handleScrobbleStatus(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	st, err := s.scrobbleStatus(me)
	if err != nil {
		slog.Warn("read ListenBrainz connection", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the ListenBrainz settings. Check the log for details.")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// PUT /api/music/listenbrainz {token?, enabled?}
func (s *Server) handleScrobbleSave(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		Token   *string `json:"token"`
		Enabled *bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Token == nil && req.Enabled == nil) {
		writeError(w, http.StatusBadRequest, "Send your ListenBrainz user token, or whether to send plays.")
		return
	}
	if req.Token != nil {
		token := trimToken(*req.Token)
		if token == "" {
			writeError(w, http.StatusBadRequest, "Paste your ListenBrainz user token.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		user, valid, err := s.scrobbler().ValidateToken(ctx, token)
		if err != nil {
			slog.Info("check ListenBrainz token failed", "err", err)
			writeError(w, http.StatusBadGateway, "Couldn't reach ListenBrainz to check the token. Try again in a moment.")
			return
		}
		if !valid {
			writeError(w, http.StatusBadRequest, "ListenBrainz doesn't know that token. Copy it again from listenbrainz.org/settings.")
			return
		}
		enc, err := s.box.Encrypt(token)
		if err != nil {
			slog.Warn("encrypt ListenBrainz token", "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't save the token. Check the log for details.")
			return
		}
		on := 1
		if req.Enabled != nil && !*req.Enabled {
			on = 0
		}
		if _, err := s.db.Exec(`INSERT INTO music_listenbrainz (user_id, token, lb_user, enabled) VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id) DO UPDATE SET token = excluded.token, lb_user = excluded.lb_user, enabled = excluded.enabled, last_error = ''`,
			me, enc, user, on); err != nil {
			slog.Warn("save ListenBrainz connection", "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't save the token. Check the log for details.")
			return
		}
		slog.Info("ListenBrainz connected", "user", me)
	} else {
		on := 0
		if *req.Enabled {
			on = 1
		}
		res, err := s.db.Exec(`UPDATE music_listenbrainz SET enabled = ?, last_error = '' WHERE user_id = ?`, on, me)
		if err != nil {
			slog.Warn("save ListenBrainz setting", "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't save the setting. Check the log for details.")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusNotFound, "Connect your ListenBrainz account first.")
			return
		}
	}
	st, _ := s.scrobbleStatus(me)
	writeJSON(w, http.StatusOK, st)
}

func trimToken(s string) string { return strings.TrimSpace(s) }

// DELETE /api/music/listenbrainz: forget the token and any unsent plays.
func (s *Server) handleScrobbleForget(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM music_listens_pending WHERE user_id = ?`, me)
	if _, err := s.db.Exec(`DELETE FROM music_listenbrainz WHERE user_id = ?`, me); err != nil {
		slog.Warn("forget ListenBrainz connection", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't remove the token. Check the log for details.")
		return
	}
	slog.Info("ListenBrainz disconnected", "user", me)
	w.WriteHeader(http.StatusNoContent)
}

// listenFor builds what is sent for a track.
func (s *Server) listenFor(trackID, at int64) (listenbrainz.Listen, error) {
	t, err := s.MusicRepo.GetTrack(trackID)
	if err != nil {
		return listenbrainz.Listen{}, err
	}
	album, err := s.MusicRepo.GetAlbum(t.AlbumID)
	if err != nil {
		return listenbrainz.Listen{}, err
	}
	artist, err := s.MusicRepo.GetArtist(album.ArtistID)
	if err != nil {
		return listenbrainz.Listen{}, err
	}
	l := listenbrainz.Listen{ArtistName: artist.Name, TrackName: t.Title, ReleaseName: album.Title, ListenedAt: at,
		DurationMs: t.LengthMs, TrackNumber: t.Position, ReleaseGroupMBID: manualBlank(album.MBID, album.MBID)}
	if artist.MBID != "" && !artist.Manual() {
		l.ArtistMBIDs = []string{artist.MBID}
	}
	return l, nil
}

// scrobbleFailed records an error shown next to the settings. A token
// ListenBrainz refuses also switches sending off, so it isn't tried again for
// every track.
func (s *Server) scrobbleFailed(userID int64, err error) {
	msg := "Couldn't reach ListenBrainz. Plays are kept and sent later."
	var limited *listenbrainz.RateLimitedError
	switch {
	case errors.Is(err, listenbrainz.ErrBadToken):
		msg = "ListenBrainz no longer accepts the token. Paste it again to carry on."
		_, _ = s.db.Exec(`UPDATE music_listenbrainz SET enabled = 0, last_error = ? WHERE user_id = ?`, msg, userID)
		return
	case errors.As(err, &limited):
		msg = "ListenBrainz asked for fewer requests. Plays are kept and sent later."
	}
	_, _ = s.db.Exec(`UPDATE music_listenbrainz SET last_error = ? WHERE user_id = ?`, msg, userID)
}

// POST /api/music/listenbrainz/playing {trackId}
func (s *Server) handleScrobblePlaying(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		TrackID int64 `json:"trackId"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TrackID <= 0 {
		writeError(w, http.StatusBadRequest, "Say which track is playing.")
		return
	}
	row, connected, err := s.scrobbleRow(me)
	if err != nil || !connected || !row.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"sent": false})
		return
	}
	l, err := s.listenFor(req.TrackID, 0)
	if err != nil {
		writeError(w, http.StatusNotFound, "That track isn't in your library.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := s.scrobbler().PlayingNow(ctx, row.Token, l); err != nil {
		slog.Info("ListenBrainz playing now failed", "user", me, "err", err)
		s.scrobbleFailed(me, err)
		writeJSON(w, http.StatusOK, map[string]any{"sent": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

// POST /api/music/listenbrainz/listen {trackId, listenedAt}: a play that has
// counted. listenedAt is the Unix time (seconds) it started.
func (s *Server) handleScrobbleListen(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		TrackID    int64 `json:"trackId"`
		ListenedAt int64 `json:"listenedAt"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TrackID <= 0 {
		writeError(w, http.StatusBadRequest, "Say which track was played.")
		return
	}
	now := time.Now()
	if req.ListenedAt < listenMinTime || time.Unix(req.ListenedAt, 0).After(now.Add(listenMaxAhead)) || time.Unix(req.ListenedAt, 0).Before(now.Add(-listenMaxAge)) {
		writeError(w, http.StatusBadRequest, "That play time isn't right.")
		return
	}
	row, connected, err := s.scrobbleRow(me)
	if err != nil || !connected || !row.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"sent": false})
		return
	}
	if _, err := s.MusicRepo.GetTrack(req.TrackID); err != nil {
		writeError(w, http.StatusNotFound, "That track isn't in your library.")
		return
	}
	// Anything kept from an earlier failure goes first, in the order played.
	if _, err := s.db.Exec(`INSERT INTO music_listens_pending (user_id, track_id, listened_at) VALUES (?, ?, ?)`, me, req.TrackID, req.ListenedAt); err != nil {
		slog.Warn("keep listen", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't keep that play. Check the log for details.")
		return
	}
	unlock := s.music.scrobbleAt.lock(me)
	defer unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	sent, err := s.flushListens(ctx, me, row.Token)
	if err != nil {
		slog.Info("ListenBrainz submit failed", "user", me, "err", err)
		s.scrobbleFailed(me, err)
	}
	var pending int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM music_listens_pending WHERE user_id = ?`, me).Scan(&pending)
	writeJSON(w, http.StatusOK, map[string]any{"sent": err == nil, "submitted": sent, "pending": pending})
}

// flushListens sends the account's kept plays, oldest first, a batch at a
// time. Plays that can't be sent stay kept (up to listensKept, oldest dropped).
// Plays of tracks that no longer exist are dropped.
func (s *Server) flushListens(ctx context.Context, userID int64, token string) (int, error) {
	_, _ = s.db.Exec(`DELETE FROM music_listens_pending WHERE user_id = ? AND id NOT IN
		(SELECT id FROM music_listens_pending WHERE user_id = ? ORDER BY listened_at DESC, id DESC LIMIT ?)`, userID, userID, listensKept)
	total := 0
	for {
		rows, err := s.db.Query(`SELECT id, track_id, listened_at FROM music_listens_pending WHERE user_id = ? ORDER BY listened_at, id LIMIT ?`,
			userID, listenbrainz.MaxListensPerSubmit)
		if err != nil {
			return total, fmt.Errorf("read kept listens: %w", err)
		}
		type kept struct{ id, track, at int64 }
		var batch []kept
		for rows.Next() {
			var k kept
			if err := rows.Scan(&k.id, &k.track, &k.at); err != nil {
				rows.Close()
				return total, fmt.Errorf("read kept listens: %w", err)
			}
			batch = append(batch, k)
		}
		rows.Close()
		if len(batch) == 0 {
			return total, nil
		}
		var send []listenbrainz.Listen
		for _, k := range batch {
			if l, err := s.listenFor(k.track, k.at); err == nil {
				send = append(send, l)
			}
		}
		if err := s.scrobbler().Submit(ctx, token, send); err != nil {
			return total, err
		}
		for _, k := range batch {
			_, _ = s.db.Exec(`DELETE FROM music_listens_pending WHERE id = ?`, k.id)
		}
		total += len(send)
		_, _ = s.db.Exec(`UPDATE music_listenbrainz SET submitted = submitted + ?, last_error = '' WHERE user_id = ?`, len(send), userID)
		if len(batch) < listenbrainz.MaxListensPerSubmit {
			return total, nil
		}
	}
}
