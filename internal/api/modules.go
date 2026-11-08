package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/rdborg/mediarium/internal/settings"
)

// The modules switchboard: which kinds of media Mediarium looks after.
// Movies and TV are on by default; music, ebooks and audiobooks are off until
// switched on. Turning
// a module off hides its pages and stops its automation (no searches, RSS
// sync or automatic grabs) and its add endpoints answer 409; nothing that
// already exists is deleted or changed, and downloads already running finish.

const (
	moduleMovies     = "movies"
	moduleTV         = "tv"
	moduleMusic      = "music"
	moduleEbooks     = "ebooks"
	moduleAudiobooks = "audiobooks"
	moduleAnime      = "anime"
	moduleLifecycle  = "lifecycle"
	moduleArchiver   = "archiver"
)

var (
	errComingSoon = errors.New("Coming soon")
	errNoModules  = errors.New("At least one media type has to stay switched on.")
	// errAnimeNeedsTV: anime shows are shows, so the anime library lives inside TV.
	errAnimeNeedsTV = errors.New("Anime is part of TV. Switch TV on first.")
	// errArchiverNotReady wraps what the Channel archiver still needs.
	errArchiverNotReady = errors.New("the Channel archiver isn't set up yet")
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

func (s *Server) ebooksEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyModulesEbooks)
	return v == "1"
}

func (s *Server) audiobooksEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyModulesAudiobooks)
	return v == "1"
}

// animeEnabled reports whether anime has its own library (anime.go). It is
// part of TV, so it is off while TV is.
func (s *Server) animeEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyModulesAnime)
	return v == "1" && s.tvEnabled()
}

// lifecycleEnabled reports whether the library lifecycle rules are switched on
// (lifecycle.go). It is off until an administrator switches it on.
func (s *Server) lifecycleEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyModulesLifecycle)
	return v == "1"
}

// archiverEnabled (archiver.go) reports whether the Channel archiver is on.

// booksEnabled reports whether either book module is on.
func (s *Server) booksEnabled() bool { return s.ebooksEnabled() || s.audiobooksEnabled() }

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
	// Anime is the anime library inside TV (its own tab, folder, profile and names).
	Anime moduleState `json:"anime"`
	// Lifecycle is the library lifecycle rules and the Leaving soon shelf. It is
	// not a kind of media: it only looks after what the library already holds.
	Lifecycle moduleState `json:"lifecycle"`
	// Archiver is the Channel archiver (archiver.go). It follows channels with a
	// yt-dlp file the administrator supplies, and stays off until its warning
	// is ticked and that file answers.
	Archiver moduleState `json:"archiver"`
	// SubtitlesEnabled says whether the subtitles switch is on, so every
	// account's pages can show or hide the subtitle parts. It is not a media
	// type and is not changed here (see PUT /api/settings).
	SubtitlesEnabled bool `json:"subtitlesEnabled"`
	// TrailerBackground is the server-wide choice to play the trailer behind
	// movie and show pages. A browser can still choose for itself.
	TrailerBackground bool `json:"trailerBackground"`
	// TrailerSound starts that trailer with its sound on.
	TrailerSound bool `json:"trailerSound"`
	// ArtworkProxy says artwork is loaded through this server instead of
	// straight from the picture host (Settings > Info, lists and subtitles).
	ArtworkProxy bool `json:"artworkProxy"`
	// TorrentsOn says whether torrent releases can be downloaded at all (the
	// built-in client, a debrid service or a download client), so every
	// account's release lists can say when they can't. Only an administrator
	// can change it (Settings > Downloading > Torrents).
	TorrentsOn bool `json:"torrentsOn"`
}

func (s *Server) modulesPayload() modulesPayload {
	return modulesPayload{
		Movies:     moduleState{Enabled: s.moviesEnabled(), Available: true},
		TV:         moduleState{Enabled: s.tvEnabled(), Available: true},
		Music:      moduleState{Enabled: s.musicEnabled(), Available: true},
		Audiobooks: moduleState{Enabled: s.audiobooksEnabled(), Available: true},
		Ebooks:     moduleState{Enabled: s.ebooksEnabled(), Available: true},
		Anime:      moduleState{Enabled: s.animeEnabled(), Available: true},
		Lifecycle:  moduleState{Enabled: s.lifecycleEnabled(), Available: true},
		Archiver:   moduleState{Enabled: s.archiverEnabled(), Available: true},

		SubtitlesEnabled: s.subtitlesEnabled(),

		TrailerBackground: s.trailerBackground(),
		TrailerSound:      s.trailerSound(),
		ArtworkProxy:      s.artworkProxy(),

		TorrentsOn: s.torrentReleasesOn(),
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
	Anime      *bool `json:"anime"`
	Lifecycle  *bool `json:"lifecycle"`
	Archiver   *bool `json:"archiver"`
}

// handlePutModules switches modules on or off (administrators) and answers
// with the new state. A change that would leave nothing on is refused (400
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
	if errors.Is(err, errComingSoon) || errors.Is(err, errNoModules) || errors.Is(err, errAnimeNeedsTV) || errors.Is(err, errArchiverNotReady) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// setModules applies a change after checking the whole result: nothing is
// written unless every rule holds.
func (s *Server) setModules(c moduleChanges) error {
	t := &settingsSave{}
	if err := s.setModulesIn(t, c); err != nil {
		return err
	}
	return s.commitSettings(t)
}

// setModulesIn is setModules into an update that is saved with other settings.
func (s *Server) setModulesIn(st settingSetter, c moduleChanges) error {
	pick := func(change *bool, current bool) bool {
		if change == nil {
			return current
		}
		return *change
	}
	movies, tv, music := pick(c.Movies, s.moviesEnabled()), pick(c.TV, s.tvEnabled()), pick(c.Music, s.musicEnabled())
	ebooks, audiobooks := pick(c.Ebooks, s.ebooksEnabled()), pick(c.Audiobooks, s.audiobooksEnabled())
	if !movies && !tv && !music && !ebooks && !audiobooks {
		return errNoModules
	}
	stored, _ := s.Settings.Get(settings.KeyModulesAnime)
	anime := pick(c.Anime, stored == "1")
	if c.Anime != nil && anime && !tv {
		return errAnimeNeedsTV
	}
	if c.Archiver != nil && *c.Archiver && !s.archiverEnabled() {
		// Switching it on needs the ticked warning and a yt-dlp file that answers.
		if err := s.archiverReady(context.Background()); err != nil {
			return &archiverGateError{err}
		}
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
		{c.Ebooks, settings.KeyModulesEbooks, ebooks},
		{c.Audiobooks, settings.KeyModulesAudiobooks, audiobooks},
		{c.Anime, settings.KeyModulesAnime, anime},
		{c.Lifecycle, settings.KeyModulesLifecycle, pick(c.Lifecycle, false)},
		{c.Archiver, settings.KeyModulesArchiver, pick(c.Archiver, false)},
	} {
		if w.changed == nil {
			continue
		}
		if err := st.Set(w.key, flag(w.on), false); err != nil {
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
	case moduleEbooks:
		if !s.ebooksEnabled() {
			writeError(w, http.StatusConflict, "Ebooks are switched off. Switch them on in Settings > Media types.")
			return false
		}
	case moduleAudiobooks:
		if !s.audiobooksEnabled() {
			writeError(w, http.StatusConflict, "Audiobooks are switched off. Switch them on in Settings > Media types.")
			return false
		}
	}
	return true
}

// trailerBackground says whether the trailer plays behind movie and show pages
// for everyone (Settings > Info, lists and subtitles > Movie and show info). It
// is on until an administrator switches it off, so it works out of the box; the
// video comes from YouTube, which the setting and the docs say, and a browser
// can switch it off for itself with the button on the page.
// trailerSound says whether the trailer behind a title page starts with its
// sound on. It is off until an administrator switches it on.
func (s *Server) trailerSound() bool {
	v, _ := s.Settings.Get(settings.KeyTrailerSound)
	return v == "1"
}

// artworkProxy says whether posters and backdrops are loaded through this
// server. It is on until an administrator switches it off.
func (s *Server) artworkProxy() bool {
	v, _ := s.Settings.Get(settings.KeyArtworkProxy)
	return v != "0"
}

func (s *Server) trailerBackground() bool {
	v, _ := s.Settings.Get(settings.KeyTrailerBackground)
	return v != "0"
}

// archiverGateError is what stopped the Channel archiver from being switched on,
// in the words to show.
type archiverGateError struct{ cause error }

func (e *archiverGateError) Error() string { return e.cause.Error() }
func (e *archiverGateError) Unwrap() error { return errArchiverNotReady }
