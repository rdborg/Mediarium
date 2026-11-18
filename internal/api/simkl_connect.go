package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/simkl"
)

// Connecting a Simkl account (Settings > Library and files > Import
// lists). The person gets a short code, enters it on Simkl's site, and
// Mediarium collects the access token. No password reaches Mediarium. The
// token is kept encrypted in the settings and never sent to the browser; only
// the code and whether an account is connected are. Which sign-in Simkl
// offers (the OAuth 2.0 device flow or the older PIN flow) is decided by the
// app, and internal/simkl works that out.

// simklPinTTL is the longest Mediarium keeps waiting for a code to be entered
// (the code's own life, which Simkl sets, is shorter or about as long).
const simklPinTTL = time.Hour

// errSimklTokenUnreadable means the saved sign-in can't be decrypted (the
// secret.key is not the one the database was saved with).
var errSimklTokenUnreadable = errors.New("the saved Simkl sign-in can't be read")

type simklState struct {
	Connected bool `json:"connected"`
	// Unreadable is true when a sign-in is saved but can't be read, so the
	// account has to be connected again.
	Unreadable  bool `json:"unreadable"`
	HasClientID bool `json:"hasClientId"`
}

func (s *Server) simklState() simklState {
	st := simklState{HasClientID: s.Simkl().HasClientID()}
	tok, err := s.Settings.Get(settings.KeySimklToken)
	switch {
	case err != nil:
		st.Unreadable = true
	case tok != "":
		st.Connected = true
	}
	return st
}

// GET /api/settings/simkl
func (s *Server) handleGetSimkl(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.simklState())
}

// POST /api/settings/simkl/pin: start connecting an account.
func (s *Server) handleStartSimklPin(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	sign, err := s.Simkl().StartSignIn(ctx)
	switch {
	case errors.Is(err, simkl.ErrNoClientID):
		writeError(w, http.StatusPreconditionFailed, "Add your Simkl client ID first.")
		return
	case err != nil:
		logSimklRefusal("start the sign-in", "", err)
		var rl *simkl.RateLimitError
		if errors.As(err, &rl) {
			writeError(w, http.StatusTooManyRequests, rl.Error()+".")
			return
		}
		if writeSimklStatusError(w, "start the Simkl sign-in", err) {
			return
		}
		writeUpstreamError(w, "start the Simkl sign-in", err)
		return
	}
	s.simklPins.Put(sign.UserCode, sign)
	slog.Info("simkl sign-in started", "flow", sign.Flow)
	writeJSON(w, http.StatusOK, map[string]any{
		"code":      sign.UserCode,
		"url":       sign.VerificationURL,
		"fullUrl":   sign.CompleteURL,
		"flow":      sign.Flow,
		"expiresIn": int(min(sign.ExpiresIn, simklPinTTL).Seconds()),
		"interval":  int(sign.Interval.Seconds()),
	})
}

// GET /api/settings/simkl/pin/{code}: has the code been entered yet?
func (s *Server) handlePollSimklPin(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	sign, ok := s.simklPins.Get(code)
	if !ok {
		writeError(w, http.StatusGone, "That Simkl code has expired. Start again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	poll, err := s.Simkl().CheckSignIn(ctx, sign)
	switch {
	case errors.Is(err, simkl.ErrPINExpired):
		s.simklPins.Delete(code)
		writeError(w, http.StatusGone, "That Simkl code has expired. Start again.")
		return
	case errors.Is(err, simkl.ErrAccessDenied):
		s.simklPins.Delete(code)
		writeError(w, http.StatusGone, "The sign-in was declined on Simkl. Start again if that was a mistake.")
		return
	case err != nil:
		var rl *simkl.RateLimitError
		if errors.As(err, &rl) {
			poll = simkl.Poll{SlowDown: true} // too fast: the page just asks again later
			break
		}
		if writeSimklStatusError(w, "check the Simkl sign-in", err) {
			return
		}
		writeUpstreamError(w, "check the Simkl sign-in", err)
		return
	}
	if poll.Token == nil {
		if poll.SlowDown {
			sign.Interval = simkl.Wait(sign.Interval, true)
			s.simklPins.Put(code, sign)
		}
		writeJSON(w, http.StatusOK, map[string]any{"done": false, "interval": int(sign.Interval.Seconds())})
		return
	}
	s.simklPins.Delete(code)
	if err := s.saveSimklToken(*poll.Token); err != nil {
		slog.Error("simkl: save the sign-in", "err", err)
		writeError(w, http.StatusInternalServerError, "The Simkl sign-in couldn't be saved.")
		return
	}
	slog.Info("simkl account connected", "flow", sign.Flow)
	// Asking who signed in proves the Client ID and the sign-in work together;
	// if it fails the account is still connected, only the name is left out.
	name, nerr := s.Simkl().UserName(ctx, poll.Token.Access)
	if nerr != nil {
		slog.Warn("simkl: asking the account's name after signing in failed", "err", nerr)
	}
	writeJSON(w, http.StatusOK, map[string]any{"done": true, "name": name, "status": s.simklState()})
}

// saveSimklToken keeps a finished sign-in: the access token and the refresh
// token encrypted, and when the access token runs out when Simkl said.
func (s *Server) saveSimklToken(t simkl.Token) error {
	if err := s.Settings.Set(settings.KeySimklToken, t.Access, true); err != nil {
		return fmt.Errorf("save the Simkl sign-in: %w", err)
	}
	if err := s.Settings.Set(settings.KeySimklRefresh, t.Refresh, true); err != nil {
		return fmt.Errorf("save the Simkl refresh token: %w", err)
	}
	expires := ""
	if !t.ExpiresAt.IsZero() {
		expires = strconv.FormatInt(t.ExpiresAt.Unix(), 10)
	}
	if err := s.Settings.Set(settings.KeySimklExpires, expires, false); err != nil {
		return fmt.Errorf("save when the Simkl sign-in runs out: %w", err)
	}
	return nil
}

// simklAccessToken returns the saved access token, trading the refresh token
// for a new one first when Simkl said the saved one has run out (or is about
// to). It returns "" with no error when no account is connected.
func (s *Server) simklAccessToken(ctx context.Context) (string, error) {
	read := func() (token string, expires int64, err error) {
		token, err = s.Settings.Get(settings.KeySimklToken)
		if err != nil {
			slog.Warn("simkl: the saved sign-in can't be read", "err", err)
			return "", 0, errSimklTokenUnreadable
		}
		raw, _ := s.Settings.Get(settings.KeySimklExpires)
		expires, _ = strconv.ParseInt(raw, 10, 64)
		return token, expires, nil
	}
	token, expires, err := read()
	if err != nil || token == "" || expires == 0 || time.Now().Unix() < expires-60 {
		return token, err
	}
	s.simklRefreshMu.Lock()
	defer s.simklRefreshMu.Unlock()
	// Another list may have refreshed it while this one waited.
	if token, expires, err = read(); err != nil || token == "" || expires == 0 || time.Now().Unix() < expires-60 {
		return token, err
	}
	refresh, rerr := s.Settings.Get(settings.KeySimklRefresh)
	if rerr != nil {
		slog.Warn("simkl: the saved refresh token can't be read", "err", rerr)
		return "", errSimklTokenUnreadable
	}
	if refresh == "" {
		return token, nil // nothing to refresh with; Simkl will say if the token is no good
	}
	fresh, ferr := s.Simkl().Refresh(ctx, refresh)
	if ferr != nil {
		slog.Warn("simkl: refreshing the sign-in failed", "err", ferr)
		return "", ferr
	}
	if err := s.saveSimklToken(fresh); err != nil {
		return "", err
	}
	slog.Info("simkl sign-in refreshed")
	return fresh.Access, nil
}

// logSimklRefusal writes one warning when Simkl turns down a Client ID test or a
// sign-in start, so a support report shows what it said. Only the first four
// characters of the Client ID are logged. key is "" for the saved one.
func logSimklRefusal(doing, key string, err error) {
	if errors.Is(err, simkl.ErrNoClientID) {
		return
	}
	attrs := []any{"while", doing, "client_id_start", clientIDStart(key)}
	var se *simkl.StatusError
	if errors.As(err, &se) {
		attrs = append(attrs, "status", se.Status, "reason", se.Message)
	} else {
		attrs = append(attrs, "err", err)
	}
	slog.Warn("simkl refused the Client ID or could not be asked", attrs...)
}

// clientIDStart is the first four characters of a Client ID, enough to tell
// two apart in a report without exposing it.
func clientIDStart(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(saved one)"
	}
	if len(key) <= 4 {
		return "(short)"
	}
	return key[:4] + "..."
}

// writeSimklStatusError shows Simkl's own answer (its status and the reason it
// gave) with what to check, instead of the generic "couldn't, try again". It
// reports whether the error was one.
func writeSimklStatusError(w http.ResponseWriter, doing string, err error) bool {
	var se *simkl.StatusError
	if !errors.As(err, &se) {
		return false
	}
	slog.Warn("simkl refused a request", "while", doing, "status", se.Status, "reason", se.Message)
	writeError(w, http.StatusBadGateway, sentence(se.Error()))
	return true
}

// POST /api/settings/simkl/test: ask Simkl who the connected account is. It
// proves the Client ID and the saved sign-in work together.
func (s *Server) handleTestSimkl(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	reply := func(ok bool, msg string) { writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "message": msg}) }
	token, err := s.simklAccessToken(ctx)
	name := ""
	if err == nil {
		name, err = s.Simkl().UserName(ctx, token)
		if errors.Is(err, simkl.ErrInvalidClientID) {
			// The account call can be refused for an account whose lists sync
			// fine, so ask for the activity times, which is what a sync does.
			err = s.Simkl().CheckToken(ctx, token)
		}
	}
	var rl *simkl.RateLimitError
	var se *simkl.StatusError
	switch {
	case err == nil && name != "":
		reply(true, fmt.Sprintf("Connected to Simkl as %s.", name))
	case err == nil:
		reply(true, "Simkl accepted the sign-in.")
	case errors.Is(err, errSimklTokenUnreadable):
		reply(false, "The saved Simkl sign-in can't be read. Connect your Simkl account again.")
	case errors.Is(err, simkl.ErrNoClientID):
		reply(false, "Add your Simkl client ID first.")
	case errors.Is(err, simkl.ErrNotConnected):
		reply(false, "No Simkl account is connected yet.")
	case errors.Is(err, simkl.ErrUnauthorized):
		reply(false, "Simkl no longer accepts the saved sign-in. Connect your Simkl account again.")
	case errors.Is(err, simkl.ErrInvalidClientID):
		reply(false, "Simkl rejected the client ID. Check it in Settings.")
	case errors.As(err, &rl):
		reply(false, sentence(rl.Error()))
	case errors.As(err, &se):
		reply(false, sentence(se.Error()))
	default:
		slog.Warn("simkl: the connection test failed", "err", err)
		reply(false, "Couldn't reach Simkl right now. Try again in a moment.")
	}
}

// DELETE /api/settings/simkl: forget the connected account. Lists already
// added to the library stay; the Simkl lists in Mediarium wait until an
// account is connected again.
func (s *Server) handleDisconnectSimkl(w http.ResponseWriter, r *http.Request) {
	if err := s.saveSimklToken(simkl.Token{}); err != nil {
		slog.Error("simkl: forget the sign-in", "err", err)
		writeError(w, http.StatusInternalServerError, "The Simkl account couldn't be disconnected.")
		return
	}
	slog.Info("simkl account disconnected")
	w.WriteHeader(http.StatusNoContent)
}
