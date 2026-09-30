package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
)

// keyRecorder is a fixture server that remembers which key each request used
// and answers with a configurable status.
type keyRecorder struct {
	srv    *httptest.Server
	status atomic.Int32
	key    atomic.Value // string
	hits   atomic.Int32
}

func newKeyRecorder(t *testing.T, header string) *keyRecorder {
	t.Helper()
	k := &keyRecorder{}
	k.status.Store(http.StatusOK)
	k.key.Store("")
	k.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k.hits.Add(1)
		k.key.Store(r.Header.Get(header))
		w.WriteHeader(int(k.status.Load()))
		fmt.Fprint(w, `{"data":[]}`)
	}))
	t.Cleanup(k.srv.Close)
	return k
}

func (k *keyRecorder) lastKey() string { return k.key.Load().(string) }

func usageService(t *testing.T, env *tvAutoEnv, id string) map[string]any {
	t.Helper()
	u := getJSON[map[string]any](t, env.client, env.baseURL+"/api/usage")
	for _, raw := range u["services"].([]any) {
		svc := raw.(map[string]any)
		if svc["id"] == id {
			return svc
		}
	}
	t.Fatalf("no service %q in %+v", id, u)
	return nil
}

func TestUsageEndpointAndTraktLimitWarning(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	trakt := newKeyRecorder(t, "trakt-api-key")
	env.server.TestSetTraktBaseURL("", trakt.srv.URL)
	env.server.SetBuiltinKeys(api.BuiltinKeys{TraktClientID: "shared-trakt"})

	u := getJSON[map[string]any](t, env.client, env.baseURL+"/api/usage")
	services := u["services"].([]any)
	ids := []string{}
	for _, s := range services {
		ids = append(ids, s.(map[string]any)["id"].(string))
	}
	if strings.Join(ids, ",") != "trakt,opensubtitles,tmdb" {
		t.Fatalf("services = %v", ids)
	}
	svc := usageService(t, env, "trakt")
	if svc["label"] != "Trakt" || svc["usingSharedKey"] != true || svc["usingOwnKey"] != false || svc["requests24h"] != float64(0) || svc["limitHits24h"] != float64(0) || svc["lastLimitHitAt"] != nil {
		t.Fatalf("idle trakt: %+v", svc)
	}
	if _, ok := healthItemIDs(t, env)["trakt-limit"]; ok {
		t.Fatal("no limit warning before any limit hit")
	}

	// Two fine requests, then Trakt starts refusing.
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := env.server.Trakt().Ping(ctx); err != nil {
			t.Fatal(err)
		}
	}
	trakt.status.Store(http.StatusTooManyRequests)
	_ = env.server.Trakt().Ping(ctx)
	_ = env.server.Trakt().Ping(ctx)

	svc = usageService(t, env, "trakt")
	if svc["requests24h"] != float64(4) || svc["limitHits24h"] != float64(2) || svc["lastLimitHitAt"] == nil {
		t.Fatalf("after limit hits: %+v", svc)
	}
	if note := svc["note"].(string); !strings.Contains(note, "Trakt has refused 2 requests") || !strings.Contains(note, "shared key is busy") {
		t.Fatalf("note = %q", note)
	}
	warn := healthItemIDs(t, env)["trakt-limit"]
	if warn == nil || warn["level"] != "warn" || warn["title"] != "The shared Trakt key is at its limit" {
		t.Fatalf("expected the trakt-limit warning, got %+v", healthItemIDs(t, env))
	}
	if a := warn["action"].(map[string]any); a["label"] != "Use my own key" || a["path"] != "/settings/metadata" {
		t.Fatalf("action = %+v", a)
	}
	if !strings.Contains(warn["impact"].(string), "free personal Trakt key") {
		t.Fatalf("impact = %q", warn["impact"])
	}

	// With their own key the warning goes away (the limit is then their own).
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"traktClientId": "my-own-trakt"}, http.StatusOK)
	if _, ok := healthItemIDs(t, env)["trakt-limit"]; ok {
		t.Fatal("the shared-key warning must not show for someone using their own key")
	}
	svc = usageService(t, env, "trakt")
	if svc["usingOwnKey"] != true || svc["usingSharedKey"] != false || !strings.Contains(svc["note"].(string), "your own key") {
		t.Fatalf("own key: %+v", svc)
	}
}

func TestUsersOwnKeysOverrideTheBuiltInOnes(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	trakt := newKeyRecorder(t, "trakt-api-key")
	subs := newKeyRecorder(t, "Api-Key")
	env.server.TestSetTraktBaseURL("", trakt.srv.URL)
	env.server.TestSetSubtitlesBaseURL("", subs.srv.URL)
	env.server.SetBuiltinKeys(api.BuiltinKeys{TMDB: "shared-tmdb", OpenSubtitles: "shared-os", TraktClientID: "shared-trakt"})
	m := seedDownloadedMovie(t, env, 1, "Alpha")
	ctx := context.Background()

	settingsURL := env.baseURL + "/api/settings"
	flags := func() (tmdb, os, trakt bool) {
		s := getJSON[map[string]any](t, env.client, settingsURL)
		return s["tmdbUsingOwnKey"].(bool), s["openSubtitlesUsingOwnKey"].(bool), s["traktUsingOwnKey"].(bool)
	}
	searchSubtitles := func() string {
		getJSON[[]map[string]any](t, env.client, fmt.Sprintf("%s/api/movies/%d/subtitles?lang=en", env.baseURL, m.ID))
		return subs.lastKey()
	}

	// On the shared keys.
	if a, b, c := flags(); a || b || c {
		t.Fatalf("nobody saved a key yet, got own-key flags %v %v %v", a, b, c)
	}
	_ = env.server.Trakt().Ping(ctx)
	if got := trakt.lastKey(); got != "shared-trakt" {
		t.Fatalf("Trakt key = %q, want the built-in one", got)
	}
	if got := searchSubtitles(); got != "shared-os" {
		t.Fatalf("OpenSubtitles key = %q, want the built-in one", got)
	}

	// The person saves their own keys: they win, and the flags say so.
	putJSONStatus(t, env.client, settingsURL, map[string]any{"traktClientId": " my-trakt ", "openSubtitlesApiKey": "my-os", "tmdbApiKey": "my-tmdb"}, http.StatusOK)
	if a, b, c := flags(); !a || !b || !c {
		t.Fatalf("own-key flags after saving: %v %v %v", a, b, c)
	}
	_ = env.server.Trakt().Ping(ctx)
	if got := trakt.lastKey(); got != "my-trakt" {
		t.Fatalf("Trakt key = %q, want the person's own", got)
	}
	if got := searchSubtitles(); got != "my-os" {
		t.Fatalf("OpenSubtitles key = %q, want the person's own", got)
	}

	// Saving an empty value clears their key and falls back to the built-in one.
	putJSONStatus(t, env.client, settingsURL, map[string]any{"traktClientId": "", "openSubtitlesApiKey": ""}, http.StatusOK)
	if a, b, c := flags(); !a || b || c {
		t.Fatalf("own-key flags after clearing (TMDB is left alone): %v %v %v", a, b, c)
	}
	_ = env.server.Trakt().Ping(ctx)
	if got := trakt.lastKey(); got != "shared-trakt" {
		t.Fatalf("Trakt key after clearing = %q, want the built-in one back", got)
	}
	if got := searchSubtitles(); got != "shared-os" {
		t.Fatalf("OpenSubtitles key after clearing = %q, want the built-in one back", got)
	}

	// An update that does not mention the keys leaves them alone.
	putJSONStatus(t, env.client, settingsURL, map[string]any{"traktClientId": "my-trakt"}, http.StatusOK)
	putJSONStatus(t, env.client, settingsURL, map[string]any{"subtitleLanguages": []string{"en"}}, http.StatusOK)
	if _, _, c := flags(); !c {
		t.Fatal("an unrelated update must not clear the person's Trakt key")
	}

	// TMDB: an empty value is ignored, as before (the built-in key is not replaceable in the UI).
	putJSONStatus(t, env.client, settingsURL, map[string]any{"tmdbApiKey": ""}, http.StatusOK)
	if a, _, _ := flags(); !a {
		t.Fatal("an empty tmdbApiKey must not clear the saved key")
	}
}

func TestClearingTheOnlyKeyLeavesNone(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"traktClientId": "mine", "openSubtitlesApiKey": "mine"}, http.StatusOK)
	s := putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"traktClientId": "", "openSubtitlesApiKey": ""}, http.StatusOK)
	if s["hasTraktClientId"] != false || s["hasOpenSubtitlesApiKey"] != false || s["traktUsingOwnKey"] != false {
		t.Fatalf("without a built-in key, clearing leaves no key: %+v", s)
	}
}

func TestOpenSubtitlesLimitAndBusyWarnings(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	fake.dlLimit.Store(6)
	env.server.TestSetSubtitlesBaseURL("", fake.srv.URL)
	env.server.SetBuiltinKeys(api.BuiltinKeys{OpenSubtitles: "shared-os"})
	var ids []int64
	for i := 1; i <= 7; i++ {
		ids = append(ids, seedDownloadedMovie(t, env, i, fmt.Sprintf("Movie%d", i)).ID)
	}

	// Four of six downloads used: under 80%, nothing to say.
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", items("movie", ids[:4]...), http.StatusOK)
	health := healthItemIDs(t, env)
	if _, ok := health["opensubtitles-busy"]; ok {
		t.Fatalf("4 of 6 is not busy: %v", health)
	}

	// Five of six: more than 80% of the reported daily limit.
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", items("movie", ids[4]), http.StatusOK)
	busy := healthItemIDs(t, env)["opensubtitles-busy"]
	if busy == nil || busy["level"] != "info" {
		t.Fatalf("expected the info item at 5 of 6, got %v", healthItemIDs(t, env))
	}
	if a := busy["action"].(map[string]any); a["label"] != "Use my own key" || a["path"] != "/settings/subtitles" {
		t.Fatalf("action = %+v", a)
	}
	if _, ok := healthItemIDs(t, env)["opensubtitles-limit"]; ok {
		t.Fatal("no limit hit yet")
	}

	// The sixth download uses up the limit (remaining 0), which counts as a limit hit.
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", items("movie", ids[5]), http.StatusOK)
	health = healthItemIDs(t, env)
	limit := health["opensubtitles-limit"]
	if limit == nil || limit["level"] != "warn" || limit["title"] != "The shared OpenSubtitles key is at its limit" {
		t.Fatalf("expected the limit warning, got %v", health)
	}
	if _, ok := health["opensubtitles-busy"]; ok {
		t.Fatal("the limit warning replaces the busy notice")
	}
	if a := limit["action"].(map[string]any); a["path"] != "/settings/subtitles" {
		t.Fatalf("action = %+v", a)
	}

	// The seventh is not even attempted: the limit is known to be used up.
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", items("movie", ids[6]), http.StatusOK)
	svc := usageService(t, env, "opensubtitles")
	if svc["limitHits24h"] != float64(1) || svc["lastLimitHitAt"] == nil {
		t.Fatalf("limit hits: %+v", svc)
	}
	if svc["requests24h"].(float64) < 6 || svc["usingSharedKey"] != true {
		t.Fatalf("opensubtitles usage: %+v", svc)
	}

	// Using their own key removes the shared-key warning.
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"openSubtitlesApiKey": "mine"}, http.StatusOK)
	health = healthItemIDs(t, env)
	if _, ok := health["opensubtitles-limit"]; ok {
		t.Fatalf("a personal key must not get the shared-key warning: %v", health)
	}
}
