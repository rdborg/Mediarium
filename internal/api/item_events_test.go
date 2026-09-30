package api_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

func signUpAdmin(t *testing.T, client *http.Client, base string) {
	t.Helper()
	postJSON[map[string]any](t, client, base+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
}

func movieEvents(t *testing.T, client *http.Client, base string, id int64) []map[string]any {
	t.Helper()
	return getJSON[[]map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/events", base, id))
}

func findEvent(events []map[string]any, kind, contains string) map[string]any {
	for _, e := range events {
		if e["kind"] == kind && strings.Contains(e["message"].(string), contains) {
			return e
		}
	}
	return nil
}

// A search that finds nothing acceptable says why, on the movie's own log
// and not in the global feed; the immediate retry after a bad release says
// when it will try again.
func TestMovieEventsExplainSearches(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	base := httpSrv.URL
	signUpAdmin(t, client, base)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Idx", "definitionId": "fixture", "apiKey": "k", "baseUrl": newTVIndexerWith(t, []string{
			"Some.Movie.2001.1080p.TeleSync.x264-GRP",
			"Some.Movie.2001.HDCAM.x264-GRP",
			"Some.Movie.1999.1080p.WEB-DL.x264-GRP",
		}).URL,
	}, http.StatusCreated)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Some Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}

	server.TestHunt(context.Background())
	events := movieEvents(t, client, base, m.ID)
	e := findEvent(events, "searched", "3 releases, none acceptable: 2 CAM/TeleSync, 1 other years")
	if e == nil || e["level"] != "warn" || e["at"] == "" {
		t.Fatalf("want a searched event explaining why nothing was taken, got %+v", events)
	}
	// The next search with the same outcome does not add another event.
	server.TestHunt(context.Background())
	if again := movieEvents(t, client, base, m.ID); len(again) != len(events) {
		t.Fatalf("a repeated outcome should not add events: %+v", again)
	}
	for _, a := range getJSON[[]map[string]any](t, client, base+"/api/activity") {
		if a["eventType"] == "searched" {
			t.Fatalf("searches belong on the movie's own log, not the global feed: %+v", a)
		}
	}

	server.TestRetryMovie(m.ID)
	if e := findEvent(movieEvents(t, client, base, m.ID), "retry", "No other acceptable release; will try again at the next scheduled search"); e == nil || e["level"] != "warn" {
		t.Fatalf("the retry should say it found nothing else, got %+v", movieEvents(t, client, base, m.ID))
	}

	if code, _ := doStatus(t, client, http.MethodGet, fmt.Sprintf("%s/api/movies/%d/events", base, 99999)); code != http.StatusNotFound {
		t.Fatalf("unknown movie: %d", code)
	}
	if code, _ := doStatus(t, client, http.MethodGet, base+"/api/series/99999/events"); code != http.StatusNotFound {
		t.Fatalf("unknown series: %d", code)
	}
	sr, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 9, Title: "Show"}, []library.Episode{{Season: 1, Episode: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if got := getJSON[[]map[string]any](t, client, fmt.Sprintf("%s/api/series/%d/events", base, sr.ID)); len(got) != 0 {
		t.Fatalf("a new show has no events: %+v", got)
	}
}

// A grab by hand is refused with 409 while the movie or an episode it
// covers is still downloading; a season pack counts for all its episodes.
func TestManualGrabRefusedWhileDownloading(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	base := httpSrv.URL
	signUpAdmin(t, client, base)

	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Some Movie", Year: 2001})
	if err != nil {
		t.Fatal(err)
	}
	busy, err := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Some.Movie.2001.1080p.WEB-DL.x264-GRP", NZBURL: "http://127.0.0.1:1/a.nzb"})
	if err != nil {
		t.Fatal(err)
	}
	grab := map[string]any{"releaseTitle": "Some.Movie.2001.1080p.BluRay.x264-GRP", "downloadUrl": "http://127.0.0.1:1/b.nzb"}
	body := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", base, m.ID), grab, http.StatusConflict)
	if body["error"] != "Already downloading: Some.Movie.2001.1080p.WEB-DL.x264-GRP. Cancel it first to pick another." {
		t.Fatalf("409 body: %+v", body)
	}

	sr, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 9, Title: "Show"},
		[]library.Episode{{Season: 1, Episode: 1}, {Season: 1, Episode: 2}, {Season: 2, Episode: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.QueueRepo.Enqueue(queue.Item{SeriesID: sr.ID, Season: 1, ReleaseTitle: "Show.S01.1080p.WEB-DL.x264-GRP", NZBURL: "http://127.0.0.1:1/p.nzb"}); err != nil {
		t.Fatal(err)
	}
	tvGrab := func(title string, want int) {
		t.Helper()
		postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/series/%d/grab", base, sr.ID),
			map[string]any{"releaseTitle": title, "downloadUrl": "http://127.0.0.1:1/e.nzb"}, want)
	}
	tvGrab("Show.S01E02.1080p.WEB-DL.x264-GRP", http.StatusConflict) // covered by the running pack
	tvGrab("Show.S01.720p.WEB-DL.x264-GRP", http.StatusConflict)     // another pack of the same season

	// Once the running download has failed, a new grab goes ahead.
	if err := server.QueueRepo.SetStatus(busy, queue.StatusFailed, "download: no"); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", base, m.ID), grab, http.StatusAccepted)
}

// Retrying a failed Usenet download whose files are all still there repeats
// post-processing on them instead of downloading again; without them it
// downloads again.
func TestRetryReusesDownloadedFiles(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	base := httpSrv.URL
	signUpAdmin(t, client, base)

	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Some Movie", Year: 2001})
	if err != nil {
		t.Fatal(err)
	}
	release := "Some.Movie.2001.1080p.WEB-DL.x264-GRP"
	// The NZB address does not answer: only a retry that reuses the files can succeed.
	id, err := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: release, NZBURL: "http://127.0.0.1:1/never.nzb", Protocol: queue.ProtocolUsenet})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.QueueRepo.SetStatus(id, queue.StatusFailed, "import into library: disk full"); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(server.TestIncompleteDir(), fmt.Sprintf("queue-%d", id))
	writeBody(t, filepath.Join(work, release+".mkv"), "the movie")
	writeBody(t, filepath.Join(work, ".mediarium-downloaded"), "0")

	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/retry", base, id), nil, http.StatusAccepted)
	waitBackground(t, server)

	got, err := server.MovieRepo.Get(m.ID)
	if err != nil || got.Status != library.StatusDownloaded || got.FilePath == "" {
		t.Fatalf("the retry should import the kept files: %+v %v (events %+v)", got, err, movieEvents(t, client, base, m.ID))
	}
	if b, err := os.ReadFile(got.FilePath); err != nil || string(b) != "the movie" {
		t.Fatalf("imported file: %q %v", b, err)
	}
	events := movieEvents(t, client, base, m.ID)
	if findEvent(events, "retried", "Retrying from the files already downloaded") == nil ||
		findEvent(events, "retried", "Reusing the files already downloaded") == nil {
		t.Fatalf("the retry should say it reused the files: %+v", events)
	}

	// No kept files: the retry downloads again (and fails here, as the
	// address does not answer).
	m2, _ := server.MovieRepo.Add(library.Movie{TMDBID: 2, Title: "Other Movie", Year: 2002})
	id2, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: m2.ID, ReleaseTitle: "Other.Movie.2002.1080p.WEB-DL.x264-GRP", NZBURL: "http://127.0.0.1:1/never.nzb"})
	_ = server.QueueRepo.SetStatus(id2, queue.StatusFailed, "download: connection refused")
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/retry", base, id2), nil, http.StatusAccepted)
	waitBackground(t, server)
	events = movieEvents(t, client, base, m2.ID)
	if findEvent(events, "retried", "Retrying, downloading again") == nil || findEvent(events, "download", "Download started") == nil {
		t.Fatalf("a retry without kept files downloads again: %+v", events)
	}
	if got, _ := server.MovieRepo.Get(m2.ID); got.Status == library.StatusDownloaded {
		t.Fatalf("nothing could be downloaded: %+v", got)
	}
}
