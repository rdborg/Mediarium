package api

import (
	"errors"
	"net/http"

	"github.com/rdborg/mediarium/internal/settings"
)

// The modules switchboard: which kinds of media Mediarium looks after.
// Movies and TV are on by default, music is off until switched on;
// audiobooks and ebooks are not built yet and cannot be switched on. Turning
// a module off hides its pages and stops its automation (no searches, RSS
// sync or automatic grabs) and its add endpoints answer 409; nothing that
// already exists is deleted or changed, and downloads already running finish.

const (
	moduleMovies = "movies"
	moduleTV     = "tv"
	moduleMusic  = "music"
)

var (
	errComingSoon = errors.New("Coming soon")
	errNoModules  = errors.New("At least one media type has to stay switched on.")
)

// moduleOnByDefault reads a module switch that is on unless it was set to "0".
func (s *Server) moduleOnByDefault(key string) bool {
	v, _ := s.Settings.Get(key)
	return v != "0"
}

func (s *Server) moviesEnabled() bool { return s.moduleOnByDefault(settings.KeyModulesMovies) }
func (s *Server) tvEnabled() bool     { return s.moduleOnByDefault(settings.KeyModulesTV) }

// musicEnabled reports whether the music module is switched on: modules.music
// when it has been set, otherwise the older music.enabled.
func (s *Server) musicEnabled() bool {
	if v, _ := s.Settings.Get(settings.KeyModulesMusic); v != "" {
		return v == "1"
	}
	v, _ := s.Settings.GetBool(settings.KeyMusicEnabled)
	return v
}

type moduleState struct {
	Enabled   bool `json:"enabled"`
	Available bool `json:"available"` // false for a module that is not built yet
}

type modulesPayload struct {
	Movies     moduleState `json:"movies"`
	TV         moduleState `json:"tv"`
	Music      moduleState `json:"music"`
	Audiobooks moduleState `json:"audiobooks"`
	Ebooks     moduleState `json:"ebooks"`
	// SubtitlesEnabled says whether the subtitles switch is on, so every
	// account's pages can show or hide the subtitle parts. It is not a media
	// type and is not changed here (see PUT /api/settings).
	SubtitlesEnabled bool `json:"subtitlesEnabled"`
}

func (s *Server) modulesPayload() modulesPayload {
	return modulesPayload{
		Movies:     moduleState{Enabled: s.moviesEnabled(), Available: true},
		TV:         moduleState{Enabled: s.tvEnabled(), Available: true},
		Music:      moduleState{Enabled: s.musicEnabled(), Available: true},
		Audiobooks: moduleState{},
		Ebooks:     moduleState{},

		SubtitlesEnabled: s.subtitlesEnabled(),
	}
}

// handleModules tells every signed-in account which modules are on, and
// whether subtitles are switched on, so the interface shows only their pages.
func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.modulesPayload())
}

// moduleChanges is a change to the switchboard; a field left out keeps its
// module as it is.
type moduleChanges struct {
	Movies     *bool `json:"movies"`
	TV         *bool `json:"tv"`
	Music      *bool `json:"music"`
	Audiobooks *bool `json:"audiobooks"`
	Ebooks     *bool `json:"ebooks"`
}

// handlePutModules switches modules on or off (administrators) and answers
// with the new state. Audiobooks and ebooks cannot be switched on yet (400
// "Coming soon"), and a change that would leave nothing on is refused (400
// "At least one media type has to stay switched on.").
func (s *Server) handlePutModules(w http.ResponseWriter, r *http.Request) {
	var req moduleChanges
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if err := s.setModules(req); err != nil {
		writeModulesError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.modulesPayload())
}

func writeModulesError(w http.ResponseWriter, err error) {
	if errors.Is(err, errComingSoon) || errors.Is(err, errNoModules) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// setModules applies a change after checking the whole result: nothing is
// written unless every rule holds.
func (s *Server) setModules(c moduleChanges) error {
	if (c.Audiobooks != nil && *c.Audiobooks) || (c.Ebooks != nil && *c.Ebooks) {
		return errComingSoon
	}
	pick := func(change *bool, current bool) bool {
		if change == nil {
			return current
		}
		return *change
	}
	movies, tv, music := pick(c.Movies, s.moviesEnabled()), pick(c.TV, s.tvEnabled()), pick(c.Music, s.musicEnabled())
	if !movies && !tv && !music {
		return errNoModules
	}
	flag := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	for _, w := range []struct {
		changed *bool
		key     string
		on      bool
	}{
		{c.Movies, settings.KeyModulesMovies, movies},
		{c.TV, settings.KeyModulesTV, tv},
		{c.Music, settings.KeyModulesMusic, music},
		{c.Music, settings.KeyMusicEnabled, music}, // the older name, kept in step
	} {
		if w.changed == nil {
			continue
		}
		if err := s.Settings.Set(w.key, flag(w.on), false); err != nil {
			return err
		}
	}
	return nil
}

// requireModule answers 409 and returns false when a module is switched off;
// used by the endpoints that add titles of that kind.
func (s *Server) requireModule(w http.ResponseWriter, module string) bool {
	switch module {
	case moduleMovies:
		if !s.moviesEnabled() {
			writeError(w, http.StatusConflict, "Movies are switched off. Switch them on in Settings > Media types.")
			return false
		}
	case moduleTV:
		if !s.tvEnabled() {
			writeError(w, http.StatusConflict, "TV is switched off. Switch it on in Settings > Media types.")
			return false
		}
	}
	return true
}
