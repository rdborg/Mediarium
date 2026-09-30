package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

// The subtitles switch: off by default, and while it is off nothing searches
// for or downloads subtitles and nothing in the app nags about them.

// subtitleHealthIDs are the health items that are about subtitles.
func subtitleHealthIDs(t *testing.T, env *tvAutoEnv) []string {
	t.Helper()
	var out []string
	for id := range healthItemIDs(t, env) {
		if strings.HasPrefix(id, "subtitles-") || strings.HasPrefix(id, "opensubtitles-") {
			out = append(out, id)
		}
	}
	return out
}

func setSubtitleSwitches(t *testing.T, env *tvAutoEnv, enabled, auto bool) {
	t.Helper()
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitlesEnabled": enabled, "subtitleAutoDownload": auto}, http.StatusOK)
}

func TestSubtitlesAreOffByDefaultAndTheSwitchIsSaved(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	if s := getJSON[map[string]any](t, env.client, env.baseURL+"/api/settings"); s["subtitlesEnabled"] != false {
		t.Fatalf("subtitles should be off on a new install, got %v", s["subtitlesEnabled"])
	}
	if s := putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitlesEnabled": true}, http.StatusOK); s["subtitlesEnabled"] != true {
		t.Fatalf("switching on was not saved: %v", s["subtitlesEnabled"])
	}
	// A save that leaves the switch out keeps it as it is.
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleAutoDownload": true}, http.StatusOK)
	if s := getJSON[map[string]any](t, env.client, env.baseURL+"/api/settings"); s["subtitlesEnabled"] != true {
		t.Fatalf("the switch changed by itself: %v", s["subtitlesEnabled"])
	}
	if s := putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitlesEnabled": false}, http.StatusOK); s["subtitlesEnabled"] != false {
		t.Fatalf("switching off was not saved: %v", s["subtitlesEnabled"])
	}
	// The other subtitle choices are remembered while it is off.
	if s := getJSON[map[string]any](t, env.client, env.baseURL+"/api/settings"); s["subtitleAutoDownload"] != true {
		t.Fatalf("the automatic choice should be kept while subtitles are off: %v", s["subtitleAutoDownload"])
	}
}

func TestSubtitleRoutesRefuseWhileTheSwitchIsOff(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL) // switches on
	m := seedDownloadedMovie(t, env, 9, "Some Movie")
	setSubtitleSwitches(t, env, false, false)

	routes := []struct {
		name, method, path string
		body               any
	}{
		{"wanted list", http.MethodGet, "/api/subtitles/wanted", nil},
		{"get now", http.MethodPost, "/api/subtitles/get", map[string]any{"all": true}},
		{"sweep", http.MethodPost, "/api/subtitles/sweep", nil},
		{"quota", http.MethodGet, "/api/subtitles/quota", nil},
		{"dismiss", http.MethodPost, "/api/subtitles/dismiss", items("movie", m.ID)},
		{"restore", http.MethodDelete, "/api/subtitles/dismiss", items("movie", m.ID)},
		{"movie search", http.MethodGet, "/api/movies/1/subtitles?lang=en", nil},
		{"movie download", http.MethodPost, "/api/movies/1/subtitles/download", map[string]any{"fileId": 7, "language": "en"}},
		{"movie status", http.MethodGet, "/api/movies/1/subtitles/status", nil},
		{"episode search", http.MethodGet, "/api/episodes/1/subtitles?lang=en", nil},
		{"episode download", http.MethodPost, "/api/episodes/1/subtitles/download", map[string]any{"fileId": 7, "language": "en"}},
		{"episode status", http.MethodGet, "/api/episodes/1/subtitles/status", nil},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			got := postJSONMethod[map[string]any](t, env.client, rt.method, env.baseURL+rt.path, rt.body, http.StatusConflict)
			msg, _ := got["error"].(string)
			if !strings.HasPrefix(msg, "Subtitles are switched off. Turn them on in Settings") {
				t.Fatalf("unexpected message: %q", msg)
			}
		})
	}
	if n := fake.searches.Load(); n != 0 {
		t.Fatalf("OpenSubtitles was asked %d times while subtitles were off", n)
	}

	// With it on, the same routes answer normally.
	setSubtitleSwitches(t, env, true, false)
	getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/subtitles/wanted")
	getJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/quota")
	getJSON[map[string]any](t, env.client, env.baseURL+fmt.Sprintf("/api/movies/%d", m.ID)+"/subtitles/status")
	if res := getJSON[[]map[string]any](t, env.client, env.baseURL+fmt.Sprintf("/api/movies/%d", m.ID)+"/subtitles?lang=en"); len(res) != 1 {
		t.Fatalf("expected the fixture subtitle, got %v", res)
	}
}

// The switch decides what is searched, downloaded and mentioned. Each row is a
// combination of the master switch and the "download automatically" choice.
func TestSubtitleSwitchDecidesSearchesDownloadsAndNotices(t *testing.T) {
	cases := []struct {
		name             string
		enabled, auto    bool
		wantSweepSearch  bool     // the scheduled sweep looks for subtitles
		wantImportSearch bool     // a fresh import looks for subtitles
		wantHealth       []string // subtitle health items, sorted
		wantWantedStatus int      // GET /api/subtitles/wanted
	}{
		{"off", false, false, false, false, nil, http.StatusConflict},
		{"off, automatic remembered", false, true, false, false, nil, http.StatusConflict},
		{"on, ask me", true, false, false, false, []string{"subtitles-offer"}, http.StatusOK},
		{"on, automatic", true, true, true, true, []string{"subtitles-anonymous"}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTVAutoEnv(t, nil, nil)
			fake := newFakeOpenSubtitles(t)
			env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
			seedDownloadedMovie(t, env, 9, "Some Movie") // the scheduled sweep looks at this
			setSubtitleSwitches(t, env, tc.enabled, tc.auto)

			got := subtitleHealthIDs(t, env)
			if strings.Join(got, ",") != strings.Join(tc.wantHealth, ",") {
				t.Fatalf("subtitle health items = %v, want %v", got, tc.wantHealth)
			}
			if status := statusOf(t, env, http.MethodGet, "/api/subtitles/wanted"); status != tc.wantWantedStatus {
				t.Fatalf("wanted list answered %d, want %d", status, tc.wantWantedStatus)
			}

			env.server.TestSubtitleSweepJob(context.Background())
			if searched := fake.searches.Load() > 0; searched != tc.wantSweepSearch {
				t.Fatalf("sweep searched = %v, want %v", searched, tc.wantSweepSearch)
			}

			// Then a title arrives that has no subtitles yet.
			imported := seedDownloadedMovie(t, env, 10, "Fresh Import")
			before := fake.searches.Load()
			env.server.TestAutoSubtitlesForMovie(imported)
			if tc.wantImportSearch {
				deadline := time.Now().Add(5 * time.Second)
				for fake.searches.Load() == before && time.Now().Before(deadline) {
					time.Sleep(20 * time.Millisecond)
				}
				if fake.searches.Load() == before {
					t.Fatal("a fresh import should have looked for subtitles")
				}
			} else {
				time.Sleep(150 * time.Millisecond)
				if fake.searches.Load() != before {
					t.Fatal("a fresh import looked for subtitles although they are not wanted")
				}
			}
		})
	}
}

// The shared-key limit notices are about downloading subtitles too, so they
// go away with the switch.
func TestSharedKeyLimitNoticesFollowTheSwitch(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
	env.server.SetBuiltinKeys(api.BuiltinKeys{OpenSubtitles: "shared"})
	env.server.Usage.RecordLimitHit("opensubtitles")

	setSubtitleSwitches(t, env, true, false)
	if _, ok := healthItemIDs(t, env)["opensubtitles-limit"]; !ok {
		t.Fatalf("with subtitles on, the shared key limit should be mentioned: %v", subtitleHealthIDs(t, env))
	}
	setSubtitleSwitches(t, env, false, false)
	if got := subtitleHealthIDs(t, env); len(got) != 0 {
		t.Fatalf("with subtitles off nothing about them should be listed, got %v", got)
	}
}

func statusOf(t *testing.T, env *tvAutoEnv, method, path string) int {
	t.Helper()
	req, err := http.NewRequest(method, env.baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}
