package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"

	"github.com/rdborg/mediarium/internal/library"
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

// defaultTVProfileID returns the profile shows with none of their own use, or
// 0 when none is set (shows then use the same default as movies).
func (s *Server) defaultTVProfileID() int64 {
	v, _ := s.Settings.Get(settings.KeyDefaultTVProfileID)
	id, _ := strconv.ParseInt(v, 10, 64)
	return id
}

// defaultTVProfileOrDefault is the profile a show with none of its own uses:
// the default for shows when one is set, else the default for everything.
func (s *Server) defaultTVProfileOrDefault() int64 {
	if id := s.defaultTVProfileID(); id != 0 {
		return id
	}
	return s.defaultProfileID()
}

// profileSet is every stored profile plus the default, loaded once per
// automation run so each item can be judged against its own profile.
type profileSet struct {
	byID map[int64]quality.Profile
	def  quality.Profile
	// defTV is the default for shows; nil means shows use def.
	defTV *quality.Profile
	// language is the preferred audio language, given to every profile
	// resolve returns.
	language string
	// languageOnly is the "Only take releases in these languages" switch,
	// given to every profile the same way.
	languageOnly bool
	// ignoreExtraLanguages is the "Extra languages don't change the ranking"
	// switch, given to every profile the same way.
	ignoreExtraLanguages bool
}

func (s *Server) loadProfiles() (profileSet, error) {
	list, err := s.QualityRepo.List()
	if err != nil {
		return profileSet{}, err
	}
	formats, err := s.FormatRepo.List()
	if err != nil {
		return profileSet{}, err
	}
	byFormat := map[int64]*quality.CustomFormat{}
	for i := range formats {
		byFormat[formats[i].ID] = &formats[i]
	}
	ps := profileSet{byID: map[int64]quality.Profile{}, def: quality.Presets()[quality.DefaultPreset], language: s.preferredLanguage(), languageOnly: s.languageOnly(), ignoreExtraLanguages: s.languageIgnoreExtra()}
	for _, p := range list {
		ps.byID[p.ID] = quality.AttachFormats(p, byFormat)
	}
	if p, ok := ps.byID[s.defaultProfileID()]; ok {
		ps.def = p
	} else if len(list) > 0 {
		ps.def = ps.byID[list[0].ID] // the one with its custom formats attached
	}
	if p, ok := ps.byID[s.defaultTVProfileID()]; ok {
		ps.defTV = &p
	}
	return ps, nil
}

// resolve returns the profile for a movie; 0 or an unknown id means the
// default. Its fallback profiles are filled in (FallbackProfiles), in order.
func (ps profileSet) resolve(id int64) quality.Profile {
	return ps.resolveWith(id, ps.def)
}

// resolveTV is resolve for a show: 0 or an unknown id means the default for
// shows when one is set, else the same default movies use.
func (ps profileSet) resolveTV(id int64) quality.Profile {
	if ps.defTV != nil {
		return ps.resolveWith(id, *ps.defTV)
	}
	return ps.resolveWith(id, ps.def)
}

func (ps profileSet) resolveWith(id int64, def quality.Profile) quality.Profile {
	p, ok := ps.byID[id]
	if !ok {
		p = def
	}
	p.FallbackProfiles = nil
	p.Language, p.LanguageOnly, p.IgnoreExtraLanguages = ps.language, ps.languageOnly, ps.ignoreExtraLanguages
	for _, fid := range p.Fallback {
		if f, ok := ps.byID[fid]; ok && fid != p.ID {
			f.FallbackProfiles = nil // a fallback's own fallbacks are not followed
			f.Language, f.LanguageOnly, f.IgnoreExtraLanguages = ps.language, ps.languageOnly, ps.ignoreExtraLanguages
			p.FallbackProfiles = append(p.FallbackProfiles, f)
		}
	}
	return p
}

// qualityItemPayload is one rank of a profile's own quality order: a quality,
// or a named group of them.
type qualityItemPayload struct {
	Name  string   `json:"name,omitempty"`
	Tiers []string `json:"tiers"`
}

func toQualityItems(in []qualityItemPayload) []quality.QualityItem {
	out := make([]quality.QualityItem, len(in))
	for i, it := range in {
		out[i].Name = it.Name
		for _, t := range it.Tiers {
			out[i].Tiers = append(out[i].Tiers, quality.Tier(t))
		}
	}
	return out
}

func toQualityItemPayloads(in []quality.QualityItem) []qualityItemPayload {
	out := make([]qualityItemPayload, len(in))
	for i, it := range in {
		out[i] = qualityItemPayload{Name: it.Name, Tiers: make([]string, len(it.Tiers))}
		for j, t := range it.Tiers {
			out[i].Tiers[j] = string(t)
		}
	}
	return out
}

type profilePayload struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Allowed []string `json:"allowed"`
	Cutoff  string   `json:"cutoff"`
	// QualityOrder is the profile's own order of qualities and groups, best
	// first; [] means the built-in order.
	QualityOrder   []qualityItemPayload `json:"qualityOrder"`
	UpgradeAllowed bool                 `json:"upgradeAllowed"`
	MustContain    []string             `json:"mustContain"`
	MustNotContain []string             `json:"mustNotContain"`
	Preferred      []preferredPayload   `json:"preferred"`
	// Fallback is the ordered list of profile ids an automatic search tries
	// when nothing is acceptable to this profile ([] = none).
	Fallback []int64 `json:"fallback"`
	InUse    int     `json:"inUse"`
	// MaxSizeGB is the largest release the profile accepts (0 = no limit).
	MaxSizeGB float64 `json:"maxSizeGB"`
	// Custom formats: the score for each format (formats scored 0 are left
	// out), the lowest total a release needs, and the total at which
	// better-scoring releases stop being looked for (0 = never).
	FormatScores      []formatScorePayload `json:"formatScores"`
	MinFormatScore    int                  `json:"minFormatScore"`
	CutoffFormatScore int                  `json:"cutoffFormatScore"`
	// NoSeasonPacks: whole-season releases are never downloaded.
	NoSeasonPacks bool `json:"noSeasonPacks"`
	// SkipBadReleases leaves out whole discs, 3D, upscales, burned-in
	// subtitles and known low-quality re-encoders.
	SkipBadReleases bool `json:"skipBadReleases"`
}

type formatScorePayload struct {
	FormatID int64 `json:"formatId"`
	Score    int   `json:"score"`
}

type preferredPayload struct {
	Term  string `json:"term"`
	Score int    `json:"score"`
}

type profileListPayload struct {
	Profiles  []profilePayload `json:"profiles"`
	Tiers     []string         `json:"tiers"`
	DefaultID int64            `json:"defaultId"`
	// DefaultTVID is the default for shows; 0 = shows use DefaultID.
	DefaultTVID int64 `json:"defaultTvId"`
	// DefaultSources is the downloader choice a title follows when it has none of its own,
	// so members can read it without the administrator-only settings.
	DefaultSources string `json:"defaultSources"`
	// AddMonitored and AddSearchNow are the starting state of the add dialog's
	// "Monitor" and "Start searching now" choices, readable by every member.
	AddMonitored bool `json:"addMonitored"`
	AddSearchNow bool `json:"addSearchNow"`
	// AddKeepBetter is the starting state of the add dialog's "Keep looking for
	// better versions" box (off unless switched on).
	AddKeepBetter bool `json:"addKeepBetter"`
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
	scores := []formatScorePayload{}
	for id, sc := range p.FormatScores {
		scores = append(scores, formatScorePayload{FormatID: id, Score: sc})
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].FormatID < scores[j].FormatID })
	return profilePayload{ID: p.ID, Name: p.Name, Allowed: allowed, Cutoff: string(p.Cutoff), UpgradeAllowed: p.UpgradeAllowed,
		MustContain: must, MustNotContain: mustNot, Preferred: prefs, Fallback: fallback, InUse: inUse, MaxSizeGB: p.MaxSizeGB,
		FormatScores: scores, MinFormatScore: p.MinFormatScore, CutoffFormatScore: p.CutoffFormatScore, NoSeasonPacks: p.NoSeasonPacks,
		SkipBadReleases: p.SkipBadReleases, QualityOrder: toQualityItemPayloads(p.QualityOrder)}
}

type profileRequest struct {
	Name           string             `json:"name"`
	Allowed        []string           `json:"allowed"`
	Cutoff         string             `json:"cutoff"`
	UpgradeAllowed bool               `json:"upgradeAllowed"`
	MustContain    []string           `json:"mustContain"`
	MustNotContain []string           `json:"mustNotContain"`
	Preferred      []preferredPayload `json:"preferred"`
	// QualityOrder is the profile's own order of qualities and groups, best
	// first. [] goes back to the built-in order; left out (null), an update
	// keeps the saved one. When it is set it decides Allowed.
	QualityOrder *[]qualityItemPayload `json:"qualityOrder"`
	// Fallback replaces the profile's fallback list; left out (null), an
	// update keeps the saved one. Ids that do not exist, or the profile's
	// own id, are ignored.
	Fallback []int64 `json:"fallback"`
	// MaxSizeGB is the largest release the profile accepts, in GB (0 = no
	// limit); left out, an update keeps the saved one.
	MaxSizeGB *float64 `json:"maxSizeGB"`
	// The custom-format settings; each left out, an update keeps the saved one.
	FormatScores      []formatScorePayload `json:"formatScores"`
	MinFormatScore    *int                 `json:"minFormatScore"`
	CutoffFormatScore *int                 `json:"cutoffFormatScore"`
	NoSeasonPacks     *bool                `json:"noSeasonPacks"`
	// SkipBadReleases: on for a new profile when left out.
	SkipBadReleases *bool `json:"skipBadReleases"`
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
	p := quality.Profile{
		ID: id, Name: req.Name, Allowed: allowed, Cutoff: quality.Tier(req.Cutoff), UpgradeAllowed: req.UpgradeAllowed,
		MustContain: req.MustContain, MustNotContain: req.MustNotContain, Preferred: prefs, Fallback: req.Fallback,
	}
	if req.QualityOrder != nil {
		p.QualityOrder = toQualityItems(*req.QualityOrder)
	}
	if req.MaxSizeGB != nil {
		p.MaxSizeGB = *req.MaxSizeGB
	}
	if req.FormatScores != nil {
		p.FormatScores = map[int64]int{}
		for _, fs := range req.FormatScores {
			if fs.FormatID > 0 {
				p.FormatScores[fs.FormatID] = fs.Score
			}
		}
	}
	if req.MinFormatScore != nil {
		p.MinFormatScore = *req.MinFormatScore
	}
	if req.CutoffFormatScore != nil {
		p.CutoffFormatScore = *req.CutoffFormatScore
	}
	if req.NoSeasonPacks != nil {
		p.NoSeasonPacks = *req.NoSeasonPacks
	}
	p.SkipBadReleases = req.SkipBadReleases == nil || *req.SkipBadReleases
	return p
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
	out := profileListPayload{Profiles: []profilePayload{}, Tiers: []string{}, DefaultID: s.defaultProfileID(), DefaultTVID: s.defaultTVProfileID(), DefaultSources: s.defaultSources(), AddMonitored: s.addMonitoredDefault(), AddSearchNow: s.addSearchNowDefault(), AddKeepBetter: s.addKeepBetterDefault()}
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
	p := req.toProfile(0)
	if err := s.keepKnownFormats(&p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	created, err := s.QualityRepo.Create(p)
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
	if req.QualityOrder == nil || req.Fallback == nil || req.MaxSizeGB == nil || req.FormatScores == nil || req.MinFormatScore == nil || req.CutoffFormatScore == nil || req.NoSeasonPacks == nil || req.SkipBadReleases == nil {
		saved, err := s.QualityRepo.Get(id)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		if req.QualityOrder == nil {
			p.QualityOrder = saved.QualityOrder
		}
		if req.Fallback == nil {
			p.Fallback = saved.Fallback
		}
		if req.MaxSizeGB == nil {
			p.MaxSizeGB = saved.MaxSizeGB
		}
		if req.FormatScores == nil {
			p.FormatScores = saved.FormatScores
		}
		if req.MinFormatScore == nil {
			p.MinFormatScore = saved.MinFormatScore
		}
		if req.CutoffFormatScore == nil {
			p.CutoffFormatScore = saved.CutoffFormatScore
		}
		if req.NoSeasonPacks == nil {
			p.NoSeasonPacks = saved.NoSeasonPacks
		}
		if req.SkipBadReleases == nil {
			p.SkipBadReleases = saved.SkipBadReleases
		}
	}
	if err := s.keepKnownFormats(&p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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
	if id == s.defaultTVProfileID() {
		writeError(w, http.StatusConflict, "That profile is the default for TV. Pick another default for TV first.")
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
	s.setItemProfile(w, r, "movie", s.MovieRepo.SetProfile, s.movieProfileChanged)
}

func (s *Server) handleSetSeriesProfile(w http.ResponseWriter, r *http.Request) {
	s.setItemProfile(w, r, "series", s.MovieRepo.SetSeriesProfile, s.seriesProfileChanged)
}

func (s *Server) setItemProfile(w http.ResponseWriter, r *http.Request, what string, set func(id, profileID int64) error, after func(id, profileID int64) bool) {
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
	writeJSON(w, http.StatusOK, map[string]bool{"searching": after(id, req.ProfileID)})
}

// movieProfileChanged starts the search for a movie whose file the new
// profile doesn't take and aims above (a 1080p file put on a 4K profile).
// Choosing that profile says "I want that quality", so "leave what I have
// alone" is switched off for it. Reports whether a search was started.
func (s *Server) movieProfileChanged(id, profileID int64) bool {
	if !s.movieNeedsUpgradeSearch(id, profileID) {
		return false
	}
	s.background(func() { s.searchMovieInBackground(id) })
	return true
}

// movieNeedsUpgradeSearch is the first half of movieProfileChanged: whether
// the new profile aims above the movie's file. When it does, "leave what I
// have alone" is switched off for the movie. It starts no search.
func (s *Server) movieNeedsUpgradeSearch(id, profileID int64) bool {
	m, err := s.MovieRepo.Get(id)
	if err != nil || m.Status != library.StatusDownloaded || !tierKnown(m.Quality) {
		return false
	}
	profiles, err := s.loadProfiles()
	if err != nil || !outgrown(profiles.resolve(profileID), quality.Tier(m.Quality)) {
		return false
	}
	if m.NoUpgrade {
		if err := s.MovieRepo.SetNoUpgrade(id, false); err != nil {
			log.Printf("profile change: movie %d: %v", id, err)
			return false
		}
	}
	return true
}

// seriesProfileChanged is movieProfileChanged for a show: when any episode
// on disk is below the new profile, the show's episodes are looked for again.
func (s *Server) seriesProfileChanged(id, profileID int64) bool {
	if !s.seriesNeedsUpgradeSearch(id, profileID) {
		return false
	}
	s.background(func() { s.searchSeriesInBackground(id) })
	return true
}

// seriesNeedsUpgradeSearch is the first half of seriesProfileChanged.
func (s *Server) seriesNeedsUpgradeSearch(id, profileID int64) bool {
	sr, err := s.MovieRepo.GetSeries(id)
	if err != nil {
		return false
	}
	eps, err := s.MovieRepo.ListEpisodes(id)
	if err != nil {
		return false
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return false
	}
	p := profiles.resolveTV(profileID)
	below := false
	for _, ep := range eps {
		if ep.Status == library.StatusDownloaded && tierKnown(ep.Quality) && outgrown(p, quality.Tier(ep.Quality)) {
			below = true
			break
		}
	}
	if !below {
		return false
	}
	if sr.NoUpgrade {
		if err := s.MovieRepo.SetSeriesNoUpgrade(id, false); err != nil {
			log.Printf("profile change: series %d: %v", id, err)
			return false
		}
	}
	return true
}

// keepKnownFormats drops scores for custom formats that don't exist, so a
// stale or mistyped id never sits in a profile.
func (s *Server) keepKnownFormats(p *quality.Profile) error {
	if len(p.FormatScores) == 0 {
		return nil
	}
	formats, err := s.FormatRepo.List()
	if err != nil {
		return err
	}
	known := map[int64]bool{}
	for _, f := range formats {
		known[f.ID] = true
	}
	for id := range p.FormatScores {
		if !known[id] {
			delete(p.FormatScores, id)
		}
	}
	return nil
}
