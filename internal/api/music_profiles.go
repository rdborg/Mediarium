package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/settings"
)

// Music quality profiles: listing, creating, editing and deleting them, and
// which one is the default. The rules are those of the movie and TV
// profiles (name, allowed tiers, a cutoff among them, a fallback chain; the
// default profile and profiles artists use cannot be deleted).

// musicDefaultProfileID is the profile artists without one of their own use:
// the one chosen with musicDefaultProfileId in the settings, else the first.
func (s *Server) musicDefaultProfileID(profiles []music.Profile) int64 {
	v, _ := s.Settings.Get(settings.KeyMusicDefaultProfileID)
	if id, _ := strconv.ParseInt(v, 10, 64); id > 0 {
		for _, p := range profiles {
			if p.ID == id {
				return id
			}
		}
	}
	if len(profiles) > 0 {
		return profiles[0].ID
	}
	return 0
}

// resolveMusicProfile is music.ResolveProfile with the chosen default: the
// profile with that id, or the default profile for 0 or an unknown id.
func (s *Server) resolveMusicProfile(profiles []music.Profile, id int64) (music.Profile, bool) {
	if id == 0 {
		id = s.musicDefaultProfileID(profiles)
	}
	return music.ResolveProfile(profiles, id)
}

// musicProfilePayload is a music quality profile.
type musicProfilePayload struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Allowed        []string `json:"allowed"`
	Cutoff         string   `json:"cutoff"`
	UpgradeAllowed bool     `json:"upgradeAllowed"`
	Fallback       []int64  `json:"fallback"`
	Default        bool     `json:"default"`
	InUse          int      `json:"inUse"` // artists that have this profile chosen
}

func toMusicProfilePayload(p music.Profile, isDefault bool, inUse int) musicProfilePayload {
	allowed := make([]string, len(p.Allowed))
	for i, t := range p.Allowed {
		allowed[i] = string(t)
	}
	fallback := p.Fallback
	if fallback == nil {
		fallback = []int64{}
	}
	return musicProfilePayload{ID: p.ID, Name: p.Name, Allowed: allowed, Cutoff: string(p.Cutoff), UpgradeAllowed: p.UpgradeAllowed,
		Fallback: fallback, Default: isDefault, InUse: inUse}
}

// handleMusicProfiles lists the music quality profiles, in the order they
// were made; the one with default true is used by artists that have none of
// their own.
func (s *Server) handleMusicProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.MusicRepo.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	usage, err := s.MusicRepo.ProfileUsage()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	def := s.musicDefaultProfileID(profiles)
	out := make([]musicProfilePayload, len(profiles))
	for i, p := range profiles {
		out[i] = toMusicProfilePayload(p, p.ID == def, usage[p.ID])
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMusicTiers lists the music quality tiers, worst to best, for
// building a profile.
func (s *Server) handleMusicTiers(w http.ResponseWriter, r *http.Request) {
	tiers := music.AllTiers()
	out := make([]string, len(tiers))
	for i, t := range tiers {
		out[i] = string(t)
	}
	writeJSON(w, http.StatusOK, out)
}

type musicProfileRequest struct {
	Name           string   `json:"name"`
	Allowed        []string `json:"allowed"`
	Cutoff         string   `json:"cutoff"`
	UpgradeAllowed bool     `json:"upgradeAllowed"`
	Fallback       []int64  `json:"fallback"`
}

func (req musicProfileRequest) toProfile(id int64) music.Profile {
	allowed := make([]music.Tier, len(req.Allowed))
	for i, t := range req.Allowed {
		allowed[i] = music.Tier(t)
	}
	return music.Profile{ID: id, Name: req.Name, Allowed: allowed, Cutoff: music.Tier(req.Cutoff), UpgradeAllowed: req.UpgradeAllowed, Fallback: req.Fallback}
}

func writeMusicProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, music.ErrProfileInvalid):
		writeError(w, http.StatusBadRequest, problemText(err, music.ErrProfileInvalid))
	case errors.Is(err, music.ErrProfileInUse):
		writeError(w, http.StatusConflict, problemText(err, music.ErrProfileInUse))
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "That music profile no longer exists.")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// handleCreateMusicProfile makes a music quality profile (administrators):
// {name, allowed[], cutoff, upgradeAllowed, fallback[]}.
func (s *Server) handleCreateMusicProfile(w http.ResponseWriter, r *http.Request) {
	var req musicProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if rejectBad(w, checkMusicProfileRequest(req)) {
		return
	}
	created, err := s.MusicRepo.CreateProfile(req.toProfile(0))
	if err != nil {
		writeMusicProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMusicProfilePayload(created, false, 0))
}

// handleUpdateMusicProfile changes a music quality profile (administrators).
// Leaving out fallback keeps the saved chain.
func (s *Server) handleUpdateMusicProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "profile")
	if !ok {
		return
	}
	var req musicProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if rejectBad(w, checkMusicProfileRequest(req)) {
		return
	}
	p := req.toProfile(id)
	if req.Fallback == nil {
		saved, err := s.MusicRepo.GetProfile(id)
		if err != nil {
			writeMusicProfileError(w, err)
			return
		}
		p.Fallback = saved.Fallback
	}
	updated, err := s.MusicRepo.UpdateProfile(p)
	if err != nil {
		writeMusicProfileError(w, err)
		return
	}
	all, _ := s.MusicRepo.ListProfiles()
	usage, _ := s.MusicRepo.ProfileUsage()
	writeJSON(w, http.StatusOK, toMusicProfilePayload(updated, updated.ID == s.musicDefaultProfileID(all), usage[updated.ID]))
}

// handleDeleteMusicProfile removes a music quality profile (administrators)
// that is not the default and that no artist uses (409 otherwise).
func (s *Server) handleDeleteMusicProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "profile")
	if !ok {
		return
	}
	all, err := s.MusicRepo.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(all) <= 1 && len(all) > 0 && all[0].ID == id {
		writeMusicProfileError(w, fmt.Errorf("%w: it is the only profile", music.ErrProfileInUse))
		return
	}
	if err := s.MusicRepo.DeleteProfile(id, s.musicDefaultProfileID(all)); err != nil {
		writeMusicProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// checkMusicProfileChoice validates an assignment target: 0 (the default) or
// a music profile that exists.
func (s *Server) checkMusicProfileChoice(id int64) error {
	if id == 0 {
		return nil
	}
	if _, err := s.MusicRepo.GetProfile(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: that profile doesn't exist", music.ErrProfileInvalid)
		}
		return err
	}
	return nil
}

// musicDefaultProfileIDNow is the effective default music profile id (0 when
// there are no profiles), for the settings answer.
func (s *Server) musicDefaultProfileIDNow() int64 {
	profiles, err := s.MusicRepo.ListProfiles()
	if err != nil {
		return 0
	}
	return s.musicDefaultProfileID(profiles)
}
