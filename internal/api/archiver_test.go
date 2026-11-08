package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A fake yt-dlp: a shell script that answers --version, lists the videos in
// list.json next to it, writes a file for a download and logs every call.
const archiverFakeScript = `#!/bin/sh
here="$(dirname "$0")"
echo "$*" >> "$here/calls.log"
case "$*" in
  *--version*) echo "2026.10.01"; exit 0 ;;
  *--dump-json*) cat "$here/list.json"; exit 0 ;;
esac
for a; do last="$a"; done
case "$last" in
  *fail*) echo "ERROR: [youtube] fail: Video unavailable" >&2; exit 1 ;;
esac
id="${last##*v=}"
case "$*" in
  *"-x"*) printf audio > "$id.mp3" ;;
  *) printf video > "$id.mp4" ;;
esac
echo '{"id":"'"$id"'"}' > "$id.info.json"
`

type archiverRig struct {
	script, folder, dir string
}

func newArchiverRig(t *testing.T) archiverRig {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake yt-dlp is a shell script")
	}
	dir := t.TempDir()
	r := archiverRig{script: filepath.Join(dir, "yt-dlp"), folder: filepath.Join(t.TempDir(), "archive"), dir: dir}
	if err := os.WriteFile(r.script, []byte(archiverFakeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	r.list(t, `{"id":"vid00000001","title":"One","channel":"The Channel"}`)
	return r
}

func (r archiverRig) list(t *testing.T, lines ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, "list.json"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r archiverRig) downloads(t *testing.T) int {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(r.dir, "calls.log"))
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "--no-playlist") {
			n++
		}
	}
	return n
}

func (r archiverRig) config(extra map[string]any) map[string]any {
	c := map[string]any{
		"ytdlpPath": r.script, "folder": r.folder, "format": "video", "maxPerRun": 5, "pauseSeconds": 0,
		"intervalHours": 12, "timeoutMinutes": 10, "extraHosts": []string{}, "acknowledged": true,
	}
	for k, v := range extra {
		c[k] = v
	}
	return c
}

func enableArchiver(t *testing.T, base string, client *http.Client, r archiverRig, extra map[string]any) {
	t.Helper()
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/archiver", r.config(extra), http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"archiver": true}, http.StatusOK)
}

func addSource(t *testing.T, base string, client *http.Client, body map[string]any) int64 {
	t.Helper()
	p := postJSON[map[string]any](t, client, base+"/api/archiver/sources", body, http.StatusOK)
	srcs := p["sources"].([]any)
	return int64(srcs[len(srcs)-1].(map[string]any)["id"].(float64))
}

func archiverStates(t *testing.T, base string, client *http.Client) map[string]int {
	t.Helper()
	p := getJSON[map[string]any](t, client, base+"/api/archiver")
	out := map[string]int{}
	for _, s := range p["sources"].([]any) {
		m := s.(map[string]any)
		out["queued"] += int(m["queued"].(float64))
		out["done"] += int(m["done"].(float64))
		out["failed"] += int(m["failed"].(float64))
	}
	return out
}

func TestArchiverCannotBeSwitchedOnWithoutTheTickAndAWorkingFile(t *testing.T) {
	_, base, client := loginNewServer(t)
	rig := newArchiverRig(t)
	notYtdlp := filepath.Join(rig.dir, "other")
	if err := os.WriteFile(notYtdlp, []byte("#!/bin/sh\necho hello there, not a version\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	on := map[string]any{"archiver": true}

	if m := getJSON[map[string]any](t, client, base+"/api/modules")["archiver"].(map[string]any); m["enabled"] != false {
		t.Fatalf("the archiver is off at first: %v", m)
	}
	tests := []struct {
		name   string
		config map[string]any
		want   string
	}{
		{"nothing set", map[string]any{"ytdlpPath": "", "folder": "", "acknowledged": false}, "read the warning"},
		{"no tick", map[string]any{"acknowledged": false}, "read the warning"},
		{"no folder", map[string]any{"folder": ""}, "download folder"},
		{"no path", map[string]any{"ytdlpPath": ""}, "yt-dlp file"},
		{"not yt-dlp", map[string]any{"ytdlpPath": notYtdlp}, "doesn't look like yt-dlp"},
	}
	for _, tc := range tests {
		postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/archiver", rig.config(tc.config), http.StatusOK)
		code, body := doJSONStatus(t, client, http.MethodPut, base+"/api/modules", on)
		msg, _ := body["error"].(string)
		if code != http.StatusBadRequest || !strings.Contains(msg, tc.want) {
			t.Errorf("%s: got %d %v, want 400 with %q", tc.name, code, body, tc.want)
		}
		if m := getJSON[map[string]any](t, client, base+"/api/modules")["archiver"].(map[string]any); m["enabled"] != false {
			t.Fatalf("%s: it was switched on anyway", tc.name)
		}
	}

	// A path that is not a file, or a number out of range, is refused when saved.
	for _, bad := range []map[string]any{{"ytdlpPath": rig.dir}, {"ytdlpPath": "relative/yt-dlp"}, {"maxPerRun": 0}, {"maxPerRun": 51}, {"format": "flac"}, {"folder": "relative"}, {"extraHosts": []string{"192.168.1.1"}}, {"extraHosts": []string{"https://vimeo.com/x"}}} {
		if code, _ := doJSONStatus(t, client, http.MethodPut, base+"/api/archiver", rig.config(bad)); code != http.StatusBadRequest {
			t.Errorf("%v: got %d, want 400", bad, code)
		}
	}

	// With everything right it switches on, and the warning can't be un-ticked while it is on.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/archiver", rig.config(nil), http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", on, http.StatusOK)
	if m := getJSON[map[string]any](t, client, base+"/api/modules")["archiver"].(map[string]any); m["enabled"] != true {
		t.Fatalf("it should be on: %v", m)
	}
	if code, _ := doJSONStatus(t, client, http.MethodPut, base+"/api/archiver", rig.config(map[string]any{"acknowledged": false})); code != http.StatusBadRequest {
		t.Errorf("the warning was un-ticked while the archiver is on: %d", code)
	}
}

func TestArchiverRoutesAreForAdministratorsAndNeedTheModule(t *testing.T) {
	_, base, client := loginNewServer(t)
	rig := newArchiverRig(t)
	// Off: following something answers 409.
	if code, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/archiver/sources", map[string]any{"url": "https://www.youtube.com/@x"}); code != http.StatusConflict {
		t.Errorf("adding while off: %d, want 409", code)
	}
	if code, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/archiver/sources/1/check", map[string]any{}); code != http.StatusConflict {
		t.Errorf("checking while off: %d, want 409", code)
	}
	enableArchiver(t, base, client, rig, nil)
	for _, bad := range []string{"http://www.youtube.com/@x", "https://vimeo.com/x", "--exec=touch /tmp/x", "https://www.youtube.com/@x --exec=y", "https://user@www.youtube.com/@x", "file:///etc/passwd"} {
		if code, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/archiver/sources", map[string]any{"url": bad}); code != http.StatusBadRequest {
			t.Errorf("%q was accepted: %d", bad, code)
		}
	}
	id := addSource(t, base, client, map[string]any{"url": "https://www.youtube.com/@SomeChannel", "format": "mp3"})
	if code, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/archiver/sources", map[string]any{"url": "https://www.youtube.com/@SomeChannel/videos"}); code != http.StatusConflict {
		t.Errorf("following the same address twice: %d, want 409", code)
	}
	p := getJSON[map[string]any](t, client, base+"/api/archiver")
	if u := p["sources"].([]any)[0].(map[string]any)["url"]; u != "https://www.youtube.com/@SomeChannel/videos" {
		t.Errorf("stored address = %v", u)
	}
	if code := deleteReq(t, client, base+"/api/archiver/sources/9999"); code != http.StatusNotFound {
		t.Errorf("removing an unknown source: %d", code)
	}
	if code := deleteReq(t, client, base+"/api/archiver/sources/"+archID(id)); code != http.StatusOK {
		t.Errorf("remove: %d", code)
	}
	if n := len(getJSON[map[string]any](t, client, base+"/api/archiver")["sources"].([]any)); n != 0 {
		t.Errorf("%d sources left", n)
	}
	// The test button.
	got := postJSON[map[string]any](t, client, base+"/api/archiver/test", map[string]any{"path": rig.script}, http.StatusOK)
	if got["version"] != "2026.10.01" {
		t.Errorf("test = %v", got)
	}
	if code, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/archiver/test", map[string]any{"path": "/no/such/file"}); code != http.StatusBadRequest {
		t.Errorf("testing a missing file: %d", code)
	}
}

func archID(n int64) string { return strconv.FormatInt(n, 10) }

func TestArchiverKeepsNewVideosOnlyOnceAndNamesThemSafely(t *testing.T) {
	server, base, client := loginNewServer(t)
	rig := newArchiverRig(t)
	rig.list(t,
		`{"id":"vid00000001","title":"One","channel":"The Channel"}`,
		`{"id":"vid00000002","title":"../../evil; touch /tmp/pwned $(id)","channel":"The Channel"}`,
	)
	enableArchiver(t, base, client, rig, nil)
	id := addSource(t, base, client, map[string]any{"url": "https://www.youtube.com/playlist?list=PL1", "backlog": true})

	if !server.TestArchiverCheck(id) {
		t.Fatal("the check did not start")
	}
	if st := archiverStates(t, base, client); st["done"] != 2 || st["failed"] != 0 || st["queued"] != 0 {
		t.Fatalf("after the first check: %v", st)
	}
	chanDir := filepath.Join(rig.folder, "The Channel")
	if b, err := os.ReadFile(filepath.Join(chanDir, "One [vid00000001].mp4")); err != nil || string(b) != "video" {
		t.Fatalf("the first video is not where it should be: %v", err)
	}
	if !exists(filepath.Join(chanDir, "One [vid00000001].info.json")) {
		t.Error("no info file beside the video")
	}
	// Whatever the title said, everything stays inside the archive folder.
	var files []string
	_ = filepath.Walk(rig.folder, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(rig.folder, p)
			if strings.HasPrefix(rel, "..") || strings.Contains(filepath.Base(p), "/") {
				t.Errorf("%s is outside the archive folder", p)
			}
			files = append(files, rel)
		}
		return nil
	})
	if len(files) != 4 {
		t.Errorf("want 2 videos and 2 info files, got %v", files)
	}
	if exists("/tmp/pwned") {
		t.Error("a title ran as a command")
	}
	if rig.downloads(t) != 2 {
		t.Fatalf("downloads = %d, want 2", rig.downloads(t))
	}

	// A second check with the same list downloads nothing.
	server.TestArchiverCheck(id)
	if rig.downloads(t) != 2 {
		t.Errorf("an old video was downloaded again: %d", rig.downloads(t))
	}

	// A new video is found and downloaded, the old ones are not.
	rig.list(t,
		`{"id":"vid00000003","title":"Three","channel":"The Channel"}`,
		`{"id":"vid00000001","title":"One","channel":"The Channel"}`,
		`{"id":"vid00000002","title":"Two","channel":"The Channel"}`,
	)
	server.TestArchiverCheck(id)
	if rig.downloads(t) != 3 || !exists(filepath.Join(chanDir, "Three [vid00000003].mp4")) {
		t.Errorf("the new video was not kept (downloads %d)", rig.downloads(t))
	}
}

func TestArchiverFollowsFromNowOnUnlessTheBacklogIsAsked(t *testing.T) {
	server, base, client := loginNewServer(t)
	rig := newArchiverRig(t)
	rig.list(t, `{"id":"vid00000001","title":"Old","channel":"The Channel"}`)
	enableArchiver(t, base, client, rig, nil)
	id := addSource(t, base, client, map[string]any{"url": "https://www.youtube.com/@x"})
	server.TestArchiverCheck(id)
	if rig.downloads(t) != 0 {
		t.Fatalf("the first check downloaded the videos that were already there")
	}
	rig.list(t, `{"id":"vid00000002","title":"Fresh","channel":"The Channel"}`, `{"id":"vid00000001","title":"Old","channel":"The Channel"}`)
	server.TestArchiverCheck(id)
	if rig.downloads(t) != 1 || !exists(filepath.Join(rig.folder, "The Channel", "Fresh [vid00000002].mp4")) {
		t.Errorf("only the fresh video should come: %d downloads", rig.downloads(t))
	}
}

func TestArchiverLimitsFailuresAndAudio(t *testing.T) {
	server, base, client := loginNewServer(t)
	rig := newArchiverRig(t)
	rig.list(t,
		`{"id":"vid00000003","title":"C","channel":"Music Chan"}`,
		`{"id":"vid00000002","title":"B","channel":"Music Chan"}`,
		`{"id":"vidfail0001","title":"Broken","channel":"Music Chan"}`,
	)
	enableArchiver(t, base, client, rig, map[string]any{"maxPerRun": 1})
	id := addSource(t, base, client, map[string]any{"url": "https://www.youtube.com/@music", "format": "mp3", "backlog": true})

	server.TestArchiverCheck(id)
	if st := archiverStates(t, base, client); st["done"] != 1 || st["queued"] != 2 {
		t.Fatalf("one video per check: %v", st)
	}
	if !exists(filepath.Join(rig.folder, "Music Chan", "C [vid00000003].mp3")) {
		t.Error("audio only: the newest video should be an mp3")
	}
	server.TestArchiverCheck(id)
	server.TestArchiverCheck(id)
	st := archiverStates(t, base, client)
	if st["done"] != 2 || st["failed"] != 1 || st["queued"] != 0 {
		t.Fatalf("after three checks: %v", st)
	}
	p := getJSON[map[string]any](t, client, base+"/api/archiver")
	var errText string
	for _, v := range p["recent"].([]any) {
		if m := v.(map[string]any); m["state"] == "failed" {
			errText, _ = m["error"].(string)
		}
	}
	if !strings.Contains(errText, "Video unavailable") {
		t.Errorf("the failure should say why: %q", errText)
	}
	// Check now tries the failed one again.
	before := rig.downloads(t)
	postJSON[map[string]any](t, client, base+"/api/archiver/sources/"+archID(id)+"/check", map[string]any{}, http.StatusOK)
	deadline := time.Now().Add(10 * time.Second)
	for rig.downloads(t) == before && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if rig.downloads(t) == before {
		t.Error("Check now did not try the failed video again")
	}
}

func TestArchiverJobWaitsForTheIntervalAndTheStartupDelay(t *testing.T) {
	server, base, client := loginNewServer(t)
	rig := newArchiverRig(t)
	rig.list(t, `{"id":"vid00000001","title":"One","channel":"The Channel"}`)
	enableArchiver(t, base, client, rig, nil)
	id := addSource(t, base, client, map[string]any{"url": "https://www.youtube.com/@x", "backlog": true})

	server.TestArchiverNotBefore(time.Now().Add(time.Hour))
	server.TestArchiverJob()
	if rig.downloads(t) != 0 {
		t.Fatal("the job ran in the first minutes after the start")
	}
	server.TestArchiverNotBefore(time.Time{})
	server.TestArchiverJob()
	if rig.downloads(t) != 1 {
		t.Fatalf("the job should have checked the new source: %d", rig.downloads(t))
	}
	rig.list(t, `{"id":"vid00000002","title":"Two","channel":"The Channel"}`, `{"id":"vid00000001","title":"One","channel":"The Channel"}`)
	server.TestArchiverJob()
	if rig.downloads(t) != 1 {
		t.Fatal("the job checked again before the interval was over")
	}
	server.TestArchiverAge(id, time.Now().Add(-13*time.Hour))
	server.TestArchiverJob()
	if rig.downloads(t) != 2 {
		t.Fatalf("the job should check once the 12 hours are over: %d", rig.downloads(t))
	}
	// Off means nothing runs.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"archiver": false}, http.StatusOK)
	server.TestArchiverAge(id, time.Now().Add(-48*time.Hour))
	server.TestArchiverJob()
	if rig.downloads(t) != 2 {
		t.Fatal("the job ran while the module was off")
	}
}
