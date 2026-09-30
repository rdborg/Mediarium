package api_test

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// The download line, end to end: real pipelines against a fake Usenet server
// that can be told to hold every answer, so a test decides when a download
// moves on.

// lineMovie adds another movie to the library of a control environment.
func lineMovie(t *testing.T, e *controlEnv, n int) library.Movie {
	t.Helper()
	m, err := e.server.MovieRepo.Add(library.Movie{TMDBID: 700 + n, Title: fmt.Sprintf("Line Movie %d", n), Year: 1999 + n, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (e *controlEnv) setAtOnce(t *testing.T, n int) {
	t.Helper()
	putJSONStatus(t, e.client, e.base+"/api/settings", map[string]any{"downloadsAtOnce": n}, http.StatusOK)
}

func (e *controlEnv) position(t *testing.T, id int64) int {
	t.Helper()
	it := e.item(t, id)
	if it == nil {
		return -1
	}
	p, _ := it["queuePosition"].(float64)
	return int(p)
}

func (e *controlEnv) countStatus(t *testing.T, ids []int64, statuses ...string) int {
	t.Helper()
	n := 0
	for _, id := range ids {
		st := e.status(t, id)
		for _, want := range statuses {
			if st == want {
				n++
				break
			}
		}
	}
	return n
}

// sampleActive watches the queue and the news-server connections while a test
// runs. It records the most downloads that were downloading or post-processing
// at the same moment, and the most connections one login had in use.
type activeSampler struct {
	stop    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	maxRun  int
	maxConn int
}

func sampleActive(t *testing.T, e *controlEnv) *activeSampler {
	s := &activeSampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		for {
			select {
			case <-s.stop:
				return
			default:
			}
			run := 0
			if items, err := e.server.QueueRepo.List(); err == nil {
				for _, it := range items {
					if it.Status == queue.StatusDownloading || it.Status == queue.StatusImporting {
						run++
					}
				}
			}
			conn := 0
			for _, st := range download.ServerStates() {
				if st.Host == fmt.Sprintf("127.0.0.1:%d", e.nntp.port) {
					conn = max(conn, st.InUse)
				}
			}
			s.mu.Lock()
			s.maxRun = max(s.maxRun, run)
			s.maxConn = max(s.maxConn, conn)
			s.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()
	t.Cleanup(func() { s.finish() })
	return s
}

func (s *activeSampler) finish() (maxRun, maxConn int) {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxRun, s.maxConn
}

func (e *controlEnv) waitAllDone(t *testing.T, ids []int64) {
	t.Helper()
	waitFor(t, "every download to finish", func() bool {
		for _, id := range ids {
			it, err := e.server.QueueRepo.Get(id)
			if err != nil || (it.Status != queue.StatusCompleted && it.Status != queue.StatusFailed) {
				return false
			}
		}
		return true
	})
}

func TestOneDownloadAtATimeFromGrabToImport(t *testing.T) {
	e := newControlEnv(t, 2) // the third article is held until the gate opens
	movies := []library.Movie{e.movie, lineMovie(t, e, 2), lineMovie(t, e, 3)}
	sampler := sampleActive(t, e)

	var ids []int64
	for _, m := range movies {
		id, err := e.server.TestGrabMovie(m, releaseName, e.nzbURL)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	waitFor(t, "the first download to reach the held article", func() bool { return e.nntp.totalHits() >= 3 })

	if got := e.status(t, ids[0]); got != "downloading" {
		t.Fatalf("the first download should be running, it is %s", got)
	}
	for i, want := range []int{1, 2} {
		id := ids[i+1]
		if got := e.status(t, id); got != "queued" {
			t.Fatalf("download %d should be waiting in line, it is %s", i+2, got)
		}
		if got := e.position(t, id); got != want {
			t.Fatalf("download %d should be number %d in line, it is %d", i+2, want, got)
		}
	}
	// Nothing the waiting ones would fetch has been asked for yet.
	if e.server.TestRunningDownloads() != 1 {
		t.Fatalf("%d downloads hold a place, want 1", e.server.TestRunningDownloads())
	}

	e.nntp.open()
	e.waitAllDone(t, ids)
	maxRun, maxConn := sampler.finish()
	if maxRun < 1 {
		t.Fatal("the check never saw a download running, so it proves nothing")
	}
	if maxRun > 1 {
		t.Fatalf("%d downloads were downloading or post-processing at the same moment, want 1", maxRun)
	}
	if maxConn > 1 {
		t.Fatalf("%d connections were open to a provider that allows 1", maxConn)
	}
	for i, m := range movies {
		it, _ := e.server.QueueRepo.Get(ids[i])
		if it.Status != queue.StatusCompleted {
			t.Fatalf("download %d ended %s: %s", i+1, it.Status, it.Error)
		}
		got, _ := e.server.MovieRepo.Get(m.ID)
		if got.Status != library.StatusDownloaded || got.FilePath == "" {
			t.Fatalf("movie %d should be in the library, it is %+v", i+1, got)
		}
	}
	// They ran one after the other, in the order they were added.
	var finished []int64
	items, _ := e.server.QueueRepo.List()
	sort.Slice(items, func(i, j int) bool { return items[i].CompletedAt < items[j].CompletedAt })
	for _, it := range items {
		finished = append(finished, it.ID)
	}
	for i := range ids {
		if finished[i] != ids[i] {
			t.Fatalf("finished in order %v, want %v", finished, ids)
		}
	}
	// A download gives its place back a moment after it ends, so wait for it.
	waitFor(t, "every place to be free after everything finished", func() bool { return e.server.TestRunningDownloads() == 0 })
}

func TestSeveralAtOnceShareTheProvidersConnections(t *testing.T) {
	e := newControlEnv(t, 2) // the provider allows one connection
	e.setAtOnce(t, 3)
	sampler := sampleActive(t, e)
	var ids []int64
	movies := []library.Movie{e.movie}
	for n := 2; n <= 5; n++ {
		movies = append(movies, lineMovie(t, e, n))
	}
	for _, m := range movies {
		id, err := e.server.TestGrabMovie(m, releaseName, e.nzbURL)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	waitFor(t, "three downloads to be running", func() bool { return e.countStatus(t, ids, "downloading", "importing") == 3 })
	if got := e.countStatus(t, ids, "queued"); got != 2 {
		t.Fatalf("%d waiting in line, want 2", got)
	}
	if e.position(t, ids[3]) != 1 || e.position(t, ids[4]) != 2 {
		t.Fatalf("the fourth and fifth should be 1st and 2nd in line, got %d and %d", e.position(t, ids[3]), e.position(t, ids[4]))
	}

	e.nntp.open()
	e.waitAllDone(t, ids)
	maxRun, maxConn := sampler.finish()
	if maxRun > 3 || maxRun < 2 {
		t.Fatalf("%d ran at the same moment, want at most 3", maxRun)
	}
	if maxConn > 1 {
		t.Fatalf("%d connections were open to a provider that allows 1", maxConn)
	}
	for i, id := range ids {
		if it, _ := e.server.QueueRepo.Get(id); it.Status != queue.StatusCompleted {
			t.Fatalf("download %d ended %s: %s", i+1, it.Status, it.Error)
		}
	}
}

func TestAPersonsDownloadGoesBeforeAutomaticOnes(t *testing.T) {
	e := newControlEnv(t, 2)
	m2, m3, m4 := lineMovie(t, e, 2), lineMovie(t, e, 3), lineMovie(t, e, 4)
	first := e.grab(t)
	waitFor(t, "the first download to be running", func() bool { return e.nntp.totalHits() >= 3 })

	auto1, err := e.server.TestGrabMovieAs(m2, releaseName, e.nzbURL, true)
	if err != nil {
		t.Fatal(err)
	}
	auto2, err := e.server.TestGrabMovieAs(m3, releaseName, e.nzbURL, true)
	if err != nil {
		t.Fatal(err)
	}
	searched, err := e.server.TestGrabMovieAs(m4, releaseName, e.nzbURL, false) // a person pressed Search now
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]int{searched: 1, auto1: 2, auto2: 3} {
		if got := e.position(t, id); got != want {
			t.Fatalf("download %d should be number %d in line, it is %d", id, want, got)
		}
	}
	// The list Activity shows keeps the running one on top, then the line in order.
	list := getJSON[[]map[string]any](t, e.client, e.base+"/api/queue")
	var order []int64
	for _, it := range list {
		order = append(order, int64(it["id"].(float64)))
	}
	if want := []int64{first, searched, auto1, auto2}; fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("Activity lists %v, want %v", order, want)
	}

	e.nntp.open()
	e.waitAllDone(t, []int64{first, searched, auto1, auto2})
	items, _ := e.server.QueueRepo.List()
	sort.Slice(items, func(i, j int) bool { return items[i].CompletedAt < items[j].CompletedAt })
	var finished []int64
	for _, it := range items {
		finished = append(finished, it.ID)
	}
	if want := []int64{first, searched, auto1, auto2}; fmt.Sprint(finished) != fmt.Sprint(want) {
		t.Fatalf("finished in order %v, want %v", finished, want)
	}
}

func TestAFailedDownloadJustMakesWayForTheNext(t *testing.T) {
	e := newControlEnv(t, -1)
	bad := lineMovie(t, e, 2)
	badID, err := e.server.TestGrabMovie(bad, "Line.Movie.2.2001.1080p.WEB-DL.x264-GRP", "http://127.0.0.1:1/never.nzb")
	if err != nil {
		t.Fatal(err)
	}
	goodID := e.grab(t)
	e.waitAllDone(t, []int64{badID, goodID})
	if it, _ := e.server.QueueRepo.Get(badID); it.Status != queue.StatusFailed || it.Error == "" {
		t.Fatalf("the bad one should have failed with a reason, got %+v", it)
	}
	if it, _ := e.server.QueueRepo.Get(goodID); it.Status != queue.StatusCompleted {
		t.Fatalf("the next one should have finished, got %s: %s", it.Status, it.Error)
	}
	// The failed one is left alone: it is not tried again by the line.
	time.Sleep(100 * time.Millisecond)
	e.server.TestKickDownloads()
	if it, _ := e.server.QueueRepo.Get(badID); it.Status != queue.StatusFailed {
		t.Fatalf("a failed download must stay failed, got %s", it.Status)
	}
}

func TestPausingFreesThePlaceAndResumingGoesBackInLine(t *testing.T) {
	e := newControlEnv(t, 2)
	m2 := lineMovie(t, e, 2)
	first := e.downloadHeld(t)
	second, err := e.server.TestGrabMovie(m2, releaseName, e.nzbURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.status(t, second); got != "queued" {
		t.Fatalf("the second should wait, it is %s", got)
	}

	postJSON[map[string]any](t, e.client, e.url(first, "pause"), nil, http.StatusOK)
	waitFor(t, "the second download to take the free place", func() bool { return e.status(t, second) == "downloading" })
	if got := e.status(t, first); got != "paused" {
		t.Fatalf("the first should be paused, it is %s", got)
	}

	// Resuming puts it back in line: the second keeps its place.
	postJSON[map[string]any](t, e.client, e.url(first, "resume"), nil, http.StatusOK)
	if got := e.status(t, first); got != "queued" {
		t.Fatalf("a resumed download waits for a free place, it is %s", got)
	}
	if e.position(t, first) != 1 {
		t.Fatalf("it should be next in line, it is %d", e.position(t, first))
	}
	if e.status(t, second) != "downloading" {
		t.Fatal("resuming must not push the running download out")
	}

	e.nntp.open()
	e.waitAllDone(t, []int64{first, second})
	for _, id := range []int64{first, second} {
		if it, _ := e.server.QueueRepo.Get(id); it.Status != queue.StatusCompleted {
			t.Fatalf("download %d ended %s: %s", id, it.Status, it.Error)
		}
	}
}

func TestLoweringTheLimitNeverStopsARunningDownload(t *testing.T) {
	e := newControlEnv(t, 2)
	e.setAtOnce(t, 2)
	m2, m3 := lineMovie(t, e, 2), lineMovie(t, e, 3)
	a := e.grab(t)
	b, _ := e.server.TestGrabMovie(m2, releaseName, e.nzbURL)
	c, _ := e.server.TestGrabMovie(m3, releaseName, e.nzbURL)
	waitFor(t, "two downloads to run", func() bool { return e.countStatus(t, []int64{a, b}, "downloading") == 2 })
	if e.status(t, c) != "queued" {
		t.Fatalf("the third should wait, it is %s", e.status(t, c))
	}

	e.setAtOnce(t, 1)
	if e.countStatus(t, []int64{a, b}, "downloading") != 2 {
		t.Fatal("lowering the limit must not touch the downloads already running")
	}
	postJSON[map[string]any](t, e.client, e.url(a, "stop"), nil, http.StatusOK)
	time.Sleep(150 * time.Millisecond)
	if e.status(t, c) != "queued" {
		t.Fatalf("one is still running and the limit is one, so the third must wait, it is %s", e.status(t, c))
	}
	postJSON[map[string]any](t, e.client, e.url(b, "stop"), nil, http.StatusOK)
	waitFor(t, "the third download to start", func() bool { return e.status(t, c) == "downloading" })

	// Raising it fills the places again.
	d, _ := e.server.TestGrabMovie(lineMovie(t, e, 4), releaseName, e.nzbURL)
	if e.status(t, d) != "queued" {
		t.Fatalf("the fourth should wait, it is %s", e.status(t, d))
	}
	e.setAtOnce(t, 2)
	waitFor(t, "the fourth download to start", func() bool { return e.status(t, d) == "downloading" })
	e.nntp.open()
	e.waitAllDone(t, []int64{c, d})
}

func TestStoppingAWaitingDownloadTakesItOutOfTheLine(t *testing.T) {
	e := newControlEnv(t, 2)
	m2, m3 := lineMovie(t, e, 2), lineMovie(t, e, 3)
	e.downloadHeld(t)
	b, _ := e.server.TestGrabMovie(m2, releaseName, e.nzbURL)
	c, _ := e.server.TestGrabMovie(m3, releaseName, e.nzbURL)
	postJSON[map[string]any](t, e.client, e.url(b, "stop"), nil, http.StatusOK)
	if e.status(t, b) != "stopped" {
		t.Fatalf("the stopped one is %s", e.status(t, b))
	}
	if e.position(t, c) != 1 {
		t.Fatalf("the last one moves up to next in line, it is %d", e.position(t, c))
	}
	// A waiting download is not "running": it cannot be removed while it waits.
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/queue/%d", e.base, c), nil)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("removing a waiting download answered %d, want 409", resp.StatusCode)
	}
	e.nntp.open()
}

func TestPauseAllAndResumeAllKeepTheOrder(t *testing.T) {
	e := newControlEnv(t, 2)
	m2, m3 := lineMovie(t, e, 2), lineMovie(t, e, 3)
	a := e.downloadHeld(t)
	b, _ := e.server.TestGrabMovie(m2, releaseName, e.nzbURL)
	c, _ := e.server.TestGrabMovie(m3, releaseName, e.nzbURL)

	resp := postJSON[map[string]any](t, e.client, e.base+"/api/queue/pause-all", nil, http.StatusOK)
	if resp["paused"].(float64) != 3 {
		t.Fatalf("pause all paused %v, want 3", resp["paused"])
	}
	for _, id := range []int64{a, b, c} {
		if got := e.status(t, id); got != "paused" {
			t.Fatalf("download %d should be paused, it is %s", id, got)
		}
	}
	waitFor(t, "every place to be free after pause all", func() bool { return e.server.TestRunningDownloads() == 0 })

	postJSON[map[string]any](t, e.client, e.base+"/api/queue/resume-all", nil, http.StatusOK)
	waitFor(t, "the oldest to start again", func() bool { return e.status(t, a) == "downloading" })
	if e.status(t, b) != "queued" || e.status(t, c) != "queued" {
		t.Fatalf("the others wait in line: %s, %s", e.status(t, b), e.status(t, c))
	}
	if e.position(t, b) != 1 || e.position(t, c) != 2 {
		t.Fatalf("resumed in their old order, got %d and %d", e.position(t, b), e.position(t, c))
	}
	e.nntp.open()
	e.waitAllDone(t, []int64{a, b, c})
}

func TestTheLimitIsASettingFromOneToFive(t *testing.T) {
	server, base, client := newControlServer(t, false)
	_ = server
	if got := getJSON[map[string]any](t, client, base+"/api/settings")["downloadsAtOnce"]; got != float64(1) {
		t.Fatalf("the default is %v, want 1", got)
	}
	for _, bad := range []int{0, -1, 6, 100} {
		putJSONStatus(t, client, base+"/api/settings", map[string]any{"downloadsAtOnce": bad}, http.StatusBadRequest)
	}
	if got := getJSON[map[string]any](t, client, base+"/api/settings")["downloadsAtOnce"]; got != float64(1) {
		t.Fatalf("a refused value must not be saved, it is %v", got)
	}
	for _, good := range []int{3, 5, 1} {
		putJSONStatus(t, client, base+"/api/settings", map[string]any{"downloadsAtOnce": good}, http.StatusOK)
		if got := getJSON[map[string]any](t, client, base+"/api/settings")["downloadsAtOnce"]; got != float64(good) {
			t.Fatalf("saved %d, it reads back as %v", good, got)
		}
	}
}

// After a restart the waiting downloads are still waiting and carry on when
// the line is next looked at. What was running comes back paused.
func TestWaitingDownloadsCarryOnAfterARestart(t *testing.T) {
	server, _, _ := newControlServer(t, false)
	var movies []library.Movie
	for n := 1; n <= 3; n++ {
		m, _ := server.MovieRepo.Add(library.Movie{TMDBID: 800 + n, Title: fmt.Sprintf("Restart Movie %d", n), Year: 2000 + n, Monitored: true})
		movies = append(movies, m)
	}
	running, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: movies[0].ID, ReleaseTitle: "Restart.Movie.1.2001.1080p.WEB-DL.x264-GRP", NZBURL: "http://127.0.0.1:1/a.nzb"})
	_ = server.QueueRepo.SetStatus(running, queue.StatusDownloading, "")
	w1, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: movies[1].ID, ReleaseTitle: "Restart.Movie.2.2002.1080p.WEB-DL.x264-GRP", NZBURL: "http://127.0.0.1:1/b.nzb"})
	w2, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: movies[2].ID, ReleaseTitle: "Restart.Movie.3.2003.1080p.WEB-DL.x264-GRP", NZBURL: "http://127.0.0.1:1/c.nzb"})

	reopened, err := server.TestReopen()
	if err != nil {
		t.Fatal(err)
	}
	if it, _ := reopened.QueueRepo.Get(running); it.Status != queue.StatusPaused || !it.Interrupted {
		t.Fatalf("what was running comes back paused, got %+v", it)
	}
	for _, id := range []int64{w1, w2} {
		if it, _ := reopened.QueueRepo.Get(id); it.Status != queue.StatusQueued {
			t.Fatalf("what was waiting stays waiting, got %s", it.Status)
		}
	}
	if reopened.TestRunningDownloads() != 0 {
		t.Fatal("nothing starts until the line is looked at")
	}

	reopened.TestKickDownloads() // what the scheduled check does a minute after start-up
	waitFor(t, "the waiting downloads to have run one after the other", func() bool {
		a, _ := reopened.QueueRepo.Get(w1)
		b, _ := reopened.QueueRepo.Get(w2)
		return a.Status == queue.StatusFailed && b.Status == queue.StatusFailed // their servers do not exist
	})
	reopened.TestWaitBackground()
	if it, _ := reopened.QueueRepo.Get(running); it.Status != queue.StatusPaused {
		t.Fatalf("the paused one must stay paused, got %s", it.Status)
	}
}

// Safe mode starts nothing by itself: a download a person asks for still runs,
// what the automatic searches added waits.
func TestSafeModeOnlyStartsWhatAPersonAskedFor(t *testing.T) {
	server, _, _ := newControlServer(t, true)
	m1, _ := server.MovieRepo.Add(library.Movie{TMDBID: 901, Title: "Safe One", Year: 2001, Monitored: true})
	m2, _ := server.MovieRepo.Add(library.Movie{TMDBID: 902, Title: "Safe Two", Year: 2002, Monitored: true})
	auto, err := server.TestGrabMovieAs(m1, "Safe.One.2001.1080p.WEB-DL.x264-GRP", "http://127.0.0.1:1/a.nzb", true)
	if err != nil {
		t.Fatal(err)
	}
	person, err := server.TestGrabMovie(m2, "Safe.Two.2002.1080p.WEB-DL.x264-GRP", "http://127.0.0.1:1/b.nzb")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the download a person asked for to run", func() bool {
		it, _ := server.QueueRepo.Get(person)
		return it.Status == queue.StatusFailed
	})
	server.TestWaitBackground()
	server.TestKickDownloads()
	if it, _ := server.QueueRepo.Get(auto); it.Status != queue.StatusQueued {
		t.Fatalf("in safe mode an automatic download keeps waiting, got %s", it.Status)
	}
}
