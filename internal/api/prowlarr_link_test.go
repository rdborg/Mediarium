package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A linked Prowlarr's indexers are added as Prowlarr's own feeds and kept in
// step: renamed, switched off and removed with it, while what was set on
// Mediarium's side stays (#134, mx4k).
func TestProwlarrLinkKeepsIndexersInStep(t *testing.T) {
	var mu sync.Mutex
	list := []map[string]any{
		{"id": 1, "name": "TorrentSyndikat", "protocol": "torrent", "enable": true, "priority": 10},
		{"id": 2, "name": "NZBgeek", "protocol": "usenet", "enable": true, "priority": 25},
	}
	prowlarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "pkey" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/status":
			w.Write([]byte(`{"appName":"Prowlarr","version":"1.30.0"}`))
		case "/api/v1/indexer":
			mu.Lock()
			defer mu.Unlock()
			_ = json.NewEncoder(w).Encode(list)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(prowlarr.Close)

	_, base, client := loginNewServer(t)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings/prowlarr", map[string]any{"url": prowlarr.URL, "apiKey": "wrong"}, http.StatusBadRequest)
	st := postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings/prowlarr", map[string]any{"url": prowlarr.URL, "apiKey": "pkey", "sync": true}, http.StatusOK)
	if st["linked"] != float64(2) || st["sync"] != true || st["hasKey"] != true {
		t.Fatalf("after linking: %v", st)
	}
	idx := getJSON[[]map[string]any](t, client, base+"/api/indexers")
	if len(idx) != 2 {
		t.Fatalf("indexers: %v", idx)
	}
	var syndikat map[string]any
	for _, i := range idx {
		if i["name"] == "TorrentSyndikat" {
			syndikat = i
		}
		if i["fromProwlarr"] != true {
			t.Errorf("%v should say it comes from Prowlarr", i["name"])
		}
	}
	if syndikat == nil || syndikat["protocol"] != "torrent" || syndikat["baseUrl"] != prowlarr.URL+"/1" {
		t.Fatalf("the torrent indexer as Prowlarr's feed: %v", syndikat)
	}
	// Something set on Mediarium's side stays through a sync.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/indexers/"+jsonID(int64(syndikat["id"].(float64))), map[string]any{"minSeeders": 7}, http.StatusOK)

	mu.Lock()
	list[0]["name"] = "TorrentSyndikat (VIP)"
	list[0]["enable"] = false
	list = list[:1] // NZBgeek was removed from Prowlarr
	mu.Unlock()
	st = postJSON[map[string]any](t, client, base+"/api/settings/prowlarr/sync", nil, http.StatusOK)
	if last := st["last"].(map[string]any); last["ok"] != true {
		t.Fatalf("sync: %v", st)
	}
	idx = getJSON[[]map[string]any](t, client, base+"/api/indexers")
	if len(idx) != 1 || idx[0]["name"] != "TorrentSyndikat (VIP)" || idx[0]["enabled"] != false || idx[0]["minSeeders"] != float64(7) {
		t.Fatalf("after Prowlarr changed: %v", idx)
	}
}
