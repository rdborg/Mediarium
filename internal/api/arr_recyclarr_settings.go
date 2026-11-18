package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
)

// The settings Recyclarr writes next to profiles and formats: the size of each
// quality (/qualitydefinition), the file and folder names (/config/naming)
// and the media management page (/config/mediamanagement).

// --- quality definitions ---

// arrDefinitionBase is where the ids of the qualities Mediarium has no tier of
// its own for start; the ones it has number from 1 in their usual order, as they
// always did.
const arrDefinitionBase = 100

// arrUnlimitedAt is the size at which an app's slider ends, which the apps
// write for "no upper limit": 2000 MB a minute for Radarr, 1000 for Sonarr.
func arrUnlimitedAt(app arrApp) float64 {
	if app == arrSonarr {
		return 1000
	}
	return 2000
}

func arrSizeKind(app arrApp) string {
	if app == arrSonarr {
		return "tv"
	}
	return "movie"
}

type arrDefinition struct {
	ID            int            `json:"id"`
	Quality       arrQualityName `json:"quality"`
	Title         string         `json:"title"`
	Weight        int            `json:"weight"`
	MinSize       float64        `json:"minSize"`
	MaxSize       *float64       `json:"maxSize"`
	PreferredSize *float64       `json:"preferredSize"`
}

// arrDefinitions lists the size of each quality: the table of the matching
// kind, with the names that share a quality (WEBRip next to WEBDL) showing the
// size of the quality they count as.
func (s *Server) arrDefinitions(app arrApp) []arrDefinition {
	table := s.sizeLimitsFor(arrSizeKind(app))
	ids := map[quality.Tier]int{}
	for i, t := range quality.AllTiers() {
		ids[t] = i + 1
	}
	out := []arrDefinition{}
	for _, c := range arrCatalog(app) {
		d := arrDefinition{Quality: c.Q, Title: c.Q.Name}
		if c.HasTier {
			d.Weight = ids[c.Tier]
			d.ID = ids[c.Tier]
			if c.Alias {
				d.ID = arrDefinitionBase + c.Q.ID
			}
			if l, ok := table.Limit(c.Tier); ok {
				d.MinSize = l.Min
				if l.Max > 0 {
					max := l.Max
					d.MaxSize = &max
				}
				if l.Preferred > 0 {
					p := l.Preferred
					d.PreferredSize = &p
				}
			}
		} else {
			d.ID = arrDefinitionBase + c.Q.ID
			d.Weight = len(ids) + 1
		}
		out = append(out, d)
	}
	return out
}

type arrDefinitionIn struct {
	ID            int            `json:"id"`
	Quality       *arrQualityRef `json:"quality"`
	MinSize       *float64       `json:"minSize"`
	MaxSize       *float64       `json:"maxSize"`
	PreferredSize *float64       `json:"preferredSize"`
}

func (s *Server) arrQualityDefinitionList(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.arrDefinitions(app))
	}
}

// arrApplyDefinitions writes sizes into the table of an app's kind. A quality
// that no Mediarium quality stands for (Raw-HD) is left out. When both a
// quality and the name that shares it are in the list, the quality's own size
// counts. The size table is switched on: sending sizes means wanting them used.
func (s *Server) arrApplyDefinitions(app arrApp, in []arrDefinitionIn) []arrProblem {
	kind := arrSizeKind(app)
	table := s.sizeLimitsFor(kind)
	limits := map[quality.Tier]quality.SizeLimit{}
	for t, l := range table.Limits {
		limits[t] = l
	}
	byID := map[int]arrCatalogItem{}
	for _, d := range s.arrDefinitions(app) {
		if c, ok := arrQualityByRef(app, d.Quality.ID, d.Quality.Name); ok {
			byID[d.ID] = c
		}
	}
	set := map[quality.Tier]bool{}
	for _, primary := range []bool{true, false} {
		for _, d := range in {
			var c arrCatalogItem
			var ok bool
			if d.Quality != nil {
				c, ok = arrQualityByRef(app, d.Quality.ID, d.Quality.Name)
			}
			if !ok {
				c, ok = byID[d.ID]
			}
			if !ok || !c.HasTier || c.Alias == primary {
				continue
			}
			if set[c.Tier] {
				continue
			}
			set[c.Tier] = true
			l := quality.SizeLimit{}
			if d.MinSize != nil {
				l.Min = *d.MinSize
			}
			if d.MaxSize != nil && *d.MaxSize > 0 && *d.MaxSize < arrUnlimitedAt(app) {
				l.Max = *d.MaxSize
			}
			if d.PreferredSize != nil && *d.PreferredSize > 0 {
				l.Preferred = *d.PreferredSize
			}
			limits[c.Tier] = l
		}
	}
	table.Limits = limits
	table.Enabled = true
	if err := table.Validate(); err != nil {
		return []arrProblem{{"MaxSize", strings.TrimSuffix(err.Error(), ".") + "."}}
	}
	raw, err := json.Marshal(table)
	if err != nil {
		return []arrProblem{{"", err.Error()}}
	}
	if err := s.Settings.Set(sizeKeys[kind], string(raw), false); err != nil {
		return []arrProblem{{"", fmt.Sprintf("The sizes could not be saved: %v", err)}}
	}
	return nil
}

// arrUpdateQualityDefinitions answers PUT /qualitydefinition/update.
func (s *Server) arrUpdateQualityDefinitions(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in []arrDefinitionIn
		if err := decodeJSON(r, &in); err != nil {
			arrBadRequestList(w, []arrProblem{{"", arrUnreadable}})
			return
		}
		if problems := s.arrApplyDefinitions(app, in); problems != nil {
			arrBadRequestList(w, problems)
			return
		}
		writeJSON(w, http.StatusAccepted, s.arrDefinitions(app))
	}
}

// arrUpdateQualityDefinition answers PUT /qualitydefinition/{id}.
func (s *Server) arrUpdateQualityDefinition(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := arrPathID(w, r)
		if !ok {
			return
		}
		var in arrDefinitionIn
		if err := decodeJSON(r, &in); err != nil {
			arrBadRequestList(w, []arrProblem{{"", arrUnreadable}})
			return
		}
		in.ID = int(id)
		if problems := s.arrApplyDefinitions(app, []arrDefinitionIn{in}); problems != nil {
			arrBadRequestList(w, problems)
			return
		}
		for _, d := range s.arrDefinitions(app) {
			if d.ID == in.ID {
				writeJSON(w, http.StatusAccepted, d)
				return
			}
		}
		arrNotFound(w)
	}
}

// --- file and folder names ---

// arrNaming is the file and folder names in force, as the settings of the
// apps show them.
type arrNaming struct {
	MovieFile, MovieFolder, EpisodeFile, SeriesFolder, AnimeFile string
}

func (s *Server) arrNamingNow() arrNaming {
	preset, _ := s.Settings.Get(settings.KeyNamingPreset)
	n := arrNaming{
		MovieFile:    organizer.Presets["plex"],
		EpisodeFile:  organizer.TVPresets["plex"],
		MovieFolder:  s.movieFolderFormat(),
		SeriesFolder: s.seriesFolderFormat(),
	}
	if preset == "custom" {
		if v, _ := s.Settings.Get(settings.KeyMovieNameFormat); strings.TrimSpace(v) != "" {
			n.MovieFile = strings.TrimSpace(v)
		}
		if v, _ := s.Settings.Get(settings.KeyEpisodeNameFormat); strings.TrimSpace(v) != "" {
			n.EpisodeFile = strings.TrimSpace(v)
		}
	} else {
		if p, ok := organizer.Presets[preset]; ok {
			n.MovieFile = p
		}
		if p, ok := organizer.TVPresets[preset]; ok {
			n.EpisodeFile = p
		}
	}
	n.AnimeFile = n.EpisodeFile
	if v, _ := s.Settings.Get(settings.KeyAnimeEpisodeFormat); strings.TrimSpace(v) != "" {
		n.AnimeFile = strings.TrimSpace(v)
	}
	return n
}

// arrNamingEchoKeys are the naming settings that are taken and kept only to be
// given back (Mediarium has one season folder, no daily format and always
// renames), with what they say until an app writes them.
var arrNamingEchoKeys = map[string]any{
	"renameMovies": true, "renameEpisodes": true, "replaceIllegalCharacters": true, "colonReplacementFormat": "delete",
	"dailyEpisodeFormat": nil, "seasonFolderFormat": organizer.TVSeasonFolder, "specialsFolderFormat": "Specials",
	"multiEpisodeStyle": float64(5), "includeSeriesTitle": false,
}

// arrConfigEcho reads what was last kept to be given back.
func (s *Server) arrConfigEcho() map[string]any {
	kept := map[string]any{}
	if raw, _ := s.Settings.Get(settings.KeyArrConfigEcho); raw != "" {
		if json.Unmarshal([]byte(raw), &kept) != nil {
			return map[string]any{}
		}
	}
	return kept
}

// arrNamingConfig answers GET /config/naming with the formats in use.
func (s *Server) arrNamingConfig(w http.ResponseWriter, r *http.Request) {
	n := s.arrNamingNow()
	out := map[string]any{
		"id": 1, "standardMovieFormat": n.MovieFile, "movieFolderFormat": n.MovieFolder,
		"standardEpisodeFormat": n.EpisodeFile, "animeEpisodeFormat": n.AnimeFile, "seriesFolderFormat": n.SeriesFolder,
	}
	for k, v := range arrNamingEchoKeys {
		out[k] = v
	}
	if out["dailyEpisodeFormat"] == nil {
		out["dailyEpisodeFormat"] = n.EpisodeFile
	}
	kept := s.arrConfigEcho()
	for k := range arrNamingEchoKeys {
		if v, ok := kept[k]; ok {
			out[k] = v
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// arrKeepEcho remembers what an app sent for the settings that are only given
// back, so the next read agrees with the write. Values of the wrong kind or
// length are not kept.
func (s *Server) arrKeepEcho(sent map[string]any, keys map[string]any) {
	kept := s.arrConfigEcho()
	changed := false
	for k, def := range keys {
		v, ok := sent[k]
		if !ok || v == nil {
			continue
		}
		switch def.(type) {
		case bool:
			if b, ok := v.(bool); ok {
				kept[k], changed = b, true
			}
		case float64:
			if f, ok := v.(float64); ok && f >= 0 && f < 100 {
				kept[k], changed = f, true
			}
		default:
			if text, ok := v.(string); ok && len(text) <= maxNameFormatLen && !strings.ContainsAny(text, "\x00\r\n") {
				kept[k], changed = text, true
			}
		}
	}
	if !changed {
		return
	}
	if raw, err := json.Marshal(kept); err == nil {
		_ = s.Settings.Set(settings.KeyArrConfigEcho, string(raw), false)
	}
}

type arrNamingIn struct {
	StandardMovieFormat   *string `json:"standardMovieFormat"`
	MovieFolderFormat     *string `json:"movieFolderFormat"`
	StandardEpisodeFormat *string `json:"standardEpisodeFormat"`
	AnimeEpisodeFormat    *string `json:"animeEpisodeFormat"`
	SeriesFolderFormat    *string `json:"seriesFolderFormat"`
}

// arrUpdateNamingConfig answers PUT /config/naming/{id}. The movie and show
// formats go to the Custom naming style of Mediarium (which is switched to).
// What has no place here (the daily episode format, the season folder, whether
// to rename at all) is accepted and only given back on the next read.
func (s *Server) arrUpdateNamingConfig(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var all map[string]any
		if err := decodeJSON(r, &all); err != nil {
			arrBadRequestList(w, []arrProblem{{"", arrUnreadable}})
			return
		}
		text := func(key string) *string {
			if v, ok := all[key].(string); ok {
				return &v
			}
			return nil
		}
		in := arrNamingIn{
			StandardMovieFormat: text("standardMovieFormat"), MovieFolderFormat: text("movieFolderFormat"),
			StandardEpisodeFormat: text("standardEpisodeFormat"), AnimeEpisodeFormat: text("animeEpisodeFormat"),
			SeriesFolderFormat: text("seriesFolderFormat"),
		}
		type change struct {
			key, property, label string
			value                *string
			kind                 organizer.TokenKind
			folder               bool
		}
		var list []change
		if app == arrSonarr {
			list = []change{
				{settings.KeyEpisodeNameFormat, "StandardEpisodeFormat", "The episode file name format", in.StandardEpisodeFormat, organizer.TokenTV, false},
				{settings.KeyAnimeEpisodeFormat, "AnimeEpisodeFormat", "The anime episode file name format", in.AnimeEpisodeFormat, organizer.TokenTV, false},
				{settings.KeySeriesFolderFormat, "SeriesFolderFormat", "The show folder format", in.SeriesFolderFormat, organizer.TokenTV, true},
			}
		} else {
			list = []change{
				{settings.KeyMovieNameFormat, "StandardMovieFormat", "The file name format", in.StandardMovieFormat, organizer.TokenMovie, false},
				{settings.KeyMovieFolderFormat, "MovieFolderFormat", "The movie folder format", in.MovieFolderFormat, organizer.TokenMovie, true},
			}
		}
		var problems []arrProblem
		var todo []change
		for _, c := range list {
			if c.value == nil {
				continue
			}
			v := strings.TrimSpace(*c.value)
			*c.value = v
			if v == "" {
				continue
			}
			msg := firstProblem(checkMaxLen(v, c.label, maxNameFormatLen), checkNoControl(v, c.label))
			if msg == "" {
				if c.folder {
					msg = organizer.CheckFolderFormat(v, c.kind)
				} else {
					msg = organizer.CheckFormat(v, c.kind)
				}
			}
			if msg != "" {
				problems = append(problems, arrProblem{c.property, msg})
				continue
			}
			todo = append(todo, c)
		}
		if len(problems) > 0 {
			arrBadRequestList(w, problems)
			return
		}
		if len(todo) > 0 {
			// The custom style is the only one that reads these formats.
			// Switching to it keeps the other kind (movies or shows) as it was:
			// a format never set falls back to the Plex style, which is what
			// the preset in use named before unless it was another one.
			s.arrKeepOtherNaming(app)
			for _, c := range todo {
				if err := s.Settings.Set(c.key, *c.value, false); err != nil {
					writeError(w, http.StatusInternalServerError, fmt.Sprintf("save naming: %v", err))
					return
				}
			}
			if err := s.Settings.Set(settings.KeyNamingPreset, "custom", false); err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("save naming: %v", err))
				return
			}
		}
		s.arrKeepEcho(all, arrNamingEchoKeys)
		s.arrNamingConfig(w, r)
	}
}

// arrKeepOtherNaming writes the formats of the kind the request is not about
// (shows for Radarr, movies for Sonarr) into the Custom style's fields before
// the style is switched, when those are empty, so the other kind keeps the
// names it had.
func (s *Server) arrKeepOtherNaming(app arrApp) {
	preset, _ := s.Settings.Get(settings.KeyNamingPreset)
	if preset == "custom" {
		return
	}
	n := s.arrNamingNow()
	type kv struct{ key, value string }
	var keep []kv
	if app == arrSonarr {
		keep = []kv{{settings.KeyMovieNameFormat, n.MovieFile}, {settings.KeyMovieFolderFormat, n.MovieFolder}}
	} else {
		keep = []kv{{settings.KeyEpisodeNameFormat, n.EpisodeFile}, {settings.KeySeriesFolderFormat, n.SeriesFolder}}
	}
	for _, k := range keep {
		if cur, _ := s.Settings.Get(k.key); strings.TrimSpace(cur) == "" {
			_ = s.Settings.Set(k.key, k.value, false)
		}
	}
}

// --- media management ---

// arrUpdateMediaManagement answers PUT /config/mediamanagement/{id}. Nothing
// on that page is used here (Mediarium has its own settings for what it shows),
// but what Recyclarr sets, propers and repacks, is given back on the next read.
func (s *Server) arrUpdateMediaManagement(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := decodeJSON(r, &in); err != nil {
		arrBadRequestList(w, []arrProblem{{"", arrUnreadable}})
		return
	}
	if v, ok := in["downloadPropersAndRepacks"].(string); ok && (v == "preferAndUpgrade" || v == "doNotUpgrade" || v == "doNotPrefer") {
		s.arrKeepEcho(in, map[string]any{"downloadPropersAndRepacks": ""})
	}
	s.arrMediaManagementConfig(w, r)
}
