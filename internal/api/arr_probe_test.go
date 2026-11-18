package api_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The apps that read the Radarr, Sonarr and Lidarr look-alike send what they
// like: negative or huge pages, ids that are not numbers, empty and very long
// terms, odd characters. None of it may give a server error or an answer that
// is not JSON, and the key is taken from the header, a Bearer token or the
// query, with a wrong one refused whichever way it comes.
func TestArrAPIToleratesOddQueriesAndTakesTheKeyThreeWays(t *testing.T) {
	server, base, admin, _, _ := familyServer(t)
	server.TestSetTMDBBaseURL("fixture-key", arrTMDB(t).URL)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "Probe"}, http.StatusCreated)["key"].(string)

	get := func(target string, set func(*http.Request)) (int, string, string) {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		set(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
	}

	// Three ways to send the key, and a wrong one each way.
	for _, app := range []string{"radarr", "sonarr", "lidarr"} {
		prefix := "/api/v3"
		if app == "lidarr" {
			prefix = "/api/v1"
		}
		status := base + "/" + app + prefix + "/system/status"
		ways := map[string]func(string) (string, func(*http.Request)){
			"header": func(k string) (string, func(*http.Request)) {
				return status, func(r *http.Request) { r.Header.Set("X-Api-Key", k) }
			},
			"lower-case header": func(k string) (string, func(*http.Request)) {
				return status, func(r *http.Request) { r.Header["x-api-key"] = []string{k} }
			},
			"bearer": func(k string) (string, func(*http.Request)) {
				return status, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+k) }
			},
			"query": func(k string) (string, func(*http.Request)) {
				return status + "?apikey=" + url.QueryEscape(k), func(*http.Request) {}
			},
		}
		for name, way := range ways {
			target, set := way(key)
			// Music is off in this server: the right key then meets "not found".
			if code, _, body := get(target, set); code != http.StatusOK && !(app == "lidarr" && code == http.StatusNotFound) {
				t.Errorf("%s, key by %s: %d %s", app, name, code, body)
			}
			target, set = way("not-the-key")
			if code, _, _ := get(target, set); code != http.StatusUnauthorized {
				t.Errorf("%s, wrong key by %s: %d, want 401", app, name, code)
			}
		}
	}

	paths := map[string][]string{
		"radarr": {"/movie", "/movie/abc", "/movie/99999999999999999999", "/movie/-1", "/movie/lookup", "/movie/lookup/tmdb", "/moviefile", "/moviefile/zzz", "/queue", "/queue/status", "/queue/details", "/history", "/history/movie", "/history/since", "/wanted/missing", "/wanted/cutoff", "/calendar", "/release", "/credit", "/parse", "/tag", "/tag/x", "/rootfolder", "/diskspace", "/qualityprofile", "/qualityprofile/schema", "/qualityprofile/abc", "/qualitydefinition", "/customformat", "/filesystem", "/filesystem/type", "/filesystem/mediafiles", "/log", "/command/xyz"},
		"sonarr": {"/series", "/series/abc", "/series/lookup", "/episode", "/episode/abc", "/episodefile", "/episodefile/q", "/queue", "/history", "/history/series", "/wanted/missing", "/wanted/cutoff", "/calendar", "/release", "/parse", "/languageprofile", "/language", "/tag/detail", "/rootfolder", "/qualityprofile"},
		"lidarr": {"/artist", "/artist/abc", "/artist/lookup", "/album", "/album/abc", "/queue", "/history", "/wanted/missing", "/calendar", "/rootfolder", "/qualityprofile", "/metadataprofile"},
	}
	queries := []string{
		"",
		"?page=-1", "?page=0&pageSize=0", "?page=abc&pageSize=xyz", "?pageSize=-1", "?pageSize=9999999999999999999", "?page=9223372036854775807&pageSize=1000",
		"?sortKey=%00&sortDirection=%ff", "?sortKey=" + strings.Repeat("a", 5000), "?sortDirection=descending&sortKey=movieTitle",
		"?term=", "?term=" + strings.Repeat("%F0%9F%8E%AC", 2000), "?term=tmdb:abc", "?term=imdb:tt", "?term=tvdb:99999999999999999999",
		"?tmdbId=", "?tmdbId=-5", "?tmdbId=1e9", "?seriesId=abc", "?movieId=-1&episodeIds=1,,x,2", "?includeSeries=maybe",
		"?start=2026-13-45&end=nope", "?unmonitored=yes&includeUnmonitored=1", "?path=%00", "?path=" + url.QueryEscape("../../../etc"), "?path=" + url.QueryEscape(`C:\Windows\System32`),
		"?filterKey=monitored&filterValue=%00", "?since=garbage", "?title=" + url.QueryEscape("../../x"), "?apikey=",
	}
	check := func(label string, code int, ctype, body string) {
		// 502 to 504 are the answer for a data service that does not answer.
		if code >= 500 && (code < 502 || code > 504) {
			t.Errorf("%s: server error %d: %.200s", label, code, body)
		}
		if strings.HasPrefix(body, "<") || strings.Contains(strings.ToLower(ctype), "text/html") {
			t.Errorf("%s: answered with a page, not JSON (%s)", label, ctype)
		}
		if strings.Contains(body, "goroutine ") || strings.Contains(body, "runtime error") || strings.Contains(body, key) {
			t.Errorf("%s: answer leaks internals: %.200s", label, body)
		}
	}
	for app, list := range paths {
		prefix := "/api/v3"
		if app == "lidarr" {
			prefix = "/api/v1"
		}
		for _, p := range list {
			for _, q := range queries {
				target := base + "/" + app + prefix + p + q
				code, ctype, body := get(target, func(r *http.Request) { r.Header.Set("X-Api-Key", key) })
				check(app+" "+p+q[:min(len(q), 60)], code, ctype, body)
			}
		}
	}
	// Write calls with bad bodies: not JSON, wrong types, huge numbers.
	for _, c := range []struct{ app, method, path, body string }{
		{"radarr", "POST", "/api/v3/movie", `not json`},
		{"radarr", "POST", "/api/v3/movie", `{"tmdbId":"x"}`},
		{"radarr", "POST", "/api/v3/movie", `{"tmdbId":99999999999999999999}`},
		{"radarr", "POST", "/api/v3/movie", `[]`},
		{"radarr", "PUT", "/api/v3/movie/abc", `{}`},
		{"radarr", "PUT", "/api/v3/movie/editor", `{"movieIds":[-1,0,"a"],"monitored":true}`},
		{"radarr", "DELETE", "/api/v3/movie/abc", ``},
		{"radarr", "POST", "/api/v3/command", `{"name":"MoviesSearch","movieIds":[1,2,"x"]}`},
		{"radarr", "POST", "/api/v3/command", `{`},
		{"radarr", "DELETE", "/api/v3/queue/bulk", `{"ids":["a",-1,0]}`},
		{"sonarr", "POST", "/api/v3/series", `{"tvdbId":"x"}`},
		{"sonarr", "PUT", "/api/v3/episode/monitor", `{"episodeIds":[999999],"monitored":"yes"}`},
		{"sonarr", "PUT", "/api/v3/series/editor", `{"seriesIds":null}`},
		{"lidarr", "POST", "/api/v1/artist", `{"foreignArtistId":"../../x"}`},
		{"radarr", "POST", "/api/v3/release", `{"guid":"","indexerId":"x"}`},
		{"radarr", "POST", "/api/v3/release/push", `{"title":""}`},
	} {
		req, _ := http.NewRequest(c.method, base+"/"+c.app+c.path, strings.NewReader(c.body))
		req.Header.Set("X-Api-Key", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		check(c.method+" "+c.path+" "+c.body, resp.StatusCode, resp.Header.Get("Content-Type"), string(b))
	}
}
