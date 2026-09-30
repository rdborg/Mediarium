package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeArrApp answers fixed JSON by path and records every method used.
type fakeArrApp struct {
	*httptest.Server
	mu      sync.Mutex
	methods []string
}

func newFakeArrApp(t *testing.T, key string, routes map[string]any) *fakeArrApp {
	t.Helper()
	f := &fakeArrApp{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.methods = append(f.methods, r.Method)
		f.mu.Unlock()
		if r.Header.Get("X-Api-Key") != key && r.URL.Query().Get("apikey") != key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		route := r.URL.Path
		if mode := r.URL.Query().Get("mode"); mode != "" {
			route += "?" + mode
		}
		body, ok := routes[route]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeArrApp) onlyGET(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.methods) == 0 {
		t.Fatalf("%s was never called", f.URL)
	}
	for _, m := range f.methods {
		if m != http.MethodGet {
			t.Fatalf("the importer sent a %s request to %s", m, f.URL)
		}
	}
}

func TestMigrateFromArrApps(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newImportTMDBServer(t).URL)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	movieFile := writeFile(t, server.TestMoviesRoot(), "Inception (2010)/Inception (2010) Bluray-1080p.mkv")
	radarr := newFakeArrApp(t, "rk", map[string]any{
		"/api/v3/system/status": map[string]any{"appName": "Radarr", "version": "5.14.0"},
		"/api/v3/movie": []map[string]any{{
			"title": "Inception", "year": 2010, "tmdbId": 27205, "monitored": true, "hasFile": true,
			"path": "/data/movies/Inception (2010)", "qualityProfileId": 1,
			"movieFile": map[string]any{"relativePath": "Inception (2010) Bluray-1080p.mkv", "quality": map[string]any{"quality": map[string]any{"name": "Bluray-1080p", "resolution": 1080}}},
		}},
		"/api/v3/qualityprofile": []map[string]any{{"id": 1, "name": "HD-1080p", "cutoff": 7, "items": []map[string]any{{"quality": map[string]any{"id": 7, "name": "Bluray-1080p", "resolution": 1080}, "allowed": true}}}},
		"/api/v3/rootfolder":     []map[string]any{{"path": "/data/movies"}},
	})
	indexer := newTVIndexerWith(t, []string{"Some.Release.1080p"})
	prowlarr := newFakeArrApp(t, "pk", map[string]any{
		"/api/v1/system/status": map[string]any{"appName": "Prowlarr", "version": "1.25.4"},
		"/api/v1/indexer": []map[string]any{{
			"name": "Fixture Indexer", "implementation": "Newznab", "protocol": "usenet", "enable": true,
			"fields": []map[string]any{{"name": "baseUrl", "value": indexer.URL}, {"name": "apiPath", "value": "/api"}, {"name": "apiKey", "value": "idx-key"}},
		}},
	})
	sab := newFakeArrApp(t, "sk", map[string]any{
		"/api?version": map[string]any{"version": "4.3.3"},
		"/api?get_config": map[string]any{"config": map[string]any{
			"misc":    map[string]any{"complete_dir": "/downloads"},
			"servers": []map[string]any{{"name": "news.example.com", "host": "news.example.com", "port": 563, "ssl": 1, "username": "u", "password": "server-password", "connections": 12, "enable": 1, "priority": 0}},
		}},
	})
	body := map[string]any{
		"radarr":   map[string]string{"url": radarr.URL, "apiKey": "rk"},
		"prowlarr": map[string]string{"url": prowlarr.URL, "apiKey": "pk"},
		"sabnzbd":  map[string]string{"url": sab.URL, "apiKey": "sk"},
	}

	postJSON[map[string]any](t, client, httpSrv.URL+"/api/migrate/preview", map[string]any{}, http.StatusBadRequest)

	pv := postJSON[map[string]any](t, client, httpSrv.URL+"/api/migrate/preview", body, http.StatusOK)
	rad := pv["radarr"].(map[string]any)
	if rad["ok"] != true || rad["summary"].(map[string]any)["add"] != float64(1) {
		t.Fatalf("radarr preview: %+v", rad)
	}
	item := rad["items"].([]any)[0].(map[string]any)
	if item["action"] != "add" || item["path"] != filepath.Join(server.TestMoviesRoot(), "Inception (2010)") || item["folderFound"] != true {
		t.Fatalf("radarr item: %+v", item)
	}
	if pm := pv["suggestedPathMap"].([]any); len(pm) != 1 || pm[0].(map[string]any)["from"] != "/data/movies" {
		t.Fatalf("suggested path map: %+v", pv["suggestedPathMap"])
	}
	if pv["sonarr"] != nil {
		t.Fatalf("sonarr wasn't asked for: %+v", pv["sonarr"])
	}
	if strings.Contains(mustJSON(t, pv), "server-password") || strings.Contains(mustJSON(t, pv), "idx-key") {
		t.Fatal("the preview must not echo passwords or API keys")
	}
	if movies := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies"); len(movies) != 0 {
		t.Fatalf("the preview added movies: %+v", movies)
	}

	run := map[string]any{"pathMap": pv["pathMap"]}
	for k, v := range body {
		run[k] = v
	}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/migrate/run", run, http.StatusAccepted)
	var st map[string]any
	deadline := time.Now().Add(15 * time.Second)
	for {
		st = getJSON[map[string]any](t, client, httpSrv.URL+"/api/migrate/status")
		if st["running"] == false {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("import still running: %+v", st)
		}
		time.Sleep(30 * time.Millisecond)
	}
	res := st["results"].(map[string]any)
	if st["step"] != "done" || res["movies"].(map[string]any)["added"] != float64(1) || res["movies"].(map[string]any)["filesLinked"] != float64(1) ||
		res["indexers"].(map[string]any)["added"] != float64(1) || res["indexers"].(map[string]any)["disabled"] != float64(0) ||
		res["usenetServers"].(map[string]any)["added"] != float64(1) {
		t.Fatalf("import results: %+v", st)
	}

	movies := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies")
	if len(movies) != 1 || movies[0]["status"] != "downloaded" || movies[0]["filePath"] != movieFile || movies[0]["quality"] != "Bluray-1080p" {
		t.Fatalf("movie not registered in place: %+v", movies)
	}
	indexers := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/indexers")
	if len(indexers) != 1 || indexers[0]["enabled"] != true || indexers[0]["hasApiKey"] != true || indexers[0]["baseUrl"] != indexer.URL {
		t.Fatalf("indexer: %+v", indexers)
	}
	servers := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/usenet-servers")
	if len(servers) != 1 || servers[0]["hasPassword"] != true || servers[0]["connections"] != float64(12) {
		t.Fatalf("server: %+v", servers)
	}

	// Running again adds nothing.
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/migrate/run", run, http.StatusAccepted)
	for {
		st = getJSON[map[string]any](t, client, httpSrv.URL+"/api/migrate/status")
		if st["running"] == false {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	res = st["results"].(map[string]any)
	if res["movies"].(map[string]any)["existing"] != float64(1) || res["indexers"].(map[string]any)["existing"] != float64(1) || res["usenetServers"].(map[string]any)["existing"] != float64(1) {
		t.Fatalf("second import: %+v", res)
	}
	for _, f := range []*fakeArrApp{radarr, prowlarr, sab} {
		f.onlyGET(t)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
