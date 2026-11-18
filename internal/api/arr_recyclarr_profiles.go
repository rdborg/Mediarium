package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/rdborg/mediarium/internal/quality"
)

// Writing quality profiles through the Radarr and Sonarr style API, which is
// what Recyclarr does to put the TRaSH Guides profiles in place.
//
// A profile of the real apps is a list of qualities and groups with an
// allowed flag each, a cutoff, and a score per custom format. Mediarium's
// profile is the same idea with its own qualities, so the list is read onto
// it: every quality maps to the Mediarium quality it counts as (arr_catalog.go),
// a group stays a group, and what the list says that Mediarium has no place for
// (the second name of a quality, Raw-HD, the minimum upgrade score, the
// language) is kept next to the profile (arrExtra) so the next read gives it
// back and Recyclarr sees nothing to change.

// arrQualityRef is the quality inside a list item.
type arrQualityRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// arrItemIn is one entry of a profile's quality list as the apps send it: a
// quality, or a group (no quality, a name and the items inside it).
type arrItemIn struct {
	ID      int            `json:"id,omitempty"`
	Name    string         `json:"name,omitempty"`
	Quality *arrQualityRef `json:"quality,omitempty"`
	Items   []arrItemIn    `json:"items,omitempty"`
	Allowed bool           `json:"allowed"`
}

type arrFormatItemIn struct {
	Format int64  `json:"format"`
	Name   string `json:"name"`
	Score  int    `json:"score"`
}

// arrProfileIn is a quality profile in a POST or PUT. A field the app left
// out stays as it was (on a new profile: its default).
type arrProfileIn struct {
	ID                    int64             `json:"id"`
	Name                  string            `json:"name"`
	UpgradeAllowed        *bool             `json:"upgradeAllowed"`
	Cutoff                *int              `json:"cutoff"`
	Items                 []arrItemIn       `json:"items"`
	MinFormatScore        *int              `json:"minFormatScore"`
	CutoffFormatScore     *int              `json:"cutoffFormatScore"`
	MinUpgradeFormatScore *int              `json:"minUpgradeFormatScore"`
	FormatItems           []arrFormatItemIn `json:"formatItems"`
	Language              *arrLanguage      `json:"language"`
}

// arrExtra is what is kept next to a profile written through this API.
type arrExtra struct {
	Items      []arrItemIn  `json:"items,omitempty"`
	Cutoff     *int         `json:"cutoff,omitempty"`
	MinUpgrade *int         `json:"minUpgrade,omitempty"`
	Language   *arrLanguage `json:"language,omitempty"`
}

func decodeArrExtra(raw string) (arrExtra, bool) {
	if raw == "" {
		return arrExtra{}, false
	}
	var e arrExtra
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return arrExtra{}, false
	}
	return e, true
}

func (e arrExtra) encode() string {
	if len(e.Items) == 0 && e.Cutoff == nil && e.MinUpgrade == nil && e.Language == nil {
		return ""
	}
	b, err := json.Marshal(e)
	if err != nil {
		return ""
	}
	return string(b)
}

// arrUnreadable is the answer to a body that is not the JSON the endpoint takes.
const arrUnreadable = "The request could not be read."

// arrProblem is one thing wrong with a request, as the apps report it.
type arrProblem struct{ property, message string }

// arrBadRequestList answers with a list of validation problems.
func arrBadRequestList(w http.ResponseWriter, problems []arrProblem) {
	out := make([]map[string]string, 0, len(problems))
	for _, p := range problems {
		out = append(out, map[string]string{"propertyName": p.property, "errorMessage": p.message, "severity": "error"})
	}
	writeJSON(w, http.StatusBadRequest, out)
}

// arrDerived is a quality list read onto Mediarium's qualities.
type arrDerived struct {
	// Order is the profile's own order, best first; nil when it is the
	// built-in order with nothing grouped.
	Order   []quality.QualityItem
	Allowed []quality.Tier
	Cutoff  quality.Tier
}

const arrMaxGroupName = 40

// arrDeriveQualities reads a quality list (worst first, as the apps write it)
// onto Mediarium's qualities. A quality counts once, in the best place it is
// allowed; whatever is not allowed, or has no Mediarium counterpart, is left
// out. cutoff is the id of a quality or a group of the list.
func arrDeriveQualities(app arrApp, items []arrItemIn, cutoff *int) arrDerived {
	groupCutoff := false
	if cutoff != nil {
		for _, it := range items {
			if it.Quality == nil && it.ID == *cutoff {
				groupCutoff = true
			}
		}
	}
	taken := map[quality.Tier]bool{}
	names := map[string]bool{}
	var order []quality.QualityItem
	var cut quality.Tier
	tierOf := func(q *arrQualityRef) (quality.Tier, bool) {
		if q == nil {
			return "", false
		}
		c, ok := arrQualityByRef(app, q.ID, q.Name)
		if !ok || !c.HasTier {
			return "", false
		}
		if app == arrSonarr && c.Tier == quality.TierPreRelease {
			return "", false
		}
		return c.Tier, true
	}
	best := func(tiers []quality.Tier) quality.Tier {
		b := tiers[0]
		for _, t := range tiers {
			if quality.Rank(t) > quality.Rank(b) {
				b = t
			}
		}
		return b
	}
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		if it.Quality != nil {
			t, ok := tierOf(it.Quality)
			if !ok || !it.Allowed || taken[t] {
				continue
			}
			taken[t] = true
			order = append(order, quality.QualityItem{Tiers: []quality.Tier{t}})
			if cutoff != nil && !groupCutoff && it.Quality.ID == *cutoff {
				cut = t
			}
			continue
		}
		if !it.Allowed {
			continue
		}
		var tiers []quality.Tier
		for j := len(it.Items) - 1; j >= 0; j-- {
			t, ok := tierOf(it.Items[j].Quality)
			if !ok || taken[t] {
				continue
			}
			taken[t] = true
			tiers = append(tiers, t)
		}
		if len(tiers) == 0 {
			continue
		}
		name := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(it.Name))
		if r := []rune(name); len(r) > arrMaxGroupName {
			name = strings.TrimSpace(string(r[:arrMaxGroupName]))
		}
		if name == "" {
			name = "Group"
		}
		base := name
		for n := 2; names[strings.ToLower(name)]; n++ {
			suffix := fmt.Sprintf(" %d", n)
			name = base
			if r := []rune(name); len(r) > arrMaxGroupName-len(suffix) {
				name = string(r[:arrMaxGroupName-len(suffix)])
			}
			name += suffix
		}
		names[strings.ToLower(name)] = true
		order = append(order, quality.QualityItem{Name: name, Tiers: tiers})
		if groupCutoff && it.ID == *cutoff {
			cut = best(tiers)
		}
	}
	d := arrDerived{Cutoff: cut}
	if len(order) == 0 {
		return d
	}
	for _, it := range order {
		d.Allowed = append(d.Allowed, it.Tiers...)
	}
	if d.Cutoff == "" {
		d.Cutoff = best(order[0].Tiers)
	}
	if !arrIsBuiltInOrder(order, d.Allowed) {
		d.Order = order
	}
	return d
}

// arrIsBuiltInOrder reports whether an order is just the built-in one: every
// quality alone, best first.
func arrIsBuiltInOrder(order []quality.QualityItem, allowed []quality.Tier) bool {
	def := quality.DefaultOrder(allowed)
	if len(def) != len(order) {
		return false
	}
	for i, it := range order {
		if it.IsGroup() || len(it.Tiers) != 1 || it.Tiers[0] != def[i].Tiers[0] {
			return false
		}
	}
	return true
}

// agrees reports whether the kept list still says what the profile does. It
// stops once the profile is changed in Mediarium.
func (e arrExtra) agrees(app arrApp, p quality.Profile) bool {
	if len(e.Items) == 0 {
		return false
	}
	d := arrDeriveQualities(app, e.Items, e.Cutoff)
	if len(d.Allowed) == 0 {
		return false
	}
	check, err := quality.Validate(quality.Profile{Name: "x", Allowed: d.Allowed, Cutoff: d.Cutoff, QualityOrder: d.Order})
	if err != nil {
		return false
	}
	return reflect.DeepEqual(check.QualityOrder, p.QualityOrder) && check.Cutoff == p.Cutoff && slices.Equal(check.Allowed, p.Allowed)
}

// arrItemOut writes a kept list item in the shape the apps read.
func arrItemOut(app arrApp, it arrItemIn) arrProfileItem {
	if it.Quality != nil {
		q := arrQualityName{ID: it.Quality.ID, Name: it.Quality.Name, Source: "unknown"}
		if c, ok := arrQualityByRef(app, it.Quality.ID, it.Quality.Name); ok {
			q = c.Q
		}
		return arrProfileItem{ID: it.ID, Quality: &q, Items: []any{}, Allowed: it.Allowed}
	}
	g := arrProfileItem{ID: it.ID, Name: it.Name, Items: []any{}, Allowed: it.Allowed}
	for _, c := range it.Items {
		g.Items = append(g.Items, arrItemOut(app, c))
	}
	return g
}

// applyArrExtra puts what was kept next to a profile into its answer: the
// language and the minimum upgrade score always, and the quality list and
// cutoff while they still match the profile.
func applyArrExtra(app arrApp, p quality.Profile, out *arrProfile) {
	out.MinUpgradeFormatScore = 1
	e, ok := decodeArrExtra(p.ArrExtra)
	if !ok {
		return
	}
	if e.MinUpgrade != nil {
		out.MinUpgradeFormatScore = *e.MinUpgrade
	}
	if e.Language != nil && app == arrRadarr {
		l := *e.Language
		out.Language = &l
	}
	if !e.agrees(app, p) {
		return
	}
	out.Items = out.Items[:0:0]
	for _, it := range e.Items {
		out.Items = append(out.Items, arrItemOut(app, it))
	}
	if e.Cutoff != nil {
		out.Cutoff = *e.Cutoff
	}
}

// arrProfileSchema answers GET /qualityprofile/schema: the template of a new
// profile, which lists every quality the app knows (none allowed) and every
// custom format with a score of 0.
func (s *Server) arrProfileSchema(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		formats, err := s.FormatRepo.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := arrProfile{Items: []arrProfileItem{}, FormatItems: []map[string]any{}, MinUpgradeFormatScore: 1}
		for _, c := range arrCatalog(app) {
			q := c.Q
			out.Items = append(out.Items, arrProfileItem{Quality: &q, Items: []any{}})
		}
		for _, f := range formats {
			if arrFormatIsOf(app, f) {
				out.FormatItems = append(out.FormatItems, map[string]any{"format": f.ID, "name": f.Name, "score": 0})
			}
		}
		if app == arrRadarr {
			out.Language = &arrEnglish
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// arrProfileProblems turns a refusal of the profile store into the list of
// problems the apps report.
func arrProfileProblems(err error) []arrProblem {
	text := problemText(err, quality.ErrInvalid)
	low := strings.ToLower(text)
	prop := ""
	switch {
	case strings.Contains(low, "already exists"):
		return []arrProblem{{"Name", "A quality profile with that name already exists."}}
	case strings.Contains(low, "name"):
		prop = "Name"
	case strings.Contains(low, "cutoff"):
		prop = "Cutoff"
	case strings.Contains(low, "score"):
		prop = "MinFormatScore"
	case strings.Contains(low, "quality") || strings.Contains(low, "group"):
		prop = "Items"
	}
	return []arrProblem{{prop, text}}
}

// arrSaveProfile answers POST /qualityprofile (create) and PUT
// /qualityprofile/{id}.
func (s *Server) arrSaveProfile(app arrApp, create bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id int64
		if !create {
			var ok bool
			if id, ok = arrPathID(w, r); !ok {
				return
			}
		}
		var in arrProfileIn
		if err := decodeJSON(r, &in); err != nil {
			arrBadRequestList(w, []arrProblem{{"", arrUnreadable}})
			return
		}
		p := quality.Profile{SkipBadReleases: true}
		if !create {
			saved, err := s.QualityRepo.Get(id)
			if errors.Is(err, sql.ErrNoRows) {
				arrNotFound(w)
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			p = saved
		}
		var problems []arrProblem
		if name := strings.TrimSpace(in.Name); name != "" {
			p.Name = name
		} else if create {
			problems = append(problems, arrProblem{"Name", "'Name' must not be empty."})
		}
		if in.UpgradeAllowed != nil {
			p.UpgradeAllowed = *in.UpgradeAllowed
		}
		if in.MinFormatScore != nil {
			p.MinFormatScore = *in.MinFormatScore
		}
		if in.CutoffFormatScore != nil {
			p.CutoffFormatScore = *in.CutoffFormatScore
		}
		extra, _ := decodeArrExtra(p.ArrExtra)
		if in.Items != nil {
			d := arrDeriveQualities(app, in.Items, in.Cutoff)
			if len(d.Allowed) == 0 {
				problems = append(problems, arrProblem{"Items", "Must have at least one allowed quality that Mediarium knows."})
			} else {
				p.Allowed, p.Cutoff, p.QualityOrder = d.Allowed, d.Cutoff, d.Order
				extra.Items, extra.Cutoff = in.Items, in.Cutoff
			}
		} else if create {
			problems = append(problems, arrProblem{"Items", "Must have at least one allowed quality that Mediarium knows."})
		}
		if in.MinUpgradeFormatScore != nil {
			extra.MinUpgrade = in.MinUpgradeFormatScore
		}
		if in.Language != nil {
			extra.Language = in.Language
		}
		if in.FormatItems != nil {
			// The scores of this app's formats are what was sent; those of
			// the other app's formats (a profile can score both) stay.
			kept := map[int64]int{}
			if formats, err := s.FormatRepo.List(); err == nil {
				for _, f := range formats {
					if sc, ok := p.FormatScores[f.ID]; ok && !arrFormatIsOf(app, f) {
						kept[f.ID] = sc
					}
				}
			}
			p.FormatScores = kept
			for _, f := range in.FormatItems {
				if f.Format > 0 && f.Score != 0 {
					p.FormatScores[f.Format] = f.Score
				}
			}
		}
		if len(problems) > 0 {
			arrBadRequestList(w, problems)
			return
		}
		p.ArrExtra = extra.encode()
		if err := s.keepKnownFormats(&p); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var saved quality.Profile
		var err error
		if create {
			saved, err = s.QualityRepo.Create(p)
		} else {
			p.ID = id
			saved, err = s.QualityRepo.Update(p)
		}
		switch {
		case errors.Is(err, quality.ErrInvalid):
			arrBadRequestList(w, arrProfileProblems(err))
			return
		case errors.Is(err, sql.ErrNoRows):
			arrNotFound(w)
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ps, err := s.loadProfiles()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		status := http.StatusAccepted
		if create {
			status = http.StatusCreated
		}
		writeJSON(w, status, toArrProfile(app, ps.resolve(saved.ID)))
	}
}

// arrDeleteProfile answers DELETE /qualityprofile/{id}.
func (s *Server) arrDeleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	if _, err := s.QualityRepo.Get(id); errors.Is(err, sql.ErrNoRows) {
		arrNotFound(w)
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if id == s.defaultTVProfileID() {
		arrBadRequestList(w, []arrProblem{{"", "That profile is the default for TV. Pick another default for TV first."}})
		return
	}
	if err := s.QualityRepo.Delete(id, s.defaultProfileID()); err != nil {
		if errors.Is(err, quality.ErrInUse) {
			arrBadRequestList(w, []arrProblem{{"", problemText(err, quality.ErrInUse)}})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}
