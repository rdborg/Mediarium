package api_test

import (
	"net/http"
	"testing"
)

func TestMusicModuleIsOffByDefaultAndSwitchesOnInSettings(t *testing.T) {
	_, base, client := loginNewServer(t)

	mods := getJSON[map[string]any](t, client, base+"/api/modules")
	if musicOn(mods) {
		t.Fatalf("music must be off on a fresh install: %v", mods)
	}
	set := getJSON[map[string]any](t, client, base+"/api/settings")
	if set["musicEnabled"] != false || set["musicPath"] != nil {
		// The test config leaves MUSIC_DIR empty, so there is no path.
		t.Fatalf("settings: want musicEnabled false and no path, got %v / %v", set["musicEnabled"], set["musicPath"])
	}

	// A partial PUT that does not mention the module leaves it alone.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"namingPreset": "plex"}, http.StatusOK)
	if mods := getJSON[map[string]any](t, client, base+"/api/modules"); musicOn(mods) {
		t.Fatalf("an unrelated settings change switched music on: %v", mods)
	}

	got := postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{
		"musicEnabled": true, "musicPath": " /data/music ",
	}, http.StatusOK)
	if got["musicEnabled"] != true || got["musicPath"] != "/data/music" {
		t.Fatalf("after switching on: %v / %v", got["musicEnabled"], got["musicPath"])
	}
	if mods := getJSON[map[string]any](t, client, base+"/api/modules"); !musicOn(mods) {
		t.Fatalf("modules should report music on: %v", mods)
	}

	got = postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"musicEnabled": false}, http.StatusOK)
	if got["musicEnabled"] != false || got["musicPath"] != "/data/music" {
		t.Fatalf("switching off must keep the folder: %v / %v", got["musicEnabled"], got["musicPath"])
	}
}

// musicOn reads music.enabled out of a GET /api/modules answer.
func musicOn(mods map[string]any) bool {
	m, _ := mods["music"].(map[string]any)
	on, _ := m["enabled"].(bool)
	return on
}
