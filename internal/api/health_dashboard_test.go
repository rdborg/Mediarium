package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanborg/mediarium/internal/api"
	"github.com/ryanborg/mediarium/internal/library"
)

func healthByID(t *testing.T, client *http.Client, base string) map[string]map[string]any {
	t.Helper()
	res := getJSON[map[string]any](t, client, base+"/api/health")
	out := map[string]map[string]any{}
	for _, raw := range res["items"].([]any) {
		it := raw.(map[string]any)
		out[it["id"].(string)] = it
	}
	return out
}

func loginNewServer(t *testing.T) (*api.Server, string, *http.Client) {
	t.Helper()
	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	return server, httpSrv.URL, client
}

func TestHealthExplainsWhatBreaksForEachMissingPiece(t *testing.T) {
	server, base, client := loginNewServer(t)

	h := healthByID(t, client, base)
	for _, id := range []string{"tmdb-key", "no-indexers"} {
		it, ok := h[id]
		if !ok || it["level"] != "error" || it["impact"] == "" || it["action"] == nil {
			t.Fatalf("%s should be an error with an impact and an action: %+v", id, it)
		}
	}

	// A TMDB key fixes only its own warning.
	server.TestSetTMDBBaseURL("k", "http://127.0.0.1:1")
	h = healthByID(t, client, base)
	if _, still := h["tmdb-key"]; still || h["no-indexers"] == nil {
		t.Fatalf("expected only the TMDB warning to clear: %v", keysOf(h))
	}

	// A Usenet indexer without a provider: NZBs can be found but not downloaded.
	idx := newTVIndexerWith(t, []string{"A.Release.2001.1080p-GRP"})
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "U", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	h = healthByID(t, client, base)
	if h["no-indexers"] != nil || h["no-usenet-server"] == nil || h["no-usenet-server"]["level"] != "warn" {
		t.Fatalf("expected a no-usenet-server warning: %v", keysOf(h))
	}
	postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{"host": "news.example.com", "port": 563}, http.StatusCreated)
	if h = healthByID(t, client, base); h["no-usenet-server"] != nil {
		t.Fatalf("adding a provider should clear the warning: %v", keysOf(h))
	}

	// Torrents without a VPN: recommended, never blocking; the kill switch
	// turns it into a real error.
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "T", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "torrent"}, http.StatusCreated)
	h = healthByID(t, client, base)
	if h["no-vpn"] == nil || h["no-vpn"]["level"] != "warn" || !strings.Contains(h["no-vpn"]["impact"].(string), "nothing is blocked") {
		t.Fatalf("expected a gentle no-VPN recommendation: %+v", h["no-vpn"])
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"requireVpnForTorrents": true}, http.StatusOK)
	h = healthByID(t, client, base)
	if h["vpn-required"] == nil || h["vpn-required"]["level"] != "error" || h["no-vpn"] != nil {
		t.Fatalf("with the kill switch on, a missing VPN is an error: %v", keysOf(h))
	}
}

func keysOf(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBuiltInKeysNeedNoSetupAndUserKeysStillWin(t *testing.T) {
	server, base, client := loginNewServer(t)

	s := getJSON[map[string]any](t, client, base+"/api/settings")
	if s["hasTmdbApiKey"] != false || s["tmdbKeyBuiltIn"] != false {
		t.Fatalf("a from-source build has no built-in keys: %+v", s)
	}

	server.SetBuiltinKeys(api.BuiltinKeys{TMDB: "app-tmdb", OpenSubtitles: "app-os", TraktClientID: "app-trakt"})
	s = getJSON[map[string]any](t, client, base+"/api/settings")
	for _, f := range []string{"tmdbKeyBuiltIn", "openSubtitlesKeyBuiltIn", "traktClientIdBuiltIn", "hasTmdbApiKey", "hasOpenSubtitlesApiKey", "hasTraktClientId"} {
		if s[f] != true {
			t.Fatalf("%s should be true with built-in keys: %+v", f, s)
		}
	}
	h := healthByID(t, client, base)
	for _, id := range []string{"tmdb-key", "subtitles-off", "trakt-off"} {
		if h[id] != nil {
			t.Fatalf("%s should not be reported when the key is built in", id)
		}
	}
}

func TestOpenSubtitlesAccountSettings(t *testing.T) {
	_, base, client := loginNewServer(t)
	put := func(body map[string]any) map[string]any {
		return putJSONStatus(t, client, base+"/api/settings", body, http.StatusOK)
	}

	got := put(map[string]any{"openSubtitlesUsername": "ryan", "openSubtitlesPassword": "pw"})
	if got["hasOpenSubtitlesAccount"] != true || got["openSubtitlesAccountName"] != "ryan" {
		t.Fatalf("account not saved: %+v", got)
	}
	if got["openSubtitlesPassword"] != nil {
		t.Fatal("the password must never be sent back")
	}
	// Same username, blank password: the saved password is kept.
	if got = put(map[string]any{"openSubtitlesUsername": "ryan", "openSubtitlesPassword": ""}); got["hasOpenSubtitlesAccount"] != true {
		t.Fatalf("a blank password should keep the saved one: %+v", got)
	}
	// Blank username removes the account.
	if got = put(map[string]any{"openSubtitlesUsername": ""}); got["hasOpenSubtitlesAccount"] != false {
		t.Fatalf("clearing the username should remove the account: %+v", got)
	}
}

func TestTestServiceEndpointChecksKeysWithoutSavingThem(t *testing.T) {
	server, base, client := loginNewServer(t)

	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	t.Cleanup(tmdb.Close)
	server.TestSetTMDBBaseURL("", tmdb.URL)

	trakt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("trakt-api-key") != "good" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "[]")
	}))
	t.Cleanup(trakt.Close)
	server.TestSetTraktBaseURL("", trakt.URL)

	os_ := newFakeOpenSubtitles(t)
	server.TestSetSubtitlesBaseURL("", os_.srv.URL)

	test := func(body map[string]any) map[string]any {
		return postJSON[map[string]any](t, client, base+"/api/settings/test-service", body, http.StatusOK)
	}
	if r := test(map[string]any{"service": "tmdb", "key": "good"}); r["ok"] != true {
		t.Fatalf("good TMDB key: %+v", r)
	}
	if r := test(map[string]any{"service": "tmdb", "key": "bad"}); r["ok"] != false || !strings.Contains(r["message"].(string), "rejected") {
		t.Fatalf("bad TMDB key should be rejected clearly: %+v", r)
	}
	if r := test(map[string]any{"service": "tmdb"}); r["ok"] != false {
		t.Fatalf("no key and none saved must not pass: %+v", r)
	}
	if r := test(map[string]any{"service": "trakt", "key": "good"}); r["ok"] != true {
		t.Fatalf("good Trakt id: %+v", r)
	}
	if r := test(map[string]any{"service": "trakt", "key": "bad"}); r["ok"] != false {
		t.Fatalf("bad Trakt id: %+v", r)
	}
	if r := test(map[string]any{"service": "opensubtitles", "key": "k"}); r["ok"] != true {
		t.Fatalf("OpenSubtitles key: %+v", r)
	}
	postJSON[map[string]any](t, client, base+"/api/settings/test-service", map[string]any{"service": "nope"}, http.StatusBadRequest)

	// Testing never saves.
	if s := getJSON[map[string]any](t, client, base+"/api/settings"); s["hasTmdbApiKey"] != false || s["hasTraktClientId"] != false {
		t.Fatalf("a test must not store the key: %+v", s)
	}
}

func TestFolderCheckEndpoint(t *testing.T) {
	_, base, client := loginNewServer(t)
	dir := t.TempDir()
	keep := filepath.Join(dir, "precious.mkv")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ok := getJSON[map[string]any](t, client, base+"/api/settings/folder-check?path="+url.QueryEscape(dir))
	if ok["exists"] != true || ok["writable"] != true || ok["totalBytes"].(float64) <= 0 {
		t.Fatalf("healthy folder: %+v", ok)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("checking a folder must not leave anything behind: %v", entries)
	}

	missing := getJSON[map[string]any](t, client, base+"/api/settings/folder-check?path="+url.QueryEscape(filepath.Join(dir, "nope")))
	if missing["exists"] != false || len(missing["warnings"].([]any)) == 0 {
		t.Fatalf("missing folder should warn: %+v", missing)
	}
	resp, err := client.Get(base + "/api/settings/folder-check")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a missing path param should be a 400, got %d", resp.StatusCode)
	}
}

func TestDashboardSummarisesLibraryFoldersAndRecentActivity(t *testing.T) {
	server, base, client := loginNewServer(t)

	dir := t.TempDir()
	movieFile := filepath.Join(dir, "Heat (1995).mkv")
	if err := os.WriteFile(movieFile, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 949, Title: "Heat", Year: 1995, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", movieFile)
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Wanted One", Year: 2020, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	series, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 5, Title: "A Show", Year: 2011, Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1, AirDate: "2011-01-01"}, {Season: 1, Episode: 2, AirDate: "2011-01-08"},
	})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := server.MovieRepo.ListEpisodes(series.ID)
	epFile := filepath.Join(dir, "A Show S01E01.mkv")
	_ = os.WriteFile(epFile, make([]byte, 1024), 0o644)
	_ = server.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "WEBDL-1080p", epFile)

	d := getJSON[map[string]any](t, client, base+"/api/dashboard")
	lib := d["library"].(map[string]any)
	movies := lib["movies"].(map[string]any)
	if movies["total"] != float64(2) || movies["downloaded"] != float64(1) || movies["missing"] != float64(1) {
		t.Fatalf("movie counts: %+v", movies)
	}
	sr := lib["series"].(map[string]any)
	if sr["total"] != float64(1) || sr["episodes"] != float64(2) || sr["episodesDownloaded"] != float64(1) || sr["episodesMissing"] != float64(1) {
		t.Fatalf("series counts: %+v", sr)
	}
	if lib["sizeBytes"] != float64(2048+1024) {
		t.Fatalf("library size = %v, want 3072", lib["sizeBytes"])
	}
	if q := lib["qualities"].([]any); len(q) != 2 {
		t.Fatalf("expected two quality tiers, got %+v", q)
	}

	folders := d["folders"].([]any)
	if len(folders) != 3 {
		t.Fatalf("expected movies, tv and downloads folders, got %+v", folders)
	}
	if first := folders[0].(map[string]any); first["key"] != "movies" || first["libraryBytes"] != float64(2048) || first["items"] != float64(1) {
		t.Fatalf("movies folder card: %+v", first)
	}

	added := d["recentlyAdded"].([]any)
	if len(added) != 3 || added[0].(map[string]any)["title"] != "A Show" {
		t.Fatalf("recently added should list newest first: %+v", added)
	}
	if _, ok := d["health"].([]any); !ok {
		t.Fatal("the dashboard must carry the health warnings")
	}
}
