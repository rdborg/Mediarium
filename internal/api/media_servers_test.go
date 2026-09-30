package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// fakePlexServer is a local stand-in for Plex with one movie library at
// /data/movies holding TMDB 603. It records refresh requests.
type fakePlexServer struct {
	*httptest.Server
	mu        sync.Mutex
	refreshes []string
}

func newFakePlexServer(t *testing.T, token string) *fakePlexServer {
	t.Helper()
	f := &fakePlexServer{}
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": v})
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity" {
			write(w, map[string]any{"machineIdentifier": "plex-machine-1", "version": "1.41.0"})
			return
		}
		if r.Header.Get("X-Plex-Token") != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/":
			write(w, map[string]any{"friendlyName": "Living Room"})
		case "/library/sections":
			write(w, map[string]any{"Directory": []map[string]any{
				{"key": "1", "type": "movie", "title": "Movies", "Location": []map[string]any{{"path": "/data/movies"}}},
			}})
		case "/library/sections/1/refresh":
			f.mu.Lock()
			f.refreshes = append(f.refreshes, r.URL.Query().Get("path"))
			f.mu.Unlock()
		case "/library/sections/1/all":
			write(w, map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "55", "type": "movie", "Guid": []map[string]any{{"id": "tmdb://603"}}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakePlexServer) scans() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.refreshes...)
}

func TestMediaServerManagement(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	plex := newFakePlexServer(t, "plex-token")
	api := base + "/api/media-servers"

	// Bad input is explained, not stored.
	for name, body := range map[string]map[string]any{
		"unknown kind":     {"kind": "kodi", "baseUrl": plex.URL},
		"no kind":          {"baseUrl": plex.URL},
		"bad address":      {"kind": "plex", "baseUrl": "ftp://plex"},
		"half a path map":  {"kind": "plex", "baseUrl": plex.URL, "pathMap": []map[string]string{{"from": "/movies"}}},
		"bad public entry": {"kind": "jellyfin", "baseUrl": plex.URL, "publicUrl": "gopher://x"},
	} {
		got := postJSON[map[string]any](t, admin, api, body, http.StatusBadRequest)
		if got["error"] == nil {
			t.Fatalf("%s: expected an error message, got %+v", name, got)
		}
	}

	// Testing before saving: wrong token, then right.
	res := postJSON[map[string]any](t, admin, api+"/test", map[string]any{"kind": "plex", "baseUrl": plex.URL, "token": "wrong"}, http.StatusOK)
	if res["ok"] != false || !strings.Contains(fmt.Sprint(res["error"]), "refused the token") {
		t.Fatalf("wrong token: %+v", res)
	}
	res = postJSON[map[string]any](t, admin, api+"/test", map[string]any{"kind": "plex", "baseUrl": plex.URL, "token": "plex-token"}, http.StatusOK)
	if res["ok"] != true || res["machineIdentifier"] != "plex-machine-1" || len(res["libraries"].([]any)) != 1 {
		t.Fatalf("good token: %+v", res)
	}

	// Saving: defaults on, token never returned.
	created := postJSON[map[string]any](t, admin, api, map[string]any{
		"kind": "plex", "baseUrl": plex.URL + "/", "token": "plex-token",
		"pathMap": []map[string]string{{"from": "/movies/", "to": "/data/movies"}},
	}, http.StatusCreated)
	if created["name"] != "Plex" || created["hasToken"] != true || created["token"] != nil ||
		created["enabled"] != true || created["refreshAfterImport"] != true || created["baseUrl"] != plex.URL || created["webUrl"] != plex.URL {
		t.Fatalf("created: %+v", created)
	}
	id := int64(created["id"].(float64))
	one := fmt.Sprintf("%s/%d", api, id)

	// Editing with a blank token keeps it; the saved server tests fine and
	// learns its machine identifier.
	updated := putJSONStatus(t, admin, one, map[string]any{"name": "Living room", "token": "", "publicUrl": "https://plex.example.com"}, http.StatusOK)
	if updated["name"] != "Living room" || updated["hasToken"] != true || updated["webUrl"] != "https://plex.example.com" {
		t.Fatalf("updated: %+v", updated)
	}
	res = postJSON[map[string]any](t, admin, one+"/test", nil, http.StatusOK)
	if res["ok"] != true {
		t.Fatalf("saved test: %+v", res)
	}
	// An edited form tested with a blank token uses the saved one.
	res = postJSON[map[string]any](t, admin, api+"/test", map[string]any{"id": id, "kind": "plex", "baseUrl": plex.URL}, http.StatusOK)
	if res["ok"] != true {
		t.Fatalf("test with the stored token: %+v", res)
	}
	list := getJSON[[]map[string]any](t, admin, api)
	if len(list) != 1 || list[0]["machineIdentifier"] != "plex-machine-1" || list[0]["lastError"] != "" || list[0]["lastCheckedAt"] == nil {
		t.Fatalf("list: %+v", list)
	}

	// Refresh now scans the library.
	res = postJSON[map[string]any](t, admin, one+"/refresh", nil, http.StatusOK)
	if res["ok"] != true || len(plex.scans()) != 1 || plex.scans()[0] != "" {
		t.Fatalf("refresh now: %+v scans=%q", res, plex.scans())
	}

	// Members can see where to watch, but not the servers themselves.
	if code, _ := doStatus(t, member, http.MethodGet, api); code != http.StatusForbidden {
		t.Fatalf("member list: %d", code)
	}
	links := getJSON[[]map[string]any](t, member, api+"/links?tmdbId=603&kind=movie")
	wantURL := "https://plex.example.com/web/index.html#!/server/plex-machine-1/details?key=%2Flibrary%2Fmetadata%2F55"
	if len(links) != 1 || links[0]["url"] != wantURL || links[0]["kind"] != "plex" || links[0]["name"] != "Living room" ||
		!strings.HasPrefix(fmt.Sprint(links[0]["appUrl"]), "https://app.plex.tv/desktop/#!/server/plex-machine-1/") {
		t.Fatalf("links: %+v", links)
	}
	if none := getJSON[[]map[string]any](t, member, api+"/links?tmdbId=604&kind=movie"); len(none) != 0 {
		t.Fatalf("a title the server doesn't have: %+v", none)
	}
	home := getJSON[[]map[string]any](t, member, api+"/links")
	if len(home) != 1 || home[0]["url"] != "https://plex.example.com/web/index.html" {
		t.Fatalf("home links: %+v", home)
	}
	if code, _ := doStatus(t, member, http.MethodGet, api+"/links?tmdbId=abc"); code != http.StatusBadRequest {
		t.Fatalf("bad tmdbId: %d", code)
	}

	// A server that stops answering shows on the dashboard until it works again.
	plex.Close()
	res = postJSON[map[string]any](t, admin, one+"/test", nil, http.StatusOK)
	if res["ok"] != false || !strings.Contains(fmt.Sprint(res["error"]), "Could not reach Plex") {
		t.Fatalf("down: %+v", res)
	}
	health := getJSON[map[string]any](t, admin, base+"/api/health")
	found := false
	for _, it := range health["items"].([]any) {
		item := it.(map[string]any)
		if item["id"] == fmt.Sprintf("media-server-failed-%d", id) {
			found = true
			if !strings.Contains(fmt.Sprint(item["impact"]), "Could not reach Plex") {
				t.Fatalf("health impact: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("no health item for the failing server: %+v", health)
	}
	_ = server

	if code := deleteReq(t, admin, one); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if code := deleteReq(t, admin, one); code != http.StatusNotFound {
		t.Fatalf("second delete: %d", code)
	}
}

// After a grab is downloaded and imported, the media server is asked to scan
// exactly the new movie's folder, translated through the path mapping.
func TestImportRefreshesMediaServer(t *testing.T) {
	movieContent := []byte("Fixture movie bytes. " + strings.Repeat("padding-", 200))
	nntpSrv := newFakeNNTPServer(t, movieContent)
	indexerSrv := newFakeIndexerServer(t)
	plex := newFakePlexServer(t, "plex-token")

	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	settings := getJSON[map[string]any](t, client, httpSrv.URL+"/api/settings")
	moviesDir, _ := settings["moviesPath"].(string)
	if moviesDir == "" {
		t.Fatalf("no movies path in settings: %+v", settings)
	}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers", map[string]any{
		"name": "Fixture Usenet", "host": nntpSrv.addr, "port": nntpSrv.port, "useSsl": false, "connections": 2,
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/media-servers", map[string]any{
		"kind": "plex", "baseUrl": plex.URL, "token": "plex-token",
		"pathMap": []map[string]string{{"from": moviesDir, "to": "/data/movies"}},
	}, http.StatusCreated)
	// A disabled server is never asked.
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/media-servers", map[string]any{
		"kind": "jellyfin", "baseUrl": "http://127.0.0.1:1", "token": "k", "enabled": false,
	}, http.StatusCreated)

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	results := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/search?q=fixture")
	if len(results) != 1 {
		t.Fatalf("search: %+v", results)
	}
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", httpSrv.URL, movie.ID), map[string]any{
		"releaseTitle": results[0]["title"], "downloadUrl": results[0]["downloadUrl"], "sizeBytes": int64(len(movieContent)),
	}, http.StatusAccepted)

	deadline := time.Now().Add(20 * time.Second)
	for {
		q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(q) == 1 && (q[0]["status"] == "completed" || q[0]["status"] == "failed") {
			if q[0]["status"] == "failed" {
				t.Fatalf("pipeline failed: %+v", q[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pipeline did not finish: %+v", q)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// "completed" is set before the pipeline's last steps (clean-up, the
	// refresh request); wait for it to finish before flushing.
	waitBackground(t, server)
	server.TestFlushMediaServerRefresh()

	final := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", httpSrv.URL, movie.ID))
	rel, err := filepath.Rel(moviesDir, filepath.Dir(fmt.Sprint(final["filePath"])))
	if err != nil {
		t.Fatal(err)
	}
	want := "/data/movies/" + filepath.ToSlash(rel)
	if got := plex.scans(); len(got) != 1 || got[0] != want {
		t.Fatalf("plex scans = %q, want [%q]", got, want)
	}
}
