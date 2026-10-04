package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTVSearchIndexer is a fake Newznab indexer that returns the same mixed
// bag of releases for any query — the point is to prove the server filters
// to the right show/season/episode itself, since real indexers return
// plenty of near-misses for "Show S01E02".
func newTVSearchIndexer(t *testing.T) *httptest.Server {
	t.Helper()
	return newTVIndexerWith(t, []string{
		"Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP",
		"Fixture.Show.S01E01.1080p.WEB-DL.x264-GRP",
		"Fixture.Show.S01.1080p.BluRay.x264-GRP",
		"Other.Show.S01E02.1080p.WEB-DL.x264-GRP",
		"Fixture.Show.S02E01.1080p.WEB-DL.x264-GRP",
	})
}

// newTVIndexerWith is newTVSearchIndexer with a caller-chosen release list.
func newTVIndexerWith(t *testing.T, titles []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
		for i, title := range titles {
			fmt.Fprintf(&b, `<item><title>%s</title><guid>g%d</guid><enclosure url="http://%s/nzb/%d.nzb" length="21474836480" type="application/x-nzb"/><newznab:attr name="size" value="21474836480"/><newznab:attr name="category" value="5000"/></item>`,
				title, i, r.Host, i)
		}
		b.WriteString(`</channel></rss>`)
		fmt.Fprint(w, b.String())
	}))
	t.Cleanup(srv.Close)
	return srv
}

func resultTitles(results []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, r := range results {
		out[r["title"].(string)] = true
	}
	return out
}

func TestSeriesInteractiveSearchFiltersToShowAndTarget(t *testing.T) {
	env := newTVEnv(t, map[string]nntpArticle{"x@example": {fileName: "x.mkv", content: []byte("x")}})
	postJSON[map[string]any](t, env.client, env.httpSrv.URL+"/api/indexers", map[string]any{
		"name": "TV Indexer", "definitionId": "fixture", "baseUrl": newTVSearchIndexer(t).URL, "apiKey": "k",
	}, http.StatusCreated)

	// Episode search: that episode + the season pack that would also
	// satisfy it — not other episodes, other seasons, or other shows.
	ep := getJSON[[]map[string]any](t, env.client, fmt.Sprintf("%s/api/series/%d/search?season=1&episode=2", env.httpSrv.URL, env.seriesID))
	got := resultTitles(ep)
	want := map[string]bool{"Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP": true, "Fixture.Show.S01.1080p.BluRay.x264-GRP": true}
	if len(got) != len(want) {
		t.Fatalf("episode search: expected %v, got %v", want, got)
	}
	for title := range want {
		if !got[title] {
			t.Fatalf("episode search: missing %q in %v", title, got)
		}
	}

	// Season search: only the pack.
	season := getJSON[[]map[string]any](t, env.client, fmt.Sprintf("%s/api/series/%d/search?season=1", env.httpSrv.URL, env.seriesID))
	got = resultTitles(season)
	if len(got) != 1 || !got["Fixture.Show.S01.1080p.BluRay.x264-GRP"] {
		t.Fatalf("season search: expected only the S01 pack, got %v", got)
	}

	// Season is required.
	resp, err := env.client.Get(fmt.Sprintf("%s/api/series/%d/search", env.httpSrv.URL, env.seriesID))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 without a season, got %d", resp.StatusCode)
	}
}

// The unified search bar returns movies and TV together, and tags TV
// releases with their season/episodes so the UI can label them.
func TestUnifiedSearchTagsTVReleases(t *testing.T) {
	env := newTVEnv(t, map[string]nntpArticle{"x@example": {fileName: "x.mkv", content: []byte("x")}})
	postJSON[map[string]any](t, env.client, env.httpSrv.URL+"/api/indexers", map[string]any{
		"name": "TV Indexer", "definitionId": "fixture", "baseUrl": newTVSearchIndexer(t).URL, "apiKey": "k",
	}, http.StatusCreated)

	results := getJSON[[]map[string]any](t, env.client, env.httpSrv.URL+"/api/search?q="+url.QueryEscape("fixture show"))
	byTitle := map[string]map[string]any{}
	for _, r := range results {
		byTitle[r["title"].(string)] = r
	}
	single := byTitle["Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP"]
	if single["season"] != float64(1) || len(single["episodes"].([]any)) != 1 || single["episodes"].([]any)[0] != float64(2) {
		t.Fatalf("expected S01E02 tagged season=1 episodes=[2], got %+v", single)
	}
	pack := byTitle["Fixture.Show.S01.1080p.BluRay.x264-GRP"]
	if pack["season"] != float64(1) || pack["episodes"] != nil {
		t.Fatalf("expected the pack tagged season=1 with no episodes, got %+v", pack)
	}
}

// Grabbing a TV release from the unified search resolves the show on TMDB,
// adds it to the library if it isn't there yet, and runs the TV pipeline.
func TestSearchGrabTVAddsSeriesOnTheFlyAndImports(t *testing.T) {
	content := []byte("episode two via search " + strings.Repeat("z", 300))
	articles := map[string]nntpArticle{"s@example": {fileName: "whatever.mkv", content: content}}

	server, httpSrv, client := newHuntTestServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newTVTMDBServer(t, nil).URL)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	tvRoot := filepath.Join(t.TempDir(), "tv")
	os.MkdirAll(tvRoot, 0o755)
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{"tvPath": tvRoot}, http.StatusOK)
	host, port := newArticleNNTPServer(t, articles)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers", map[string]any{
		"name": "n", "host": host, "port": port, "useSsl": false, "connections": 2,
	}, http.StatusCreated)
	nzbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, nzbFor(articles)) }))
	t.Cleanup(nzbSrv.Close)

	// No series in the library yet.
	if list := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/series"); len(list) != 0 {
		t.Fatalf("expected an empty series list to start, got %+v", list)
	}

	resp := postJSON[map[string]any](t, client, httpSrv.URL+"/api/search/grab", map[string]any{
		"releaseTitle": "Fixture.Show.2011.S01E02.1080p.WEB-DL.x264-GRP", "downloadUrl": nzbSrv.URL + "/r.nzb", "sizeBytes": 1000,
	}, http.StatusAccepted)
	if resp["seriesId"] == nil || resp["queueId"] == nil {
		t.Fatalf("expected seriesId and queueId in the response, got %+v", resp)
	}

	deadline := time.Now().Add(20 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(q) == 1 {
			status, _ = q[0]["status"].(string)
			if status == "completed" || status == "failed" {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "completed" {
		t.Fatalf("expected the search-initiated TV grab to complete, got %q", status)
	}
	if list := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/series"); len(list) != 1 || list[0]["downloadedCount"] != float64(1) {
		t.Fatalf("expected the show added with one downloaded episode, got %+v", list)
	}
	if _, err := os.Stat(filepath.Join(tvRoot, "Fixture Show (2011)", "Season 01", "Fixture Show - S01E02 - Second.mkv")); err != nil {
		t.Fatalf("expected the imported episode file: %v", err)
	}
}
