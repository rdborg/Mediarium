package api

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/settings"
)

// The anime library (V4 D26, from an idea sent through the website: "anime
// and all its settings in a separate library"). Anime shows stay shows: the
// same episodes, searches, numbering (absolute numbers, TheXEM, the anime
// lists) and downloads as before. With the anime module on (Settings > Media
// types, part of TV) they also get a place of their own:
//
//   - their own tab in the Library, and the TV tab shows the other shows;
//   - their own folder: an anime show added while the module is on goes to
//     the anime folder instead of the TV folder (one already in the library
//     stays where it is; Move to another folder moves it);
//   - their own default quality profile, given to an anime show when it is
//     added without one;
//   - their own episode file names, which can use {Absolute:000}, the number
//     counted from the very first episode, the way anime releases are named.
//
// The anime folder is one of the TV folders, so everything that knows about
// TV folders (moving, rescans, Health, the folder choice) knows about it.

// animeRoot is the folder new anime shows go to, or "" when the module is off
// or no anime folder is set.
func (s *Server) animeRoot() string {
	if !s.animeEnabled() {
		return ""
	}
	v, _ := s.Settings.Get(settings.KeyAnimePath)
	return strings.TrimSpace(v)
}

// animeFolderFor is where a show being added goes: an anime show added
// without a folder of its own choosing, or into the main TV folder, goes to
// the anime folder when there is one.
func (s *Server) animeFolderFor(seriesType, rootPath string) string {
	root := s.animeRoot()
	if seriesType != library.SeriesAnime || root == "" {
		return rootPath
	}
	if rootPath == "" || filepath.Clean(rootPath) == filepath.Clean(s.tvRoot()) {
		return root
	}
	return rootPath
}

// animeMovieFolderFor is where a film being added goes: an anime film added
// without a folder of its own choosing, or into the main movie folder, goes
// to the anime films folder when there is one.
func (s *Server) animeMovieFolderFor(m metadata.Movie, rootPath string) string {
	if !s.animeEnabled() || !m.IsAnime() {
		return rootPath
	}
	v, _ := s.Settings.Get(settings.KeyAnimeMoviesPath)
	root := strings.TrimSpace(v)
	if root == "" {
		return rootPath
	}
	if rootPath == "" || filepath.Clean(rootPath) == filepath.Clean(s.moviesRoot()) || filepath.Clean(rootPath) == filepath.Clean(s.newMovieFolder()) {
		return root
	}
	return rootPath
}

// animeDefaultProfileID is the quality profile an anime show added without
// one gets, or 0 for the TV default.
func (s *Server) animeDefaultProfileID() int64 {
	if !s.animeEnabled() {
		return 0
	}
	v, _ := s.Settings.Get(settings.KeyDefaultAnimeProfileID)
	id, _ := strconv.ParseInt(v, 10, 64)
	return id
}

// animeEpisodeFormat is the file name format for an anime show's episodes, or
// "" to use the one for every show.
func (s *Server) animeEpisodeFormat(sr library.Series) string {
	if sr.SeriesType != library.SeriesAnime || !s.animeEnabled() {
		return ""
	}
	v, _ := s.Settings.Get(settings.KeyAnimeEpisodeFormat)
	return strings.TrimSpace(v)
}

// absoluteNumber is an episode's number counted from the show's first episode
// (specials left out), or 0 when it isn't one of the counted episodes.
func (s *Server) absoluteNumber(sr library.Series, ep library.Episode) int {
	if ep.Season <= 0 || sr.ID == 0 {
		return 0
	}
	eps, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return 0
	}
	for i, e := range library.AbsoluteOrder(eps) {
		if e.Season == ep.Season && e.Episode == ep.Episode {
			return i + 1
		}
	}
	return 0
}

// ---- Settings > Media types > Anime library

type animeSettings struct {
	Enabled       bool   `json:"enabled"`       // the module (read only here: PUT /api/modules switches it)
	Path          string `json:"path"`          // where new anime shows go; "" = the TV folder
	MoviesPath    string `json:"moviesPath"`    // where new anime films go; "" = where other movies go
	EpisodeFormat string `json:"episodeFormat"` // anime episode file names; "" = the format for every show
	ProfileID     int64  `json:"profileId"`     // default quality profile for anime; 0 = the TV default
	// SuggestedFormat is a format that names anime the way their releases do.
	SuggestedFormat string `json:"suggestedFormat"`
}

func (s *Server) animeSettingsNow() animeSettings {
	path, _ := s.Settings.Get(settings.KeyAnimePath)
	moviesPath, _ := s.Settings.Get(settings.KeyAnimeMoviesPath)
	format, _ := s.Settings.Get(settings.KeyAnimeEpisodeFormat)
	v, _ := s.Settings.Get(settings.KeyDefaultAnimeProfileID)
	id, _ := strconv.ParseInt(v, 10, 64)
	return animeSettings{Enabled: s.animeEnabled(), Path: path, MoviesPath: moviesPath, EpisodeFormat: format, ProfileID: id, SuggestedFormat: organizer.AnimeFormat}
}

// handleGetAnimeSettings answers the anime library's folder, file names and
// default quality profile.
func (s *Server) handleGetAnimeSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.animeSettingsNow())
}

// handlePutAnimeSettings saves the anime library's folder, file names and
// default quality profile. Each field is checked first and nothing is saved
// when one is wrong.
func (s *Server) handlePutAnimeSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path          *string `json:"path"`
		MoviesPath    *string `json:"moviesPath"`
		EpisodeFormat *string `json:"episodeFormat"`
		ProfileID     *int64  `json:"profileId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	tx := &settingsSave{}
	chosen := ""
	if req.Path != nil {
		p := strings.TrimSpace(*req.Path)
		if p != "" {
			clean, problem := s.checkExtraFolders([]string{p}, "", s.moviesRoot(), s.tvRoot(), "anime")
			if problem != "" {
				writeError(w, http.StatusBadRequest, problem)
				return
			}
			p = clean[0]
			if s.changed(settings.KeyAnimePath, p) {
				chosen = p
			}
		}
		_ = tx.Set(settings.KeyAnimePath, p, false)
	}
	chosenMovies := ""
	if req.MoviesPath != nil {
		p := strings.TrimSpace(*req.MoviesPath)
		if p != "" {
			clean, problem := s.checkExtraFolders([]string{p}, "", s.moviesRoot(), s.tvRoot(), "anime films")
			if problem != "" {
				writeError(w, http.StatusBadRequest, problem)
				return
			}
			p = clean[0]
			if s.changed(settings.KeyAnimeMoviesPath, p) {
				chosenMovies = p
			}
		}
		_ = tx.Set(settings.KeyAnimeMoviesPath, p, false)
	}
	if req.EpisodeFormat != nil {
		f := strings.TrimSpace(*req.EpisodeFormat)
		if msg := organizer.CheckFormat(f, organizer.TokenTV); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		_ = tx.Set(settings.KeyAnimeEpisodeFormat, f, false)
	}
	if req.ProfileID != nil {
		v := ""
		if *req.ProfileID != 0 {
			if err := s.checkProfileChoice(*req.ProfileID); err != nil {
				writeProfileError(w, err)
				return
			}
			v = strconv.FormatInt(*req.ProfileID, 10)
		}
		_ = tx.Set(settings.KeyDefaultAnimeProfileID, v, false)
	}
	if err := s.commitSettings(tx); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	if chosen != "" {
		s.confirmLibraryRoot(chosen, true)
	}
	if chosenMovies != "" {
		s.confirmLibraryRoot(chosenMovies, true)
	}
	writeJSON(w, http.StatusOK, s.animeSettingsNow())
}
