package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

const (
	badRelease  = "Fixture.Movie.2001.1080p.BluRay.x264-BAD"
	goodRelease = "Fixture.Movie.2001.1080p.WEB-DL.x264-GOOD"
)

// newBlocklistIndexer offers a Bluray release whose NZB points at articles
// the NNTP server does not have (a dud) and a WEB-DL one that downloads fine.
func newBlocklistIndexer(t *testing.T, good map[string]nntpArticle) *httptest.Server {
	t.Helper()
	bad := map[string]nntpArticle{"missing@nowhere": {fileName: "Fixture.Movie.2001.mkv", content: []byte("x")}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>
<item><title>%s</title><guid>g-bad</guid><enclosure url="http://%s/nzb/bad.nzb" length="21474836480" type="application/x-nzb"/><newznab:attr name="size" value="22548578304"/><newznab:attr name="category" value="2000"/></item>
<item><title>%s</title><guid>g-good</guid><enclosure url="http://%s/nzb/good.nzb" length="21474836480" type="application/x-nzb"/><newznab:attr name="size" value="21474836480"/><newznab:attr name="category" value="2000"/></item>
</channel></rss>`, badRelease, r.Host, goodRelease, r.Host)
	})
	mux.HandleFunc("/nzb/bad.nzb", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, nzbFor(bad)) })
	mux.HandleFunc("/nzb/good.nzb", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, nzbFor(good)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func waitForQueue(t *testing.T, client *http.Client, baseURL string, wantItems int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var list []map[string]any
	for time.Now().Before(deadline) {
		list = getJSON[[]map[string]any](t, client, baseURL+"/api/queue")
		settled := len(list) >= wantItems
		for _, it := range list {
			if s, _ := it["status"].(string); s != "completed" && s != "failed" {
				settled = false
			}
		}
		if settled {
			return list
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("queue never settled with %d items: %+v", wantItems, list)
	return nil
}

func TestBadReleaseIsBlocklistedAndNextBestIsTried(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	moviesRoot := filepath.Join(t.TempDir(), "movies")
	if err := os.MkdirAll(moviesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	putJSONStatus(t, client, httpSrv.URL+"/api/settings", map[string]any{"moviesPath": moviesRoot, "namingPreset": "plex"}, http.StatusOK)

	good := map[string]nntpArticle{"good@x": {fileName: "Fixture.Movie.2001.mkv", content: []byte("video-bytes")}}
	host, port := newArticleNNTPServer(t, good)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers", map[string]any{
		"name": "Usenet", "host": host, "port": port, "useSsl": false, "connections": 1,
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Idx", "definitionId": "fixture", "baseUrl": newBlocklistIndexer(t, good).URL, "apiKey": "k",
	}, http.StatusCreated)

	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Fixture Movie", Year: 2001, Monitored: true}); err != nil {
		t.Fatal(err)
	}

	server.TestHunt(context.Background())

	// The Bluray dud fails, gets blocklisted, and the WEB-DL is tried next.
	queueItems := waitForQueue(t, client, httpSrv.URL, 2)
	statuses := map[string]string{}
	for _, it := range queueItems {
		statuses[it["releaseTitle"].(string)] = it["status"].(string)
	}
	if statuses[badRelease] != "failed" || statuses[goodRelease] != "completed" {
		t.Fatalf("expected the dud to fail and the next-best to complete, got %v", statuses)
	}
	movies := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies")
	if len(movies) != 1 || movies[0]["status"] != "downloaded" || movies[0]["quality"] != "WEBDL-1080p" {
		t.Fatalf("movie should be downloaded from the fallback release: %+v", movies)
	}

	// The failed download is in the problem log, with the title it was for.
	server.Problems.Flush()
	logged := getJSON[map[string]any](t, client, httpSrv.URL+"/api/system/problems")
	rows := logged["items"].([]any)
	if len(rows) != 1 {
		t.Fatalf("want one problem for the dud, got %+v", rows)
	}
	row := rows[0].(map[string]any)
	if row["level"] != "error" || row["area"] == "" || row["forTitle"] != "Fixture Movie" || row["forLink"] != "/title/1" || row["downloadId"] == nil || row["detail"] == "" {
		t.Fatalf("the failed download was not logged properly: %+v", row)
	}

	list := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/blocklist")
	if len(list) != 1 || list[0]["releaseTitle"] != badRelease {
		t.Fatalf("expected exactly the dud on the blocklist, got %+v", list)
	}

	// Interactive search still shows it, flagged.
	results := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/search?q=fixture+movie")
	flagged := map[string]bool{}
	for _, r := range results {
		flagged[r["title"].(string)], _ = r["blocklisted"].(bool)
	}
	if !flagged[badRelease] || flagged[goodRelease] {
		t.Fatalf("search should flag only the blocklisted release, got %v", flagged)
	}

	// Removing it from the blocklist makes it eligible again.
	if code := deleteReq(t, client, fmt.Sprintf("%s/api/blocklist/%v", httpSrv.URL, list[0]["id"])); code != http.StatusOK {
		t.Fatalf("remove status %d", code)
	}
	if list := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/blocklist"); len(list) != 0 {
		t.Fatalf("blocklist should be empty, got %+v", list)
	}
}

// A failure that isn't the release's fault (here: no download client set up)
// must not blocklist anything, or fixing the setup would leave every
// release permanently banned.
func TestSetupFailureDoesNotBlocklist(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	good := map[string]nntpArticle{"good@x": {fileName: "Fixture.Movie.2001.mkv", content: []byte("v")}}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Idx", "definitionId": "fixture", "baseUrl": newBlocklistIndexer(t, good).URL, "apiKey": "k",
	}, http.StatusCreated)
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Fixture Movie", Year: 2001, Monitored: true}); err != nil {
		t.Fatal(err)
	}

	server.TestHunt(context.Background())

	items := waitForQueue(t, client, httpSrv.URL, 1)
	if len(items) != 1 || items[0]["status"] != "failed" {
		t.Fatalf("expected one failed grab, got %+v", items)
	}
	time.Sleep(300 * time.Millisecond)
	if list := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/blocklist"); len(list) != 0 {
		t.Fatalf("a setup problem blocklisted something: %+v", list)
	}
	if items := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue"); len(items) != 1 {
		t.Fatalf("no automatic retry expected for a setup failure, got %+v", items)
	}
}
