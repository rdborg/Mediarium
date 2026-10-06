package api_test

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/store"
)

// --- a Usenet server that can be told to stop answering ---

// gatedNNTP serves articles by message-id like a real news server, and after
// gateAfter answers it holds every further request until open is called. That
// keeps a download in the middle of its transfer for as long as a test needs.
type gatedNNTP struct {
	port      int
	bodies    map[string][]byte
	gateAfter int
	delay     time.Duration // how long each answer takes; zero for none

	mu    sync.Mutex
	hits  map[string]int
	total int

	gate     chan struct{}
	openOnce sync.Once
}

func newGatedNNTP(t *testing.T, bodies map[string][]byte, gateAfter int) *gatedNNTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &gatedNNTP{bodies: bodies, gateAfter: gateAfter, hits: map[string]int{}, gate: make(chan struct{})}
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	g.port, _ = strconv.Atoi(p)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go g.serve(conn)
		}
	}()
	t.Cleanup(func() { g.open(); ln.Close() })
	return g
}

func (g *gatedNNTP) open() { g.openOnce.Do(func() { close(g.gate) }) }

func (g *gatedNNTP) totalHits() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.total
}

func (g *gatedNNTP) hitsFor(id string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.hits[id]
}

func (g *gatedNNTP) serve(conn net.Conn) {
	defer conn.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	rw.WriteString("200 ready\r\n")
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
			g.mu.Lock()
			g.total++
			g.hits[id]++
			held := g.gateAfter >= 0 && g.total > g.gateAfter
			g.mu.Unlock()
			if held {
				<-g.gate
			}
			if g.delay > 0 {
				time.Sleep(g.delay)
			}
			body, ok := g.bodies[id]
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
}

// --- a release of five files: four small ones, then the movie ---

type fixtureFile struct {
	name string
	data []byte
}

var releaseFiles = []fixtureFile{
	{"a.txt", []byte("file a")},
	{"b.txt", []byte("file b")},
	{"c.txt", []byte("file c")},
	{"d.txt", []byte("file d")},
	{"movie.mkv", []byte("Fixture movie bytes standing in for a real video file. " + strings.Repeat("padding-", 300))},
}

func articleID(i int) string { return fmt.Sprintf("art-%d@example", i+1) }

func releaseBodies() map[string][]byte {
	out := map[string][]byte{}
	for i, f := range releaseFiles {
		out[articleID(i)] = encodeFixtureYenc(f.name, f.data)
	}
	return out
}

func releaseNZB() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`)
	for i, f := range releaseFiles {
		fmt.Fprintf(&b, `<file subject="[%d/%d] &quot;%s&quot; yEnc (1/1)"><groups><group>alt.binaries.test</group></groups><segments><segment bytes="%d" number="1">%s</segment></segments></file>`,
			i+1, len(releaseFiles), f.name, len(f.data), articleID(i))
	}
	b.WriteString(`</nzb>`)
	return b.String()
}

// --- the test setup ---

type controlEnv struct {
	server   *api.Server
	base     string
	client   *http.Client
	nntp     *gatedNNTP
	nzbURL   string
	movie    library.Movie
	movies   string // the movie library folder
	workDir  string
	wantFile []byte
}

const releaseName = "The.Fixture.Movie.1999.1080p.WEB-DL.x264-FIXTURE"

// newControlServer builds a server with its own folders. safeMode turns on
// MEDIARIUM_PAUSE_AUTOMATION.
func newControlServer(t *testing.T, safeMode bool) (*api.Server, string, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
		PauseAutomation:     safeMode,
	}
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitBackground(t, server) })
	httpSrv := httptest.NewServer(server.Routes())
	t.Cleanup(httpSrv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	return server, httpSrv.URL, client
}

// newControlEnv is a server with a Usenet provider, a movie and a release for
// it that stops answering after gateAfter articles (-1: never).
func newControlEnv(t *testing.T, gateAfter int) *controlEnv {
	t.Helper()
	server, base, client := newControlServer(t, false)
	nntp := newGatedNNTP(t, releaseBodies(), gateAfter)
	postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{
		"name": "Fixture Usenet", "host": "127.0.0.1", "port": nntp.port, "useSsl": false, "connections": 1,
	}, http.StatusCreated)

	nzb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-nzb")
		fmt.Fprint(w, releaseNZB())
	}))
	t.Cleanup(nzb.Close)

	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	return &controlEnv{
		server: server, base: base, client: client, nntp: nntp, nzbURL: nzb.URL + "/x.nzb", movie: m,
		movies: server.TestMoviesRoot(), workDir: server.TestWorkDir(), wantFile: releaseFiles[4].data,
	}
}

func (e *controlEnv) grab(t *testing.T) int64 {
	t.Helper()
	id, err := e.server.TestGrabMovie(e.movie, releaseName, e.nzbURL)
	if err != nil {
		t.Fatalf("grab: %v", err)
	}
	return id
}

func (e *controlEnv) url(id int64, action string) string {
	return fmt.Sprintf("%s/api/queue/%d/%s", e.base, id, action)
}

func (e *controlEnv) item(t *testing.T, id int64) map[string]any {
	t.Helper()
	for _, it := range getJSON[[]map[string]any](t, e.client, e.base+"/api/queue") {
		if int64(it["id"].(float64)) == id {
			return it
		}
	}
	return nil
}

func (e *controlEnv) status(t *testing.T, id int64) string {
	t.Helper()
	if it := e.item(t, id); it != nil {
		return it["status"].(string)
	}
	return "gone"
}

func (e *controlEnv) movieStatus(t *testing.T) string {
	t.Helper()
	return getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/movies/%d", e.base, e.movie.ID))["status"].(string)
}

func (e *controlEnv) workDirFor(id int64) string { return e.server.TestWorkDirFor(id) }

// waitFor polls until cond is true.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// downloadHeld starts a download and waits until it is stuck on its third
// article: the first two files are saved.
func (e *controlEnv) downloadHeld(t *testing.T) int64 {
	t.Helper()
	id := e.grab(t)
	waitFor(t, "the download to reach the held article", func() bool { return e.nntp.totalHits() >= 3 })
	return id
}

// --- pause and resume ---

func TestPauseThenResumeCarriesOnFromTheSavedFiles(t *testing.T) {
	e := newControlEnv(t, 2)
	id := e.downloadHeld(t)
	if got := e.status(t, id); got != "downloading" {
		t.Fatalf("expected downloading, got %s", got)
	}

	postJSON[map[string]any](t, e.client, e.url(id, "pause"), nil, http.StatusOK)
	it := e.item(t, id)
	if it["status"] != "paused" || it["interrupted"] == true || it["keptFiles"] != true {
		t.Fatalf("a paused download keeps its files and says nobody interrupted it: %+v", it)
	}
	if it["progressPct"].(float64) <= 0 {
		t.Fatalf("a paused download keeps its progress: %+v", it)
	}
	if _, err := os.Stat(filepath.Join(e.workDirFor(id), download.ResumeFile)); err != nil {
		t.Fatalf("the saved articles should be recorded in the download folder: %v", err)
	}
	if got := e.movieStatus(t); got != "downloading" {
		t.Fatalf("a paused download still holds its movie, got %s", got)
	}

	// The transfer really stopped.
	hits := e.nntp.totalHits()
	time.Sleep(300 * time.Millisecond)
	if e.nntp.totalHits() != hits {
		t.Fatal("a paused download must not ask the news server for more")
	}

	e.nntp.open()
	postJSON[map[string]any](t, e.client, e.url(id, "resume"), nil, http.StatusOK)
	waitFor(t, "the resumed download to finish", func() bool { return e.status(t, id) == "completed" })

	// The two files saved before the pause were not fetched again; the one
	// that was held was asked for twice, once for each attempt.
	for i, want := range []int{1, 1, 2, 1, 1} {
		if got := e.nntp.hitsFor(articleID(i)); got != want {
			t.Errorf("article %d (%s) was requested %d times, want %d", i+1, releaseFiles[i].name, got, want)
		}
	}
	movie := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/movies/%d", e.base, e.movie.ID))
	got, err := os.ReadFile(movie["filePath"].(string))
	if err != nil || !bytes.Equal(got, e.wantFile) {
		t.Fatalf("the movie in the library should be the complete file: %v", err)
	}
	// "completed" is set just before the clean-up runs, so give it a moment.
	waitFor(t, "the download folder to be cleaned up after the import", func() bool { return !exists(e.workDirFor(id)) })
}

func TestStopTable(t *testing.T) {
	tests := []struct {
		name       string
		pauseFirst bool
		deleteAll  bool
	}{
		{"stop a running download and delete the partial files", false, true},
		{"stop a running download and keep the partial files", false, false},
		{"stop a paused download and delete the partial files", true, true},
		{"stop a paused download and keep the partial files", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newControlEnv(t, 2)
			// Something already in the library must be left alone.
			sentinel := filepath.Join(e.movies, "Other Movie (2001)", "Other Movie (2001).mkv")
			if err := os.MkdirAll(filepath.Dir(sentinel), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sentinel, []byte("precious"), 0o644); err != nil {
				t.Fatal(err)
			}

			id := e.downloadHeld(t)
			if tc.pauseFirst {
				postJSON[map[string]any](t, e.client, e.url(id, "pause"), nil, http.StatusOK)
			}
			postJSON[map[string]any](t, e.client, e.url(id, "stop"), map[string]any{"deleteFiles": tc.deleteAll}, http.StatusOK)

			waitFor(t, "the item to be stopped", func() bool { return e.status(t, id) == "stopped" })
			it := e.item(t, id)
			if it["completedAt"] == nil || it["completedAt"] == "" {
				t.Fatalf("a stopped item has a finish time: %+v", it)
			}
			if kept := exists(e.workDirFor(id)); kept == tc.deleteAll {
				t.Fatalf("partial files kept = %v, but deleteFiles = %v", kept, tc.deleteAll)
			}
			if it["keptFiles"] == true == tc.deleteAll {
				t.Fatalf("the list should say whether the files are kept: %+v", it)
			}
			if got := e.movieStatus(t); got != "missing" {
				t.Fatalf("a stopped download frees its movie, got %s", got)
			}
			if b, err := os.ReadFile(sentinel); err != nil || string(b) != "precious" {
				t.Fatal("stopping a download must not touch the library")
			}
			entries, _ := os.ReadDir(e.movies)
			if len(entries) != 1 {
				t.Fatalf("nothing should have been imported: %v", entries)
			}

			// Nothing more is fetched after a stop.
			hits := e.nntp.totalHits()
			time.Sleep(200 * time.Millisecond)
			if e.nntp.totalHits() != hits {
				t.Fatal("a stopped download must not ask the news server for more")
			}

			// A stopped download can be tried again; with its files kept it
			// carries on from them instead of starting over.
			e.nntp.open()
			resp := postJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/queue/%d/retry", e.base, id), nil, http.StatusAccepted)
			newID := int64(resp["queueId"].(float64))
			waitFor(t, "the retry to finish", func() bool { return e.status(t, newID) == "completed" })
			if e.status(t, id) != "gone" {
				t.Fatal("the retry replaces the stopped entry")
			}
			wantA := 1
			if tc.deleteAll {
				wantA = 2 // the files were deleted, so they are fetched again
			}
			if got := e.nntp.hitsFor(articleID(0)); got != wantA {
				t.Fatalf("first file requested %d times, want %d", got, wantA)
			}
		})
	}
}

func TestRemoveAPausedDownload(t *testing.T) {
	tests := []struct {
		name        string
		deleteFiles bool
	}{
		{"and delete its files", true},
		{"and keep its files for the daily clean-up", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newControlEnv(t, 2)
			id := e.downloadHeld(t)
			postJSON[map[string]any](t, e.client, e.url(id, "pause"), nil, http.StatusOK)

			url := fmt.Sprintf("%s/api/queue/%d", e.base, id)
			if tc.deleteFiles {
				url += "?deleteFiles=1"
			}
			if code := deleteReq(t, e.client, url); code != http.StatusOK {
				t.Fatalf("remove: %d", code)
			}
			if e.status(t, id) != "gone" {
				t.Fatal("the entry should be gone")
			}
			if kept := exists(e.workDirFor(id)); kept == tc.deleteFiles {
				t.Fatalf("files kept = %v, deleteFiles = %v", kept, tc.deleteFiles)
			}
			if got := e.movieStatus(t); got != "missing" {
				t.Fatalf("removing a paused download frees its movie, got %s", got)
			}
		})
	}
}

func TestPauseAllAndResumeAll(t *testing.T) {
	e := newControlEnv(t, 2)
	id := e.downloadHeld(t)
	other, err := e.server.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Another.Release", NZBURL: e.nzbURL})
	if err != nil {
		t.Fatal(err)
	}
	// This one is waiting for its turn (no pipeline), so it can be paused too.
	if resp := postJSON[map[string]any](t, e.client, e.base+"/api/queue/pause-all", nil, http.StatusOK); resp["paused"] != float64(2) {
		t.Fatalf("expected 2 paused, got %+v", resp)
	}
	if e.status(t, id) != "paused" || e.status(t, other) != "paused" {
		t.Fatalf("both should be paused: %s, %s", e.status(t, id), e.status(t, other))
	}
	// Pausing again has nothing to do.
	if resp := postJSON[map[string]any](t, e.client, e.base+"/api/queue/pause-all", nil, http.StatusOK); resp["paused"] != float64(0) {
		t.Fatalf("expected 0 paused the second time, got %+v", resp)
	}

	// The second item is not linked to a title, so it cannot start: it is
	// reported, and the real one still resumes.
	e.nntp.open()
	resp := postJSON[map[string]any](t, e.client, e.base+"/api/queue/resume-all", nil, http.StatusOK)
	if resp["resumed"] != float64(1) || resp["skipped"] != float64(1) || resp["problem"] == "" {
		t.Fatalf("expected one resumed and one that could not start, with a reason: %+v", resp)
	}
	waitFor(t, "the resumed download to finish", func() bool { return e.status(t, id) == "completed" })
	if e.status(t, other) != "paused" {
		t.Fatalf("an item that could not start stays paused, got %s", e.status(t, other))
	}
}

// --- state transitions ---

func TestQueueControlRefusesWhatDoesNotApply(t *testing.T) {
	server, base, client := newControlServer(t, false)
	m, _ := server.MovieRepo.Add(library.Movie{TMDBID: 5, Title: "Some Movie", Monitored: true})
	seed := func(st queue.Status) int64 {
		id, err := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "r-" + string(st)})
		if err != nil {
			t.Fatal(err)
		}
		if st == queue.StatusPaused {
			_ = server.QueueRepo.Pause(id, false)
		} else if st != queue.StatusQueued {
			_ = server.QueueRepo.SetStatus(id, st, "")
		}
		return id
	}
	tests := []struct {
		from   queue.Status
		action string
		want   int
	}{
		{queue.StatusCompleted, "pause", http.StatusConflict},
		{queue.StatusFailed, "pause", http.StatusConflict},
		{queue.StatusStopped, "pause", http.StatusConflict},
		{queue.StatusPaused, "pause", http.StatusConflict},
		{queue.StatusQueued, "resume", http.StatusConflict},
		{queue.StatusDownloading, "resume", http.StatusConflict},
		{queue.StatusFailed, "resume", http.StatusConflict},
		{queue.StatusStopped, "resume", http.StatusConflict},
		{queue.StatusCompleted, "stop", http.StatusConflict},
		{queue.StatusFailed, "stop", http.StatusConflict},
		{queue.StatusStopped, "stop", http.StatusConflict},
		{queue.StatusCompleted, "retry", http.StatusConflict},
		{queue.StatusPaused, "retry", http.StatusConflict},
		{queue.StatusDownloading, "retry", http.StatusConflict},
	}
	for _, tc := range tests {
		id := seed(tc.from)
		resp, err := client.Post(fmt.Sprintf("%s/api/queue/%d/%s", base, id, tc.action), "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s on a %s item: got %d, want %d", tc.action, tc.from, resp.StatusCode, tc.want)
		}
		if got, _ := server.QueueRepo.Get(id); got.Status != tc.from {
			t.Errorf("%s on a %s item changed it to %s", tc.action, tc.from, got.Status)
		}
	}
	for _, action := range []string{"pause", "resume", "stop"} {
		postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/9999/%s", base, action), nil, http.StatusNotFound)
	}
	// A running download must be paused or stopped before it can be removed.
	if code := deleteReq(t, client, fmt.Sprintf("%s/api/queue/%d", base, seed(queue.StatusDownloading))); code != http.StatusConflict {
		t.Fatalf("removing a running download: %d", code)
	}
	// Removing a stopped one is fine.
	if code := deleteReq(t, client, fmt.Sprintf("%s/api/queue/%d", base, seed(queue.StatusStopped))); code != http.StatusOK {
		t.Fatalf("removing a stopped download: %d", code)
	}
}

func TestMembersCannotPauseOrStop(t *testing.T) {
	access := (&api.Server{}).TestRouteAccess()
	for _, route := range []string{
		"POST /api/queue/{id}/pause", "POST /api/queue/{id}/resume", "POST /api/queue/{id}/stop",
		"POST /api/queue/pause-all", "POST /api/queue/resume-all",
	} {
		if access[route] != "admin" {
			t.Errorf("%s must be for administrators, is %q", route, access[route])
		}
	}
	if access["POST /api/queue/{id}/retry"] != "member" {
		t.Error("retrying stays open to members")
	}
}

// --- restarts ---

func TestAPausedDownloadStaysPausedAfterARestart(t *testing.T) {
	e := newControlEnv(t, 2)
	id := e.downloadHeld(t)
	postJSON[map[string]any](t, e.client, e.url(id, "pause"), nil, http.StatusOK)

	reopened, err := e.server.TestReopen()
	if err != nil {
		t.Fatal(err)
	}
	e.nntp.open()
	hits := e.nntp.totalHits()
	time.Sleep(400 * time.Millisecond)
	reopened.TestWaitBackground()

	got, err := reopened.QueueRepo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusPaused || got.Interrupted {
		t.Fatalf("a download paused by a person is still paused after a restart: %+v", got)
	}
	if e.nntp.totalHits() != hits {
		t.Fatal("nothing may be downloaded after a restart until someone presses Resume")
	}
	if !exists(e.workDirFor(id)) {
		t.Fatal("the partial files must survive a restart")
	}
}

func TestADownloadCutShortByARestartComesBackPausedAndCanBeResumed(t *testing.T) {
	e := newControlEnv(t, 2)
	id := e.downloadHeld(t)
	postJSON[map[string]any](t, e.client, e.url(id, "pause"), nil, http.StatusOK)
	// As if the app had been killed while it was downloading: the row still
	// says "downloading" and nothing is running it.
	if err := e.server.QueueRepo.SetStatus(id, queue.StatusDownloading, ""); err != nil {
		t.Fatal(err)
	}

	reopened, err := e.server.TestReopen()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reopened.QueueRepo.Get(id)
	if got.Status != queue.StatusPaused || !got.Interrupted {
		t.Fatalf("expected paused and marked as interrupted, got %+v", got)
	}
	hits := e.nntp.totalHits()
	e.nntp.open()
	time.Sleep(300 * time.Millisecond)
	if e.nntp.totalHits() != hits {
		t.Fatal("an interrupted download must wait for someone to resume it")
	}

	if err := reopened.TestResumeQueueItem(id); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitFor(t, "the resumed download to finish", func() bool {
		it, err := reopened.QueueRepo.Get(id)
		return err == nil && it.Status == queue.StatusCompleted
	})
	reopened.TestWaitBackground()
	for i, want := range []int{1, 1, 2, 1, 1} {
		if got := e.nntp.hitsFor(articleID(i)); got != want {
			t.Errorf("article %d requested %d times, want %d", i+1, got, want)
		}
	}
}

func TestSafeModeNeverResumesAnything(t *testing.T) {
	for _, safe := range []bool{false, true} {
		t.Run(fmt.Sprintf("safe mode %v", safe), func(t *testing.T) {
			server, _, _ := newControlServer(t, safe)
			m, _ := server.MovieRepo.Add(library.Movie{TMDBID: 7, Title: "Some Movie", Monitored: true})
			var ids []int64
			for _, st := range []queue.Status{queue.StatusQueued, queue.StatusDownloading, queue.StatusImporting} {
				id, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "r-" + string(st), NZBURL: "http://127.0.0.1:1/x.nzb"})
				_ = server.QueueRepo.SetStatus(id, st, "")
				ids = append(ids, id)
			}
			reopened, err := server.TestReopen()
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(200 * time.Millisecond)
			reopened.TestWaitBackground()
			for _, id := range ids {
				it, _ := reopened.QueueRepo.Get(id)
				// What was running always comes back paused. What was only waiting in
				// line stays in line, except in safe mode, which starts nothing.
				if it.Status == queue.StatusQueued && !safe {
					continue
				}
				if it.Status != queue.StatusPaused || !it.Interrupted {
					t.Errorf("item %d should be paused after a restart, got %+v", id, it)
				}
			}
		})
	}
}

// --- the library is never touched ---

func TestStopNeverDeletesFromTheLibrary(t *testing.T) {
	t.Run("a download folder that is really a link to the library", func(t *testing.T) {
		server, base, client := newControlServer(t, false)
		m, _ := server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Monitored: true})
		lib := server.TestMoviesRoot()
		keep := filepath.Join(lib, "Film (2000)", "Film (2000).mkv")
		_ = os.MkdirAll(filepath.Dir(keep), 0o755)
		_ = os.WriteFile(keep, []byte("precious"), 0o644)

		id, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "r", NZBURL: "http://127.0.0.1:1/x.nzb"})
		_ = server.QueueRepo.Pause(id, false)
		if err := os.Symlink(lib, server.TestWorkDirFor(id)); err != nil {
			t.Skipf("no symbolic links here: %v", err)
		}
		postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/stop", base, id), map[string]any{"deleteFiles": true}, http.StatusOK)
		if b, err := os.ReadFile(keep); err != nil || string(b) != "precious" {
			t.Fatal("the library file must survive")
		}
	})

	t.Run("a downloads folder set inside the library", func(t *testing.T) {
		server, base, client := newControlServer(t, false)
		m, _ := server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Monitored: true})
		lib := server.TestMoviesRoot()
		inside := filepath.Join(lib, "downloads")
		_ = os.MkdirAll(filepath.Join(inside, "incomplete"), 0o755)
		postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"downloadsPath": inside}, http.StatusOK)
		keep := filepath.Join(lib, "Film (2000)", "Film (2000).mkv")
		_ = os.MkdirAll(filepath.Dir(keep), 0o755)
		_ = os.WriteFile(keep, []byte("precious"), 0o644)

		id, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "r", NZBURL: "http://127.0.0.1:1/x.nzb"})
		_ = server.QueueRepo.Pause(id, false)
		work := server.TestWorkDirFor(id)
		_ = os.MkdirAll(work, 0o755)
		_ = os.WriteFile(filepath.Join(work, "part.rar"), []byte("x"), 0o644)

		postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/stop", base, id), map[string]any{"deleteFiles": true}, http.StatusOK)
		if b, err := os.ReadFile(keep); err != nil || string(b) != "precious" {
			t.Fatal("the library file must survive")
		}
		if !exists(filepath.Join(work, "part.rar")) {
			t.Fatal("nothing inside a library folder is deleted, even a download folder placed there")
		}
	})

	t.Run("only the queue folder of that download is removed", func(t *testing.T) {
		server, base, client := newControlServer(t, false)
		m, _ := server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Monitored: true})
		id, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "r", NZBURL: "http://127.0.0.1:1/x.nzb"})
		_ = server.QueueRepo.Pause(id, false)
		mine := server.TestWorkDirFor(id)
		neighbour := filepath.Join(server.TestWorkDir(), "queue-9999")
		stray := filepath.Join(server.TestWorkDir(), "notes.txt")
		for _, d := range []string{mine, neighbour} {
			_ = os.MkdirAll(d, 0o755)
			_ = os.WriteFile(filepath.Join(d, "part"), []byte("x"), 0o644)
		}
		_ = os.WriteFile(stray, []byte("x"), 0o644)

		postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/stop", base, id), map[string]any{"deleteFiles": true}, http.StatusOK)
		if exists(mine) || !exists(neighbour) || !exists(stray) {
			t.Fatalf("expected only this download's folder to go: mine=%v neighbour=%v stray=%v", exists(mine), exists(neighbour), exists(stray))
		}
	})
}

// --- order ---

func TestQueueListComesInActivityOrder(t *testing.T) {
	server, base, client := newControlServer(t, false)
	seed := func(st queue.Status) {
		id, _ := server.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "r-" + string(st)})
		switch st {
		case queue.StatusQueued:
		case queue.StatusPaused:
			_ = server.QueueRepo.Pause(id, false)
		default:
			_ = server.QueueRepo.SetStatus(id, st, "")
		}
		time.Sleep(5 * time.Millisecond) // separate start times
	}
	for _, st := range []queue.Status{
		queue.StatusCompleted, queue.StatusStopped, queue.StatusFailed, queue.StatusPaused,
		queue.StatusQueued, queue.StatusImporting, queue.StatusConflict, queue.StatusDownloading,
	} {
		seed(st)
	}
	var got []string
	for _, it := range getJSON[[]map[string]any](t, client, base+"/api/queue") {
		got = append(got, it["status"].(string))
	}
	want := "importing downloading queued paused failed conflict stopped completed"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

// --- torrents ---

// A torrent release is paused, resumed and stopped the same way: pausing takes
// it out of the shared torrent engine, resuming puts it back. The magnet link
// has no peers, so the torrent waits for its details for as long as it runs.
func TestTorrentPauseResumeAndStop(t *testing.T) {
	server, base, client := newControlServer(t, false)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 77, Title: "Torrent Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := server.TestGrabMovieTorrent(m, "Torrent.Movie.2001.1080p.BluRay.x264-GRP", "magnet:?xt=urn:btih:1111111111111111111111111111111111111111&dn=x")
	if err != nil {
		t.Fatal(err)
	}
	url := func(action string) string { return fmt.Sprintf("%s/api/queue/%d/%s", base, id, action) }
	status := func() string {
		for _, it := range getJSON[[]map[string]any](t, client, base+"/api/queue") {
			if int64(it["id"].(float64)) == id {
				return it["status"].(string)
			}
		}
		return "gone"
	}

	waitFor(t, "the torrent to join the engine", func() bool { return server.TestTorrentRunning(id) })
	postJSON[map[string]any](t, client, url("pause"), nil, http.StatusOK)
	if got := status(); got != "paused" {
		t.Fatalf("expected paused, got %s", got)
	}
	if server.TestTorrentRunning(id) {
		t.Fatal("a paused torrent must leave the engine, so nothing is transferred")
	}

	postJSON[map[string]any](t, client, url("resume"), nil, http.StatusOK)
	waitFor(t, "the resumed torrent to join the engine", func() bool { return server.TestTorrentRunning(id) })
	if got := status(); got != "downloading" && got != "queued" {
		t.Fatalf("expected the resumed torrent to be running, got %s", got)
	}

	postJSON[map[string]any](t, client, url("stop"), map[string]any{"deleteFiles": true}, http.StatusOK)
	if got := status(); got != "stopped" {
		t.Fatalf("expected stopped, got %s", got)
	}
	if server.TestTorrentRunning(id) {
		t.Fatal("a stopped torrent must leave the engine")
	}
}
