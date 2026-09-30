package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// fakeTMDB is a local stand-in for the movie database with 1000+ movies
// named "Movie 001", "Movie 002"... (id 1001, 1002...). It counts requests,
// can hold detail requests until released, can make them fail, and records
// how many were in flight at once.
type fakeTMDB struct {
	srv *httptest.Server

	searchCalls atomic.Int64
	detailCalls atomic.Int64
	arrived     atomic.Int64 // detail requests that reached the server

	mu          sync.Mutex
	gate        chan struct{} // detail requests wait for this to close, when set
	failStatus  int           // detail requests answer this status, when set
	inFlight    int
	maxInFlight int
	delay       time.Duration
}

func newFakeTMDB(t *testing.T) *fakeTMDB {
	t.Helper()
	f := &fakeTMDB{}
	mux := http.NewServeMux()
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		f.searchCalls.Add(1)
		q := r.URL.Query().Get("query")
		var results []map[string]any
		if n, ok := movieNumber(q); ok {
			results = append(results, map[string]any{"id": 1000 + n, "title": movieName(n), "release_date": "2000-05-01", "poster_path": fmt.Sprintf("/s%d.jpg", n)})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
	mux.HandleFunc("/movie/", func(w http.ResponseWriter, r *http.Request) {
		f.arrived.Add(1)
		f.mu.Lock()
		f.inFlight++
		if f.inFlight > f.maxInFlight {
			f.maxInFlight = f.inFlight
		}
		gate, fail, delay := f.gate, f.failStatus, f.delay
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			f.inFlight--
			f.mu.Unlock()
		}()
		if gate != nil {
			<-gate
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		f.detailCalls.Add(1)
		if fail != 0 {
			w.WriteHeader(fail)
			return
		}
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/movie/"))
		n := id - 1000
		json.NewEncoder(w).Encode(map[string]any{
			"id": id, "title": movieName(n), "release_date": "2000-05-01", "overview": "About " + movieName(n),
			"poster_path": fmt.Sprintf("/p%d.jpg", n), "genres": []map[string]any{{"id": 18, "name": "Drama"}},
		})
	})
	// Shows: "Show 01"... (id 2001...), each with 3 seasons of 8 episodes.
	mux.HandleFunc("/search/tv", func(w http.ResponseWriter, r *http.Request) {
		f.searchCalls.Add(1)
		var results []map[string]any
		if rest, ok := strings.CutPrefix(strings.ToLower(r.URL.Query().Get("query")), "show "); ok {
			if n, err := strconv.Atoi(rest); err == nil {
				results = append(results, map[string]any{"id": 2000 + n, "name": showName(n), "first_air_date": "2010-01-01", "poster_path": fmt.Sprintf("/t%d.jpg", n)})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
	mux.HandleFunc("/tv/", func(w http.ResponseWriter, r *http.Request) {
		f.arrived.Add(1)
		f.mu.Lock()
		gate, fail, delay := f.gate, f.failStatus, f.delay
		f.mu.Unlock()
		if gate != nil {
			<-gate
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		f.detailCalls.Add(1)
		if fail != 0 {
			w.WriteHeader(fail)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/tv/"), "/")
		id, _ := strconv.Atoi(parts[0])
		if len(parts) == 3 { // /tv/{id}/season/{n}
			season, _ := strconv.Atoi(parts[2])
			var eps []map[string]any
			for e := 1; e <= 8; e++ {
				eps = append(eps, map[string]any{"season_number": season, "episode_number": e, "name": fmt.Sprintf("Episode %d", e), "air_date": "2011-01-01"})
			}
			json.NewEncoder(w).Encode(map[string]any{"episodes": eps})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": id, "name": showName(id - 2000), "first_air_date": "2010-01-01", "overview": "About " + showName(id-2000),
			"poster_path": fmt.Sprintf("/q%d.jpg", id-2000), "genres": []map[string]any{{"id": 18, "name": "Drama"}},
			"seasons": []map[string]any{{"season_number": 0}, {"season_number": 1}, {"season_number": 2}, {"season_number": 3}},
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func showName(n int) string { return fmt.Sprintf("Show %02d", n) }

func movieName(n int) string { return fmt.Sprintf("Movie %03d", n) }

func movieNumber(q string) (int, bool) {
	rest, ok := strings.CutPrefix(strings.ToLower(q), "movie ")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil
}

// hold makes detail requests wait until the returned function is called
// (it is also called when the test ends, so no request is left hanging).
func (f *fakeTMDB) hold(t *testing.T) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	f.mu.Lock()
	f.gate = gate
	f.mu.Unlock()
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return release
}

func (f *fakeTMDB) failWith(status int) {
	f.mu.Lock()
	f.failStatus = status
	f.mu.Unlock()
}

func (f *fakeTMDB) maxConcurrent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxInFlight
}

// makeMovieFolder writes n movie files, "Movie 001 (2000)/..." and so on,
// with file n dated n days after 2020-01-01, and returns the root.
func makeMovieFolder(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()
	base := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= n; i++ {
		path := writeFile(t, root, fmt.Sprintf("%s (2000)/%s (2000) 1080p BluRay.mkv", movieName(i), movieName(i)))
		if err := os.Chtimes(path, base, base.AddDate(0, 0, i)); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func signIn(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	postJSON[map[string]any](t, client, baseURL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
}

func TestImportConfirmReturnsAtOnceAndFillsDetailsInTheBackground(t *testing.T) {
	const n = 60
	server, httpSrv, client := newHuntTestServer(t)
	fake := newFakeTMDB(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", fake.srv.URL)
	signIn(t, client, httpSrv.URL)
	root := makeMovieFolder(t, n)

	started := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "movie"}, http.StatusAccepted)
	jobID := started["jobId"].(string)
	job := waitForImportPhase(t, client, httpSrv.URL, jobID, "ready")
	items := job["items"].([]any)
	if len(items) != n {
		t.Fatalf("expected %d rows to review, got %d", n, len(items))
	}
	var selections []map[string]any
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["match"] != "matched" {
			t.Fatalf("expected a confident match: %+v", item)
		}
		selections = append(selections, map[string]any{"key": item["key"], "tmdbId": item["candidates"].([]any)[0].(map[string]any)["tmdbId"]})
	}

	// While the details are held back, the titles must already be in the library.
	release := fake.hold(t)
	begin := time.Now()
	confirmed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", map[string]any{"jobId": jobID, "selections": selections}, http.StatusAccepted)
	if took := time.Since(begin); took > 2*time.Second {
		t.Fatalf("confirming %d titles took %v, want about a second", n, took)
	}
	if got := fake.detailCalls.Load(); got != 0 {
		t.Fatalf("confirming made %d detail lookups, want none", got)
	}
	movies := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies")
	if len(movies) != n {
		t.Fatalf("expected %d movies straight away, got %d", n, len(movies))
	}
	for _, m := range movies {
		if m["status"] != "downloaded" || m["detailsState"] != "pending" || m["noUpgrade"] != true || m["monitored"] != false || m["quality"] != "Bluray-1080p" || m["posterUrl"] == nil {
			t.Fatalf("movie should be registered with what the scan knows: %+v", m)
		}
	}
	// "Recently added" follows the files' dates: the newest file comes first.
	if movies[0]["title"] != movieName(n) || movies[n-1]["title"] != movieName(1) {
		t.Fatalf("movies should be ordered by file date, got first %v last %v", movies[0]["title"], movies[n-1]["title"])
	}

	// The banner and the import page see the job while it runs.
	active := getJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import/active")
	batches := active["batches"].([]any)
	if len(batches) != 1 || batches[0].(map[string]any)["running"] != true || batches[0].(map[string]any)["total"] != float64(n) {
		t.Fatalf("expected one running import of %d titles, got %+v", n, active)
	}

	// A second import of the same kind is refused, another kind is not.
	second := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "movie"}, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(second["error"]), "already running") {
		t.Fatalf("expected a plain refusal, got %+v", second)
	}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": t.TempDir(), "kind": "tv"}, http.StatusAccepted)

	release()
	batch := waitForImportBatch(t, client, httpSrv.URL, confirmed["batchId"].(float64))
	if batch["added"] != float64(n) || batch["problems"] != float64(0) || batch["done"] != float64(n) {
		t.Fatalf("unexpected totals: %+v", batch)
	}
	if batch["monitor"] != false || batch["noUpgrade"] != true || batch["monitorMissing"] != false {
		t.Fatalf("the report should say the titles were added safe: %+v", batch)
	}
	if got := fake.maxConcurrent(); got < 2 || got > 5 {
		t.Fatalf("details were looked up %d at a time, want several but at most 5", got)
	}
	movies = getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies")
	for _, m := range movies {
		if m["detailsState"] != nil || m["overview"] == nil || !strings.HasPrefix(fmt.Sprint(m["overview"]), "About Movie") || fmt.Sprint(m["genres"]) != "[Drama]" {
			t.Fatalf("details were not filled in: %+v", m)
		}
	}

	// The finished import stays on the banner until it is dismissed.
	active = getJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import/active")
	finished := active["batches"].([]any)[0].(map[string]any)
	if finished["running"] != false {
		t.Fatalf("expected the finished import to show: %+v", active)
	}
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/library/import/batches/%.0f/dismiss", httpSrv.URL, finished["id"].(float64)), nil, http.StatusNoContent)
	active = getJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import/active")
	if len(active["batches"].([]any)) != 0 {
		t.Fatalf("a dismissed import should leave the banner: %+v", active)
	}
}

func TestImportDetailsRetriedNotLost(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantState  string
		wantInNote string
		afterTries int
	}{
		{"server error keeps trying and becomes a problem after three tries", http.StatusInternalServerError, "problem", "isn't answering", 3},
		{"slow down is a temporary problem", http.StatusTooManyRequests, "problem", "slow down", 3},
		{"unknown title is a problem at once", http.StatusNotFound, "problem", "doesn't have this title", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, httpSrv, client := newHuntTestServer(t)
			fake := newFakeTMDB(t)
			server.TestSetTMDBBaseURL("fixture-tmdb-key", fake.srv.URL)
			server.TestSetImportWorker(2, func(int, bool) time.Duration { return 0 })
			signIn(t, client, httpSrv.URL)
			root := makeMovieFolder(t, 1)

			fake.failWith(tc.status)
			started := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "movie"}, http.StatusAccepted)
			job := waitForImportPhase(t, client, httpSrv.URL, started["jobId"].(string), "ready")
			item := job["items"].([]any)[0].(map[string]any)
			confirmed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", map[string]any{
				"jobId": started["jobId"], "selections": []map[string]any{{"key": item["key"], "tmdbId": 1001}},
			}, http.StatusAccepted)
			batchID := confirmed["batchId"].(float64)

			// Each pass through the worker makes one more try.
			for try := 1; try <= tc.afterTries; try++ {
				server.TestWaitBackground()
				if try < tc.afterTries {
					server.TestKickImportWorker()
				}
			}
			server.TestWaitBackground()
			batch := waitForImportBatch(t, client, httpSrv.URL, batchID)
			got := batch["items"].([]any)[0].(map[string]any)
			if got["state"] != tc.wantState || !strings.Contains(fmt.Sprint(got["note"]), tc.wantInNote) || batch["problems"] != float64(1) {
				t.Fatalf("unexpected item %+v in batch %+v", got, batch)
			}
			movies := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies")
			if len(movies) != 1 || movies[0]["detailsState"] != "problem" || movies[0]["detailsNote"] == nil {
				t.Fatalf("the card should show the problem: %+v", movies)
			}

			// The movie database comes back: the title is picked up again.
			fake.failWith(0)
			server.TestKickImportWorker()
			server.TestWaitBackground()
			batch = getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/library/import/batches/%.0f", httpSrv.URL, batchID))
			got = batch["items"].([]any)[0].(map[string]any)
			if got["state"] != "done" || batch["problems"] != float64(0) {
				t.Fatalf("expected the title to be filled in later: %+v", batch)
			}
			movies = getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies")
			if movies[0]["detailsState"] != nil || !strings.HasPrefix(fmt.Sprint(movies[0]["overview"]), "About") {
				t.Fatalf("details should be there now: %+v", movies[0])
			}
		})
	}
}

func TestImportContinuesAfterRestart(t *testing.T) {
	first, _, _ := newHuntTestServer(t)
	fake := newFakeTMDB(t)

	// The titles are confirmed and registered, then the app stops before
	// the worker has looked anything up.
	var entries []library.ImportEntry
	for i := 1; i <= 12; i++ {
		entries = append(entries, library.ImportEntry{
			Kind: "movie", TMDBID: 1000 + i, Title: movieName(i), Year: 2000, Quality: "Bluray-1080p",
			FilePath: fmt.Sprintf("/movies/%s.mkv", movieName(i)),
		})
	}
	batchID, outcomes, err := first.MovieRepo.RegisterImport(library.ImportOptions{Kind: "movie", NoUpgrade: true}, entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 12 || outcomes[0].Outcome != library.ImportAdded {
		t.Fatalf("unexpected outcomes: %+v", outcomes)
	}
	if b, _ := first.MovieRepo.ImportBatchByID(batchID); !b.Running() || b.Pending != 12 {
		t.Fatalf("expected an unfinished import: %+v", b)
	}

	// A new server on the same database picks the unfinished work up.
	restarted, err := first.TestReopen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restarted.TestWaitBackground() })
	restarted.TestSetTMDBBaseURL("fixture-tmdb-key", fake.srv.URL)
	restarted.TestKickImportWorker()
	restarted.TestWaitBackground()

	batch, err := restarted.MovieRepo.ImportBatchByID(batchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Running() || batch.Added != 12 || batch.Pending != 0 || batch.Problems != 0 {
		t.Fatalf("expected the restarted app to finish the import: %+v", batch)
	}
	movies, err := restarted.MovieRepo.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range movies {
		if m.DetailsState != "" || m.Overview == "" || m.PosterPath == "" {
			t.Fatalf("details missing after the restart: %+v", m)
		}
	}
}

func TestImportOfTitlesAlreadyInTheLibraryLooksNothingUp(t *testing.T) {
	// Titles already in the library are not looked up again.
	server, httpSrv, client := newHuntTestServer(t)
	fake := newFakeTMDB(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", fake.srv.URL)
	signIn(t, client, httpSrv.URL)
	root := makeMovieFolder(t, 3)

	run := func() map[string]any {
		started := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "movie"}, http.StatusAccepted)
		job := waitForImportPhase(t, client, httpSrv.URL, started["jobId"].(string), "ready")
		var selections []map[string]any
		for _, raw := range job["items"].([]any) {
			item := raw.(map[string]any)
			selections = append(selections, map[string]any{"key": item["key"], "tmdbId": item["candidates"].([]any)[0].(map[string]any)["tmdbId"]})
		}
		confirmed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", map[string]any{"jobId": started["jobId"], "selections": selections}, http.StatusAccepted)
		return waitForImportBatch(t, client, httpSrv.URL, confirmed["batchId"].(float64))
	}
	first := run()
	if first["added"] != float64(3) || first["already"] != float64(0) {
		t.Fatalf("unexpected first totals: %+v", first)
	}
	callsAfterFirst := fake.detailCalls.Load()
	second := run()
	if second["added"] != float64(0) || second["already"] != float64(3) || second["problems"] != float64(0) {
		t.Fatalf("expected everything to be in the library already: %+v", second)
	}
	if got := fake.detailCalls.Load(); got != callsAfterFirst {
		t.Fatalf("titles already in the library were looked up again (%d more requests)", got-callsAfterFirst)
	}
	if len(getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies")) != 3 {
		t.Fatal("importing twice must not add titles twice")
	}
}

// TestImportSpeed measures the whole flow against a local stand-in for the
// movie database that takes 25 ms to answer each request (real answers take
// longer, which the parallel lookups hide the same way): 188 movies and 25
// shows of 3 seasons of 8 episodes. Confirming must be quick whatever the
// answers cost, and the details must arrive several titles at a time.
func TestImportSpeed(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	fake := newFakeTMDB(t)
	fake.delay = 25 * time.Millisecond
	server.TestSetTMDBBaseURL("fixture-tmdb-key", fake.srv.URL)
	signIn(t, client, httpSrv.URL)

	movieRoot := makeMovieFolder(t, 188)
	showRoot := t.TempDir()
	for n := 1; n <= 25; n++ {
		for season := 1; season <= 3; season++ {
			for ep := 1; ep <= 8; ep++ {
				writeFile(t, showRoot, fmt.Sprintf("%s (2010)/Season %02d/%s.S%02dE%02d.720p.HDTV.x264-GRP.mkv", showName(n), season, strings.ReplaceAll(showName(n), " ", "."), season, ep))
			}
		}
	}

	for _, tc := range []struct {
		kind, root string
		titles     int
		tmdbBase   int
	}{{"movie", movieRoot, 188, 1000}, {"tv", showRoot, 25, 2000}} {
		begin := time.Now()
		started := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": tc.root, "kind": tc.kind}, http.StatusAccepted)
		job := waitForImportPhase(t, client, httpSrv.URL, started["jobId"].(string), "ready")
		scanned := time.Since(begin)
		var selections []map[string]any
		for _, raw := range job["items"].([]any) {
			item := raw.(map[string]any)
			selections = append(selections, map[string]any{"key": item["key"], "tmdbId": item["candidates"].([]any)[0].(map[string]any)["tmdbId"]})
		}
		if len(selections) != tc.titles {
			t.Fatalf("%s: found %d titles, want %d", tc.kind, len(selections), tc.titles)
		}

		begin = time.Now()
		confirmed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", map[string]any{"jobId": started["jobId"], "selections": selections}, http.StatusAccepted)
		confirm := time.Since(begin)
		batch := waitForImportBatch(t, client, httpSrv.URL, confirmed["batchId"].(float64))
		total := time.Since(begin)
		t.Logf("%d %s: scan and match %v, confirm %v, details finished %v after confirming", tc.titles, tc.kind, scanned.Round(time.Millisecond), confirm.Round(time.Millisecond), total.Round(time.Millisecond))
		if confirm > 2*time.Second {
			t.Errorf("%s: confirming took %v", tc.kind, confirm)
		}
		if batch["added"] != float64(tc.titles) || batch["problems"] != float64(0) {
			t.Fatalf("%s: unexpected totals %+v", tc.kind, batch)
		}
	}
	series := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/series")
	if len(series) != 25 || series[0]["episodeCount"] != float64(24) || series[0]["downloadedCount"] != float64(24) {
		t.Fatalf("shows should have all 24 episodes marked as downloaded: %+v", series[0])
	}
}
