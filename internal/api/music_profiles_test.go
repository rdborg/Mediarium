package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestMusicProfilesCanBeEditedAndChosenAsDefault(t *testing.T) {
	e := newMusicEnv(t)
	base := e.base + "/api/music/profiles"

	tiers := getJSON[[]string](t, e.client, e.base+"/api/music/tiers")
	if len(tiers) != 7 || tiers[0] != "Unknown" || tiers[6] != "FLAC 24bit" {
		t.Fatalf("tiers: %v", tiers)
	}
	list := getJSON[[]map[string]any](t, e.client, base)
	lossy, lossless := list[0], list[1]
	if lossy["default"] != true || lossy["inUse"] != float64(0) || lossless["default"] != false {
		t.Fatalf("list: %v", list)
	}

	// Validation.
	for name, body := range map[string]map[string]any{
		"no name":       {"name": " ", "allowed": []string{"FLAC"}, "cutoff": "FLAC"},
		"no tiers":      {"name": "X", "allowed": []string{}, "cutoff": "FLAC"},
		"bad tier":      {"name": "X", "allowed": []string{"WAV"}, "cutoff": "WAV"},
		"cutoff absent": {"name": "X", "allowed": []string{"FLAC"}, "cutoff": "MP3-192"},
		"duplicate":     {"name": "Lossy (MP3 320)", "allowed": []string{"FLAC"}, "cutoff": "FLAC"},
	} {
		if status, out := doJSONStatus(t, e.client, http.MethodPost, base, body); status != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}

	created := postJSON[map[string]any](t, e.client, base, map[string]any{
		"name": "Road trip", "allowed": []string{"MP3-192", "MP3-256", "MP3-320/V0"}, "cutoff": "MP3-256", "upgradeAllowed": true,
		"fallback": []int64{int64(lossy["id"].(float64))},
	}, http.StatusCreated)
	id := int64(created["id"].(float64))
	if created["default"] != false || fmt.Sprint(created["fallback"]) != fmt.Sprint([]any{lossy["id"]}) {
		t.Fatalf("created: %v", created)
	}

	// Update; leaving out fallback keeps the chain.
	updated := postJSONMethod[map[string]any](t, e.client, http.MethodPut, fmt.Sprintf("%s/%d", base, id),
		map[string]any{"name": "Road trip 2", "allowed": []string{"MP3-256", "MP3-320/V0"}, "cutoff": "MP3-320/V0", "upgradeAllowed": false}, http.StatusOK)
	if updated["name"] != "Road trip 2" || updated["cutoff"] != "MP3-320/V0" || len(updated["fallback"].([]any)) != 1 {
		t.Fatalf("updated: %v", updated)
	}
	if status, _ := doJSONStatus(t, e.client, http.MethodPut, base+"/99999", map[string]any{"name": "x", "allowed": []string{"FLAC"}, "cutoff": "FLAC"}); status != http.StatusNotFound {
		t.Fatalf("unknown profile: %d", status)
	}

	// An artist using it: the count shows and it cannot be deleted.
	postJSON[map[string]any](t, e.client, e.base+"/api/music/artists", map[string]any{
		"mbid": "11111111-1111-4111-8111-111111111111", "monitor": "none", "profileId": id,
	}, http.StatusCreated)
	for _, p := range getJSON[[]map[string]any](t, e.client, base) {
		if p["id"] == created["id"] && p["inUse"] != float64(1) {
			t.Fatalf("inUse: %v", p)
		}
	}
	if status, out := doJSONStatus(t, e.client, http.MethodDelete, fmt.Sprintf("%s/%d", base, id), nil); status != http.StatusConflict {
		t.Fatalf("delete in use: %d %v", status, out)
	}

	// The default (the Lossy profile at first): change it in the settings; the old default is then free to go.
	if status, _ := doJSONStatus(t, e.client, http.MethodPut, e.base+"/api/settings", map[string]any{"musicDefaultProfileId": 99999}); status != http.StatusBadRequest {
		t.Fatalf("unknown default: %d", status)
	}
	got := postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/settings", map[string]any{"musicDefaultProfileId": lossless["id"]}, http.StatusOK)
	if got["musicDefaultProfileId"] != lossless["id"] {
		t.Fatalf("settings: %v", got["musicDefaultProfileId"])
	}
	list = getJSON[[]map[string]any](t, e.client, base)
	if list[0]["default"] != false || list[1]["default"] != true {
		t.Fatalf("default flag: %v", list)
	}
	if status, _ := doJSONStatus(t, e.client, http.MethodDelete, fmt.Sprintf("%s/%v", base, lossless["id"]), nil); status != http.StatusConflict {
		t.Fatalf("the default cannot be deleted: %d", status)
	}
	if status, _ := doJSONStatus(t, e.client, http.MethodDelete, fmt.Sprintf("%s/%v", base, lossy["id"]), nil); status != http.StatusOK {
		t.Fatalf("the old default can go now: %d", status)
	}
	if status, _ := doJSONStatus(t, e.client, http.MethodDelete, fmt.Sprintf("%s/%v", base, lossy["id"]), nil); status != http.StatusNotFound {
		t.Fatalf("delete twice: %d", status)
	}

	// Artists with no profile of their own now use the new default.
	other := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/artists")
	if other[0]["profileId"] != created["id"] || other[0]["profileName"] != "Road trip 2" {
		t.Fatalf("artist keeps its own profile: %v", other[0])
	}
}

func TestOnlyAdministratorsEditMusicProfiles(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	server.TestSetMusicBrainz("http://127.0.0.1:1")
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/modules", map[string]any{"music": true}, http.StatusOK)
	body := map[string]any{"name": "Mine", "allowed": []string{"FLAC"}, "cutoff": "FLAC"}
	if status, _ := doJSONStatus(t, member, http.MethodPost, base+"/api/music/profiles", body); status != http.StatusForbidden {
		t.Fatalf("member create: %d", status)
	}
	if status, _ := doJSONStatus(t, member, http.MethodDelete, base+"/api/music/profiles/1", nil); status != http.StatusForbidden {
		t.Fatalf("member delete: %d", status)
	}
	if status, _ := doStatus(t, member, http.MethodGet, base+"/api/music/profiles"); status != http.StatusOK {
		t.Fatalf("member list: %d", status)
	}
}
