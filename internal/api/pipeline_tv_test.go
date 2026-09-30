package api_test

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

// nntpArticle is one file's worth of content to serve, addressed by the
// message id the fixture NZB references it with.
type nntpArticle struct {
	fileName string
	content  []byte
}

// newArticleNNTPServer is a fake NNTP server that, unlike the single-body
// one in pipeline_integration_test.go, serves a different yEnc article per
// requested message id — needed for season packs, where one NZB lists
// several files.
func newArticleNNTPServer(t *testing.T, articles map[string]nntpArticle) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	bodies := map[string][]byte{}
	for id, a := range articles {
		bodies[id] = encodeFixtureYenc(a.fileName, a.content)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
				rw.WriteString("200 fake nntp ready\r\n")
				rw.Flush()
				for {
					line, err := rw.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimRight(line, "\r\n")
					upper := strings.ToUpper(line)
					switch {
					case strings.HasPrefix(upper, "AUTHINFO USER"):
						rw.WriteString("381 password required\r\n")
					case strings.HasPrefix(upper, "AUTHINFO PASS"):
						rw.WriteString("281 ok\r\n")
					case strings.HasPrefix(upper, "GROUP"):
						rw.WriteString("211 0 0 0 group\r\n")
					case strings.HasPrefix(upper, "BODY"):
						id := strings.Trim(strings.TrimSpace(line[len("BODY"):]), "<>")
						body, ok := bodies[id]
						if !ok {
							rw.WriteString("430 no such article\r\n")
							break
						}
						rw.WriteString("222 body follows\r\n")
						for _, l := range strings.Split(string(body), "\n") {
							l = strings.TrimRight(l, "\r")
							if strings.HasPrefix(l, ".") {
								rw.WriteString(".")
							}
							rw.WriteString(l)
							rw.WriteString("\r\n")
						}
						rw.WriteString(".\r\n")
					case upper == "QUIT":
						rw.WriteString("205 bye\r\n")
						rw.Flush()
						return
					default:
						rw.WriteString("500 unrecognized\r\n")
					}
					rw.Flush()
				}
			}()
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(portStr)
	return "127.0.0.1", p
}

// nzbFor builds an NZB listing every article, one file each.
func nzbFor(articles map[string]nntpArticle) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`)
	for id, a := range articles {
		fmt.Fprintf(&b, `<file subject="[1/1] &quot;%s&quot; yEnc (1/1)"><groups><group>alt.binaries.test</group></groups><segments><segment bytes="%d" number="1">%s</segment></segments></file>`,
			a.fileName, len(a.content), id)
	}
	b.WriteString(`</nzb>`)
	return b.String()
}

type tvEnv struct {
	server   *api.Server
	httpSrv  *httptest.Server
	client   *http.Client
	tvRoot   string
	seriesID int64
	nzbURL   string
}

// newTVEnv boots a server with a fake TMDB (show 1399: S1E1, S1E2, S2E1), a
// fake NNTP server serving articles, and the fixture show already added to
// the library with its TV root pointed at a temp dir.
func newTVEnv(t *testing.T, articles map[string]nntpArticle) *tvEnv {
	t.Helper()
	server, httpSrv, client := newHuntTestServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newTVTMDBServer(t, nil).URL)

	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	tvRoot := filepath.Join(t.TempDir(), "tv")
	if err := os.MkdirAll(tvRoot, 0o755); err != nil {
		t.Fatalf("mkdir tv root: %v", err)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{
		"tvPath": tvRoot, "namingPreset": "plex",
	}, http.StatusOK)

	host, port := newArticleNNTPServer(t, articles)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers", map[string]any{
		"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2,
	}, http.StatusCreated)

	nzbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, nzbFor(articles))
	}))
	t.Cleanup(nzbSrv.Close)

	created := postJSON[map[string]any](t, client, httpSrv.URL+"/api/series", map[string]any{"tmdbId": 1399}, http.StatusCreated)
	return &tvEnv{
		server: server, httpSrv: httpSrv, client: client, tvRoot: tvRoot,
		seriesID: int64(created["id"].(float64)), nzbURL: nzbSrv.URL + "/release.nzb",
	}
}

func (e *tvEnv) grab(t *testing.T, releaseTitle string) {
	t.Helper()
	postJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/series/%d/grab", e.httpSrv.URL, e.seriesID), map[string]any{
		"releaseTitle": releaseTitle, "downloadUrl": e.nzbURL, "sizeBytes": 1000,
	}, http.StatusAccepted)
}

// waitForTerminal polls until the single queue item finishes and returns
// its final status and error.
func (e *tvEnv) waitForTerminal(t *testing.T) (status, errMsg string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		list := getJSON[[]map[string]any](t, e.client, e.httpSrv.URL+"/api/queue")
		if len(list) == 1 {
			status, _ = list[0]["status"].(string)
			errMsg, _ = list[0]["error"].(string)
			if status == "completed" || status == "failed" {
				return status, errMsg
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for the TV queue item to finish")
	return "", ""
}

func (e *tvEnv) episodeStatuses(t *testing.T) map[string]string {
	t.Helper()
	detail := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/series/%d", e.httpSrv.URL, e.seriesID))
	out := map[string]string{}
	for _, raw := range detail["episodes"].([]any) {
		ep := raw.(map[string]any)
		out[fmt.Sprintf("S%02dE%02d", int(ep["season"].(float64)), int(ep["episode"].(float64)))] = ep["status"].(string)
	}
	return out
}

// TestTVSingleEpisodeGrabImportsIntoSeasonFolder covers the everyday case:
// grab one episode, its downloaded file (whose own name carries no S/E
// marker) is matched to the grab's episode, and it lands in
// "Series (Year)/Season NN/" under the configured naming preset — with the
// other episodes untouched.
func TestTVSingleEpisodeGrabImportsIntoSeasonFolder(t *testing.T) {
	content := []byte("episode two bytes " + strings.Repeat("x", 500))
	env := newTVEnv(t, map[string]nntpArticle{"ep-two@example": {fileName: "release-file.mkv", content: content}})

	env.grab(t, "Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the TV grab to complete, got status=%q error=%q", status, errMsg)
	}

	want := filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01", "Fixture Show - S01E02 - Second.mkv")
	got, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected the episode at %s: %v", want, err)
	}
	if string(got) != string(content) {
		t.Fatalf("imported file content doesn't match what was downloaded")
	}

	statuses := env.episodeStatuses(t)
	if statuses["S01E02"] != "downloaded" || statuses["S01E01"] != "missing" || statuses["S02E01"] != "missing" {
		t.Fatalf("expected only S01E02 downloaded, got %v", statuses)
	}
}

// TestTVSeasonPackImportsEveryEpisode covers a whole-season pack: every
// episode file inside is matched by its own S/E filename and imported, and
// the sample clip is ignored.
func TestTVSeasonPackImportsEveryEpisode(t *testing.T) {
	e1 := []byte("episode one " + strings.Repeat("a", 400))
	e2 := []byte("episode two " + strings.Repeat("b", 400))
	env := newTVEnv(t, map[string]nntpArticle{
		"e1@example":     {fileName: "Fixture.Show.S01E01.1080p.BluRay.x264-GRP.mkv", content: e1},
		"e2@example":     {fileName: "Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv", content: e2},
		"sample@example": {fileName: "fixture.show.s01e01.sample.mkv", content: []byte("tiny sample")},
	})

	env.grab(t, "Fixture.Show.S01.1080p.BluRay.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the season pack to complete, got status=%q error=%q", status, errMsg)
	}

	for name, content := range map[string][]byte{"Fixture Show - S01E01 - Pilot.mkv": e1, "Fixture Show - S01E02 - Second.mkv": e2} {
		path := filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01", name)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if string(got) != string(content) {
			t.Fatalf("%s: content mismatch", name)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01"))
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 imported files (sample ignored), got %d", len(entries))
	}

	statuses := env.episodeStatuses(t)
	if statuses["S01E01"] != "downloaded" || statuses["S01E02"] != "downloaded" || statuses["S02E01"] != "missing" {
		t.Fatalf("expected season 1 downloaded and season 2 untouched, got %v", statuses)
	}
	ep, _ := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 1)
	if ep.Quality != "Bluray-1080p" {
		t.Fatalf("expected the classified tier to be stored, got %q", ep.Quality)
	}
}

// TestTVPackMissingAnEpisodeReturnsItToMissing proves an episode a pack
// didn't actually deliver isn't left stuck in "downloading".
func TestTVPackMissingAnEpisodeReturnsItToMissing(t *testing.T) {
	e1 := []byte("episode one " + strings.Repeat("a", 400))
	env := newTVEnv(t, map[string]nntpArticle{
		"e1@example": {fileName: "Fixture.Show.S01E01.720p.HDTV.x264-GRP.mkv", content: e1},
	})

	env.grab(t, "Fixture.Show.S01.720p.HDTV.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("a partial pack should still complete (one episode imported), got status=%q error=%q", status, errMsg)
	}
	statuses := env.episodeStatuses(t)
	if statuses["S01E01"] != "downloaded" || statuses["S01E02"] != "missing" {
		t.Fatalf("expected E01 downloaded and the undelivered E02 back to missing, got %v", statuses)
	}
}

// A release with no recognisable season and no hint can't be routed.
func TestTVGrabWithoutSeasonIsRejected(t *testing.T) {
	env := newTVEnv(t, map[string]nntpArticle{"x@example": {fileName: "x.mkv", content: []byte("x")}})
	postJSON[map[string]any](t, env.client, fmt.Sprintf("%s/api/series/%d/grab", env.httpSrv.URL, env.seriesID), map[string]any{
		"releaseTitle": "Fixture Show Some Special", "downloadUrl": env.nzbURL,
	}, http.StatusBadRequest)
}
