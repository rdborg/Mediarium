package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/notify"
)

// putScript writes an executable shell script into the scripts folder.
func putScript(t *testing.T, dir, name, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), mode); err != nil {
		t.Fatal(err)
	}
}

func TestScriptChoice(t *testing.T) {
	server, base, admin := loginNewServer(t)
	dir := server.TestScriptsDir()
	putScript(t, dir, "ok.sh", "exit 0", 0o755)
	putScript(t, dir, "not-runnable.sh", "exit 0", 0o644)
	putScript(t, dir, ".hidden.sh", "exit 0", 0o755)
	outside := filepath.Join(t.TempDir(), "outside.sh")
	putScript(t, filepath.Dir(outside), "outside.sh", "exit 0", 0o755)
	if err := os.Symlink(outside, filepath.Join(dir, "link.sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := getJSON[struct {
		Scripts []string `json:"scripts"`
	}](t, admin, base+"/api/scripts")
	if len(got.Scripts) != 1 || got.Scripts[0] != "ok.sh" {
		t.Fatalf("only ok.sh can be picked, listed %v", got.Scripts)
	}

	tests := []struct {
		script string
		want   int
	}{
		{"ok.sh", http.StatusOK},
		{"", http.StatusOK}, // off
		{"not-runnable.sh", http.StatusBadRequest},
		{".hidden.sh", http.StatusBadRequest},
		{"link.sh", http.StatusBadRequest},
		{"folder", http.StatusBadRequest},
		{"../ok.sh", http.StatusBadRequest},
		{"/bin/sh", http.StatusBadRequest},
		{"missing.sh", http.StatusBadRequest},
	}
	for _, tt := range tests {
		postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/scripts", map[string]any{"script": tt.script}, tt.want)
	}
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/scripts", map[string]any{"script": "ok.sh", "timeoutSec": 7200}, http.StatusBadRequest)
}

func TestScriptCannotBeChosenOrRunWithAnAPIKey(t *testing.T) {
	server, base, admin := loginNewServer(t)
	putScript(t, server.TestScriptsDir(), "ok.sh", "exit 0", 0o755)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/scripts", map[string]any{"script": "ok.sh"}, http.StatusOK)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]string{"name": "script"}, http.StatusCreated)
	raw, _ := key["key"].(string)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/scripts", `{"script":"ok.sh"}`},
		{http.MethodPost, "/api/scripts/test", ``},
	} {
		req, _ := http.NewRequest(c.method, base+c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", raw)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s %s with an API key: %d", c.method, c.path, resp.StatusCode)
		}
	}
}

func TestScriptRunsAfterImport(t *testing.T) {
	t.Setenv("SOME_SECRET", "not-for-scripts")
	server, base, admin := loginNewServer(t)
	dir := server.TestScriptsDir()
	putScript(t, dir, "log.sh", `env | grep -e '^MEDIARIUM_' -e SOME_SECRET | sort > ran.txt`, 0o755)

	it := notify.Item{Media: "episode", Title: "Severance", Episode: "S02E03", Quality: "WEBDL-1080p", Path: "/data/TV/Severance/Season 02/Severance - S02E03.mkv", SizeBytes: 1234, Release: "Severance.S02E03.1080p.WEB-GRP"}
	server.TestNotifyImported(it) // no script chosen: nothing runs
	server.TestWaitBackground()
	if _, err := os.Stat(filepath.Join(dir, "ran.txt")); err == nil {
		t.Fatal("a script ran while none was chosen")
	}

	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/scripts", map[string]any{"script": "log.sh"}, http.StatusOK)
	server.TestNotifyImported(it)
	server.TestWaitBackground()
	b, err := os.ReadFile(filepath.Join(dir, "ran.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"MEDIARIUM_EVENT=imported", "MEDIARIUM_MEDIA=episode", "MEDIARIUM_TITLE=Severance", "MEDIARIUM_EPISODE=S02E03",
		"MEDIARIUM_PATH=/data/TV/Severance/Season 02/Severance - S02E03.mkv", "MEDIARIUM_FOLDER=/data/TV/Severance/Season 02",
		"MEDIARIUM_SIZE=1234", "MEDIARIUM_RELEASE=Severance.S02E03.1080p.WEB-GRP",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SOME_SECRET") {
		t.Fatalf("Mediarium's own environment reached the script:\n%s", got)
	}
}

func TestScriptTestRun(t *testing.T) {
	server, base, admin := loginNewServer(t)
	dir := server.TestScriptsDir()
	putScript(t, dir, "ok.sh", `echo "event $MEDIARIUM_EVENT"`, 0o755)
	putScript(t, dir, "fails.sh", `echo boom >&2; exit 3`, 0o755)
	putScript(t, dir, "slow.sh", `sleep 5`, 0o755)

	postJSON[map[string]any](t, admin, base+"/api/scripts/test", nil, http.StatusConflict) // nothing chosen yet

	tests := []struct {
		script          string
		timeout         int
		exit            int
		problem, output string
		timedOut        bool
	}{
		{script: "ok.sh", exit: 0},
		{script: "fails.sh", exit: 3, problem: "exit code 3", output: "boom"},
		{script: "slow.sh", timeout: 1, exit: -1, problem: "stopped after 1s", timedOut: true},
	}
	for _, tt := range tests {
		t.Run(tt.script, func(t *testing.T) {
			body := map[string]any{"script": tt.script}
			if tt.timeout > 0 {
				body["timeoutSec"] = tt.timeout
			}
			postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/scripts", body, http.StatusOK)
			run := postJSON[map[string]any](t, admin, base+"/api/scripts/test", nil, http.StatusOK)
			if run["exitCode"] != float64(tt.exit) || run["event"] != "test" || (run["timedOut"] == true) != tt.timedOut {
				t.Fatalf("run: %+v", run)
			}
			problem, _ := run["problem"].(string)
			output, _ := run["output"].(string)
			if (tt.problem == "") != (problem == "") || !strings.Contains(problem, tt.problem) || !strings.Contains(output, tt.output) {
				t.Fatalf("problem %q output %q, want %q and %q", problem, output, tt.problem, tt.output)
			}
			last := getJSON[map[string]any](t, admin, base+"/api/scripts")["lastRun"].(map[string]any)
			if last["script"] != tt.script {
				t.Fatalf("last run: %+v", last)
			}
		})
	}
}

func TestScriptLeavesNothingRunning(t *testing.T) {
	server, base, admin := loginNewServer(t)
	dir := server.TestScriptsDir()
	putScript(t, dir, "bg.sh", `(sleep 1; touch late.txt) >/dev/null 2>&1 &
exit 0`, 0o755)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/scripts", map[string]any{"script": "bg.sh"}, http.StatusOK)
	postJSON[map[string]any](t, admin, base+"/api/scripts/test", nil, http.StatusOK)
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(filepath.Join(dir, "late.txt")); err == nil {
		t.Fatal("what the script left running in the background kept going")
	}
}
