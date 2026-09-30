package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// The stress test runs everything a busy install does at the same time on
// one database: a big library import, several downloads writing progress
// from many connections, the media server refresher, the connectivity
// monitor, the automatic search loops and a browser polling the pages. The
// pages must keep answering the whole time. If one takes too long the test
// prints every goroutine, which shows what is holding the database.

const (
	stressMovies  = 188
	stressShows   = 25
	stressStall   = 8 * time.Second // a page slower than this counts as a stall
	stressSegs    = 600             // articles per download
	stressWorkers = 24              // simulated download connections
)

func stressMovieName(n int) string { return fmt.Sprintf("Stress Movie %03d", n) }
func stressShowName(n int) string  { return fmt.Sprintf("Stress Show %02d", n) }

// newStressTMDB answers searches and details for "Stress Movie NNN" (id
// 1000+N) and "Stress Show NN" (id 2000+N, 2 seasons of 6 episodes).
func newStressTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	num := func(q, prefix string) (int, bool) {
		rest, ok := strings.CutPrefix(strings.ToLower(q), prefix)
		if !ok {
			return 0, false
		}
		n, err := strconv.Atoi(rest)
		return n, err == nil
	}
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		var results []map[string]any
		if n, ok := num(r.URL.Query().Get("query"), "stress movie "); ok {
			results = append(results, map[string]any{"id": 1000 + n, "title": stressMovieName(n), "release_date": "2000-05-01"})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
	mux.HandleFunc("/movie/", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/movie/"))
		json.NewEncoder(w).Encode(map[string]any{"id": id, "title": stressMovieName(id - 1000), "release_date": "2000-05-01", "overview": "x",
			"genres": []map[string]any{{"id": 18, "name": "Drama"}}})
	})
	mux.HandleFunc("/search/tv", func(w http.ResponseWriter, r *http.Request) {
		var results []map[string]any
		if n, ok := num(r.URL.Query().Get("query"), "stress show "); ok {
			results = append(results, map[string]any{"id": 2000 + n, "name": stressShowName(n), "first_air_date": "2010-01-01"})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
	mux.HandleFunc("/tv/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/tv/"), "/")
		id, _ := strconv.Atoi(parts[0])
		if len(parts) == 3 {
			season, _ := strconv.Atoi(parts[2])
			var eps []map[string]any
			for e := 1; e <= 6; e++ {
				eps = append(eps, map[string]any{"season_number": season, "episode_number": e, "name": fmt.Sprintf("Episode %d", e), "air_date": "2011-01-01"})
			}
			json.NewEncoder(w).Encode(map[string]any{"episodes": eps})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": id, "name": stressShowName(id - 2000), "first_air_date": "2010-01-01", "overview": "x",
			"genres":  []map[string]any{{"id": 18, "name": "Drama"}},
			"seasons": []map[string]any{{"season_number": 1}, {"season_number": 2}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newStressIndexer serves a Newznab feed with a few releases ("Grab Movie
// 001"...) and an NZB of stressSegs articles for each. Their titles differ
// from the imported library's so the downloads never land on an imported file.
func newStressIndexer(t *testing.T, releases int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
		for i := 0; i < releases; i++ {
			fmt.Fprintf(&b, `<item><title>Grab.Movie.%03d.2000.1080p.WEB-DL.x264-GRP</title><guid>g%d</guid><enclosure url="http://%s/nzb/%d.nzb" length="1000" type="application/x-nzb"/><newznab:attr name="size" value="1000"/><newznab:attr name="category" value="2000"/></item>`,
				i+1, i, r.Host, i)
		}
		b.WriteString(`</channel></rss>`)
		fmt.Fprint(w, b.String())
	})
	mux.HandleFunc("/nzb/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file subject="[1/1] &quot;fixture-movie.mkv&quot; yEnc (1/1)"><groups><group>alt.binaries.test</group></groups><segments>`)
		for i := 1; i <= stressSegs; i++ {
			fmt.Fprintf(&b, `<segment bytes="1000" number="%d">seg-%s-%d@example</segment>`, i, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/nzb/"), ".nzb"), i)
		}
		b.WriteString(`</segments></file></nzb>`)
		fmt.Fprint(w, b.String())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func stressSignIn(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	postJSON[map[string]any](t, client, baseURL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
}

func stressWriteFile(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stressWaitPhase waits for a library scan to be ready for review.
func stressWaitPhase(t *testing.T, client *http.Client, baseURL, jobID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		job := getJSON[map[string]any](t, client, baseURL+"/api/library/scan/"+jobID)
		switch job["phase"] {
		case "ready":
			return job
		case "failed":
			t.Fatalf("scan failed: %v", job["error"])
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for the scan to be ready")
	return nil
}

// stressWaitBatch waits until an import has finished filling in details.
func stressWaitBatch(t *testing.T, client *http.Client, baseURL string, id float64) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		b := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/library/import/batches/%.0f", baseURL, id))
		if b["running"] == false {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for import %v to finish", id)
}

// goroutineDump returns every goroutine's stack, for the failure message.
func goroutineDump() string {
	var buf bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&buf, 2)
	return buf.String()
}

func TestNoStallUnderLoad(t *testing.T) {
	content := []byte("Fixture movie bytes. " + strings.Repeat("padding-", 200))
	nntpSrv := newFakeNNTPServer(t, content)
	indexerSrv := newStressIndexer(t, 3)
	plex := newFakePlexServer(t, "plex-token")
	tmdb := newStressTMDB(t)

	server, httpSrv, client := newHuntTestServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", tmdb.URL)
	stressSignIn(t, client, httpSrv.URL)
	base := httpSrv.URL

	settings := getJSON[map[string]any](t, client, base+"/api/settings")
	moviesDir, _ := settings["moviesPath"].(string)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "k",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{
		"name": "Fixture Usenet", "host": nntpSrv.addr, "port": nntpSrv.port, "useSsl": false, "connections": 10,
	}, http.StatusCreated)
	ms := postJSON[map[string]any](t, client, base+"/api/media-servers", map[string]any{
		"kind": "plex", "baseUrl": plex.URL, "token": "plex-token",
		"pathMap": []map[string]string{{"from": moviesDir, "to": "/data/movies"}},
	}, http.StatusCreated)

	// A library to import: movies and shows already on disk.
	movieRoot := t.TempDir()
	for i := 1; i <= stressMovies; i++ {
		stressWriteFile(t, movieRoot, fmt.Sprintf("%s (2000)/%s (2000) 1080p BluRay.mkv", stressMovieName(i), stressMovieName(i)))
	}
	showRoot := t.TempDir()
	for n := 1; n <= stressShows; n++ {
		for season := 1; season <= 2; season++ {
			for ep := 1; ep <= 6; ep++ {
				stressWriteFile(t, showRoot, fmt.Sprintf("%s (2010)/Season %02d/%s.S%02dE%02d.720p.HDTV.x264-GRP.mkv", stressShowName(n), season, strings.ReplaceAll(stressShowName(n), " ", "."), season, ep))
			}
		}
	}

	// Movies to download, and a queue item for the bookkeeping hammer.
	var downloads []library.Movie
	for i := 0; i < 3; i++ {
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: 9000 + i, Title: fmt.Sprintf("Grab Movie %03d", i+1), Year: 2000, Monitored: true})
		if err != nil {
			t.Fatalf("seed movie: %v", err)
		}
		downloads = append(downloads, m)
	}
	idle, err := server.MovieRepo.Add(library.Movie{TMDBID: 9100, Title: "Idle Movie", Year: 2000, Monitored: false})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	hammerItem, err := server.QueueRepo.Enqueue(queue.Item{MovieID: idle.ID, ReleaseTitle: "hammer", NZBURL: "http://example.invalid/x.nzb", Status: queue.StatusDownloading})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var stopOnce sync.Once
	halt := func() { stopOnce.Do(func() { close(stop) }) }
	defer halt()
	running := func() bool {
		select {
		case <-stop:
			return false
		default:
			return true
		}
	}
	loop := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for running() {
				fn()
			}
		}()
	}

	// What the downloader's connections did before progress writes were
	// throttled: one database write per article, from many goroutines.
	for i := 0; i < stressWorkers; i++ {
		i := i
		loop(func() {
			_ = server.QueueRepo.SetProgress(hammerItem, float64(i%100))
		})
	}

	// Browser: pages polled the way the UI does.
	var (
		slowest   atomic.Int64
		requests  atomic.Int64
		inFlight  sync.Map // request number -> start time
		reqSerial atomic.Int64
		stalled   atomic.Bool
	)
	pages := []string{"/api/auth/me", "/api/queue", "/api/dashboard", "/api/activity", "/api/health", "/api/movies", "/api/series", "/api/wanted", "/api/onboarding/status"}
	probe := func(path string) {
		id := reqSerial.Add(1)
		inFlight.Store(id, time.Now())
		start := time.Now()
		resp, err := client.Get(base + path)
		took := time.Since(start)
		inFlight.Delete(id)
		requests.Add(1)
		if err != nil {
			if running() {
				t.Errorf("GET %s: %v", path, err)
			}
			return
		}
		resp.Body.Close()
		for {
			cur := slowest.Load()
			if int64(took) <= cur || slowest.CompareAndSwap(cur, int64(took)) {
				break
			}
		}
		if resp.StatusCode >= 500 {
			t.Errorf("GET %s answered %d", path, resp.StatusCode)
		}
	}
	for i := 0; i < 4; i++ {
		i := i
		n := 0
		loop(func() {
			probe(pages[(n+i)%len(pages)])
			n++
			time.Sleep(20 * time.Millisecond)
		})
	}

	// Watchdog: prints what everything is doing the moment a page has waited too long.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for running() {
			time.Sleep(500 * time.Millisecond)
			inFlight.Range(func(_, v any) bool {
				if age := time.Since(v.(time.Time)); age > stressStall && stalled.CompareAndSwap(false, true) {
					t.Errorf("a page has been waiting %v: the app is stalled. All goroutines:\n%s", age.Round(time.Millisecond), goroutineDump())
				}
				return true
			})
		}
	}()

	// Media server refresher, connectivity monitor, automatic search loops.
	msID := fmt.Sprint(ms["id"])
	loop(func() {
		postJSON[map[string]any](t, client, base+"/api/media-servers/"+msID+"/refresh", map[string]any{}, http.StatusOK)
		time.Sleep(50 * time.Millisecond)
	})
	loop(func() {
		postJSON[[]map[string]any](t, client, base+"/api/monitor/run", map[string]any{}, http.StatusOK)
		time.Sleep(50 * time.Millisecond)
	})
	loop(func() { server.TestHunt(context.Background()) })
	loop(func() { server.TestRSSSync(context.Background()) })

	// The hunt and RSS loops above find the three releases and start their downloads.

	// The import runs on this goroutine while everything else goes on.
	importStart := time.Now()
	for _, tc := range []struct {
		kind, root string
		titles     int
	}{{"movie", movieRoot, stressMovies}, {"tv", showRoot, stressShows}} {
		started := postJSON[map[string]any](t, client, base+"/api/library/scan", map[string]any{"path": tc.root, "kind": tc.kind}, http.StatusAccepted)
		job := stressWaitPhase(t, client, base, started["jobId"].(string))
		var selections []map[string]any
		for _, raw := range job["items"].([]any) {
			item := raw.(map[string]any)
			cands, _ := item["candidates"].([]any)
			if len(cands) == 0 {
				continue
			}
			selections = append(selections, map[string]any{"key": item["key"], "tmdbId": cands[0].(map[string]any)["tmdbId"]})
		}
		if len(selections) != tc.titles {
			t.Fatalf("%s: matched %d titles, want %d", tc.kind, len(selections), tc.titles)
		}
		confirmed := postJSON[map[string]any](t, client, base+"/api/library/import", map[string]any{"jobId": started["jobId"], "selections": selections}, http.StatusAccepted)
		if id, ok := confirmed["batchId"].(float64); ok {
			stressWaitBatch(t, client, base, id)
		}
	}
	t.Logf("import of %d movies and %d shows finished in %v", stressMovies, stressShows, time.Since(importStart).Round(time.Millisecond))

	// Wait for the downloads to finish.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		done := 0
		for _, q := range getJSON[[]map[string]any](t, client, base+"/api/queue") {
			if q["releaseTitle"] == "hammer" {
				continue
			}
			if st := q["status"]; st == "completed" || st == "failed" {
				done++
			}
		}
		if done >= 3 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	halt()
	wg.Wait()
	waitBackground(t, server)

	t.Logf("%d page requests, slowest %v", requests.Load(), time.Duration(slowest.Load()).Round(time.Millisecond))
	if d := time.Duration(slowest.Load()); d > stressStall {
		t.Errorf("slowest page took %v", d)
	}
	for _, q := range getJSON[[]map[string]any](t, client, base+"/api/queue") {
		if q["releaseTitle"] != "hammer" && q["status"] != "completed" {
			t.Errorf("download did not finish: %+v", q)
		}
	}
	if got := len(getJSON[[]map[string]any](t, client, base+"/api/series")); got != stressShows {
		t.Errorf("library has %d shows, want %d", got, stressShows)
	}
}
