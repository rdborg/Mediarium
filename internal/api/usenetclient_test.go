package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/usenetclient/fake"
)

// usenetEnv is a client environment with an NZB to grab and a stand-in for
// SABnzbd or NZBGet to send it to.
type usenetEnv struct {
	*clientEnv
	nzbURL string
	sab    *fake.SAB
	sabURL string
	ng     *fake.NZBGet
	ngURL  string
}

const fixtureNZB = `<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file subject="[1/1] &quot;The.Fixture.Movie.1999.mkv&quot; yEnc (1/1)"><groups><group>alt.binaries.test</group></groups><segments><segment bytes="1000" number="1">abc-1@example</segment></segments></file></nzb>`

func newUsenetEnv(t *testing.T) *usenetEnv {
	t.Helper()
	e := &usenetEnv{clientEnv: newClientEnv(t)}
	nzb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, fixtureNZB)
	}))
	t.Cleanup(nzb.Close)
	e.nzbURL = nzb.URL + "/release.nzb"
	e.sab = &fake.SAB{APIKey: "sab-key-1", Categories: []string{"films", "shows"}, Steps: 2}
	sabSrv := httptest.NewServer(e.sab)
	t.Cleanup(sabSrv.Close)
	e.sabURL = sabSrv.URL
	e.ng = &fake.NZBGet{User: "nzbget", Pass: "ng-pass", Categories: []string{"films"}, Steps: 2}
	ngSrv := httptest.NewServer(e.ng)
	t.Cleanup(ngSrv.Close)
	e.ngURL = ngSrv.URL
	return e
}

// saveSAB connects the SABnzbd stand-in and sends Usenet releases to it.
func (e *usenetEnv) saveSAB(t *testing.T, extra map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"kind": "sabnzbd", "use": true, "url": e.sabURL, "apiKey": "sab-key-1", "category": "films"}
	for k, v := range extra {
		body[k] = v
	}
	return postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.url+"/api/usenet-client", body, http.StatusOK)
}

func (e *usenetEnv) saveNZBGet(t *testing.T, extra map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"kind": "nzbget", "use": true, "url": e.ngURL, "username": "nzbget", "password": "ng-pass", "category": "films"}
	for k, v := range extra {
		body[k] = v
	}
	return postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.url+"/api/usenet-client", body, http.StatusOK)
}

// grabUsenet sends a Usenet release for a new movie and returns its queue
// number.
func (e *usenetEnv) grabUsenet(t *testing.T) (library.Movie, int64) {
	t.Helper()
	m, err := e.server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/movies/%d/grab", e.url, m.ID), map[string]any{
		"releaseTitle": "The.Fixture.Movie.1999.1080p.WEB-DL.x264-FIXTURE",
		"downloadUrl":  e.nzbURL,
		"sizeBytes":    1000,
		"protocol":     "usenet",
	}, http.StatusAccepted)
	q := getJSON[[]map[string]any](t, e.client, e.url+"/api/queue")
	if len(q) != 1 {
		t.Fatalf("queue: %v", q)
	}
	return m, int64(q[0]["id"].(float64))
}

// finished waits until the only queue item is completed, failed or stopped.
func (e *usenetEnv) finished(t *testing.T) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		q := getJSON[[]map[string]any](t, e.client, e.url+"/api/queue")
		if len(q) == 1 && (q[0]["status"] == "completed" || q[0]["status"] == "failed") {
			e.server.TestWaitBackground()
			return q[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the download didn't finish in time")
	return nil
}

func (e *usenetEnv) waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func hasAction(actions []string, want string) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}

// A release goes to SABnzbd as a file, is followed until the job is in the
// history, and is imported from the folder SABnzbd reports (seen by Mediarium
// under another path). The files stay where they are.
func TestUsenetClientSABnzbdImport(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	local := filepath.Join(e.dir, "sab-complete")
	folder := filepath.Join(local, "films", "The.Fixture.Movie.1999")
	writeClientFile(t, filepath.Join(folder, "The.Fixture.Movie.1999.mkv"), movieBytes)
	writeClientFile(t, filepath.Join(folder, "The.Fixture.Movie.1999.en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nHello\n")
	e.sab.Result = fake.Result{Path: "/complete/films/The.Fixture.Movie.1999"}
	e.saveSAB(t, map[string]any{"remotePath": "/complete", "localPath": local, "priority": "high"})

	m, _ := e.grabUsenet(t)
	item := e.finished(t)
	if item["status"] != "completed" {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
	movie := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/movies/%d", e.url, m.ID))
	got, err := os.ReadFile(movie["filePath"].(string))
	if err != nil || string(got) != movieBytes {
		t.Fatalf("imported file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, "The.Fixture.Movie.1999.mkv")); err != nil {
		t.Errorf("SABnzbd's file is gone: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(e.dir, "downloads", "incomplete")); len(entries) != 0 {
		t.Errorf("the download's folder should be cleaned up, found %v", entries)
	}
	adds, actions := e.sab.Snapshot()
	if len(adds) != 1 || adds[0].NZB != fixtureNZB || adds[0].URL != "" || adds[0].Category != "films" || adds[0].Priority != "1" {
		t.Errorf("what SABnzbd got: %+v", adds)
	}
	if len(actions) != 0 {
		t.Errorf("nothing may be removed from SABnzbd unless asked to: %v", actions)
	}
}

// With "Remove from the downloader after import" on, the job is removed from
// NZBGet once the movie is in the library.
func TestUsenetClientNZBGetImportAndRemove(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	folder := filepath.Join(e.dir, "ng-dst", "Movies", "The.Fixture.Movie.1999")
	writeClientFile(t, filepath.Join(folder, "The.Fixture.Movie.1999.mkv"), movieBytes)
	e.ng.Result = fake.Result{Path: folder}
	e.saveNZBGet(t, map[string]any{"removeAfterImport": true, "sendUrl": true})

	m, _ := e.grabUsenet(t)
	if item := e.finished(t); item["status"] != "completed" {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
	movie := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/movies/%d", e.url, m.ID))
	if got, _ := os.ReadFile(movie["filePath"].(string)); string(got) != movieBytes {
		t.Fatal("the wrong file was imported")
	}
	adds, actions := e.ng.Snapshot()
	// The address is passed on so NZBGet fetches the file itself.
	if len(adds) != 1 || adds[0].URL != e.nzbURL || adds[0].NZB != "" || adds[0].Category != "films" {
		t.Errorf("what NZBGet got: %+v", adds)
	}
	if !hasAction(actions, "HistoryFinalDelete 1") {
		t.Errorf("the job should be removed after the import: %v", actions)
	}
	if _, err := os.Stat(filepath.Join(folder, "The.Fixture.Movie.1999.mkv")); err != nil {
		t.Errorf("Mediarium must not delete NZBGet's files itself: %v", err)
	}
}

// A job SABnzbd gave up on (articles missing) is the release's fault: it is
// blocklisted and cleared out of SABnzbd, so another release can be tried.
func TestUsenetClientFailureBlocklists(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	e.sab.Result = fake.Result{Failed: true, Message: "Aborted, cannot be completed - https://sabnzbd.org/not-complete"}
	e.saveSAB(t, nil)
	e.grabUsenet(t)
	item := e.finished(t)
	if item["status"] != "failed" || !strings.Contains(fmt.Sprint(item["error"]), "SABnzbd couldn't finish it: Aborted") {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
	list, err := e.server.Blocklist.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("the release must be blocklisted: %v, %v", list, err)
	}
	if _, actions := e.sab.Snapshot(); !hasAction(actions, "queue delete SABnzbd_nzo_0001 files=1") {
		t.Errorf("the failed job should be cleared out of SABnzbd: %v", actions)
	}
}

func TestUsenetClientFailureNZBGet(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	e.ng.Result = fake.Result{Failed: true, Message: "FAILURE/HEALTH"}
	e.saveNZBGet(t, nil)
	e.grabUsenet(t)
	item := e.finished(t)
	if item["status"] != "failed" || !strings.Contains(fmt.Sprint(item["error"]), "too many articles were missing") {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
	if list, _ := e.server.Blocklist.List(); len(list) != 1 {
		t.Errorf("the release must be blocklisted: %v", list)
	}
}

// The downloader's own trouble (a full disk) is not the release's fault.
func TestUsenetClientOwnTroubleKeepsTheJob(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	e.sab.Result = fake.Result{Failed: true, Message: "Disk full while unpacking"}
	e.saveSAB(t, nil)
	e.grabUsenet(t)
	item := e.finished(t)
	if item["status"] != "failed" || !strings.Contains(fmt.Sprint(item["error"]), "reports a problem") {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
	if list, _ := e.server.Blocklist.List(); len(list) != 0 {
		t.Errorf("the release must not be blocklisted: %v", list)
	}
	if _, actions := e.sab.Snapshot(); len(actions) != 0 {
		t.Errorf("the job must stay in SABnzbd: %v", actions)
	}
}

// A finished download Mediarium can't see says what to do about the path.
func TestUsenetClientUnseenPath(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	e.sab.Result = fake.Result{Path: "/somewhere/only/sab/sees"}
	e.saveSAB(t, nil)
	e.grabUsenet(t)
	item := e.finished(t)
	if item["status"] != "failed" || !strings.Contains(fmt.Sprint(item["error"]), "Settings > Downloading > Usenet") {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
}

// With the switch off, Usenet releases stay with the built-in downloader and
// the external one is not asked.
func TestUsenetClientOffKeepsTheBuiltInDownloader(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	e.saveSAB(t, map[string]any{"use": false})
	e.grabUsenet(t)
	item := e.finished(t)
	// No news server is set up, so the built-in downloader has nothing to use.
	if item["status"] != "failed" || !strings.Contains(fmt.Sprint(item["error"]), "no Usenet server") {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
	if adds, _ := e.sab.Snapshot(); len(adds) != 0 {
		t.Errorf("SABnzbd must not be asked: %+v", adds)
	}
}

// Stopping a download that is still running removes its job from the
// downloader; a download paused first is paused there too.
func TestUsenetClientPauseAndStop(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	e.sab.Hold = true
	e.saveSAB(t, nil)
	_, id := e.grabUsenet(t)
	e.waitFor(t, "SABnzbd to have the job and Mediarium to follow it", func() bool {
		q := getJSON[[]map[string]any](t, e.client, e.url+"/api/queue")
		adds, _ := e.sab.Snapshot()
		return len(adds) == 1 && len(q) == 1 && q[0]["status"] == "downloading"
	})
	post := func(path string) {
		postJSON[map[string]any](t, e.client, e.url+path, map[string]any{}, http.StatusOK)
	}
	post(fmt.Sprintf("/api/queue/%d/pause", id))
	e.waitFor(t, "the job to be paused in SABnzbd", func() bool {
		_, actions := e.sab.Snapshot()
		return hasAction(actions, "queue pause SABnzbd_nzo_0001 files=")
	})
	post(fmt.Sprintf("/api/queue/%d/resume", id))
	e.waitFor(t, "the job to be started again in SABnzbd", func() bool {
		_, actions := e.sab.Snapshot()
		return hasAction(actions, "queue resume SABnzbd_nzo_0001 files=")
	})
	post(fmt.Sprintf("/api/queue/%d/stop", id))
	e.waitFor(t, "the job to be removed from SABnzbd", func() bool {
		_, actions := e.sab.Snapshot()
		return hasAction(actions, "queue delete SABnzbd_nzo_0001 files=1")
	})
}

// The settings: secrets are never given back, a blank one keeps the saved one,
// and the test says clearly what is wrong.
func TestUsenetClientSettingsAndTest(t *testing.T) {
	e := newUsenetEnv(t)
	post := func(body map[string]any) map[string]any {
		return postJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/test", body, http.StatusOK)
	}
	errorOf := func(body map[string]any) string {
		r := post(body)
		if r["ok"] != false {
			t.Fatalf("expected a problem: %v", r)
		}
		return fmt.Sprint(r["error"])
	}

	saved := e.saveSAB(t, nil)
	if saved["kind"] != "sabnzbd" || saved["use"] != true || saved["hasApiKey"] != true || saved["priority"] != "normal" {
		t.Fatalf("saved: %v", saved)
	}
	for k, v := range saved {
		if s, ok := v.(string); ok && (strings.Contains(s, "sab-key-1") || strings.Contains(s, "ng-pass")) {
			t.Errorf("%s gives a secret back: %q", k, s)
		}
	}
	// A blank key tests the saved one when the address is the saved one.
	if r := post(map[string]any{"kind": "sabnzbd", "url": e.sabURL, "category": "films"}); r["ok"] != true || r["version"] != "4.3.2" {
		t.Errorf("saved key: %v", r)
	}
	if msg := errorOf(map[string]any{"kind": "sabnzbd", "url": e.sabURL, "apiKey": "wrong", "category": "films"}); !strings.Contains(msg, "refused the API key") {
		t.Errorf("wrong key: %q", msg)
	}
	if msg := errorOf(map[string]any{"kind": "sabnzbd", "url": e.sabURL, "category": "movies"}); !strings.Contains(msg, `no category called "movies"`) {
		t.Errorf("missing category: %q", msg)
	}
	if msg := errorOf(map[string]any{"kind": "sabnzbd", "url": e.sabURL, "category": "films", "categoryTv": "tele"}); !strings.Contains(msg, `no category called "tele"`) {
		t.Errorf("missing kind category: %q", msg)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	if msg := errorOf(map[string]any{"kind": "sabnzbd", "url": deadURL, "apiKey": "k"}); !strings.Contains(msg, "couldn't reach SABnzbd") {
		t.Errorf("nothing there: %q", msg)
	}
	if msg := errorOf(map[string]any{"kind": "nzbget", "url": e.ngURL, "username": "nzbget", "password": "bad"}); !strings.Contains(msg, "refused the user name or password") {
		t.Errorf("wrong NZBGet password: %q", msg)
	}
	if msg := errorOf(map[string]any{"kind": "nzbget", "url": e.ngURL, "username": "nzbget", "password": "ng-pass", "category": "tv"}); !strings.Contains(msg, `no category called "tv"`) {
		t.Errorf("NZBGet category: %q", msg)
	}
	if r := post(map[string]any{"kind": "nzbget", "url": e.ngURL, "username": "nzbget", "password": "ng-pass", "category": "films"}); r["ok"] != true || r["version"] != "21.1" {
		t.Errorf("NZBGet: %v", r)
	}
	// Not the right program at that address.
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html></html>") }))
	defer web.Close()
	if msg := errorOf(map[string]any{"kind": "sabnzbd", "url": web.URL, "apiKey": "k"}); !strings.Contains(msg, "not like SABnzbd") {
		t.Errorf("a web page: %q", msg)
	}

	// Saving: SABnzbd needs its key; another address drops the saved one.
	putJSONStatus(t, e.client, e.url+"/api/usenet-client", map[string]any{"kind": "sabnzbd", "url": "http://other.example:8080"}, http.StatusBadRequest)
	putJSONStatus(t, e.client, e.url+"/api/usenet-client", map[string]any{"kind": "other", "url": e.sabURL}, http.StatusBadRequest)
	putJSONStatus(t, e.client, e.url+"/api/usenet-client", map[string]any{"kind": "sabnzbd", "url": e.sabURL, "priority": "urgent"}, http.StatusBadRequest)
	kept := putJSONStatus(t, e.client, e.url+"/api/usenet-client", map[string]any{"kind": "sabnzbd", "url": e.sabURL, "use": false}, http.StatusOK)
	if kept["hasApiKey"] != true || kept["use"] != false {
		t.Errorf("a blank key keeps the saved one: %v", kept)
	}
	off := putJSONStatus(t, e.client, e.url+"/api/usenet-client", map[string]any{"kind": ""}, http.StatusOK)
	if off["kind"] != "" || off["hasApiKey"] != false || off["use"] != false {
		t.Errorf("turned off: %v", off)
	}
	// Both switched on at the same time is the point of the status.
	e.saveSAB(t, nil)
	if st := getJSON[map[string]any](t, e.client, e.url+"/api/downloads/status")["usenet"].(map[string]any); st["external"] != "sabnzbd" || st["ready"] != true {
		t.Errorf("downloads status: %v", st)
	}
}

// The Activity page lists the jobs in the downloader and controls them.
func TestUsenetClientJobsCanBeSeenAndControlled(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	none := getJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs")
	if none["configured"] != false {
		t.Fatalf("nothing set up: %v", none)
	}
	e.sab.Hold = true
	e.saveSAB(t, nil)
	// One of Mediarium's (in its category), one someone else added.
	postJSON[map[string]any](t, e.client, e.sabURL+"/api?mode=addurl&name=x&nzbname=Mine.Movie&cat=films&apikey=sab-key-1", map[string]any{}, http.StatusOK)
	postJSON[map[string]any](t, e.client, e.sabURL+"/api?mode=addurl&name=y&nzbname=Somebody.Elses&cat=other&apikey=sab-key-1", map[string]any{}, http.StatusOK)
	mine := getJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs")
	jobs := mine["jobs"].([]any)
	if mine["configured"] != true || mine["label"] != "SABnzbd" || len(jobs) != 1 || jobs[0].(map[string]any)["name"] != "Mine.Movie" || jobs[0].(map[string]any)["mine"] != true {
		t.Fatalf("mine: %v", mine)
	}
	if all := getJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs?scope=all")["jobs"].([]any); len(all) != 2 {
		t.Fatalf("all: %v", all)
	}
	id := jobs[0].(map[string]any)["id"].(string)
	postJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs/"+id+"/pause", map[string]any{}, http.StatusOK)
	j := getJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs")["jobs"].([]any)[0].(map[string]any)
	if j["stage"] != "paused" || j["canResume"] != true || j["canPause"] != false {
		t.Errorf("paused: %v", j)
	}
	postJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs/"+id+"/resume", map[string]any{}, http.StatusOK)
	postJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs/"+id+"/remove-files", map[string]any{}, http.StatusOK)
	if left := getJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs")["jobs"].([]any); len(left) != 0 {
		t.Errorf("removed: %v", left)
	}
	postJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs/"+id+"/explode", map[string]any{}, http.StatusBadRequest)
	postJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs/bad%20id/pause", map[string]any{}, http.StatusBadRequest)
	// Wrong key: the page is told, in words.
	e.saveSAB(t, map[string]any{"apiKey": "changed"})
	bad := getJSON[map[string]any](t, e.client, e.url+"/api/usenet-client/jobs")
	if !strings.Contains(fmt.Sprint(bad["error"]), "refused the API key") {
		t.Errorf("wrong key: %v", bad)
	}
}

// Retry after the downloader's own trouble removes the failed job there before
// sending the release again, so it isn't in the downloader twice.
func TestUsenetClientRetryRemovesTheFailedJob(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newUsenetEnv(t)
	e.sab.Result = fake.Result{Failed: true, Message: "Disk full while unpacking"}
	e.saveSAB(t, nil)
	_, id := e.grabUsenet(t)
	e.finished(t)
	postJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/queue/%d/retry", e.url, id), map[string]any{}, http.StatusAccepted)
	e.waitFor(t, "the release to be sent again", func() bool {
		adds, _ := e.sab.Snapshot()
		return len(adds) == 2
	})
	e.finished(t)
	adds, actions := e.sab.Snapshot()
	removed := false
	for _, a := range actions {
		if strings.Contains(a, "delete SABnzbd_nzo_0001") {
			removed = true
		}
	}
	if !removed || len(adds) != 2 {
		t.Errorf("the failed job must be removed before sending again: %d sent, actions %v", len(adds), actions)
	}
}
