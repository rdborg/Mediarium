package api_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/store"
)

// newImportConflictTestServer sets up a full server + fake indexer/NNTP
// pair (reusing pipeline_integration_test.go's fixtures) with the "always
// ask" import conflict policy already configured, and a pre-existing file
// sitting exactly where the grab's destination will resolve to — so the
// pipeline is guaranteed to hit a real naming collision, not a contrived
// one.
func newImportConflictTestServer(t *testing.T, movieContent []byte) (client *http.Client, httpSrv *httptest.Server, movie library.Movie, existingPath string) {
	t.Helper()
	nntpSrv := newFakeNNTPServer(t, movieContent)
	indexerSrv := newFakeIndexerServer(t)

	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
	}
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	db, err := store.Open(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatalf("new api server: %v", err)
	}
	t.Cleanup(func() { waitBackground(t, server) })

	httpSrv = httptest.NewServer(server.Routes())
	t.Cleanup(httpSrv.Close)

	jar, _ := cookiejar.New(nil)
	client = &http.Client{Jar: jar}

	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{
		"moviesPath":           cfg.MoviesDir,
		"namingPreset":         "plex",
		"importConflictPolicy": "ask",
	}, http.StatusOK)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers", map[string]any{
		"name": "Fixture Usenet", "host": nntpSrv.addr, "port": nntpSrv.port, "useSsl": false, "connections": 2,
	}, http.StatusCreated)

	movie, err = server.MovieRepo.Add(library.Movie{TMDBID: 608, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	// buildDestPath always uses the "plex" preset for the folder and the
	// configured preset (also "plex" here) for the filename — see
	// internal/api/pipeline.go's buildDestPath.
	existingPath = filepath.Join(cfg.MoviesDir, "The Fixture Movie (1999)", "The Fixture Movie (1999).mkv")
	if err := os.MkdirAll(filepath.Dir(existingPath), 0o755); err != nil {
		t.Fatalf("mkdir existing file's dir: %v", err)
	}
	if err := os.WriteFile(existingPath, []byte("pre-existing file content"), 0o644); err != nil {
		t.Fatalf("write pre-existing file: %v", err)
	}

	return client, httpSrv, movie, existingPath
}

// conflictWorkDir is the working folder of a parked download (the test
// server keeps downloads next to the movies folder), checked to exist while
// the download waits for a decision.
func conflictWorkDir(t *testing.T, existingPath string, queueID int64, mustExist bool) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(existingPath))), "downloads", "incomplete", fmt.Sprintf("queue-%d", queueID))
	if _, err := os.Stat(dir); mustExist && err != nil {
		t.Fatalf("a parked download keeps its working folder: %v", err)
	}
	return dir
}

func grabFixtureAndWaitForConflict(t *testing.T, client *http.Client, httpSrv *httptest.Server, movie library.Movie, movieContent []byte) map[string]any {
	t.Helper()
	searchResp := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/search?q=fixture")
	if len(searchResp) != 1 {
		t.Fatalf("expected 1 search result, got %d: %+v", len(searchResp), searchResp)
	}
	result := searchResp[0]

	grabResp := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", httpSrv.URL, movie.ID), map[string]any{
		"releaseTitle": result["title"],
		"downloadUrl":  result["downloadUrl"],
		"sizeBytes":    int64(len(movieContent)),
	}, http.StatusAccepted)
	if grabResp["queueId"] == nil {
		t.Fatalf("expected queueId in grab response, got %+v", grabResp)
	}

	deadline := time.Now().Add(20 * time.Second)
	var item map[string]any
	for time.Now().Before(deadline) {
		queueList := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(queueList) == 1 {
			if status, _ := queueList[0]["status"].(string); status == "conflict" || status == "failed" || status == "completed" {
				item = queueList[0]
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if item == nil {
		t.Fatal("timed out waiting for the queue item to reach conflict")
	}
	if item["status"] != "conflict" {
		t.Fatalf("expected the grab to park in status=conflict (destination collision), got %+v", item)
	}
	return item
}

// TestImportConflictAskParksForManualReview proves the "always ask" policy
// doesn't silently skip a naming collision like the old
// hardcoded ConflictSkip behavior did — it parks the queue item with
// enough state (destPath) for a person to act on, and leaves the existing
// file completely untouched in the meantime.
func TestImportConflictAskParksForManualReview(t *testing.T) {
	movieContent := []byte("new fixture movie content, different from what's already there")
	client, httpSrv, movie, existingPath := newImportConflictTestServer(t, movieContent)

	item := grabFixtureAndWaitForConflict(t, client, httpSrv, movie, movieContent)

	destPath, _ := item["destPath"].(string)
	if destPath == "" {
		t.Fatal("expected destPath to be populated on a conflict item")
	}

	got, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(got) != "pre-existing file content" {
		t.Fatalf("expected the existing file to be untouched while parked, got %q", got)
	}
}

// TestResolveConflictOverwrite proves choosing "overwrite" on a parked
// conflict actually replaces the file with the new download and completes
// the queue item/movie status, not just that the HTTP call returns 200.
func TestResolveConflictOverwrite(t *testing.T) {
	movieContent := []byte("new fixture movie content, different from what's already there")
	client, httpSrv, movie, existingPath := newImportConflictTestServer(t, movieContent)

	item := grabFixtureAndWaitForConflict(t, client, httpSrv, movie, movieContent)
	queueID := int64(item["id"].(float64))
	workDir := conflictWorkDir(t, existingPath, queueID, true)

	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/resolve-conflict", httpSrv.URL, queueID), map[string]any{
		"overwrite": true,
	}, http.StatusOK)
	if _, err := os.Stat(workDir); err == nil {
		t.Fatal("the download's working folder should be removed once the conflict is resolved")
	}

	deadline := time.Now().Add(5 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		queueList := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(queueList) == 1 {
			status, _ = queueList[0]["status"].(string)
			if status != "conflict" {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "completed" {
		t.Fatalf("expected the resolved queue item to complete, got status=%q", status)
	}

	got, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read file after overwrite: %v", err)
	}
	if string(got) != string(movieContent) {
		t.Fatalf("expected the file to be overwritten with the new download, got %q", got)
	}

	finalMovie := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", httpSrv.URL, movie.ID))
	if finalMovie["status"] != "downloaded" {
		t.Fatalf("expected movie status downloaded after resolving, got %+v", finalMovie)
	}
}

// TestResolveConflictSkip proves choosing "skip" leaves the existing file
// completely alone and fails the queue item rather than half-applying
// something.
func TestResolveConflictSkip(t *testing.T) {
	movieContent := []byte("new fixture movie content, different from what's already there")
	client, httpSrv, movie, existingPath := newImportConflictTestServer(t, movieContent)

	item := grabFixtureAndWaitForConflict(t, client, httpSrv, movie, movieContent)
	queueID := int64(item["id"].(float64))
	workDir := conflictWorkDir(t, existingPath, queueID, true)

	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/resolve-conflict", httpSrv.URL, queueID), map[string]any{
		"overwrite": false,
	}, http.StatusOK)
	if _, err := os.Stat(workDir); err == nil {
		t.Fatal("a skipped download's working folder should be removed")
	}

	deadline := time.Now().Add(5 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		queueList := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(queueList) == 1 {
			status, _ = queueList[0]["status"].(string)
			if status != "conflict" {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "failed" {
		t.Fatalf("expected the skipped queue item to end up failed, got status=%q", status)
	}

	got, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(got) != "pre-existing file content" {
		t.Fatalf("expected the existing file to remain untouched after skip, got %q", got)
	}

	finalMovie := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", httpSrv.URL, movie.ID))
	if finalMovie["status"] != "missing" {
		t.Fatalf("expected movie status back to missing after skip, got %+v", finalMovie)
	}
}
