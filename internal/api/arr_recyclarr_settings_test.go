package api_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

func TestRecyclarrQualitySizes(t *testing.T) {
	env, admin := newRecyclarrEnv(t)
	sizeTable := func(kind string) map[string]map[string]any {
		out := map[string]map[string]any{}
		tab := getJSON[map[string]any](t, admin, env.base+"/api/quality/size-limits?kind="+kind)
		out["enabled"] = map[string]any{"v": tab["enabled"]}
		for _, r := range list(tab["rows"]) {
			out[obj(r)["quality"].(string)] = obj(r)
		}
		return out
	}
	sizesBody := func(prefix, file string, edit func(name string, d map[string]any)) []any {
		guide := recyclarrFixture(t, file)
		defs := list(env.call("GET", prefix+"/qualitydefinition", nil, http.StatusOK))
		byName := map[string]map[string]any{}
		ids := map[float64]bool{}
		for _, d := range defs {
			dm := obj(d)
			byName[obj(dm["quality"])["name"].(string)] = dm
			if ids[dm["id"].(float64)] {
				t.Errorf("two definitions share the id %v", dm["id"])
			}
			ids[dm["id"].(float64)] = true
		}
		var out []any
		for _, q := range guide["qualities"].([]any) {
			qm := q.(map[string]any)
			d, ok := byName[qm["quality"].(string)]
			if !ok {
				t.Fatalf("%s: no definition for %v, so Recyclarr would skip it with a warning", file, qm["quality"])
			}
			cp := map[string]any{}
			for k, v := range d {
				cp[k] = v
			}
			cp["minSize"], cp["maxSize"], cp["preferredSize"] = qm["min"], qm["max"], qm["preferred"]
			if edit != nil {
				edit(qm["quality"].(string), cp)
			}
			out = append(out, cp)
		}
		return out
	}

	before := sizeTable("movie")
	body := sizesBody(env.radarr, "radarr_quality-size_movie.json", nil)
	env.call("PUT", env.radarr+"/qualitydefinition/update", body, http.StatusAccepted)
	after := sizeTable("movie")
	if after["enabled"]["v"] != true {
		t.Errorf("sending sizes switches the table on")
	}
	guide := recyclarrFixture(t, "radarr_quality-size_movie.json")
	defs := map[string]map[string]any{}
	for _, d := range list(env.call("GET", env.radarr+"/qualitydefinition", nil, http.StatusOK)) {
		defs[obj(obj(d)["quality"])["name"].(string)] = obj(d)
	}
	for _, q := range guide["qualities"].([]any) {
		qm := q.(map[string]any)
		name := qm["quality"].(string)
		d := defs[name]
		if d["minSize"] != qm["min"] || d["preferredSize"] != qm["preferred"] {
			t.Errorf("%s: min %v preferred %v, sent %v and %v", name, d["minSize"], d["preferredSize"], qm["min"], qm["preferred"])
		}
		if qm["max"] == float64(2000) && d["maxSize"] != nil {
			t.Errorf("%s: the slider's end means no limit, got %v", name, d["maxSize"])
		}
	}
	// Mediarium's own size table holds them, per quality.
	if r := after["WEBDL-1080p"]; r["min"] != defs["WEBDL-1080p"]["minSize"] {
		t.Errorf("the table of Mediarium: %v", r)
	}
	if after["WEBDL-1080p"]["min"] == before["WEBDL-1080p"]["min"] {
		t.Errorf("the table did not change")
	}
	// The show table is a different one: Radarr's sync leaves it be.
	if tv := sizeTable("tv"); tv["enabled"]["v"] == true {
		t.Errorf("a Radarr sync switched on the table for shows")
	}

	// Again: nothing moves.
	env.call("PUT", env.radarr+"/qualitydefinition/update", body, http.StatusAccepted)
	again := map[string]map[string]any{}
	for _, d := range list(env.call("GET", env.radarr+"/qualitydefinition", nil, http.StatusOK)) {
		again[obj(obj(d)["quality"])["name"].(string)] = obj(d)
	}
	for name, d := range defs {
		for _, k := range []string{"minSize", "maxSize", "preferredSize"} {
			if again[name][k] != d[k] {
				t.Errorf("%s %s changed on a repeated sync: %v then %v", name, k, d[k], again[name][k])
			}
		}
	}

	// Sonarr has its own sizes and slider end.
	tv := sizesBody(env.sonarr, "sonarr_quality-size_series.json", nil)
	env.call("PUT", env.sonarr+"/qualitydefinition/update", tv, http.StatusAccepted)
	if sizeTable("tv")["enabled"]["v"] != true {
		t.Errorf("the table for shows is on after Sonarr's sync")
	}

	// A quality and the name that shares it: the quality itself wins, in any order.
	pair := sizesBody(env.radarr, "radarr_quality-size_movie.json", func(name string, d map[string]any) {
		switch name {
		case "WEBDL-1080p":
			d["minSize"] = 11.0
		case "WEBRip-1080p":
			d["minSize"] = 99.0
		}
	})
	sort.SliceStable(pair, func(i, j int) bool { return obj(obj(pair[i])["quality"])["name"] == "WEBRip-1080p" })
	env.call("PUT", env.radarr+"/qualitydefinition/update", pair, http.StatusAccepted)
	if r := sizeTable("movie")["WEBDL-1080p"]; r["min"] != float64(11) {
		t.Errorf("WEBDL-1080p wins over WEBRip-1080p: %v", r)
	}

	// Sizes that cannot be true are refused as a list of problems.
	bad := sizesBody(env.radarr, "radarr_quality-size_movie.json", func(name string, d map[string]any) {
		if name == "Bluray-1080p" {
			d["minSize"], d["maxSize"] = 500.0, 100.0
		}
	})
	problemsOf(t, env.call("PUT", env.radarr+"/qualitydefinition/update", bad, http.StatusBadRequest))
	// A single definition by its number, and a quality Mediarium has no row for.
	one := obj(list(env.call("GET", env.radarr+"/qualitydefinition", nil, http.StatusOK))[8])
	one["minSize"] = 7.5
	env.call("PUT", env.radarr+"/qualitydefinition/"+fnum(one["id"].(float64)), one, http.StatusAccepted)
	rawhd := map[string]any{"quality": map[string]any{"id": 10, "name": "Raw-HD"}, "minSize": 1, "maxSize": 2}
	env.call("PUT", env.radarr+"/qualitydefinition/update", []any{rawhd}, http.StatusAccepted)
}

func TestRecyclarrNaming(t *testing.T) {
	env, admin := newRecyclarrEnv(t)
	getSettings := func() map[string]any { return getJSON[map[string]any](t, admin, env.base+"/api/settings") }
	def := obj(env.call("GET", env.radarr+"/config/naming", nil, http.StatusOK))
	if def["id"] != float64(1) || def["standardMovieFormat"] == "" || def["movieFolderFormat"] == "" {
		t.Fatalf("the naming settings: %v", def)
	}
	// Every format the TRaSH Guides give for Radarr and Sonarr is taken.
	for _, c := range []struct {
		prefix, file string
		keys         [][2]string // naming key, guide group
	}{
		{env.radarr, "radarr_naming_radarr-naming.json", [][2]string{{"standardMovieFormat", "file"}, {"movieFolderFormat", "folder"}}},
		{env.sonarr, "sonarr_naming_sonarr-naming.json", [][2]string{{"standardEpisodeFormat", "episodes/standard"}, {"animeEpisodeFormat", "episodes/anime"}, {"seriesFolderFormat", "series"}}},
	} {
		guide := recyclarrFixture(t, c.file)
		for _, k := range c.keys {
			var group map[string]any
			node := any(guide)
			for _, step := range strings.Split(k[1], "/") {
				node = node.(map[string]any)[step]
			}
			group, ok := node.(map[string]any)
			if !ok {
				t.Fatalf("%s has no %q", c.file, k[1])
			}
			names := make([]string, 0, len(group))
			for n := range group {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				format := group[n].(string)
				t.Run(c.file+"/"+k[1]+"/"+n, func(t *testing.T) {
					cur := obj(arrCall(t, "GET", c.prefix+"/config/naming", env.key, nil, http.StatusOK))
					cur[k[0]] = format
					if n == "original" {
						// "{Original Title}" alone: Mediarium wants a title in every
						// name, so this one is refused with the reason, not taken.
						problemsOf(t, arrCall(t, "PUT", c.prefix+"/config/naming/1", env.key, cur, http.StatusBadRequest))
						return
					}
					out := obj(arrCall(t, "PUT", c.prefix+"/config/naming/1", env.key, cur, http.StatusOK))
					if out[k[0]] != format {
						t.Errorf("%s: sent %q, answer %q", k[0], format, out[k[0]])
					}
					if back := obj(arrCall(t, "GET", c.prefix+"/config/naming", env.key, nil, http.StatusOK)); back[k[0]] != format {
						t.Errorf("%s: sent %q, read back %q", k[0], format, back[k[0]])
					}
				})
			}
		}
	}
	// It lands in Mediarium's own naming settings, which are switched to Custom.
	cur := obj(env.call("GET", env.radarr+"/config/naming", nil, http.StatusOK))
	cur["standardMovieFormat"] = "{Movie CleanTitle} {(Release Year)} {tmdb-{TmdbId}}"
	cur["movieFolderFormat"] = "{Movie CleanTitle} ({Release Year})"
	env.call("PUT", env.radarr+"/config/naming/1", cur, http.StatusOK)
	s := getSettings()
	if s["namingPreset"] != "custom" || s["movieNameFormat"] != "{Movie CleanTitle} {(Release Year)} {tmdb-{TmdbId}}" || s["movieFolderFormat"] != "{Movie CleanTitle} ({Release Year})" {
		t.Errorf("Mediarium's naming settings: preset %v, file %v, folder %v", s["namingPreset"], s["movieNameFormat"], s["movieFolderFormat"])
	}
	// Syncing Radarr does not take the show names away.
	if s["episodeNameFormat"] == "" {
		t.Errorf("the episode format was lost")
	}

	// Formats that cannot work are refused with the reason.
	for name, v := range map[string]string{"no title": "{Release Year}", "a made-up token": "{Movie Title} {Nonsense}", "an unclosed brace": "{Movie Title", "a slash": "{Movie Title}/x"} {
		cur["standardMovieFormat"] = v
		p := problemsOf(t, env.call("PUT", env.radarr+"/config/naming/1", cur, http.StatusBadRequest))
		if p[0]["propertyName"] != "StandardMovieFormat" {
			t.Errorf("%s: %v", name, p)
		}
	}
	// What Mediarium has no place for is taken and ignored.
	cur = obj(env.call("GET", env.sonarr+"/config/naming", nil, http.StatusOK))
	cur["dailyEpisodeFormat"], cur["seasonFolderFormat"], cur["renameEpisodes"], cur["multiEpisodeStyle"] = "{Series TitleYear} - {Air-Date}", "Season {season:00}", false, 3
	cur["somethingNew"] = true
	env.call("PUT", env.sonarr+"/config/naming/1", cur, http.StatusOK)
	// ... and given back as it was sent, so Recyclarr sees nothing to change.
	back := obj(env.call("GET", env.sonarr+"/config/naming", nil, http.StatusOK))
	if back["dailyEpisodeFormat"] != "{Series TitleYear} - {Air-Date}" || back["seasonFolderFormat"] != "Season {season:00}" || back["renameEpisodes"] != false || back["multiEpisodeStyle"] != float64(3) {
		t.Errorf("the settings that are only given back: %v", back)
	}
	if _, there := back["somethingNew"]; there {
		t.Errorf("an unknown setting is not kept")
	}

	// The media management page takes its PUT.
	mm := obj(env.call("GET", env.radarr+"/config/mediamanagement", nil, http.StatusOK))
	mm["downloadPropersAndRepacks"] = "doNotPrefer"
	if out := obj(env.call("PUT", env.radarr+"/config/mediamanagement/1", mm, http.StatusOK)); out["id"] != float64(1) {
		t.Errorf("media management: %v", out)
	}
	env.call("PUT", env.sonarr+"/config/mediamanagement/1", mm, http.StatusOK)
	if back := obj(env.call("GET", env.radarr+"/config/mediamanagement", nil, http.StatusOK)); back["downloadPropersAndRepacks"] != "doNotPrefer" {
		t.Errorf("propers and repacks are given back as sent: %v", back["downloadPropersAndRepacks"])
	}
	mm["downloadPropersAndRepacks"] = "nonsense"
	env.call("PUT", env.radarr+"/config/mediamanagement/1", mm, http.StatusOK)
	if back := obj(env.call("GET", env.radarr+"/config/mediamanagement", nil, http.StatusOK)); back["downloadPropersAndRepacks"] != "doNotPrefer" {
		t.Errorf("a value the apps do not have is not kept: %v", back["downloadPropersAndRepacks"])
	}
}
