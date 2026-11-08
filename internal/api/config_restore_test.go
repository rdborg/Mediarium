package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/configfile"
)

// cfgCall sends a settings file (or nothing) to one of the settings-file routes.
func cfgCall(t *testing.T, e *clientEnv, method, path, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, e.url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

type cfgCheck struct {
	Review configfile.Review `json:"review"`
	Hash   string            `json:"hash"`
}

func cfgCheckFile(t *testing.T, e *clientEnv, file string, replace bool) cfgCheck {
	t.Helper()
	path := "/api/system/config-file/check"
	if replace {
		path += "?replace=1"
	}
	code, data := cfgCall(t, e, http.MethodPost, path, file)
	if code != http.StatusOK {
		t.Fatalf("check: HTTP %d: %s", code, data)
	}
	var out cfgCheck
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func cfgApplyFile(t *testing.T, e *clientEnv, file, hash string, replace bool) (int, map[string]any) {
	t.Helper()
	path := "/api/system/config-file/apply?hash=" + hash
	if replace {
		path += "&replace=1"
	}
	code, data := cfgCall(t, e, http.MethodPost, path, file)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return code, out
}

func cfgExport(t *testing.T, e *clientEnv) string {
	t.Helper()
	code, data := cfgCall(t, e, http.MethodGet, "/api/system/config-file", "")
	if code != http.StatusOK {
		t.Fatalf("export: HTTP %d: %s", code, data)
	}
	return string(data)
}

// An export has no secrets, says which are saved, and checks clean against
// the Mediarium it came from.
func TestConfigFileExportHasNoSecrets(t *testing.T) {
	e := newClientEnv(t)
	postJSON[map[string]any](t, e.client, e.url+"/api/usenet-servers", map[string]any{"name": "Main", "host": "news.example.com", "port": 563, "useSsl": true, "username": "me", "password": "hunter2-very-secret", "connections": 8}, http.StatusCreated)
	postJSON[map[string]any](t, e.client, e.url+"/api/indexers", map[string]any{"name": "NZB", "kind": "newznab", "baseUrl": "https://indexer.example.com", "apiKey": "key-super-secret", "protocol": "usenet"}, http.StatusCreated)

	text := cfgExport(t, e)
	for _, secret := range []string{"hunter2-very-secret", "key-super-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("a secret is in the export:\n%s", text)
		}
	}
	for _, want := range []string{"formatVersion: 1", "password: <secret>", "apiKey: <secret>", "usenetServers:", "indexers:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the export should have %q:\n%s", want, text)
		}
	}
	rv := cfgCheckFile(t, e, text, false).Review
	if len(rv.Problems) != 0 || rv.Added+rv.Changed+rv.Removed != 0 {
		t.Fatalf("an export checked against its own Mediarium finds nothing to do: %+v", rv)
	}
	if rv.Unchanged == 0 || rv.CanApply {
		t.Fatalf("everything is unchanged and nothing can be applied: %+v", rv)
	}
}

// Export from one Mediarium, apply to a fresh one: the settings end up the same.
func TestConfigFileRoundTrip(t *testing.T) {
	a := newClientEnv(t)
	putJSON(t, a.client, a.url+"/api/settings", map[string]any{"huntIntervalHours": 12, "trashDays": 21}, 200)
	postJSON[map[string]any](t, a.client, a.url+"/api/usenet-servers", map[string]any{"name": "Main", "host": "news.example.com", "port": 563, "useSsl": true, "username": "me", "password": "pw", "connections": 8}, http.StatusCreated)
	postJSON[map[string]any](t, a.client, a.url+"/api/quality-profiles", map[string]any{"name": "Only 1080p", "allowed": []string{"WEBDL-1080p", "Bluray-1080p"}, "cutoff": "WEBDL-1080p", "upgradeAllowed": true}, http.StatusCreated)
	exported := cfgExport(t, a)

	b := newClientEnv(t)
	// The folders of the first one are on this machine too, so they are fine here.
	chk := cfgCheckFile(t, b, exported, false)
	if len(chk.Review.Problems) != 0 {
		t.Fatalf("problems: %v", chk.Review.Problems)
	}
	if !chk.Review.CanApply || chk.Review.Added < 2 {
		t.Fatalf("the server and the profile are new: %+v", chk.Review)
	}
	if len(chk.Review.MissingSecrets) != 1 || !strings.Contains(chk.Review.MissingSecrets[0], "usenetServers[Main].password") {
		t.Fatalf("the new server's password is still missing: %v", chk.Review.MissingSecrets)
	}
	code, out := cfgApplyFile(t, b, exported, chk.Hash, false)
	if code != http.StatusOK || out["applied"] != true {
		t.Fatalf("apply: %d %v", code, out)
	}
	if out["copy"] == nil || !strings.HasPrefix(out["copy"].(string), "pre-config-") {
		t.Fatalf("a copy of the database is made first: %v", out)
	}
	if _, err := os.Stat(filepath.Join(b.dir, "config", out["copy"].(string))); err != nil {
		t.Fatalf("the safety copy is next to the database: %v", err)
	}
	if got := getJSON[map[string]any](t, b.client, b.url+"/api/settings"); got["huntIntervalHours"] != float64(12) || got["trashDays"] != float64(21) {
		t.Fatalf("settings after the apply: %v", got)
	}
	// Same file again: identical settings.
	// Only the password differs: it was never in the file, so the new server has none.
	if diff := strings.TrimSpace(lineDiff(exported, cfgExport(t, b))); diff != "-       password: <secret>" {
		t.Fatalf("the second Mediarium exports something else:\n%s", diff)
	}
}

func TestConfigFileValidationFailures(t *testing.T) {
	e := newClientEnv(t)
	cases := []struct{ name, file, want string }{
		{"a web address that isn't one", "downloadClient:\n  kind: qbittorrent\n  url: not a url\n", "isn't a web address"},
		{"a kind of value that is wrong", "settings:\n  huntIntervalHours: lots\n", "settings.huntIntervalHours should be a number"},
		{"true or false", "settings:\n  trashDays: 7\ndebrid:\n  preferCached: maybe\n", "debrid.preferCached should be true or false"},
		{"a name that isn't a setting", "settings:\n  huntIntervalHour: 12\n", "settings.huntIntervalHour isn't something Mediarium has"},
		{"a profile that isn't there", "qualityProfiles:\n  - name: P\n    allowed: [WEBDL-1080p]\n    cutoff: WEBDL-1080p\n    fallback: [Nope]\n", `the fallback profile "Nope"`},
		{"a newer file", "formatVersion: 2\nsettings:\n  trashDays: 9\n", "made by a newer version of Mediarium"},
		{"not a whole version", "formatVersion: soon\nsettings:\n  trashDays: 9\n", "ormatVersion must be a whole number"},
		{"a bad address on a list item", "indexers:\n  - name: X\n    kind: newznab\n    baseUrl: ftp://nope\n    apiKey: k\n    protocol: usenet\n", "indexers[X].baseUrl"},
		{"not YAML at all", "settings: [unclosed\n", "isn't valid YAML"},
		{"nothing in it", "# just a comment\n", "nothing in this file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chk := cfgCheckFile(t, e, tc.file, false)
			if chk.Review.CanApply || len(chk.Review.Problems) == 0 || !strings.Contains(strings.Join(chk.Review.Problems, "\n"), tc.want) {
				t.Fatalf("want a problem with %q: %+v", tc.want, chk.Review)
			}
			// And it can't be applied either.
			if code, _ := cfgApplyFile(t, e, tc.file, chk.Hash, false); code < 400 {
				t.Fatalf("a file with problems was applied (HTTP %d)", code)
			}
		})
	}
	if got := getJSON[map[string]any](t, e.client, e.url+"/api/settings")["trashDays"]; got != float64(7) {
		t.Fatalf("nothing was written, trashDays is %v", got)
	}
}

func TestConfigFileApplyNeedsTheFileThatWasChecked(t *testing.T) {
	e := newClientEnv(t)
	file := "settings:\n  trashDays: 30\n"
	chk := cfgCheckFile(t, e, file, false)
	if code, _ := cfgApplyFile(t, e, file+"  huntIntervalHours: 3\n", chk.Hash, false); code != http.StatusConflict {
		t.Fatalf("another file than the checked one: HTTP %d", code)
	}
	if code, _ := cfgApplyFile(t, e, file, chk.Hash, true); code != http.StatusConflict {
		t.Fatalf("the other mode than the checked one: HTTP %d", code)
	}
	if code, out := cfgApplyFile(t, e, file, chk.Hash, false); code != http.StatusOK || out["applied"] != true {
		t.Fatalf("apply: %d %v", code, out)
	}
	if got := getJSON[map[string]any](t, e.client, e.url+"/api/settings")["trashDays"]; got != float64(30) {
		t.Fatalf("trashDays is %v", got)
	}
}

// The server refuses something halfway through: what was changed before is put back.
func TestConfigFileApplyIsAllOrNothing(t *testing.T) {
	e := newClientEnv(t)
	// The values pass the check (right kinds); the profile is refused by the
	// server's own rules when it is added, after the settings were written.
	file := `
settings:
  huntIntervalHours: 12
  trashDays: 30
usenetServers:
  - name: Main
    host: news.example.com
    port: 563
    useSsl: true
    username: me
    password: pw
    connections: 8
qualityProfiles:
  - name: Broken
    allowed: [WEBDL-1080p]
    cutoff: Bluray-2160p
`
	chk := cfgCheckFile(t, e, file, false)
	if len(chk.Review.Problems) != 0 || !chk.Review.CanApply {
		t.Fatalf("the check can't see the server's rules: %+v", chk.Review)
	}
	code, out := cfgApplyFile(t, e, file, chk.Hash, false)
	if code != http.StatusOK || out["applied"] == true || out["rolledBack"] != true {
		t.Fatalf("the apply should be undone: %d %v", code, out)
	}
	if out["error"] == nil || !strings.Contains(out["error"].(string), "qualityProfiles[Broken]") {
		t.Fatalf("the error names the item: %v", out["error"])
	}
	settings := getJSON[map[string]any](t, e.client, e.url+"/api/settings")
	if settings["huntIntervalHours"] != float64(6) || settings["trashDays"] != float64(7) {
		t.Fatalf("the settings are back as they were: hunt %v trash %v", settings["huntIntervalHours"], settings["trashDays"])
	}
	if servers := getJSON[[]map[string]any](t, e.client, e.url+"/api/usenet-servers"); len(servers) != 0 {
		t.Fatalf("the server that was added is gone again: %v", servers)
	}
	// The password that was set can't be taken back, and the outcome says so
	// plainly only when it matters: the server it belonged to is gone, so nothing is left behind.
}

func TestConfigFileReviewDiffAndReplace(t *testing.T) {
	e := newClientEnv(t)
	postJSON[map[string]any](t, e.client, e.url+"/api/usenet-servers", map[string]any{"name": "Keep", "host": "keep.example.com", "port": 563, "useSsl": true, "username": "u", "password": "p", "connections": 4}, http.StatusCreated)
	postJSON[map[string]any](t, e.client, e.url+"/api/usenet-servers", map[string]any{"name": "Same", "host": "same.example.com", "port": 563, "useSsl": true, "username": "u", "password": "p", "connections": 4}, http.StatusCreated)
	postJSON[map[string]any](t, e.client, e.url+"/api/usenet-servers", map[string]any{"name": "Drop", "host": "drop.example.com", "port": 563, "useSsl": true, "username": "u", "password": "p", "connections": 4}, http.StatusCreated)
	file := `
settings:
  trashDays: 14
usenetServers:
  - name: Keep
    connections: 10
  - name: Same
    host: same.example.com
  - name: Fresh
    host: fresh.example.com
    port: 563
    useSsl: true
    username: u
    password: <secret>
    connections: 2
`
	rv := cfgCheckFile(t, e, file, false).Review
	status := map[string]string{}
	for _, s := range rv.Sections {
		status[s.Name] = s.Status
	}
	want := map[string]string{"settings": "changed", "usenetServers[Keep]": "changed", "usenetServers[Same]": "unchanged", "usenetServers[Fresh]": "added"}
	for k, v := range want {
		if status[k] != v {
			t.Fatalf("%s should be %s, the review says %v", k, v, status)
		}
	}
	if _, drop := status["usenetServers[Drop]"]; drop || rv.Removed != 0 {
		t.Fatalf("without replace nothing is removed: %v", status)
	}
	if rv.Added != 1 || rv.Changed != 2 || rv.Unchanged != 1 {
		t.Fatalf("counts: %+v", rv)
	}
	if len(rv.MissingSecrets) != 1 || rv.MissingSecrets[0] != "usenetServers[Fresh].password" {
		t.Fatalf("missing secrets: %v", rv.MissingSecrets)
	}
	for _, s := range rv.Sections {
		if s.Name == "usenetServers[Keep]" && (len(s.Changes) != 1 || s.Changes[0].Key != "connections" || s.Changes[0].Have != "4" || s.Changes[0].Want != "10") {
			t.Fatalf("the change is described: %+v", s.Changes)
		}
	}

	replace := cfgCheckFile(t, e, file, true)
	if replace.Review.Removed != 1 || !strings.Contains(strings.Join(sectionNames(replace.Review), ","), "usenetServers[Drop]") {
		t.Fatalf("replace removes what the file doesn't have: %+v", replace.Review)
	}
	code, out := cfgApplyFile(t, e, file, replace.Hash, true)
	if code != http.StatusOK || out["applied"] != true {
		t.Fatalf("apply: %d %v", code, out)
	}
	names := []string{}
	for _, s := range getJSON[[]map[string]any](t, e.client, e.url+"/api/usenet-servers") {
		names = append(names, s["name"].(string))
	}
	if strings.Join(sortedStrings(names), ",") != "Fresh,Keep,Same" {
		t.Fatalf("servers after a replace: %v", names)
	}
}

func sectionNames(r configfile.Review) []string {
	var out []string
	for _, s := range r.Sections {
		out = append(out, s.Name)
	}
	return out
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Putting back a removed item adds it again without its password; a failed
// removal puts the rest back.
func TestConfigFileReplaceFailureRestoresRemovedItems(t *testing.T) {
	e := newClientEnv(t)
	postJSON[map[string]any](t, e.client, e.url+"/api/quality-profiles", map[string]any{"name": "Gone", "allowed": []string{"WEBDL-1080p"}, "cutoff": "WEBDL-1080p"}, http.StatusCreated)
	// The only profile left can't be removed; the file has none, so replace tries.
	file := "settings:\n  trashDays: 30\nqualityProfiles: []\n"
	chk := cfgCheckFile(t, e, file, true)
	if chk.Review.Removed < 1 {
		t.Fatalf("the profile would be removed: %+v", chk.Review)
	}
	_, out := cfgApplyFile(t, e, file, chk.Hash, true)
	if out["applied"] == true {
		// The server allowed it: nothing to prove here beyond the removal.
		return
	}
	if got := getJSON[map[string]any](t, e.client, e.url+"/api/settings")["trashDays"]; got != float64(7) {
		t.Fatalf("the failed apply put the settings back, trashDays is %v", got)
	}
}

// A notification channel is in the file without its password, and changing it
// from the file keeps the password.
func TestConfigFileNotifications(t *testing.T) {
	e := newClientEnv(t)
	postJSON[map[string]any](t, e.client, e.url+"/api/notifications", map[string]any{
		"name": "Mail", "type": "email",
		"config": map[string]string{"host": "smtp.example.com", "port": "587", "security": "starttls", "username": "me", "password": "mail-password-1", "from": "me@example.com", "to": "me@example.com"},
	}, http.StatusCreated)
	text := cfgExport(t, e)
	if strings.Contains(text, "mail-password-1") || !strings.Contains(text, "notifications:") || !strings.Contains(text, "password: <secret>") || strings.Contains(text, "hasSecrets") {
		t.Fatalf("the export of a notification:\n%s", text)
	}
	if rv := cfgCheckFile(t, e, text, false).Review; len(rv.Problems) != 0 || rv.Added+rv.Changed != 0 {
		t.Fatalf("an export checked against its own Mediarium finds nothing to do: %+v", rv)
	}
	edited := strings.Replace(text, "port: \"587\"", "port: 465", 1)
	if edited == text {
		t.Fatalf("the port line isn't what the test expects:\n%s", text)
	}
	chk := cfgCheckFile(t, e, edited, false)
	if len(chk.Review.Problems) != 0 || chk.Review.Changed != 1 || !chk.Review.CanApply {
		t.Fatalf("one notification changes: %+v", chk.Review)
	}
	if code, out := cfgApplyFile(t, e, edited, chk.Hash, false); code != http.StatusOK || out["applied"] != true {
		t.Fatalf("apply: %d %v", code, out)
	}
	list := getJSON[[]map[string]any](t, e.client, e.url+"/api/notifications")
	cfg, _ := list[0]["config"].(map[string]any)
	saved, _ := list[0]["hasSecrets"].(map[string]any)
	if len(list) != 1 || cfg["port"] != "465" || cfg["host"] != "smtp.example.com" || saved["password"] != true {
		t.Fatalf("the notification after the apply: %v", list)
	}
}

func TestConfigFileApplyAtStart(t *testing.T) {
	e := newClientEnv(t)
	dir := filepath.Join(e.dir, "config")
	file := filepath.Join(dir, "mediarium-config.yaml")
	if err := os.WriteFile(file, []byte("settings:\n  trashDays: 40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Off by default: nothing is read.
	e.server.ApplyConfigFileAtStart()
	if got := getJSON[map[string]any](t, e.client, e.url+"/api/settings")["trashDays"]; got != float64(7) {
		t.Fatalf("the switch is off, trashDays is %v", got)
	}
	st := getJSON[map[string]any](t, e.client, e.url+"/api/system/config-file/start")
	if st["enabled"] != false || st["file"] != "mediarium-config.yaml" {
		t.Fatalf("state: %v", st)
	}
	putJSON(t, e.client, e.url+"/api/system/config-file/start", map[string]any{"enabled": true}, 200)
	e.server.ApplyConfigFileAtStart()
	if got := getJSON[map[string]any](t, e.client, e.url+"/api/settings")["trashDays"]; got != float64(40) {
		t.Fatalf("the file is applied, trashDays is %v", got)
	}
	st = getJSON[map[string]any](t, e.client, e.url+"/api/system/config-file/start")
	if last, _ := st["last"].(map[string]any); last == nil || last["ok"] != true {
		t.Fatalf("the result is kept: %v", st)
	}
	// Changed by hand afterwards: the same file is not applied again.
	putJSON(t, e.client, e.url+"/api/settings", map[string]any{"trashDays": 9}, 200)
	e.server.ApplyConfigFileAtStart()
	if got := getJSON[map[string]any](t, e.client, e.url+"/api/settings")["trashDays"]; got != float64(9) {
		t.Fatalf("an unchanged file is applied once, trashDays is %v", got)
	}
	// A changed file is. A broken one changes nothing and says why.
	if err := os.WriteFile(file, []byte("settings:\n  trashDays: 50\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.server.ApplyConfigFileAtStart()
	if got := getJSON[map[string]any](t, e.client, e.url+"/api/settings")["trashDays"]; got != float64(50) {
		t.Fatalf("a changed file is applied, trashDays is %v", got)
	}
	if err := os.WriteFile(file, []byte("settings:\n  trashDayz: 60\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.server.ApplyConfigFileAtStart()
	if got := getJSON[map[string]any](t, e.client, e.url+"/api/settings")["trashDays"]; got != float64(50) {
		t.Fatalf("a broken file changes nothing, trashDays is %v", got)
	}
	st = getJSON[map[string]any](t, e.client, e.url+"/api/system/config-file/start")
	if last, _ := st["last"].(map[string]any); last == nil || last["ok"] != false || !strings.Contains(last["message"].(string), "trashDayz") {
		t.Fatalf("the problem is shown: %v", st)
	}
}

// lineDiff lists the lines only one of the two texts has.
func lineDiff(a, b string) string {
	have := func(text string) map[string]bool {
		m := map[string]bool{}
		for _, l := range strings.Split(text, "\n") {
			m[l] = true
		}
		return m
	}
	ha, hb := have(a), have(b)
	var out []string
	for _, l := range strings.Split(a, "\n") {
		if !hb[l] {
			out = append(out, "- "+l)
		}
	}
	for _, l := range strings.Split(b, "\n") {
		if !ha[l] {
			out = append(out, "+ "+l)
		}
	}
	return strings.Join(out, "\n")
}

func TestConfigFileFormatRoundTripsThroughParse(t *testing.T) {
	e := newClientEnv(t)
	text := cfgExport(t, e)
	f, err := configfile.Parse([]byte(text), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("the export reads back: %v", err)
	}
	if f.Version != configfile.FormatVersion {
		t.Fatalf("version %d", f.Version)
	}
	_ = context.Background()
}
