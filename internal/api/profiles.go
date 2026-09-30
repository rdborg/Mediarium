package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
)

// initProfiles seeds the built-in profiles on first start (or brings an older
// install's untouched presets up to date, once) and makes sure a default
// profile is set, carrying over the old single global preset choice
// (library.quality_profile) if the user had one.
func (s *Server) initProfiles() error {
	seeded := 0
	if v, _ := s.Settings.Get(settings.KeyPresetsVersion); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("read %s: %w", settings.KeyPresetsVersion, err)
		}
		seeded = n
	}
	profiles, err := s.QualityRepo.SeedPresets(seeded)
	if err != nil {
		return fmt.Errorf("seed quality presets: %w", err)
	}
	if seeded < quality.PresetsVersion {
		if err := s.Settings.Set(settings.KeyPresetsVersion, strconv.Itoa(quality.PresetsVersion), false); err != nil {
			return err
		}
	}
	if len(profiles) == 0 {
		return nil // every profile deleted by hand; items fall back to the built-in default
	}
	if id := s.defaultProfileID(); id != 0 {
		for _, p := range profiles {
			if p.ID == id {
				return nil
			}
		}
	}

	wantName := quality.Presets()[quality.DefaultPreset].Name
	if legacy, _ := s.Settings.Get(settings.KeyQualityProfile); legacy != "" {
		if name, ok := quality.PresetName(legacy); ok {
			wantName = name
		}
	}
	chosen := profiles[0]
	for _, p := range profiles {
		if p.Name == wantName {
			chosen = p
			break
		}
	}
	return s.Settings.Set(settings.KeyDefaultProfileID, strconv.FormatInt(chosen.ID, 10), false)
}

// defaultProfileID returns the default profile's id, or 0 if unset.
func (s *Server) defaultProfileID() int64 {
	v, _ := s.Settings.Get(settings.KeyDefaultProfileID)
	id, _ := strconv.ParseInt(v, 10, 64)
	return id
}

// profileSet is every stored profile plus the default, loaded once per
// automation run so each item can be judged against its own profile.
type profileSet struct {
	byID map[int64]quality.Profile
	def  quality.Profile
	// language is the preferred audio language, given to every profile
	// resolve returns.
	language string
}

func (s *Server) loadProfiles() (profileSet, error) {
	list, err := s.QualityRepo.List()
	if err != nil {
		return profileSet{}, err
	}
	ps := profileSet{byID: map[int64]quality.Profile{}, def: quality.Presets()[quality.DefaultPreset], language: s.preferredLanguage()}
	for _, p := range list {
		ps.byID[p.ID] = p
	}
	if p, ok := ps.byID[s.defaultProfileID()]; ok {
		ps.def = p
	} else if len(list) > 0 {
		ps.def = list[0]
	}
	return ps, nil
}

// resolve returns the profile for an item; 0 or an unknown id means default.
// Its fallback profiles are filled in (FallbackProfiles), in order.
func (ps profileSet) resolve(id int64) quality.Profile {
	p, ok := ps.byID[id]
	if !ok {
		p = ps.def
	}
	p.FallbackProfiles = nil
	p.Language = ps.language
	for _, fid := range p.Fallback {
		if f, ok := ps.byID[fid]; ok && fid != p.ID {
			f.FallbackProfiles = nil // a fallback's own fallbacks are not followed
			f.Language = ps.language
			p.FallbackProfiles = append(p.FallbackProfiles, f)
		}
	}
	return p
}

type profilePayload struct {
	ID             int64              `json:"id"`
	Name           string             `json:"name"`
	Allowed        []string           `json:"allowed"`
	Cutoff         string             `json:"cutoff"`
	UpgradeAllowed bool               `json:"upgradeAllowed"`
	MustContain    []string           `json:"mustContain"`
	MustNotContain []string           `json:"mustNotContain"`
	Preferred      []preferredPayload `json:"preferred"`
	// Fallback is the ordered list of profile ids an automatic search tries
	// when nothing is acceptable to this profile ([] = none).
	Fallback []int64 `json:"fallback"`
	InUse    int     `json:"inUse"`
}

type preferredPayload struct {
	Term  string `json:"term"`
	Score int    `json:"score"`
}

type profileListPayload struct {
	Profiles  []profilePayload `json:"profiles"`
	Tiers     []string         `json:"tiers"`
	DefaultID int64            `json:"defaultId"`
}

func toProfilePayload(p quality.Profile, inUse int) profilePayload {
	allowed := make([]string, len(p.Allowed))
	for i, t := range p.Allowed {
		allowed[i] = string(t)
	}
	prefs := make([]preferredPayload, len(p.Preferred))
	for i, pr := range p.Preferred {
		prefs[i] = preferredPayload{Term: pr.Term, Score: pr.Score}
	}
	must, mustNot := p.MustContain, p.MustNotContain
	if must == nil {
		must = []string{}
	}
	if mustNot == nil {
		mustNot = []string{}
	}
	fallback := p.Fallback
	if fallback == nil {
		fallback = []int64{}
	}
	return profilePayload{ID: p.ID, Name: p.Name, Allowed: allowed, Cutoff: string(p.Cutoff), UpgradeAllowed: p.UpgradeAllowed,
		MustContain: must, MustNotContain: mustNot, Preferred: prefs, Fallback: fallback, InUse: inUse}
}

type profileRequest struct {
	Name           string             `json:"name"`
	Allowed        []string           `json:"allowed"`
	Cutoff         string             `json:"cutoff"`
	UpgradeAllowed bool               `json:"upgradeAllowed"`
	MustContain    []string           `json:"mustContain"`
	MustNotContain []string           `json:"mustNotContain"`
	Preferred      []preferredPayload `json:"preferred"`
	// Fallback replaces the profile's fallback list; left out (null), an
	// update keeps the saved one. Ids that do not exist, or the profile's
	// own id, are ignored.
	Fallback []int64 `json:"fallback"`
}

func (req profileRequest) toProfile(id int64) quality.Profile {
	allowed := make([]quality.Tier, len(req.Allowed))
	for i, t := range req.Allowed {
		allowed[i] = quality.Tier(t)
	}
	prefs := make([]quality.Preferred, len(req.Preferred))
	for i, pr := range req.Preferred {
		prefs[i] = quality.Preferred{Term: pr.Term, Score: pr.Score}
	}
	return quality.Profile{
		ID: id, Name: req.Name, Allowed: allowed, Cutoff: quality.Tier(req.Cutoff), UpgradeAllowed: req.UpgradeAllowed,
		MustContain: req.MustContain, MustNotContain: req.MustNotContain, Preferred: prefs, Fallback: req.Fallback,
	}
}

func (s *Server) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	list, err := s.QualityRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	usage, err := s.QualityRepo.Usage()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := profileListPayload{Profiles: []profilePayload{}, Tiers: []string{}, DefaultID: s.defaultProfileID()}
	for _, p := range list {
		out.Profiles = append(out.Profiles, toProfilePayload(p, usage[p.ID]))
	}
	for _, t := range quality.AllTiers() {
		out.Tiers = append(out.Tiers, string(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var req profileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if rejectBad(w, checkQualityProfileRequest(req)) {
		return
	}
	created, err := s.QualityRepo.Create(req.toProfile(0))
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toProfilePayload(created, 0))
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid profile ID.")
		return
	}
	var req profileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if rejectBad(w, checkQualityProfileRequest(req)) {
		return
	}
	p := req.toProfile(id)
	if req.Fallback == nil {
		saved, err := s.QualityRepo.Get(id)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		p.Fallback = saved.Fallback
	}
	updated, err := s.QualityRepo.Update(p)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toProfilePayload(updated, 0))
}

func (s *Server) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid profile ID.")
		return
	}
	if err := s.QualityRepo.Delete(id, s.defaultProfileID()); err != nil {
		writeProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

func writeProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, quality.ErrInvalid):
		writeError(w, http.StatusBadRequest, problemText(err, quality.ErrInvalid))
	case errors.Is(err, quality.ErrInUse):
		writeError(w, http.StatusConflict, problemText(err, quality.ErrInUse))
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "That quality profile no longer exists.")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

type setProfileRequest struct {
	ProfileID int64 `json:"profileId"` // 0 = use the default profile
}

// checkProfileChoice validates an assignment target: 0 (default) or a real
// stored profile.
func (s *Server) checkProfileChoice(id int64) error {
	if id == 0 {
		return nil
	}
	if _, err := s.QualityRepo.Get(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: that profile doesn't exist", quality.ErrInvalid)
		}
		return err
	}
	return nil
}

func (s *Server) handleSetMovieProfile(w http.ResponseWriter, r *http.Request) {
	s.setItemProfile(w, r, "movie", s.MovieRepo.SetProfile)
}

func (s *Server) handleSetSeriesProfile(w http.ResponseWriter, r *http.Request) {
	s.setItemProfile(w, r, "series", s.MovieRepo.SetSeriesProfile)
}

func (s *Server) setItemProfile(w http.ResponseWriter, r *http.Request, what string, set func(id, profileID int64) error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid "+what+" ID.")
		return
	}
	var req setProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if err := s.checkProfileChoice(req.ProfileID); err != nil {
		writeProfileError(w, err)
		return
	}
	if err := set(id, req.ProfileID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
