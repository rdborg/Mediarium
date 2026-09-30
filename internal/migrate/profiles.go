package migrate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rdborg/mediarium/internal/quality"
)

// ProfileMapping says which Mediarium quality profile a Radarr/Sonarr
// profile's titles get.
type ProfileMapping struct {
	App         string `json:"app"` // "radarr" or "sonarr"
	ArrID       int    `json:"arrId"`
	ArrName     string `json:"arrName"`
	ProfileID   int64  `json:"profileId"`   // 0 = Mediarium's default profile
	ProfileName string `json:"profileName"` // the profile's name, or "Default (<name>)"
	How         string `json:"how"`         // why this one, in plain words
	Titles      int    `json:"titles"`      // titles using the arr profile
}

// resolutionClass buckets a Servarr resolution (pixels of height).
func resolutionClass(res int) string {
	switch {
	case res >= 2160:
		return quality.Preset4K
	case res >= 1080:
		return quality.Preset1080p
	case res >= 720:
		return quality.Preset720p
	case res > 0:
		return "sd"
	}
	return ""
}

// qualities lists the qualities of an item (a single quality or a group).
func (it arrProfileItem) qualities() []arrQuality {
	if it.Quality != nil {
		return []arrQuality{*it.Quality}
	}
	var out []arrQuality
	for _, sub := range it.Items {
		out = append(out, sub.qualities()...)
	}
	return out
}

func maxResolution(qs []arrQuality) int {
	best := 0
	for _, q := range qs {
		if q.Resolution > best {
			best = q.Resolution
		}
	}
	return best
}

// closestPreset picks the built-in preset nearest to a Radarr/Sonarr
// profile: "Any" when the profile takes SD next to HD (or is called Any),
// else the resolution of its cutoff (or of the best quality it allows).
func closestPreset(p arrProfile) (key, why string) {
	if strings.Contains(strings.ToLower(p.Name), "any") {
		return quality.PresetAny, "named Any"
	}
	classes := map[string]bool{}
	cutoffRes, bestRes := 0, 0
	for _, it := range p.Items {
		qs := it.qualities()
		isCutoff := (it.Quality != nil && it.Quality.ID == p.Cutoff) || (it.Quality == nil && it.ID == p.Cutoff)
		if isCutoff {
			cutoffRes = maxResolution(qs)
		}
		if !it.Allowed {
			continue
		}
		for _, q := range qs {
			if c := resolutionClass(q.Resolution); c != "" {
				classes[c] = true
			}
		}
		if r := maxResolution(qs); r > bestRes {
			bestRes = r
		}
	}
	if classes["sd"] && len(classes) >= 2 || len(classes) >= 3 {
		return quality.PresetAny, "allows several resolutions, SD included"
	}
	res, from := cutoffRes, "cutoff"
	if resolutionClass(res) == "" {
		res, from = bestRes, "best allowed quality"
	}
	switch c := resolutionClass(res); c {
	case "":
		return "", "no resolution found in the profile"
	case "sd":
		return quality.PresetAny, fmt.Sprintf("%s is SD (%dp)", from, res)
	default:
		return c, fmt.Sprintf("closest resolution (%s %dp)", from, res)
	}
}

// profileChoice resolves every arr profile to a Mediarium profile.
type profileChoice struct {
	byArrID map[int]ProfileMapping
}

func (pc profileChoice) forArr(id int) ProfileMapping { return pc.byArrID[id] }

// mapProfiles maps an app's profiles: an explicit choice in override (by
// arr profile name, to a Mediarium profile id) wins; otherwise the preset
// with the closest resolution, found by its built-in name among stored
// profiles; otherwise the default profile.
func mapProfiles(app string, arr []arrProfile, stored []quality.Profile, defaultName string, override map[string]int64, titles map[int]int) (profileChoice, []ProfileMapping) {
	byName := map[string]quality.Profile{}
	byID := map[int64]quality.Profile{}
	for _, p := range stored {
		byName[strings.ToLower(p.Name)] = p
		byID[p.ID] = p
	}
	presets := quality.Presets()
	defaultLabel := "Default"
	if defaultName != "" {
		defaultLabel = "Default (" + defaultName + ")"
	}
	pc := profileChoice{byArrID: map[int]ProfileMapping{}}
	var list []ProfileMapping
	for _, p := range arr {
		m := ProfileMapping{App: app, ArrID: p.ID, ArrName: p.Name, ProfileName: defaultLabel, Titles: titles[p.ID]}
		if id, ok := lookupOverride(override, p.Name); ok {
			if sp, found := byID[id]; found {
				m.ProfileID, m.ProfileName, m.How = sp.ID, sp.Name, "chosen by you"
			} else if id == 0 {
				m.How = "chosen by you"
			} else {
				m.How = fmt.Sprintf("the profile you chose (id %d) doesn't exist; the default profile is used", id)
			}
		} else {
			key, why := closestPreset(p)
			switch {
			case key == "":
				m.How = why + "; the default profile is used"
			default:
				name := presets[key].Name
				if sp, found := byName[strings.ToLower(name)]; found {
					m.ProfileID, m.ProfileName, m.How = sp.ID, sp.Name, why
				} else {
					m.How = fmt.Sprintf("%s, but the %q profile was deleted; the default profile is used", why, name)
				}
			}
		}
		pc.byArrID[p.ID] = m
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ArrName < list[j].ArrName })
	return pc, list
}

func lookupOverride(override map[string]int64, name string) (int64, bool) {
	if id, ok := override[name]; ok {
		return id, true
	}
	for k, id := range override {
		if strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(name)) {
			return id, true
		}
	}
	return 0, false
}

// tierFromArr turns a Radarr/Sonarr quality name into Mediarium's tier of
// the same name (Mediarium's ladder uses the same names), for files whose
// own name says nothing about their quality. WEBRip counts as WEBDL, as it
// does in Mediarium.
func tierFromArr(name string) quality.Tier {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "WEBRip-") {
		name = "WEBDL-" + strings.TrimPrefix(name, "WEBRip-")
	}
	switch strings.ToUpper(name) {
	case "CAM", "TELESYNC", "TELECINE", "WORKPRINT", "DVDSCR", "REGIONAL":
		return quality.TierPreRelease
	}
	for _, t := range quality.AllTiers() {
		if strings.EqualFold(string(t), name) {
			return t
		}
	}
	return quality.TierUnknown
}
