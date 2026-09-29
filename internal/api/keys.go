package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ryanborg/mediarium/internal/metadata"
	"github.com/ryanborg/mediarium/internal/settings"
	"github.com/ryanborg/mediarium/internal/subtitles"
	"github.com/ryanborg/mediarium/internal/trakt"
)

// BuiltinKeys are the app-wide API keys an official release ships with, so
// users don't have to sign up for each service themselves. They are injected
// at build time (see cmd/app/main.go and the Dockerfile), never committed.
// A key the user enters in Settings always wins over the built-in one; on a
// release that has a built-in key the Settings UI simply hides the field.
type BuiltinKeys struct {
	TMDB          string
	OpenSubtitles string
	TraktClientID string
}

// SetBuiltinKeys installs the built-in keys and applies each one to its
// client unless the user already configured their own. Call it once at
// startup, before serving requests.
func (s *Server) SetBuiltinKeys(k BuiltinKeys) {
	s.builtin = k
	if k.TMDB != "" && !s.TMDB().HasAPIKey() {
		s.tmdbMu.Lock()
		s.tmdb.SetAPIKey(k.TMDB)
		s.tmdbMu.Unlock()
	}
	if k.TraktClientID != "" && !s.Trakt().HasClientID() {
		s.traktMu.Lock()
		s.trakt.SetClientID(k.TraktClientID)
		s.traktMu.Unlock()
	}
	s.rebuildSubtitles()
}

// rebuildSubtitles recreates the OpenSubtitles client from the current
// settings: the user's own API key if set, else the built-in one, plus the
// optional account.
func (s *Server) rebuildSubtitles() {
	key, _ := s.Settings.Get(settings.KeyOpenSubtitlesAPIKey)
	if key == "" {
		key = s.builtin.OpenSubtitles
	}
	user, _ := s.Settings.Get(settings.KeyOpenSubtitlesUsername)
	pass, _ := s.Settings.Get(settings.KeyOpenSubtitlesPassword)

	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	next := s.subs.WithKey(key)
	next.SetCredentials(user, pass)
	s.instrumentSubtitles(next)
	s.subs = next
}

// usingOwnKey reports whether the person saved their own key for a service
// (setting key), as opposed to running on the key that ships with the app.
func (s *Server) usingOwnKey(settingKey string) bool {
	v, _ := s.Settings.Get(settingKey)
	return v != ""
}

// SetOpenSubtitlesAccount stores the optional OpenSubtitles.com login.
// Empty values clear it.
func (s *Server) SetOpenSubtitlesAccount(username, password string) error {
	if err := s.Settings.Set(settings.KeyOpenSubtitlesUsername, username, false); err != nil {
		return err
	}
	if err := s.Settings.Set(settings.KeyOpenSubtitlesPassword, password, true); err != nil {
		return err
	}
	s.rebuildSubtitles()
	return nil
}

type testServiceRequest struct {
	Service  string `json:"service"` // tmdb | opensubtitles | trakt
	Key      string `json:"key"`     // blank = test the key currently in use
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleTestService checks a third-party key against its service without
// saving it, so onboarding and Settings can say "this works" or "this was
// rejected" before the user moves on.
func (s *Server) handleTestService(w http.ResponseWriter, r *http.Request) {
	var req testServiceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	switch req.Service {
	case "tmdb":
		client := s.TMDB()
		if req.Key != "" {
			client = client.WithAPIKey(req.Key)
		}
		switch _, err := client.SearchMovies(ctx, "the matrix"); {
		case err == nil:
			writeJSON(w, http.StatusOK, connTestResult{OK: true, Message: "TMDB accepted the key."})
		case errors.Is(err, metadata.ErrNoAPIKey):
			writeJSON(w, http.StatusOK, connTestResult{Message: "Enter a TMDB API key first."})
		default:
			writeJSON(w, http.StatusOK, connTestResult{Message: err.Error()})
		}
	case "opensubtitles":
		client := s.Subtitles()
		if req.Key != "" || req.Username != "" {
			key := req.Key
			if key == "" {
				key = s.effectiveSubtitlesKey()
			}
			client = client.WithKey(key)
			client.SetCredentials(req.Username, req.Password)
		}
		msg, err := client.TestConnection(ctx)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, connTestResult{OK: true, Message: msg})
		case errors.Is(err, subtitles.ErrNoAPIKey):
			writeJSON(w, http.StatusOK, connTestResult{Message: "Enter an OpenSubtitles API key first."})
		default:
			writeJSON(w, http.StatusOK, connTestResult{Message: err.Error()})
		}
	case "trakt":
		client := s.Trakt()
		if req.Key != "" {
			client = client.WithClientID(req.Key)
		}
		switch err := client.Ping(ctx); {
		case err == nil:
			writeJSON(w, http.StatusOK, connTestResult{OK: true, Message: "Trakt accepted the client ID."})
		case errors.Is(err, trakt.ErrNoClientID):
			writeJSON(w, http.StatusOK, connTestResult{Message: "Enter a Trakt client ID first."})
		default:
			writeJSON(w, http.StatusOK, connTestResult{Message: err.Error()})
		}
	default:
		writeError(w, http.StatusBadRequest, `service must be "tmdb", "opensubtitles" or "trakt"`)
	}
}

func (s *Server) effectiveSubtitlesKey() string {
	if key, _ := s.Settings.Get(settings.KeyOpenSubtitlesAPIKey); key != "" {
		return key
	}
	return s.builtin.OpenSubtitles
}
