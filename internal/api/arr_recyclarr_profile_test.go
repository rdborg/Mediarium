package api_test

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// trashProfileBody builds the profile Recyclarr sends for a TRaSH Guides
// profile, the way it does: start from the schema (every quality the app
// knows), take the guide's list best first (groups numbered from 1000), and
// write it worst first with the scores of the formats by name.
func (e *recyclarrEnv) trashProfileBody(prefix, file string, scores map[string]int) map[string]any {
	e.t.Helper()
	guide := recyclarrFixture(e.t, file)
	schema := obj(e.call("GET", prefix+"/qualityprofile/schema", nil, http.StatusOK))
	byName := map[string]map[string]any{}
	for _, it := range list(schema["items"]) {
		m := obj(it)
		byName[strings.ToLower(obj(m["quality"])["name"].(string))] = m
	}
	pick := func(name string, allowed bool) map[string]any {
		src, ok := byName[strings.ToLower(name)]
		if !ok {
			e.t.Fatalf("%s: the schema does not know the quality %q, so Recyclarr would warn about it", file, name)
		}
		cp := map[string]any{}
		for k, v := range src {
			cp[k] = v
		}
		cp["allowed"] = allowed
		return cp
	}
	var items []any
	cutoff := any(nil)
	for i, raw := range guide["items"].([]any) {
		g := raw.(map[string]any)
		allowed := g["allowed"].(bool)
		if kids, ok := g["items"].([]any); ok && len(kids) > 0 {
			group := map[string]any{"id": 1000 + i, "name": g["name"], "quality": nil, "allowed": allowed, "items": []any{}}
			for _, k := range kids {
				group["items"] = append(group["items"].([]any), pick(k.(string), allowed))
			}
			items = append(items, group)
			if g["name"] == guide["cutoff"] {
				cutoff = 1000 + i
			}
			continue
		}
		q := pick(g["name"].(string), allowed)
		items = append(items, q)
		if g["name"] == guide["cutoff"] {
			cutoff = obj(q["quality"])["id"]
		}
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	formatItems := []any{}
	for _, f := range list(schema["formatItems"]) {
		fm := obj(f)
		sc := 0
		if v, ok := scores[fm["name"].(string)]; ok {
			sc = v
		}
		formatItems = append(formatItems, map[string]any{"format": fm["format"], "name": fm["name"], "score": sc})
	}
	return map[string]any{
		"name": guide["name"], "upgradeAllowed": guide["upgradeAllowed"], "cutoff": cutoff, "items": items,
		"minFormatScore": guide["minFormatScore"], "cutoffFormatScore": guide["cutoffFormatScore"],
		"minUpgradeFormatScore": guide["minUpgradeFormatScore"], "formatItems": formatItems,
	}
}

// shape reduces a quality list to what Recyclarr compares: ids, names, flags
// and the nesting.
func shape(items any) []string {
	var out []string
	for _, it := range list(items) {
		m := obj(it)
		q := ""
		if qm := obj(m["quality"]); qm != nil {
			q = fmt.Sprintf("q%v:%v", qm["id"], qm["name"])
		}
		id := ""
		if v, ok := m["id"]; ok && v != nil && v != float64(0) {
			id = fmt.Sprint(v)
		}
		out = append(out, fmt.Sprintf("[%s|%v|%s|%v|%v]", id, m["name"], q, m["allowed"], shape(m["items"])))
	}
	return out
}

func (e *recyclarrEnv) makeFormats(prefix string, names ...string) {
	for _, n := range names {
		e.call("POST", prefix+"/customformat", map[string]any{"name": n, "specifications": []any{
			map[string]any{"name": n + " pattern", "implementation": "ReleaseTitleSpecification", "fields": []any{map[string]any{"name": "value", "value": "\\b" + strings.ReplaceAll(n, " ", "") + "\\b"}}},
		}}, http.StatusCreated)
	}
}

func profileNamed(t *testing.T, profiles any, name string) map[string]any {
	t.Helper()
	for _, p := range list(profiles) {
		if obj(p)["name"] == name {
			return obj(p)
		}
	}
	t.Fatalf("no profile called %q in %v", name, profiles)
	return nil
}

func TestRecyclarrProfileSyncRadarr(t *testing.T) {
	env, admin := newRecyclarrEnv(t)
	env.makeFormats(env.radarr, "HD Bluray Tier 01", "HD Bluray Tier 02", "HD Bluray Tier 03", "WEB Tier 01", "WEB Tier 02", "WEB Tier 03", "Repack/Proper", "Repack2", "Repack3")
	scores := map[string]int{"HD Bluray Tier 01": 1800, "WEB Tier 01": 1700, "WEB Tier 02": 1650, "Repack/Proper": 5, "Repack2": 6, "Repack3": 7}
	body := env.trashProfileBody(env.radarr, "radarr_quality-profiles_hd-bluray-web.json", scores)
	body["language"] = map[string]any{"id": -2, "name": "Original"}

	created := obj(env.call("POST", env.radarr+"/qualityprofile", body, http.StatusCreated))
	id := created["id"].(float64)
	if id <= 0 || created["name"] != "HD Bluray + WEB" || created["upgradeAllowed"] != true || created["cutoffFormatScore"] != float64(10000) || created["minUpgradeFormatScore"] != float64(1) {
		t.Fatalf("created: %v", created)
	}
	// What Recyclarr reads next time is what it wrote: the same list, the same
	// cutoff, nothing for it to change.
	got := profileNamed(t, env.call("GET", env.radarr+"/qualityprofile", nil, http.StatusOK), "HD Bluray + WEB")
	if !reflect.DeepEqual(shape(got["items"]), shape(body["items"])) {
		t.Errorf("the list read back differs from the list sent:\n got  %v\n sent %v", shape(got["items"]), shape(body["items"]))
	}
	if got["cutoff"] != fmt.Sprint(0) && fmt.Sprint(got["cutoff"]) != fmt.Sprint(body["cutoff"]) {
		t.Errorf("cutoff %v, sent %v", got["cutoff"], body["cutoff"])
	}
	if l := obj(got["language"]); l["name"] != "Original" {
		t.Errorf("the language is given back: %v", got["language"])
	}
	scored := map[string]float64{}
	for _, f := range list(got["formatItems"]) {
		scored[obj(f)["name"].(string)] = obj(f)["score"].(float64)
	}
	for n, sc := range scores {
		if scored[n] != float64(sc) {
			t.Errorf("score of %s: %v, sent %d", n, scored[n], sc)
		}
	}

	// And Mediarium's own Quality profiles page shows it as a profile of its own.
	own := getJSON[map[string]any](t, admin, env.base+"/api/quality-profiles")
	mine := profileNamed(t, own["profiles"], "HD Bluray + WEB")
	if !reflect.DeepEqual(mine["allowed"], []any{"Bluray-720p", "WEBDL-1080p", "Bluray-1080p"}) || mine["cutoff"] != "Bluray-1080p" || mine["upgradeAllowed"] != true || mine["cutoffFormatScore"] != float64(10000) {
		t.Errorf("Mediarium's profile: %v", mine)
	}
	if order := list(mine["qualityOrder"]); len(order) != 3 || obj(order[1])["name"] != "WEB 1080p" {
		t.Errorf("the WEB group stays a group: %v", mine["qualityOrder"])
	}
	if fs := list(mine["formatScores"]); len(fs) != len(scores) {
		t.Errorf("%d format scores, want %d: %v", len(fs), len(scores), fs)
	}

	// Syncing again: the same PUT any number of times changes nothing and makes
	// no second profile.
	for i := 0; i < 3; i++ {
		env.call("PUT", env.radarr+"/qualityprofile/"+fnum(id), body, http.StatusAccepted)
	}
	again := env.call("GET", env.radarr+"/qualityprofile", nil, http.StatusOK)
	if len(list(again)) != len(list(own["profiles"])) {
		t.Errorf("a repeated sync changed the number of profiles: %d, was %d", len(list(again)), len(list(own["profiles"])))
	}
	if !reflect.DeepEqual(profileNamed(t, again, "HD Bluray + WEB"), got) {
		t.Errorf("a repeated sync changed the profile:\n%v\n%v", profileNamed(t, again, "HD Bluray + WEB"), got)
	}
	one := obj(env.call("GET", env.radarr+"/qualityprofile/"+fnum(id), nil, http.StatusOK))
	if !reflect.DeepEqual(one, got) {
		t.Errorf("GET /qualityprofile/{id} differs from the list entry")
	}

	// A change in the config reaches the profile.
	scores["WEB Tier 01"] = 1710
	body2 := env.trashProfileBody(env.radarr, "radarr_quality-profiles_hd-bluray-web.json", scores)
	body2["minFormatScore"], body2["upgradeAllowed"] = 100, false
	upd := obj(env.call("PUT", env.radarr+"/qualityprofile/"+fnum(id), body2, http.StatusAccepted))
	if upd["minFormatScore"] != float64(100) || upd["upgradeAllowed"] != false {
		t.Errorf("update: %v", upd)
	}
	mine = profileNamed(t, getJSON[map[string]any](t, admin, env.base+"/api/quality-profiles")["profiles"], "HD Bluray + WEB")
	if mine["minFormatScore"] != float64(100) || mine["upgradeAllowed"] != false {
		t.Errorf("Mediarium's profile after the update: %v", mine)
	}

	// Once the profile is changed in Mediarium the kept list is not trusted:
	// what Mediarium has now is what is shown.
	p, err := env.server.QualityRepo.Get(int64(id))
	if err != nil {
		t.Fatal(err)
	}
	p.QualityOrder = nil
	p.Allowed = append(p.Allowed, "DVD")
	if _, err := env.server.QualityRepo.Update(p); err != nil {
		t.Fatal(err)
	}
	edited := obj(env.call("GET", env.radarr+"/qualityprofile/"+fnum(id), nil, http.StatusOK))
	dvd := false
	for _, it := range list(edited["items"]) {
		if q := obj(obj(it)["quality"]); q != nil && q["name"] == "DVD" && obj(it)["allowed"] == true {
			dvd = true
		}
	}
	if !dvd {
		t.Errorf("a quality added in Mediarium shows as allowed: %v", shape(edited["items"]))
	}
}

func TestRecyclarrProfileSyncSonarr(t *testing.T) {
	env, admin := newRecyclarrEnv(t)
	env.makeFormats(env.sonarr, "WEB Tier 01", "WEB Tier 02", "WEB Tier 03", "WEB Scene", "Repack/Proper", "Repack2", "Repack3")
	body := env.trashProfileBody(env.sonarr, "sonarr_quality-profiles_web-1080p.json", map[string]int{"WEB Tier 01": 1700})
	created := obj(env.call("POST", env.sonarr+"/qualityprofile", body, http.StatusCreated))
	if created["name"] != "WEB-1080p" {
		t.Fatalf("created: %v", created)
	}
	got := obj(env.call("GET", env.sonarr+"/qualityprofile/"+fnum(created["id"].(float64)), nil, http.StatusOK))
	if !reflect.DeepEqual(shape(got["items"]), shape(body["items"])) {
		t.Errorf("sonarr list read back differs:\n got  %v\n sent %v", shape(got["items"]), shape(body["items"]))
	}
	mine := profileNamed(t, getJSON[map[string]any](t, admin, env.base+"/api/quality-profiles")["profiles"], "WEB-1080p")
	if !reflect.DeepEqual(mine["allowed"], []any{"WEBDL-1080p"}) || mine["cutoff"] != "WEBDL-1080p" {
		t.Errorf("Mediarium's profile: %v", mine)
	}
	// The Radarr side does not list the Sonarr formats on its profiles.
	radarrView := env.call("GET", env.radarr+"/qualityprofile/"+fnum(created["id"].(float64)), nil, http.StatusOK)
	if fi := list(obj(radarrView)["formatItems"]); len(fi) != 0 {
		t.Errorf("the Radarr side shows %d Sonarr format scores", len(fi))
	}
}

func TestRecyclarrProfileRefusals(t *testing.T) {
	env, admin := newRecyclarrEnv(t)
	good := func() map[string]any {
		return map[string]any{"name": "Mine", "upgradeAllowed": true, "items": []any{
			map[string]any{"quality": map[string]any{"id": 7, "name": "Bluray-1080p"}, "items": []any{}, "allowed": true},
		}}
	}
	cases := []struct {
		name   string
		change func(m map[string]any)
		want   string
	}{
		{"no name", func(m map[string]any) { m["name"] = "  " }, "Name"},
		{"no qualities", func(m map[string]any) { delete(m, "items") }, "Items"},
		{"nothing allowed", func(m map[string]any) {
			m["items"].([]any)[0].(map[string]any)["allowed"] = false
		}, "Items"},
		{"only a quality Mediarium has no tier for", func(m map[string]any) {
			m["items"] = []any{map[string]any{"quality": map[string]any{"id": 10, "name": "Raw-HD"}, "items": []any{}, "allowed": true}}
		}, "Items"},
		{"a name that is too long", func(m map[string]any) { m["name"] = strings.Repeat("p", 80) }, "Name"},
		{"a score out of range", func(m map[string]any) { m["minFormatScore"] = 2000000 }, "MinFormatScore"},
		{"a negative upgrade-until score", func(m map[string]any) { m["cutoffFormatScore"] = -5 }, "MinFormatScore"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := good()
			c.change(m)
			p := problemsOf(t, env.call("POST", env.radarr+"/qualityprofile", m, http.StatusBadRequest))
			if p[0]["propertyName"] != c.want {
				t.Errorf("property %v, want %v: %v", p[0]["propertyName"], c.want, p)
			}
		})
	}
	// Things Recyclarr may send that Mediarium cannot use are not an error: an
	// unknown quality inside a group, a cutoff that is gone, a format that does
	// not exist, a language, extra fields.
	m := good()
	m["cutoff"] = 9999
	m["language"] = map[string]any{"id": 8, "name": "Japanese"}
	m["formatItems"] = []any{map[string]any{"format": 424242, "name": "Gone", "score": 50}}
	m["somethingNew"] = map[string]any{"a": 1}
	m["items"] = []any{
		map[string]any{"id": 1001, "name": "Disc", "allowed": true, "quality": nil, "items": []any{
			map[string]any{"quality": map[string]any{"id": 22, "name": "BR-DISK"}, "items": []any{}, "allowed": true},
			map[string]any{"quality": map[string]any{"id": 7, "name": "Bluray-1080p"}, "items": []any{}, "allowed": true},
			map[string]any{"quality": map[string]any{"id": 4242, "name": "Quality From The Future"}, "items": []any{}, "allowed": true},
		}},
	}
	ok := obj(env.call("POST", env.radarr+"/qualityprofile", m, http.StatusCreated))
	if ok["name"] != "Mine" {
		t.Errorf("created: %v", ok)
	}
	// The same name again is refused, like the apps do.
	problemsOf(t, env.call("POST", env.radarr+"/qualityprofile", m, http.StatusBadRequest))
	// Another profile cannot take the name by renaming either.
	other := obj(env.call("POST", env.radarr+"/qualityprofile", func() map[string]any { x := good(); x["name"] = "Other"; return x }(), http.StatusCreated))
	rename := good()
	rename["name"] = "Mine"
	problemsOf(t, env.call("PUT", env.radarr+"/qualityprofile/"+fnum(other["id"].(float64)), rename, http.StatusBadRequest))
	// A profile that is not there.
	env.call("PUT", env.radarr+"/qualityprofile/987654", good(), http.StatusNotFound)
	env.call("DELETE", env.radarr+"/qualityprofile/987654", nil, http.StatusNotFound)
	// A body that is not JSON.
	env.call("POST", env.radarr+"/qualityprofile", "not an object", http.StatusBadRequest)

	// The default profile cannot be deleted, another one can.
	def := getJSON[map[string]any](t, admin, env.base+"/api/quality-profiles")["defaultId"].(float64)
	problemsOf(t, env.call("DELETE", env.radarr+"/qualityprofile/"+fnum(def), nil, http.StatusBadRequest))
	env.call("DELETE", env.radarr+"/qualityprofile/"+fnum(other["id"].(float64)), nil, http.StatusOK)
	env.call("GET", env.radarr+"/qualityprofile/"+fnum(other["id"].(float64)), nil, http.StatusNotFound)
}

func TestRecyclarrWritesNeedAnAdministratorKey(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	memberKey := postJSON[map[string]any](t, member, base+"/api/auth/api-keys", map[string]any{"name": "mine"}, http.StatusCreated)["key"].(string)
	body := map[string]any{"name": "Nope", "items": []any{}}
	for _, app := range []string{"radarr", "sonarr"} {
		for _, p := range []string{"/qualityprofile", "/customformat"} {
			arrCall(t, "POST", base+"/"+app+"/api/v3"+p, "", body, http.StatusUnauthorized)
			arrCall(t, "POST", base+"/"+app+"/api/v3"+p, memberKey, body, http.StatusForbidden)
		}
		arrCall(t, "PUT", base+"/"+app+"/api/v3/qualitydefinition/update", memberKey, []any{}, http.StatusForbidden)
		arrCall(t, "PUT", base+"/"+app+"/api/v3/config/naming/1", memberKey, map[string]any{}, http.StatusForbidden)
	}
	// A read-only administrator key reads and nothing more.
	ro := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "ro", "readOnly": true}, http.StatusCreated)
	if k, _ := ro["key"].(string); k != "" {
		arrCall(t, "GET", base+"/radarr/api/v3/customformat", k, nil, http.StatusOK)
		arrCall(t, "POST", base+"/radarr/api/v3/customformat", k, body, http.StatusForbidden)
		arrCall(t, "PUT", base+"/sonarr/api/v3/qualitydefinition/update", k, []any{}, http.StatusForbidden)
	}
}
