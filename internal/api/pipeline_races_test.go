package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// queuedItemFor puts a download for movieID in the queue and marks the movie
// as taken, as grabbing a release does.
func queuedItemFor(t *testing.T, repo *queue.Repo, movieID int64, downloadURL string) queue.Item {
	t.Helper()
	id, err := repo.Enqueue(queue.Item{MovieID: movieID, ReleaseTitle: releaseName, NZBURL: downloadURL})
	if err != nil {
		t.Fatal(err)
	}
	item, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func movieStatusOf(t *testing.T, repo *library.Repo, id int64) library.Status {
	t.Helper()
	m, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return m.Status
}

// A pipeline runs in its own goroutine, where an unrecovered panic would end
// the whole app. It must fail that download, hand the movie back, and give
// the place in the line up.
func TestAPipelineThatPanicsFailsTheDownloadAndFreesItsPlace(t *testing.T) {
	server, _, _ := newControlServer(t, false)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	item := queuedItemFor(t, server.QueueRepo, m.ID, "http://127.0.0.1:1/x.nzb")
	if err := server.QueueRepo.SetStatus(item.ID, queue.StatusDownloading, ""); err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloading, "", ""); err != nil {
		t.Fatal(err)
	}

	var freed atomic.Int32
	server.TestLaunch(item, func() { freed.Add(1) }, func() { panic("something unexpected deep in the pipeline") })
	waitBackground(t, server)

	if freed.Load() != 1 {
		t.Fatalf("the place in the line was given back %d times, want once", freed.Load())
	}
	got, err := server.QueueRepo.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusFailed {
		t.Errorf("the download is %s, want failed", got.Status)
	}
	if got.Error == "" || got.Error == "something unexpected deep in the pipeline" {
		t.Errorf("the person is told %q; want a plain message, not the crash text", got.Error)
	}
	if st := movieStatusOf(t, server.MovieRepo, m.ID); st != library.StatusMissing {
		t.Errorf("the movie is %s, want missing", st)
	}
}

// Stopping a download in the moment between the line handing it over and its
// movie being marked as downloading must not leave the movie marked.
func TestStopJustBeforeStartLeavesTheMovieFree(t *testing.T) {
	server, _, _ := newControlServer(t, false)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	item := queuedItemFor(t, server.QueueRepo, m.ID, "http://127.0.0.1:1/x.nzb")
	// The line claimed it; a person stopped it before it started.
	if err := server.QueueRepo.SetStatus(item.ID, queue.StatusStopped, ""); err != nil {
		t.Fatal(err)
	}

	var freed atomic.Int32
	if err := server.TestStartQueued(item, func() { freed.Add(1) }); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitBackground(t, server)

	if freed.Load() != 1 {
		t.Errorf("the place in the line was given back %d times, want once", freed.Load())
	}
	if st := movieStatusOf(t, server.MovieRepo, m.ID); st != library.StatusMissing {
		t.Errorf("the movie is %s, want missing", st)
	}
	if got, _ := server.QueueRepo.Get(item.ID); got.Status != queue.StatusStopped {
		t.Errorf("the download is %s, want stopped", got.Status)
	}
}

// A download whose queue entry was removed between the line handing it over
// and its pipeline starting must not run: it would fetch and import a release
// for something that was taken out of the library.
func TestARemovedDownloadDoesNotRun(t *testing.T) {
	server, _, _ := newControlServer(t, false)
	var fetched atomic.Int32
	nzb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(nzb.Close)

	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	item := queuedItemFor(t, server.QueueRepo, m.ID, nzb.URL+"/x.nzb")
	run, err := server.TestPreparePipeline(item)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.QueueRepo.Delete(item.ID); err != nil { // removed just after it was prepared
		t.Fatal(err)
	}
	run()

	if n := fetched.Load(); n != 0 {
		t.Fatalf("the release of a removed download was fetched %d times", n)
	}
}

// Two clicks on "download this release" at once must give one download.
func TestTwoManualGrabsAtOnceQueueOnlyOne(t *testing.T) {
	e := newControlEnv(t, 2)

	const clicks = 12
	var (
		wg      sync.WaitGroup
		ok      atomic.Int32
		start   = make(chan struct{})
		release = releaseName
	)
	for i := 0; i < clicks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := e.server.TestGrabMovie(e.movie, release, e.nzbURL); err == nil {
				ok.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if ok.Load() != 1 {
		t.Errorf("%d of %d simultaneous grabs of one movie went through, want 1", ok.Load(), clicks)
	}
	items, err := e.server.QueueRepo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("%d downloads in the queue, want 1", len(items))
	}
	e.nntp.open()
}

// Removing a movie while its download is importing: the import finishes
// before the download can be cancelled, and the file it placed in the library
// goes with the movie when files are deleted.
func TestRemovingAMovieDuringItsImportDeletesTheImportedFile(t *testing.T) {
	server, base, client := loginNewServer(t)
	movies := server.TestMoviesRoot()
	id := seedMovie(t, server.MovieRepo, 41, "Racing", 2020, "")
	if err := server.MovieRepo.SetStatus(id, library.StatusDownloading, "", ""); err != nil {
		t.Fatal(err)
	}
	qid, err := server.QueueRepo.Enqueue(queue.Item{MovieID: id, ReleaseTitle: "Racing.2020.1080p"})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.QueueRepo.SetStatus(qid, queue.StatusImporting, "")

	ctx, end := server.TestBeginPipeline(qid)
	imported := filepath.Join(movies, "Racing (2020)", "Racing (2020).mkv")
	go func() {
		defer end()
		<-ctx.Done() // the removal cancels the pipeline, which is mid-import and finishes it
		putFileQuiet(imported, "the movie")
		_ = server.MovieRepo.SetStatus(id, library.StatusDownloaded, "WEBDL-1080p", imported)
	}()

	if code := deleteReq(t, client, base+"/api/movies/"+strconv.FormatInt(id, 10)+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if exists(imported) || exists(filepath.Dir(imported)) {
		t.Fatalf("the imported file was left in the library: %s", tree(t, movies))
	}
}

// The same for a show: episodes imported while it is being removed go too.
func TestRemovingAShowDuringItsImportDeletesTheImportedFiles(t *testing.T) {
	env := newTVAutoEnv(t, nil, []episodeSpec{{1, 1, "2020-01-01", library.StatusMissing, ""}})
	tvRoot := filepath.Join(t.TempDir(), "tv")
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"tvPath": tvRoot}, http.StatusOK)
	server := env.server

	qid, err := server.QueueRepo.Enqueue(queue.Item{SeriesID: env.seriesID, Season: 1, Episode: 1, ReleaseTitle: "Fixture.Show.S01E01"})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.QueueRepo.SetStatus(qid, queue.StatusImporting, "")

	ctx, end := server.TestBeginPipeline(qid)
	show := filepath.Join(tvRoot, "Fixture Show (2020)")
	file := filepath.Join(show, "Season 01", "Fixture Show (2020) - S01E01 - Pilot.mkv")
	go func() {
		defer end()
		<-ctx.Done()
		putFileQuiet(file, "the episode")
		eps, _ := server.MovieRepo.ListEpisodes(env.seriesID)
		_ = server.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "WEBDL-1080p", file)
	}()

	url := env.baseURL + "/api/series/" + strconv.FormatInt(env.seriesID, 10)
	if code := deleteReq(t, env.client, url+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if exists(show) {
		t.Fatalf("the imported episode was left in the library: %s", tree(t, tvRoot))
	}
}

// putFileQuiet is putFile for use off the test goroutine.
func putFileQuiet(path, content string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(content), 0o644)
}
