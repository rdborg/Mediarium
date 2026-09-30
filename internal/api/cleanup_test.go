package api_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/queue"
)

func putFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeOld(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func enqueueWithStatus(t *testing.T, repo *queue.Repo, title string, status queue.Status) int64 {
	t.Helper()
	id, err := repo.Enqueue(queue.Item{ReleaseTitle: title})
	if err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusQueued {
		if err := repo.SetStatus(id, status, ""); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func queueDir(work string, id int64) string {
	return filepath.Join(work, "queue-"+strconv.FormatInt(id, 10))
}

// TestCleanupFindsAndRemovesOnlyWhatIsSafe sets up every kind of entry the
// downloads working folder can hold and checks what the scan offers, what a
// run removes, and that the library is never touched, including through a
// symbolic link that points into it.
func TestCleanupFindsAndRemovesOnlyWhatIsSafe(t *testing.T) {
	server, base, client := loginNewServer(t)
	work, movies := server.TestWorkDir(), server.TestMoviesRoot()
	precious := filepath.Join(movies, "Keep (2020)", "Keep (2020).mkv")
	putFile(t, precious, "precious")

	done := enqueueWithStatus(t, server.QueueRepo, "Done", queue.StatusCompleted)
	linked := enqueueWithStatus(t, server.QueueRepo, "Linked", queue.StatusCompleted)
	failed := enqueueWithStatus(t, server.QueueRepo, "Failed", queue.StatusFailed)
	queued := enqueueWithStatus(t, server.QueueRepo, "Queued", queue.StatusQueued)

	putFile(t, filepath.Join(queueDir(work, done), "a.mkv"), "12345")
	putFile(t, filepath.Join(queueDir(work, failed), "b.mkv"), "failed")
	putFile(t, filepath.Join(queueDir(work, queued), "c.part"), "running")
	putFile(t, filepath.Join(work, "queue-999", "old.mkv"), "orphan")
	makeOld(t, filepath.Join(work, "queue-999"))
	putFile(t, filepath.Join(work, "stray.nfo"), "stray")
	makeOld(t, filepath.Join(work, "stray.nfo"))
	putFile(t, filepath.Join(work, "fresh", "x.nfo"), "new")
	if err := os.MkdirAll(filepath.Join(work, "empty", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinked := true
	if err := os.Symlink(filepath.Dir(precious), queueDir(work, linked)); err != nil {
		symlinked = false
	}

	status := getJSON[map[string]any](t, client, base+"/api/system/cleanup")
	if status["auto"] != true || status["historyRetentionDays"] != float64(90) {
		t.Fatalf("defaults: %+v", status)
	}
	got := map[string]map[string]any{}
	for _, raw := range status["items"].([]any) {
		it := raw.(map[string]any)
		got[it["path"].(string)] = it
	}
	want := map[string]string{
		"incomplete/queue-" + strconv.FormatInt(done, 10): "imported-leftover",
		"incomplete/queue-999":                            "orphaned",
		"incomplete/stray.nfo":                            "orphaned",
		"incomplete/empty":                                "empty-folder",
	}
	if symlinked {
		want["incomplete/queue-"+strconv.FormatInt(linked, 10)] = "imported-leftover"
	}
	if len(got) != len(want) {
		t.Fatalf("scan found %v, want %v", keysOfMap(got), want)
	}
	for p, reason := range want {
		if got[p] == nil || got[p]["reason"] != reason {
			t.Fatalf("%s: got %+v, want reason %s", p, got[p], reason)
		}
	}
	if got["incomplete/queue-"+strconv.FormatInt(done, 10)]["sizeBytes"] != float64(5) {
		t.Fatalf("size: %+v", got)
	}
	if status["reclaimableBytes"] != float64(5+6+5) {
		t.Fatalf("reclaimableBytes = %v, want 16", status["reclaimableBytes"])
	}

	run := postJSON[map[string]any](t, client, base+"/api/system/cleanup", nil, http.StatusOK)
	if n := len(run["removed"].([]any)); n != len(want) || run["removedBytes"] != float64(16) || run["lastRunAt"] == "" {
		t.Fatalf("run: %+v", run)
	}
	for p := range want {
		if _, err := os.Lstat(filepath.Join(filepath.Dir(work), filepath.FromSlash(p))); err == nil {
			t.Fatalf("%s should be gone", p)
		}
	}
	for _, keep := range []string{queueDir(work, failed), queueDir(work, queued), filepath.Join(work, "fresh"), work} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("%s must be kept: %v", keep, err)
		}
	}
	if data, err := os.ReadFile(precious); err != nil || string(data) != "precious" {
		t.Fatalf("the library was touched: %v", err)
	}
	after := getJSON[map[string]any](t, client, base+"/api/system/cleanup")
	if len(after["items"].([]any)) != 0 || after["lastRunAt"] == nil {
		t.Fatalf("after the run: %+v", after)
	}

	// A day later the failed download's folder is fair game too; the queued
	// one never is.
	server.TestRunCleanup(time.Now().Add(25 * time.Hour))
	if _, err := os.Stat(queueDir(work, failed)); err == nil {
		t.Fatal("a day-old failed download's folder should be removed")
	}
	if _, err := os.Stat(queueDir(work, queued)); err != nil {
		t.Fatal("a queued download's folder must never be removed")
	}
	if data, err := os.ReadFile(precious); err != nil || string(data) != "precious" {
		t.Fatalf("the library was touched: %v", err)
	}
}

func keysOfMap(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestCleanupPrunesOldHistory(t *testing.T) {
	server, base, client := loginNewServer(t)
	enqueueWithStatus(t, server.QueueRepo, "Finished", queue.StatusCompleted)
	running := enqueueWithStatus(t, server.QueueRepo, "Running", queue.StatusDownloading)
	if err := server.QueueRepo.LogActivity(0, "grabbed", "something happened"); err != nil {
		t.Fatal(err)
	}

	// Within the 90 days: nothing goes.
	if _, q, a := server.TestRunCleanup(time.Now().Add(89 * 24 * time.Hour)); q != 0 || a != 0 {
		t.Fatalf("pruned %d queue items and %d activity entries within the retention", q, a)
	}
	// Keeping forever: nothing goes, however old.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"historyRetentionDays": 0}, http.StatusOK)
	if _, q, a := server.TestRunCleanup(time.Now().Add(1000 * 24 * time.Hour)); q != 0 || a != 0 {
		t.Fatalf("0 days must keep everything, pruned %d/%d", q, a)
	}
	// 30 days, run 31 days later: the finished download and old activity go,
	// the running download stays.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"historyRetentionDays": 30}, http.StatusOK)
	_, q, a := server.TestRunCleanup(time.Now().Add(31 * 24 * time.Hour))
	if q != 1 || a == 0 {
		t.Fatalf("pruned %d queue items and %d activity entries", q, a)
	}
	list := getJSON[[]map[string]any](t, client, base+"/api/queue")
	if len(list) != 1 || list[0]["id"] != float64(running) {
		t.Fatalf("only the running download should be left: %+v", list)
	}
}

func TestCleanupSettingsAndDailyJob(t *testing.T) {
	server, base, client := loginNewServer(t)

	st := getJSON[map[string]any](t, client, base+"/api/settings")
	if st["cleanupAuto"] != true || st["historyRetentionDays"] != float64(90) {
		t.Fatalf("defaults: cleanupAuto=%v historyRetentionDays=%v", st["cleanupAuto"], st["historyRetentionDays"])
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"historyRetentionDays": -1}, http.StatusBadRequest)

	// The hourly check runs the clean-up when it has never run...
	server.TestCleanupJob(context.Background())
	first := getJSON[map[string]any](t, client, base+"/api/system/cleanup")["lastRunAt"]
	if first == nil {
		t.Fatal("the automatic clean-up should have run")
	}
	// ...but not again within the day.
	putFile(t, filepath.Join(server.TestWorkDir(), "stray.nfo"), "x")
	makeOld(t, filepath.Join(server.TestWorkDir(), "stray.nfo"))
	server.TestCleanupJob(context.Background())
	if _, err := os.Stat(filepath.Join(server.TestWorkDir(), "stray.nfo")); err != nil {
		t.Fatal("the clean-up ran twice in one day")
	}

	// Switched off, it never runs on its own.
	st = putJSONStatus(t, client, base+"/api/settings", map[string]any{"cleanupAuto": false}, http.StatusOK)
	if st["cleanupAuto"] != false {
		t.Fatalf("cleanupAuto should read back false: %v", st["cleanupAuto"])
	}
}

// TestImportRemovesWorkingFolderAfterConflictResolution is covered in
// import_conflict_test.go; members may not use the clean-up.
func TestCleanupIsAdminOnly(t *testing.T) {
	server, _, _ := loginNewServer(t)
	access := server.TestRouteAccess()
	for _, route := range []string{"GET /api/system/cleanup", "POST /api/system/cleanup"} {
		if access[route] != "admin" {
			t.Fatalf("%s should be admin only, is %q", route, access[route])
		}
	}
}

// TestSeedingGoalRemovesImportedTorrentData: once a torrent's seeding goal
// is met its data goes, but only when the download has been imported.
func TestSeedingGoalRemovesImportedTorrentData(t *testing.T) {
	server, base, client := loginNewServer(t)
	work := server.TestWorkDir()
	imported := enqueueWithStatus(t, server.QueueRepo, "Imported", queue.StatusCompleted)
	importing := enqueueWithStatus(t, server.QueueRepo, "Importing", queue.StatusImporting)
	for _, id := range []int64{imported, importing} {
		putFile(t, filepath.Join(queueDir(work, id), "movie.mkv"), "data")
		server.TestSeedingGoalMet(id)
	}
	if _, err := os.Stat(queueDir(work, imported)); err == nil {
		t.Fatal("an imported torrent's data should be removed once its seeding goal is met")
	}
	if _, err := os.Stat(queueDir(work, importing)); err != nil {
		t.Fatal("data still being imported must be kept")
	}
	// Once imported, the scan reports it as seeding-finished.
	if err := server.QueueRepo.SetStatus(importing, queue.StatusCompleted, ""); err != nil {
		t.Fatal(err)
	}
	items := getJSON[map[string]any](t, client, base+"/api/system/cleanup")["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["reason"] != "seeding-finished" {
		t.Fatalf("scan after seeding: %+v", items)
	}
}
