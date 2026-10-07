package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newImportTMDBServer serves movie search/detail fixtures: "Inception" has
// one clear match, "Heat" has two exact-title entries that only the year
// (1995, from the folder name) can tell apart, and anything else finds
// nothing.
func newImportTMDBServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		var results []map[string]any
		switch strings.ToLower(r.URL.Query().Get("query")) {
		case "inception":
			results = []map[string]any{
				{"id": 27205, "title": "Inception", "release_date": "2010-07-16", "poster_path": "/i.jpg"},
				{"id": 5, "title": "Inception: The Cobol Job", "release_date": "2010-12-07"},
			}
		case "heat":
			results = []map[string]any{
				{"id": 111, "title": "Heat", "release_date": "1986-01-01"},
				{"id": 949, "title": "Heat", "release_date": "1995-12-15"},
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
	detail := func(id int, title, date string) {
		mux.HandleFunc(fmt.Sprintf("/movie/%d", id), func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"id": id, "title": title, "release_date": date, "overview": "x"})
		})
	}
	detail(27205, "Inception", "2010-07-16")
	detail(949, "Heat", "1995-12-15")
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func waitForImportPhase(t *testing.T, client *http.Client, baseURL, jobID, phase string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		job := getJSON[map[string]any](t, client, baseURL+"/api/library/scan/"+jobID)
		switch job["phase"] {
		case phase:
			return job
		case "failed":
			t.Fatalf("import job failed: %v", job["error"])
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for import phase %q", phase)
	return nil
}

// waitForImportBatch waits until an import has finished filling in details
// and returns its record.
func waitForImportBatch(t *testing.T, client *http.Client, baseURL string, batchID float64) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		b := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/library/import/batches/%.0f", baseURL, batchID))
		if b["running"] == false {
			return b
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for import %v to finish", batchID)
	return nil
}

func itemByTitle(t *testing.T, job map[string]any, title string) map[string]any {
	t.Helper()
	for _, raw := range job["items"].([]any) {
		item := raw.(map[string]any)
		if item["title"] == title {
			return item
		}
	}
	t.Fatalf("no item titled %q in %+v", title, job["items"])
	return nil
}

func writeFile(t *testing.T, root, rel string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportExistingMovieLibrary(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newImportTMDBServer(t).URL)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	root := t.TempDir()
	inception := writeFile(t, root, "Inception (2010)/Inception (2010) 1080p BluRay.mkv")
	heat := writeFile(t, root, "Heat (1995)/movie.mkv")
	writeFile(t, root, "Zzz Nonexistent (2020)/whatever.mkv")

	// Bad requests are rejected up front.
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": filepath.Join(root, "nope"), "kind": "movie"}, http.StatusBadRequest)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "music"}, http.StatusBadRequest)

	started := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "movie"}, http.StatusAccepted)
	jobID := started["jobId"].(string)
	job := waitForImportPhase(t, client, httpSrv.URL, jobID, "ready")

	inc := itemByTitle(t, job, "Inception")
	if inc["match"] != "matched" || inc["quality"] != "Bluray-1080p" || inc["candidates"].([]any)[0].(map[string]any)["tmdbId"] != float64(27205) {
		t.Fatalf("Inception review row wrong: %+v", inc)
	}
	ht := itemByTitle(t, job, "Heat")
	if ht["match"] != "matched" || ht["candidates"].([]any)[0].(map[string]any)["tmdbId"] != float64(949) {
		t.Fatalf("Heat should match the 1995 entry by year, got %+v", ht)
	}
	if itemByTitle(t, job, "Zzz Nonexistent")["match"] != "unmatched" {
		t.Fatalf("expected the unknown title to be unmatched")
	}

	// Import a selection that isn't in the scan: rejected, nothing registered.
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", map[string]any{
		"jobId": jobID, "selections": []map[string]any{{"key": "made-up", "tmdbId": 1}},
	}, http.StatusBadRequest)

	confirmed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", map[string]any{
		"jobId": jobID, "selections": []map[string]any{
			{"key": inc["key"], "tmdbId": 27205},
			{"key": ht["key"], "tmdbId": 949},
		},
	}, http.StatusAccepted)
	batch := waitForImportBatch(t, client, httpSrv.URL, confirmed["batchId"].(float64))
	if batch["added"] != float64(2) || batch["problems"] != float64(0) || batch["already"] != float64(0) {
		t.Fatalf("unexpected import totals: %+v", batch)
	}
	for _, raw := range batch["items"].([]any) {
		it := raw.(map[string]any)
		if it["outcome"] != "added" || it["state"] != "done" {
			t.Fatalf("unexpected import item %+v", it)
		}
	}

	// Registered in place: same paths, downloaded, nothing moved.
	movies := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies")
	if len(movies) != 2 {
		t.Fatalf("expected 2 movies, got %+v", movies)
	}
	byTitle := map[string]map[string]any{}
	for _, m := range movies {
		byTitle[m["title"].(string)] = m
	}
	if m := byTitle["Inception"]; m["status"] != "downloaded" || m["filePath"] != inception || m["quality"] != "Bluray-1080p" || m["overview"] != "x" || m["noUpgrade"] != true || m["monitored"] != false || m["detailsState"] != nil {
		t.Fatalf("Inception not registered in place: %+v", m)
	}
	if m := byTitle["Heat"]; m["status"] != "downloaded" || m["filePath"] != heat {
		t.Fatalf("Heat not registered in place: %+v", m)
	}
	if _, err := os.Stat(inception); err != nil {
		t.Fatalf("import must not move or delete the original file: %v", err)
	}

	// A rescan now flags both as already in the library.
	second := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "movie"}, http.StatusAccepted)
	rescan := waitForImportPhase(t, client, httpSrv.URL, second["jobId"].(string), "ready")
	if itemByTitle(t, rescan, "Inception")["inLibrary"] != true {
		t.Fatalf("expected the rescan to flag Inception as already in the library")
	}

	// Heat's name told us nothing about its quality, so automation must not
	// go replacing it: an indexer offering a Bluray release changes nothing.
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Idx", "definitionId": "fixture", "baseUrl": newTVIndexerWith(t, []string{"Heat.1995.1080p.BluRay.x264-GRP"}).URL, "apiKey": "k",
	}, http.StatusCreated)
	server.TestHunt(context.Background())
	time.Sleep(300 * time.Millisecond)
	if q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue"); len(q) != 0 {
		t.Fatalf("automation grabbed a replacement for an imported movie of unknown quality: %+v", q)
	}
}

func TestImportExistingTVLibrary(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newTVTMDBServer(t, nil).URL)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	root := t.TempDir()
	e1 := writeFile(t, root, "Fixture Show (2011)/Season 01/Fixture.Show.S01E01.720p.HDTV.x264-GRP.mkv")
	writeFile(t, root, "Fixture Show (2011)/Season 01/Fixture.Show.S01E02.720p.HDTV.x264-GRP.mkv")
	writeFile(t, root, "Fixture Show (2011)/Season 02/Fixture.Show.S02E01.mkv")
	writeFile(t, root, "Fixture Show (2011)/Season 02/Fixture.Show.S02E09.mkv") // TMDB has no S02E09

	started := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "tv"}, http.StatusAccepted)
	jobID := started["jobId"].(string)
	job := waitForImportPhase(t, client, httpSrv.URL, jobID, "ready")
	item := itemByTitle(t, job, "Fixture Show")
	if item["match"] != "matched" || item["fileCount"] != float64(4) {
		t.Fatalf("unexpected TV review row: %+v", item)
	}

	confirmed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", map[string]any{
		"jobId": jobID, "selections": []map[string]any{{"key": item["key"], "tmdbId": 1399}},
	}, http.StatusAccepted)
	batch := waitForImportBatch(t, client, httpSrv.URL, confirmed["batchId"].(float64))
	res := batch["items"].([]any)[0].(map[string]any)
	if res["imported"] != float64(3) || res["state"] != "done" || !strings.Contains(fmt.Sprint(res["note"]), "1 episode") {
		t.Fatalf("unexpected result: %+v", res)
	}

	series := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/series")
	if len(series) != 1 || series[0]["downloadedCount"] != float64(3) || series[0]["episodeCount"] != float64(3) {
		t.Fatalf("expected the series registered with 3/3 episodes downloaded, got %+v", series)
	}
	detail := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/series/%v", httpSrv.URL, series[0]["id"]))
	for _, raw := range detail["episodes"].([]any) {
		ep := raw.(map[string]any)
		if ep["status"] != "downloaded" {
			t.Fatalf("episode not downloaded: %+v", ep)
		}
		if ep["season"] == float64(1) && ep["episode"] == float64(1) && ep["filePath"] != e1 {
			t.Fatalf("S01E01 should point at the original file, got %v", ep["filePath"])
		}
	}
}

// A title matched by hand is recognised by its file on the next scan, even
// though its name still matches nothing (GitHub #24).
func TestImportRescanKnowsHandMatchedTitles(t *testing.T) {
	srv, httpSrv, client := newHuntTestServer(t)
	srv.TestSetTMDBBaseURL("fixture-tmdb-key", newImportTMDBServer(t).URL)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	root := t.TempDir()
	writeFile(t, root, "Qqq Odd Name/qqq.mkv")
	first := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "movie"}, http.StatusAccepted)
	job := waitForImportPhase(t, client, httpSrv.URL, first["jobId"].(string), "ready")
	odd := itemByTitle(t, job, "qqq")
	if odd["match"] != "unmatched" {
		t.Fatalf("expected the odd name to be unmatched first, got %+v", odd)
	}
	confirmed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", map[string]any{
		"jobId": first["jobId"], "selections": []map[string]any{{"key": odd["key"], "tmdbId": 27205}},
	}, http.StatusAccepted)
	waitForImportBatch(t, client, httpSrv.URL, confirmed["batchId"].(float64))

	second := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "movie"}, http.StatusAccepted)
	rescan := waitForImportPhase(t, client, httpSrv.URL, second["jobId"].(string), "ready")
	again := itemByTitle(t, rescan, "qqq")
	cands := again["candidates"].([]any)
	if again["inLibrary"] != true || again["match"] != "matched" || len(cands) == 0 || cands[0].(map[string]any)["tmdbId"] != float64(27205) {
		t.Fatalf("the hand-matched title should come back as already in the library under Inception, got %+v", again)
	}
}
