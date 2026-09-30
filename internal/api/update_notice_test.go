package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGitHub answers the release list like api.github.com does.
type fakeGitHub struct {
	srv     *httptest.Server
	mu      sync.Mutex
	status  int
	body    string
	hits    int
	agents  []string
	headers []http.Header
}

func newFakeGitHub(t *testing.T, body string) *fakeGitHub {
	f := &fakeGitHub{status: 200, body: body}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits++
		f.agents = append(f.agents, r.Header.Get("User-Agent"))
		f.headers = append(f.headers, r.Header.Clone())
		if r.URL.Path != "/repos/rdborg/Mediarium/releases" {
			http.NotFound(w, r)
			return
		}
		if f.status == http.StatusForbidden {
			w.Header().Set("X-RateLimit-Remaining", "0")
		}
		w.WriteHeader(f.status)
		fmt.Fprint(w, f.body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) set(status int, body string) {
	f.mu.Lock()
	f.status, f.body = status, body
	f.mu.Unlock()
}

func (f *fakeGitHub) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits
}

func releaseJSON(items ...string) string { return "[" + strings.Join(items, ",") + "]" }

func oneRelease(tag string, pre bool, body string) string {
	b, _ := json.Marshal(map[string]any{"tag_name": tag, "name": "Mediarium " + tag, "body": body, "prerelease": pre, "draft": false, "published_at": "2026-10-01T10:00:00Z"})
	return string(b)
}

func check(t *testing.T, e *updateEnv) map[string]any {
	t.Helper()
	return postJSON[map[string]any](t, e.client, e.base+"/api/system/update/check", nil, http.StatusOK)
}

func TestUpdateNoticeVersions(t *testing.T) {
	list := releaseJSON(
		oneRelease("v1.10.0", false, "## Added\n- Faster search\n"),
		oneRelease("v1.9.0", false, "old"),
		oneRelease("v2.0.0-rc.1", true, "soon"),
		oneRelease("nightly", false, "junk"),
	)
	cases := []struct {
		running   string
		available bool
		latest    string
	}{
		{"1.9.0", true, "1.10.0"},
		{"1.9.5", true, "1.10.0"},
		{"1.10.0", false, "1.10.0"},
		{"1.11.0", false, "1.10.0"},
		{"2.0.0-beta.1", true, "2.0.0-rc.1"}, // a pre-release install is offered pre-releases
		{"1.8.0-rc.1", true, "2.0.0-rc.1"},
		{"dev", false, "1.10.0"}, // a build with no number is never told to update
	}
	for _, tc := range cases {
		t.Run(tc.running, func(t *testing.T) {
			e := newUpdateEnv(t)
			e.server.TestSetUpdateEnv(tc.running, "")
			gh := newFakeGitHub(t, list)
			e.server.TestSetGitHub(gh.srv.URL)
			n := check(t, e)
			if n["available"] != tc.available {
				t.Fatalf("available = %v, want %v (%v)", n["available"], tc.available, n)
			}
			latest, _ := n["latest"].(map[string]any)
			if latest == nil || latest["version"] != tc.latest {
				t.Fatalf("latest = %v, want %s", latest, tc.latest)
			}
			if !strings.HasPrefix(fmt.Sprint(latest["url"]), "https://github.com/rdborg/Mediarium/releases/tag/v") {
				t.Fatalf("release page = %v", latest["url"])
			}
			if n["running"] != tc.running || n["error"] != nil || n["checkedAt"] == nil {
				t.Fatalf("notice = %v", n)
			}
		})
	}
}

func TestUpdateNoticeCarriesShortPlainNotes(t *testing.T) {
	e := newUpdateEnv(t)
	var lines []string
	for i := 1; i <= 15; i++ {
		lines = append(lines, fmt.Sprintf("- change %d <script>alert(1)</script>", i))
	}
	gh := newFakeGitHub(t, releaseJSON(oneRelease("v1.2.0", false, "## What's new\n"+strings.Join(lines, "\n"))))
	e.server.TestSetGitHub(gh.srv.URL)
	n := check(t, e)
	latest := n["latest"].(map[string]any)
	notes := latest["notes"].(string)
	if strings.Contains(notes, "<") {
		t.Fatalf("markup got through: %q", notes)
	}
	if got := strings.Count(notes, "\n") + 1; got != 10 || latest["moreNotes"] != true {
		t.Fatalf("notes are %d lines (moreNotes %v): %q", got, latest["moreNotes"], notes)
	}
}

func TestUpdateNoticeIsQuietWhenItCannotCheck(t *testing.T) {
	e := newUpdateEnv(t)
	gh := newFakeGitHub(t, releaseJSON(oneRelease("v1.2.0", false, "")))
	e.server.TestSetGitHub(gh.srv.URL)

	before := getJSON[map[string]any](t, e.client, e.base+"/api/system/update/latest")
	if before["available"] != false || before["latest"] != nil || before["enabled"] != true {
		t.Fatalf("before any check: %v", before)
	}

	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"rate limited", 403, `{"message":"API rate limit exceeded"}`}, {"server error", 500, "oops"}, {"garbage", 200, "<html>"}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newUpdateEnv(t)
			gh := newFakeGitHub(t, "")
			gh.set(tc.status, tc.body)
			e.server.TestSetGitHub(gh.srv.URL)
			n := check(t, e) // still a normal 200 answer
			if n["error"] != "Couldn't check just now." || n["available"] != false {
				t.Fatalf("notice = %v", n)
			}
		})
	}

	// No network at all.
	gh.srv.Close()
	n := check(t, e)
	if n["error"] != "Couldn't check just now." {
		t.Fatalf("notice = %v", n)
	}
}

func TestUpdateNoticeKeepsTheLastGoodAnswerWhenACheckFails(t *testing.T) {
	e := newUpdateEnv(t)
	gh := newFakeGitHub(t, releaseJSON(oneRelease("v1.2.0", false, "")))
	e.server.TestSetGitHub(gh.srv.URL)
	if n := check(t, e); n["available"] != true {
		t.Fatalf("%v", n)
	}
	gh.set(500, "down")
	e.server.TestUpdateTick(context.Background(), time.Now().Add(25*time.Hour))
	n := getJSON[map[string]any](t, e.client, e.base+"/api/system/update/latest")
	if n["available"] != true || n["error"] != "Couldn't check just now." {
		t.Fatalf("notice = %v", n)
	}
}

func TestUpdateCheckSendsNothingButTheUserAgent(t *testing.T) {
	e := newUpdateEnv(t)
	e.server.TestSetUpdateEnv("1.1.0", "1.1.0")
	gh := newFakeGitHub(t, releaseJSON(oneRelease("v1.2.0", false, "")))
	e.server.TestSetGitHub(gh.srv.URL)
	check(t, e)
	if len(gh.agents) != 1 || gh.agents[0] != "Mediarium/1.1.0" {
		t.Fatalf("User-Agent = %v", gh.agents)
	}
	for _, name := range []string{"Authorization", "Cookie", "X-Api-Key", "X-Forwarded-For", "Referer"} {
		if v := gh.headers[0].Get(name); v != "" {
			t.Errorf("%s was sent: %q", name, v)
		}
	}
}

func TestDailyCheckFollowsTheSwitchAndTheClock(t *testing.T) {
	e := newUpdateEnv(t)
	gh := newFakeGitHub(t, releaseJSON(oneRelease("v1.2.0", false, "")))
	e.server.TestSetGitHub(gh.srv.URL)
	ctx := context.Background()
	now := time.Now()

	e.server.TestUpdateTick(ctx, now) // the first check is due at once
	if gh.count() != 1 {
		t.Fatalf("hits = %d, want 1", gh.count())
	}
	e.server.TestUpdateTick(ctx, now.Add(3*time.Hour)) // not due yet
	if gh.count() != 1 {
		t.Fatalf("hits = %d after 3 hours, want still 1", gh.count())
	}
	e.server.TestUpdateTick(ctx, now.Add(25*time.Hour)) // due again
	if gh.count() != 2 {
		t.Fatalf("hits = %d after 25 hours, want 2", gh.count())
	}

	// Switched off: no automatic check, but the button still works.
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/system/options", map[string]any{"updateCheck": false}, http.StatusOK)
	e.server.TestUpdateTick(ctx, now.Add(80*time.Hour))
	if gh.count() != 2 {
		t.Fatalf("hits = %d with the check switched off, want 2", gh.count())
	}
	if n := getJSON[map[string]any](t, e.client, e.base+"/api/system/update/latest"); n["enabled"] != false {
		t.Fatalf("notice = %v", n)
	}
}

func TestNewVersionMessageIsSentOncePerVersion(t *testing.T) {
	e := newUpdateEnv(t)
	hook, rec := newHookRecorder(t, 200)
	postJSON[map[string]any](t, e.client, e.base+"/api/notifications", map[string]any{
		"name": "Updates", "type": "webhook", "config": map[string]string{"url": hook.URL}, "events": []string{"update"},
	}, http.StatusCreated)
	other, otherRec := newHookRecorder(t, 200)
	postJSON[map[string]any](t, e.client, e.base+"/api/notifications", map[string]any{
		"name": "Failures only", "type": "webhook", "config": map[string]string{"url": other.URL}, "events": []string{"failed"},
	}, http.StatusCreated)

	gh := newFakeGitHub(t, releaseJSON(oneRelease("v1.2.0", false, "notes")))
	e.server.TestSetGitHub(gh.srv.URL)
	ctx := context.Background()
	now := time.Now()

	e.server.TestUpdateTick(ctx, now)
	waitForDeliveries(t, rec, 1)
	if !strings.Contains(rec.bodies[0], `"event":"update"`) || !strings.Contains(rec.bodies[0], "Mediarium 1.2.0 is available") || !strings.Contains(rec.bodies[0], "releases/tag/v1.2.0") {
		t.Fatalf("message = %s", rec.bodies[0])
	}
	// The same version found again, on later days: no second message.
	e.server.TestUpdateTick(ctx, now.Add(25*time.Hour))
	e.server.TestUpdateTick(ctx, now.Add(50*time.Hour))
	time.Sleep(150 * time.Millisecond)
	if rec.count() != 1 {
		t.Fatalf("%d messages for one version", rec.count())
	}
	// A newer version is a new message.
	gh.set(200, releaseJSON(oneRelease("v1.3.0", false, ""), oneRelease("v1.2.0", false, "")))
	e.server.TestUpdateTick(ctx, now.Add(75*time.Hour))
	waitForDeliveries(t, rec, 2)
	if !strings.Contains(rec.bodies[1], "1.3.0") {
		t.Fatalf("second message = %s", rec.bodies[1])
	}
	if otherRec.count() != 0 {
		t.Fatal("a target that did not ask for updates was sent one")
	}
}

func TestNoMessageWhenAlreadyUpToDate(t *testing.T) {
	e := newUpdateEnv(t)
	hook, rec := newHookRecorder(t, 200)
	postJSON[map[string]any](t, e.client, e.base+"/api/notifications", map[string]any{
		"name": "Updates", "type": "webhook", "config": map[string]string{"url": hook.URL}, "events": []string{"update"},
	}, http.StatusCreated)
	gh := newFakeGitHub(t, releaseJSON(oneRelease("v1.1.0", false, "")))
	e.server.TestSetGitHub(gh.srv.URL)
	n := check(t, e)
	time.Sleep(150 * time.Millisecond)
	if n["available"] != false || rec.count() != 0 {
		t.Fatalf("available %v, %d messages", n["available"], rec.count())
	}
}

func TestOptionsAreSavedAndDefaultsAreRight(t *testing.T) {
	e := newUpdateEnv(t)
	got := getJSON[map[string]any](t, e.client, e.base+"/api/system/options")
	want := map[string]any{"updateCheck": true, "autoInstall": false, "allowPush": false, "autoRestartWhenStuck": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("default %s = %v, want %v", k, got[k], v)
		}
	}
	out := postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/system/options", map[string]any{"autoInstall": true, "autoRestartWhenStuck": false}, http.StatusOK)
	if out["autoInstall"] != true || out["autoRestartWhenStuck"] != false || out["updateCheck"] != true || out["allowPush"] != false {
		t.Fatalf("after change: %v", out)
	}
	// A request that says nothing changes nothing.
	out = postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/system/options", map[string]any{}, http.StatusOK)
	if out["autoInstall"] != true || out["autoRestartWhenStuck"] != false {
		t.Fatalf("empty request changed things: %v", out)
	}
	// The diagnostics report carries the update row, with nothing secret in it.
	diag := getJSON[map[string]any](t, e.client, e.base+"/api/system/diagnostics")
	up, _ := diag["update"].(map[string]any)
	if up == nil || up["autoInstall"] != true || up["supervisor"] == nil || up["allowPush"] != false {
		t.Fatalf("diagnostics update row = %v", diag["update"])
	}
}
