package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
)

// What Recyclarr sends, tested with the files of the TRaSH Guides
// (testdata/recyclarr, MIT licence, copied unchanged): the same calls in the
// same order, with the bodies shaped the way Recyclarr's own client writes
// them (fields as a list of name and value, the quality list worst first).

func recyclarrFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "recyclarr", name))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

// recyclarrCF is a TRaSH format as Recyclarr posts it: the guide's own keys
// (trash_id, scores) are dropped and each condition's fields become a list.
func recyclarrCF(t *testing.T, name string) map[string]any {
	t.Helper()
	cf := recyclarrFixture(t, name)
	out := map[string]any{"name": cf["name"], "includeCustomFormatWhenRenaming": cf["includeCustomFormatWhenRenaming"]}
	specs := []any{}
	for _, raw := range cf["specifications"].([]any) {
		s := raw.(map[string]any)
		fields := []any{}
		if obj, ok := s["fields"].(map[string]any); ok {
			for k, v := range obj {
				fields = append(fields, map[string]any{"name": k, "value": v})
			}
		}
		specs = append(specs, map[string]any{"name": s["name"], "implementation": s["implementation"], "negate": s["negate"], "required": s["required"], "fields": fields})
	}
	out["specifications"] = specs
	return out
}

type recyclarrEnv struct {
	t      *testing.T
	server *api.Server
	base   string
	key    string
	radarr string
	sonarr string
}

func newRecyclarrEnv(t *testing.T) (*recyclarrEnv, *http.Client) {
	t.Helper()
	server, base, admin, _, _ := familyServer(t)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "Recyclarr"}, http.StatusCreated)["key"].(string)
	return &recyclarrEnv{t: t, server: server, base: base, key: key, radarr: base + "/radarr/api/v3", sonarr: base + "/sonarr/api/v3"}, admin
}

func (e *recyclarrEnv) call(method, url string, body any, want int) any {
	e.t.Helper()
	return arrCall(e.t, method, url, e.key, body, want)
}

func problemsOf(t *testing.T, v any) []map[string]any {
	t.Helper()
	out := []map[string]any{}
	for _, p := range list(v) {
		m := obj(p)
		if m["propertyName"] == nil || m["errorMessage"] == nil {
			t.Fatalf("a problem needs propertyName and errorMessage: %v", p)
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		t.Fatalf("expected a list of problems, got %v", v)
	}
	return out
}

func fieldValue(spec map[string]any, name string) any {
	for _, f := range list(spec["fields"]) {
		if obj(f)["name"] == name {
			return obj(f)["value"]
		}
	}
	return nil
}

func TestRecyclarrCustomFormatRoundTrip(t *testing.T) {
	env, admin := newRecyclarrEnv(t)
	cases := []struct {
		app, prefix, file string
		specs             int
		check             func(t *testing.T, specs []any)
	}{
		{"radarr", env.radarr, "radarr_cf_hdr.json", 0, func(t *testing.T, specs []any) {
			// Sources and resolutions are numbers on the wire, patterns text.
			for _, s := range specs {
				switch obj(s)["implementation"] {
				case "ReleaseTitleSpecification":
					if _, ok := fieldValue(obj(s), "value").(string); !ok {
						t.Errorf("a pattern is text: %v", s)
					}
				}
			}
		}},
		{"radarr", env.radarr, "radarr_cf_remux-tier-01.json", 0, nil},
		{"radarr", env.radarr, "radarr_cf_language-not-english.json", 1, func(t *testing.T, specs []any) {
			s := obj(specs[0])
			if fieldValue(s, "value") != float64(1) || s["negate"] != true || fieldValue(s, "exceptLanguage") != false {
				t.Errorf("a language condition keeps its number and switches: %v", s)
			}
		}},
		{"radarr", env.radarr, "radarr_cf_x265-hd.json", 0, nil},
		{"radarr", env.radarr, "radarr_cf_imax.json", 0, nil},
		{"radarr", env.radarr, "radarr_cf_2160p.json", 0, func(t *testing.T, specs []any) {
			if v := fieldValue(obj(specs[0]), "value"); v != float64(2160) {
				t.Errorf("a resolution is a number: %v", specs[0])
			}
		}},
		{"radarr", env.radarr, "radarr_cf_dv-boost.json", 0, nil},
		{"sonarr", env.sonarr, "sonarr_cf_season-pack.json", 1, func(t *testing.T, specs []any) {
			if fieldValue(obj(specs[0]), "value") != float64(3) || obj(specs[0])["implementation"] != "ReleaseTypeSpecification" {
				t.Errorf("a release type: %v", specs[0])
			}
		}},
		{"sonarr", env.sonarr, "sonarr_cf_web-tier-01.json", 0, nil},
		{"sonarr", env.sonarr, "sonarr_cf_language-not-original.json", 0, nil},
		{"sonarr", env.sonarr, "sonarr_cf_hdr.json", 0, nil},
	}
	var created []float64
	for _, c := range cases {
		body := recyclarrCF(t, c.file)
		name := body["name"].(string)
		wantSpecs := len(body["specifications"].([]any))
		if sameNameExists(env, c.prefix, name) {
			t.Fatalf("the test files must have different names for one app: %s", name)
		}
		made := obj(env.call("POST", c.prefix+"/customformat", body, http.StatusCreated))
		id := made["id"].(float64)
		created = append(created, id)
		if made["name"] != name || len(list(made["specifications"])) != wantSpecs {
			t.Fatalf("%s: created %v", c.file, made)
		}
		// Reading it back gives what was sent (Recyclarr compares them to
		// decide that nothing changed).
		got := obj(env.call("GET", c.prefix+"/customformat/"+fnum(id), nil, http.StatusOK))
		if got["includeCustomFormatWhenRenaming"] != body["includeCustomFormatWhenRenaming"] {
			t.Errorf("%s: includeCustomFormatWhenRenaming is %v, sent %v", c.file, got["includeCustomFormatWhenRenaming"], body["includeCustomFormatWhenRenaming"])
		}
		for i, raw := range body["specifications"].([]any) {
			sent, back := raw.(map[string]any), obj(list(got["specifications"])[i])
			if sent["name"] != back["name"] || sent["implementation"] != back["implementation"] || sent["negate"] != back["negate"] || sent["required"] != back["required"] {
				t.Errorf("%s spec %d: sent %v, got %v", c.file, i, sent, back)
			}
			for _, f := range sent["fields"].([]any) {
				fm := f.(map[string]any)
				if v := fieldValue(back, fm["name"].(string)); !reflect.DeepEqual(v, fm["value"]) {
					t.Errorf("%s spec %q field %v: sent %#v, got %#v", c.file, sent["name"], fm["name"], fm["value"], v)
				}
			}
		}
		if c.check != nil {
			c.check(t, list(got["specifications"]))
		}
	}

	// All of them are in the list, once, and in Mediarium's own page.
	radarrCases := 0
	for _, c := range cases {
		if c.app == "radarr" {
			radarrCases++
		}
	}
	// HDR is a name of both guides: two formats, each app sees its own.
	if n := len(list(env.call("GET", env.radarr+"/customformat", nil, http.StatusOK))); n != radarrCases {
		t.Fatalf("the Radarr list holds %d formats, want %d", n, radarrCases)
	}
	if n := len(list(env.call("GET", env.sonarr+"/customformat", nil, http.StatusOK))); n != len(cases)-radarrCases {
		t.Fatalf("the Sonarr list holds %d formats, want %d", n, len(cases)-radarrCases)
	}
	env.call("GET", env.sonarr+"/customformat/"+fnum(created[0]), nil, http.StatusNotFound)
	own := getJSON[map[string]any](t, admin, env.base+"/api/custom-formats")
	if n := len(list(own["formats"])); n != len(cases) {
		t.Errorf("Mediarium's own Custom formats page shows %d, want %d", n, len(cases))
	}

	// Syncing again (a PUT of the same thing) changes nothing and adds nothing.
	body := recyclarrCF(t, "radarr_cf_hdr.json")
	before := env.call("GET", env.radarr+"/customformat/"+fnum(created[0]), nil, http.StatusOK)
	env.call("PUT", env.radarr+"/customformat/"+fnum(created[0]), body, http.StatusAccepted)
	env.call("PUT", env.radarr+"/customformat/"+fnum(created[0]), body, http.StatusAccepted)
	if after := env.call("GET", env.radarr+"/customformat/"+fnum(created[0]), nil, http.StatusOK); !reflect.DeepEqual(before, after) {
		t.Errorf("a repeated PUT changed the format:\n%v\n%v", before, after)
	}
	if n := len(list(env.call("GET", env.radarr+"/customformat", nil, http.StatusOK))); n != radarrCases {
		t.Errorf("a repeated PUT left %d formats, want %d", n, radarrCases)
	}

	// Update: a changed condition and a new name.
	specs := body["specifications"].([]any)
	specs[0].(map[string]any)["negate"] = !specs[0].(map[string]any)["negate"].(bool)
	body["name"] = "HDR renamed"
	body["includeCustomFormatWhenRenaming"] = true
	upd := obj(env.call("PUT", env.radarr+"/customformat/"+fnum(created[0]), body, http.StatusAccepted))
	if upd["name"] != "HDR renamed" || upd["includeCustomFormatWhenRenaming"] != true || obj(list(upd["specifications"])[0])["negate"] != specs[0].(map[string]any)["negate"] {
		t.Errorf("update: %v", upd)
	}

	// Posting a name that is taken is refused like the apps do, with a list
	// of problems, whatever the case.
	dup := recyclarrCF(t, "radarr_cf_imax.json")
	dup["name"] = strings.ToUpper(dup["name"].(string))
	if p := problemsOf(t, env.call("POST", env.radarr+"/customformat", dup, http.StatusBadRequest)); p[0]["propertyName"] != "Name" {
		t.Errorf("duplicate name: %v", p)
	}
	// Renaming onto another format's name too.
	body["name"] = "x265 (HD)"
	problemsOf(t, env.call("PUT", env.radarr+"/customformat/"+fnum(created[0]), body, http.StatusBadRequest))

	// Delete, and the gone format is gone everywhere.
	env.call("DELETE", env.radarr+"/customformat/"+fnum(created[0]), nil, http.StatusOK)
	env.call("GET", env.radarr+"/customformat/"+fnum(created[0]), nil, http.StatusNotFound)
	env.call("DELETE", env.radarr+"/customformat/"+fnum(created[0]), nil, http.StatusNotFound)
	env.call("PUT", env.radarr+"/customformat/"+fnum(created[0]), body, http.StatusNotFound)
	if n := len(list(env.call("GET", env.radarr+"/customformat", nil, http.StatusOK))); n != radarrCases-1 {
		t.Errorf("after the delete there are %d Radarr formats, want %d", n, radarrCases-1)
	}
	if n := len(list(env.call("GET", env.sonarr+"/customformat", nil, http.StatusOK))); n != len(cases)-radarrCases {
		t.Errorf("deleting a Radarr format left %d Sonarr formats, want %d", n, len(cases)-radarrCases)
	}
}

func sameNameExists(env *recyclarrEnv, prefix, name string) bool {
	for _, f := range list(env.call("GET", prefix+"/customformat", nil, http.StatusOK)) {
		if strings.EqualFold(obj(f)["name"].(string), name) {
			return true
		}
	}
	return false
}

func fnum(f float64) string { return strconv.FormatFloat(f, 'f', 0, 64) }

func TestRecyclarrCustomFormatRefusals(t *testing.T) {
	env, _ := newRecyclarrEnv(t)
	spec := map[string]any{"name": "x", "implementation": "ReleaseTitleSpecification", "negate": false, "required": false, "fields": []any{map[string]any{"name": "value", "value": "abc"}}}
	cases := []struct {
		name string
		body any
		want string
	}{
		{"no name", map[string]any{"name": "", "specifications": []any{spec}}, "Name"},
		{"no conditions", map[string]any{"name": "Empty", "specifications": []any{}}, "Specifications"},
		{"a list instead of one", []any{map[string]any{"name": "A", "specifications": []any{spec}}}, ""},
		{"a name that is too long", map[string]any{"name": strings.Repeat("n", 120), "specifications": []any{spec}}, "Name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := problemsOf(t, env.call("POST", env.radarr+"/customformat", c.body, http.StatusBadRequest))
			if p[0]["propertyName"] != c.want {
				t.Errorf("property %v, want %q: %v", p[0]["propertyName"], c.want, p)
			}
		})
	}
	// A condition kind Mediarium does not know is accepted (and never matches),
	// as are indexer flags: Recyclarr must not fail on them.
	odd := map[string]any{"name": "Odd", "specifications": []any{
		map[string]any{"name": "flag", "implementation": "IndexerFlagSpecification", "fields": []any{map[string]any{"name": "value", "value": 1}}},
		map[string]any{"name": "new", "implementation": "SomethingNewSpecification", "fields": []any{map[string]any{"name": "value", "value": "q"}}},
	}}
	made := obj(env.call("POST", env.sonarr+"/customformat", odd, http.StatusCreated))
	if len(list(made["specifications"])) != 2 {
		t.Errorf("unknown conditions are kept: %v", made)
	}
	// The schema lists the kinds.
	if sc := list(env.call("GET", env.radarr+"/customformat/schema", nil, http.StatusOK)); len(sc) < 8 || obj(sc[0])["implementation"] == nil {
		t.Errorf("schema: %v", sc)
	}
}

// Recyclarr compares the names of the conditions letter for letter, and the
// TRaSH Guides have one with a space at the end.
func TestRecyclarrConditionNamesAreKeptAsSent(t *testing.T) {
	env, _ := newRecyclarrEnv(t)
	body := map[string]any{"name": "Spaces", "includeCustomFormatWhenRenaming": false, "specifications": []any{
		map[string]any{"name": "Not Dolby Digital Plus ", "implementation": "ReleaseTitleSpecification", "negate": true, "required": true, "fields": []any{map[string]any{"name": "value", "value": "\bDD[P+]"}}},
	}}
	made := obj(env.call("POST", env.radarr+"/customformat", body, http.StatusCreated))
	got := obj(env.call("GET", env.radarr+"/customformat/"+fnum(made["id"].(float64)), nil, http.StatusOK))
	if name := obj(list(got["specifications"])[0])["name"]; name != "Not Dolby Digital Plus " {
		t.Errorf("the condition's name is %q", name)
	}
}
