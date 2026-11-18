package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// Second copies: a movie or show kept again in another quality, such as a 4K
// copy next to the 1080p one. Each copy is a library row of its own (its
// own quality profile, status and file) pointing at the main title, and its
// files go in the copies folder set in Settings > Library and files > Folders and file
// names, never next to the main ones. Everything else (searching, grabbing,
// upgrades, removing) works on a copy by its id like on any title.

type copyPayload struct {
	ID          int64  `json:"id"`
	ProfileID   int64  `json:"profileId"`
	ProfileName string `json:"profileName"`
	Monitored   bool   `json:"monitored"`
	// Movies: status, quality and size of the file. Shows: how many episodes
	// are in and how many are listed.
	Status          string `json:"status,omitempty"`
	Quality         string `json:"quality,omitempty"`
	SizeBytes       int64  `json:"sizeBytes,omitempty"`
	EpisodeCount    int    `json:"episodeCount,omitempty"`
	DownloadedCount int    `json:"downloadedCount,omitempty"`
}

type copiesAnswer struct {
	Folder        string        `json:"folder"`        // "" while the copies folder isn't set
	MainProfileID int64         `json:"mainProfileId"` // the profile the main title uses (its own or the default)
	Items         []copyPayload `json:"items"`
}

type addCopyRequest struct {
	ProfileID int64 `json:"profileId"`
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// GET /api/movies/{id}/copies
func (s *Server) handleListMovieCopies(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	main, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	copies, err := s.MovieRepo.ListCopies(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := copiesAnswer{Folder: folderShown(r, []string{s.movieCopiesRoot()}, s.movieCopiesRoot()), Items: []copyPayload{}}
	out.MainProfileID = profiles.resolve(main.ProfileID).ID
	now := time.Now()
	for _, c := range copies {
		out.Items = append(out.Items, copyPayload{
			ID: c.ID, ProfileID: c.ProfileID, ProfileName: profiles.resolve(c.ProfileID).Name, Monitored: c.Monitored,
			Status: string(c.Status), Quality: c.Quality, SizeBytes: cachedFileSize(c.FilePath, now),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/series/{id}/copies
func (s *Server) handleListSeriesCopies(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	main, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	copies, err := s.MovieRepo.ListSeriesCopies(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := copiesAnswer{Folder: folderShown(r, []string{s.tvCopiesRoot()}, s.tvCopiesRoot()), Items: []copyPayload{}}
	out.MainProfileID = profiles.resolveTV(main.ProfileID).ID
	for _, c := range copies {
		out.Items = append(out.Items, copyPayload{
			ID: c.ID, ProfileID: c.ProfileID, ProfileName: profiles.resolve(c.ProfileID).Name, Monitored: c.Monitored,
			EpisodeCount: c.EpisodeCount, DownloadedCount: c.DownloadedCount,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// checkCopyProfile checks the profile asked for a copy: it has to exist and
// differ from the one the main title uses (its own or the default).
func (s *Server) checkCopyProfile(w http.ResponseWriter, want, mainProfile int64, tv bool) bool {
	if want <= 0 {
		writeError(w, http.StatusBadRequest, "Pick the quality profile for the second copy.")
		return false
	}
	if err := s.checkProfileChoice(want); err != nil {
		writeProfileError(w, err)
		return false
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	main := profiles.resolve(mainProfile)
	if tv {
		main = profiles.resolveTV(mainProfile)
	}
	if main.ID == want {
		writeError(w, http.StatusBadRequest, "The main copy already uses that quality profile. Pick another one, such as 4K & over.")
		return false
	}
	return true
}

func writeCopyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrCopyExists):
		writeError(w, http.StatusConflict, "There's already a second copy in that quality.")
	case errors.Is(err, library.ErrCopySameProfile):
		writeError(w, http.StatusBadRequest, "The main copy already uses that quality profile.")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// POST /api/movies/{id}/copies {"profileId": 4} adds a second copy of a
// movie and starts looking for it.
func (s *Server) handleAddMovieCopy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	var req addCopyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if m.CopyOf != 0 {
		writeError(w, http.StatusBadRequest, "That's already a second copy. Add copies from the main movie.")
		return
	}
	if s.movieCopiesRoot() == "" {
		writeError(w, http.StatusConflict, "Choose a folder for second copies first, in Settings > Library and files > Folders.")
		return
	}
	if !s.checkCopyProfile(w, req.ProfileID, m.ProfileID, false) {
		return
	}
	c, err := s.MovieRepo.AddCopy(id, req.ProfileID)
	if err != nil {
		writeCopyError(w, err)
		return
	}
	profiles, _ := s.loadProfiles()
	_ = s.QueueRepo.LogActivity(m.ID, "added", fmt.Sprintf("%s: a second copy in %s was added", m.Title, profiles.resolve(c.ProfileID).Name))
	go s.searchMovieInBackground(c.ID)
	writeJSON(w, http.StatusCreated, copyPayload{ID: c.ID, ProfileID: c.ProfileID, ProfileName: profiles.resolve(c.ProfileID).Name, Monitored: c.Monitored, Status: string(c.Status)})
}

// searchSeriesCopyInBackground looks for a new copy's monitored episodes
// (unlike a newly added show, not every episode whatever its box says).
func (s *Server) searchSeriesCopyInBackground(seriesID int64) {
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return
	}
	s.autoGrabTV(s.targetedTVSearcher(context.Background(), instances, maxTVSearchesPerHunt, false), "add", profiles, tvScope{seriesID: seriesID})
}

// POST /api/series/{id}/copies {"profileId": 4} adds a second copy of a
// show, with the same episodes monitored, and starts looking for them.
func (s *Server) handleAddSeriesCopy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	var req addCopyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sr.CopyOf != 0 {
		writeError(w, http.StatusBadRequest, "That's already a second copy. Add copies from the main show.")
		return
	}
	if s.tvCopiesRoot() == "" {
		writeError(w, http.StatusConflict, "Choose a folder for second copies first, in Settings > Library and files > Folders.")
		return
	}
	if !s.checkCopyProfile(w, req.ProfileID, sr.ProfileID, true) {
		return
	}
	c, err := s.MovieRepo.AddSeriesCopy(id, req.ProfileID)
	if err != nil {
		writeCopyError(w, err)
		return
	}
	profiles, _ := s.loadProfiles()
	_ = s.QueueRepo.LogSeriesActivity(sr.ID, "added", fmt.Sprintf("%s: a second copy in %s was added", sr.Title, profiles.resolve(c.ProfileID).Name))
	go s.searchSeriesCopyInBackground(c.ID)
	writeJSON(w, http.StatusCreated, copyPayload{ID: c.ID, ProfileID: c.ProfileID, ProfileName: profiles.resolve(c.ProfileID).Name, Monitored: c.Monitored,
		EpisodeCount: c.EpisodeCount, DownloadedCount: c.DownloadedCount})
}

func ptr[T any](v T) *T { return &v }
