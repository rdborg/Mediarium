package api_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

// realProgram builds the app with a version number, once per version, and
// returns its bytes. The push and install paths run it (--version-check), so
// these tests use real programs, not stand-ins.
var (
	programMu    sync.Mutex
	programDir   string
	programCache = map[string][]byte{}
)

func realProgram(t *testing.T, version string) []byte {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("pushed updates are for Linux containers")
	}
	programMu.Lock()
	defer programMu.Unlock()
	if b, ok := programCache[version]; ok {
		return b
	}
	if programDir == "" {
		dir, err := os.MkdirTemp("", "mediarium-api-programs-*")
		if err != nil {
			t.Fatal(err)
		}
		programDir = dir
	}
	out := filepath.Join(programDir, "app-"+version)
	cmd := exec.Command("go", "build", "-o", out, "-ldflags", "-X main.version="+version, "github.com/rdborg/mediarium/cmd/app")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build version %s: %v\n%s", version, err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	programCache[version] = b
	return b
}

func sumOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// updateEnv is a running server that believes it was started by the container
// image (image version 1.1.0, running 1.1.0), with pushed updates switched on.
type updateEnv struct {
	server *api.Server
	base   string
	client *http.Client
	exited <-chan struct{}
	dir    string
}

func newUpdateEnv(t *testing.T) *updateEnv {
	t.Helper()
	server, base, client := loginNewServer(t)
	server.TestSetUpdateEnv("1.1.0", "1.1.0")
	return &updateEnv{server: server, base: base, client: client, exited: exitSpy(server), dir: server.TestUpdateDir()}
}

func (e *updateEnv) allowPush(t *testing.T, on bool) {
	t.Helper()
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/system/options", map[string]any{"allowPush": on}, http.StatusOK)
}

// push uploads body as the raw request body with the given checksum header.
func (e *updateEnv) push(t *testing.T, body []byte, sum string, query string, mutate ...func(*http.Request)) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.base+"/api/system/update"+query, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if sum != "" {
		req.Header.Set("X-Update-SHA256", sum)
	}
	for _, m := range mutate {
		if m != nil {
			m(req)
		}
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *updateEnv) mustNotExit(t *testing.T) {
	t.Helper()
	select {
	case <-e.exited:
		t.Fatal("the app restarted although nothing was installed")
	case <-time.After(120 * time.Millisecond):
	}
}

func (e *updateEnv) mustExit(t *testing.T) {
	t.Helper()
	select {
	case <-e.exited:
	case <-time.After(3 * time.Second):
		t.Fatal("the app did not restart")
	}
}

func (e *updateEnv) nothingInstalled(t *testing.T) {
	t.Helper()
	entries, _ := os.ReadDir(e.dir)
	for _, f := range entries {
		t.Errorf("the update folder should be empty, found %s", f.Name())
	}
}

func (e *updateEnv) state(t *testing.T) map[string]any {
	t.Helper()
	return getJSON[map[string]any](t, e.client, e.base+"/api/system/update")
}

func message(m map[string]any) string {
	s, _ := m["error"].(string)
	return s
}

func TestPushIsRefusedWhileSwitchedOff(t *testing.T) {
	e := newUpdateEnv(t)
	prog := realProgram(t, "9.9.9")
	code, body := e.push(t, prog, sumOf(prog), "")
	if code != http.StatusForbidden || !strings.Contains(message(body), "switched off") {
		t.Fatalf("status %d %v, want 403 saying pushing is switched off", code, body)
	}
	e.mustNotExit(t)
	e.nothingInstalled(t)
	if st := e.state(t); st["allowPush"] != false || st["running"] != "1.1.0" || st["image"] != "1.1.0" || st["pushed"] != nil {
		t.Fatalf("state = %v", st)
	}
	// Switching it on and off again closes the door again.
	e.allowPush(t, true)
	e.allowPush(t, false)
	if code, _ := e.push(t, prog, sumOf(prog), ""); code != http.StatusForbidden {
		t.Fatalf("after switching off again: status %d", code)
	}
}

func TestPushNeedsAnAdministrator(t *testing.T) {
	e := newUpdateEnv(t)
	e.allowPush(t, true)
	if _, err := e.server.Auth.CreateUser("guest", "another-long-password"); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	guest := &http.Client{Jar: jar}
	postJSON[map[string]any](t, guest, e.base+"/api/auth/login", map[string]string{"username": "guest", "password": "another-long-password"}, http.StatusOK)
	prog := realProgram(t, "9.9.9")

	req, _ := http.NewRequest(http.MethodPost, e.base+"/api/system/update", bytes.NewReader(prog))
	req.Header.Set("X-Update-SHA256", sumOf(prog))
	resp, err := guest.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a member pushed an update: status %d", resp.StatusCode)
	}
	// And someone who is not signed in at all.
	anon := &http.Client{}
	req, _ = http.NewRequest(http.MethodPost, e.base+"/api/system/update", bytes.NewReader(prog))
	req.Header.Set("X-Update-SHA256", sumOf(prog))
	resp, err = anon.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("an anonymous push: status %d", resp.StatusCode)
	}
	e.mustNotExit(t)
	e.nothingInstalled(t)
}

func TestEveryUpdateRouteIsAdminOnly(t *testing.T) {
	e := newUpdateEnv(t)
	if _, err := e.server.Auth.CreateUser("guest", "another-long-password"); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	guest := &http.Client{Jar: jar}
	postJSON[map[string]any](t, guest, e.base+"/api/auth/login", map[string]string{"username": "guest", "password": "another-long-password"}, http.StatusOK)
	routes := []struct{ method, path string }{
		{"GET", "/api/system/update"}, {"POST", "/api/system/update"}, {"DELETE", "/api/system/update"},
		{"GET", "/api/system/update/latest"}, {"POST", "/api/system/update/check"},
		{"GET", "/api/system/update/install"}, {"POST", "/api/system/update/install"},
		{"GET", "/api/system/options"}, {"PUT", "/api/system/options"},
		{"POST", "/api/system/restart"}, {"POST", "/api/system/restart?safe=true"}, {"POST", "/api/system/shutdown"},
	}
	for _, rt := range routes {
		if status, body := doStatus(t, guest, rt.method, e.base+rt.path); status != http.StatusForbidden || !strings.Contains(message(body), "administrator") {
			t.Errorf("%s %s as a member: %d %v, want 403", rt.method, rt.path, status, body)
		}
		if status, _ := doStatus(t, &http.Client{}, rt.method, e.base+rt.path); status != http.StatusUnauthorized {
			t.Errorf("%s %s signed out: %d, want 401", rt.method, rt.path, status)
		}
	}
	e.mustNotExit(t)
}

func TestAllowPushCannotBeSwitchedOnWithAnAPIKey(t *testing.T) {
	e := newUpdateEnv(t)
	key := postJSON[map[string]any](t, e.client, e.base+"/api/auth/api-keys", map[string]string{"name": "script"}, http.StatusCreated)["key"].(string)

	viaKey := func(method, path string, body any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, e.base+path, bytes.NewReader(b))
		req.Header.Set("X-API-Key", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	code, body := viaKey("PUT", "/api/system/options", map[string]any{"allowPush": true})
	if code != http.StatusForbidden || !strings.Contains(message(body), "signed in") {
		t.Fatalf("switching on with a key: %d %v, want 403", code, body)
	}
	if e.state(t)["allowPush"] != false {
		t.Fatal("the switch changed although the key was refused")
	}
	// The same key can read the state, change the other switches, and switch pushing OFF.
	if code, _ := viaKey("GET", "/api/system/update", nil); code != http.StatusOK {
		t.Fatalf("reading with a key: %d", code)
	}
	if code, _ := viaKey("PUT", "/api/system/options", map[string]any{"updateCheck": false}); code != http.StatusOK {
		t.Fatalf("other switches with a key: %d", code)
	}
	e.allowPush(t, true) // a browser session may
	if e.state(t)["allowPush"] != true {
		t.Fatal("a signed-in session could not switch it on")
	}
	if code, _ := viaKey("PUT", "/api/system/options", map[string]any{"allowPush": false}); code != http.StatusOK {
		t.Fatalf("switching off with a key: %d", code)
	}
	if e.state(t)["allowPush"] != false {
		t.Fatal("switching off with a key did not work")
	}
	// Once it is on, the key can push (that is what it is for).
	e.allowPush(t, true)
	prog := realProgram(t, "9.9.9")
	req, _ := http.NewRequest(http.MethodPost, e.base+"/api/system/update", bytes.NewReader(prog))
	req.Header.Set("X-API-Key", key)
	req.Header.Set("X-Update-SHA256", sumOf(prog))
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("a push with a key: %d", resp.StatusCode)
	}
	e.mustExit(t)
}

func TestPushRules(t *testing.T) {
	newer := realProgram(t, "9.9.9")
	older := realProgram(t, "0.0.1") // older than the image (1.1.0)
	same := realProgram(t, "1.1.0")  // the running version

	cases := []struct {
		name       string
		body       []byte
		sum        string // "" = the right one
		header     bool   // send the header at all
		query      string
		mutate     func(*http.Request)
		status     int
		wantInText string
	}{
		{"no checksum header", newer, "", false, "", nil, 400, "X-Update-SHA256"},
		{"checksum too short", newer, "abc123", true, "", nil, 400, "X-Update-SHA256"},
		{"checksum not hex", newer, strings.Repeat("z", 64), true, "", nil, 400, "X-Update-SHA256"},
		{"checksum of something else", newer, sumOf([]byte("something else")), true, "", nil, 400, "doesn't match the SHA-256"},
		{"one byte changed after the checksum was made", append(append([]byte(nil), newer...), 0), sumOf(newer), true, "", nil, 400, "doesn't match the SHA-256"},
		{"a form upload", newer, "", true, "", func(r *http.Request) { r.Header.Set("Content-Type", "multipart/form-data; boundary=x") }, 415, "not as a form"},
		{"empty body", nil, sumOf(nil), true, "", nil, 400, "empty"},
		{"a shell script, not a program", []byte("#!/bin/sh\necho hi\n"), "", true, "", nil, 422, "not a Mediarium program"},
		{"random bytes", bytes.Repeat([]byte{7, 1, 4}, 500), "", true, "", nil, 422, "not a Mediarium program"},
		{"the same version", same, "", true, "", nil, 409, "not newer"},
		{"older than the running version", older, "", true, "", nil, 409, "older than the version inside the Docker image"},
		{"older than the image, even when forced", older, "", true, "?force=true", nil, 409, "older than the version inside the Docker image"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newUpdateEnv(t)
			e.allowPush(t, true)
			sum := tc.sum
			if sum == "" {
				sum = sumOf(tc.body)
			}
			if !tc.header {
				sum = ""
			}
			code, body := e.push(t, tc.body, sum, tc.query, tc.mutate)
			if code != tc.status || !strings.Contains(message(body), tc.wantInText) {
				t.Fatalf("status %d %v, want %d containing %q", code, body, tc.status, tc.wantInText)
			}
			e.mustNotExit(t)
			e.nothingInstalled(t)
		})
	}
}

func TestPushOfTheSameVersionNeedsForce(t *testing.T) {
	e := newUpdateEnv(t)
	e.allowPush(t, true)
	same := realProgram(t, "1.1.0")
	if code, body := e.push(t, same, sumOf(same), ""); code != http.StatusConflict || !strings.Contains(message(body), "force=true") {
		t.Fatalf("status %d %v", code, body)
	}
	code, body := e.push(t, same, sumOf(same), "?force=true")
	if code != http.StatusAccepted || body["version"] != "1.1.0" {
		t.Fatalf("forced push: %d %v", code, body)
	}
	e.mustExit(t)
}

func TestPushInstallsAndRestarts(t *testing.T) {
	e := newUpdateEnv(t)
	e.allowPush(t, true)
	v2 := realProgram(t, "9.9.9")
	sum := sumOf(v2)

	code, body := e.push(t, v2, strings.ToUpper(sum), "") // upper-case hex is fine
	if code != http.StatusAccepted || body["version"] != "9.9.9" || body["restartingIn"] == nil || body["sha256"] != sum {
		t.Fatalf("status %d %v", code, body)
	}
	e.mustExit(t)

	// What is on disk: the program (executable), its version and checksum.
	app := filepath.Join(e.dir, "app")
	got, err := os.ReadFile(app)
	if err != nil || !bytes.Equal(got, v2) {
		t.Fatalf("installed program differs: %v", err)
	}
	if fi, _ := os.Stat(app); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if v, _ := os.ReadFile(filepath.Join(e.dir, "VERSION")); strings.TrimSpace(string(v)) != "9.9.9" {
		t.Fatalf("VERSION = %q", v)
	}
	if s, _ := os.ReadFile(filepath.Join(e.dir, "app.sha256")); !strings.HasPrefix(string(s), sum) {
		t.Fatalf("app.sha256 = %q", s)
	}
	entries, _ := os.ReadDir(e.dir)
	for _, f := range entries {
		if strings.HasPrefix(f.Name(), ".upload") {
			t.Errorf("temporary file %s was left behind", f.Name())
		}
	}

	// It shows in the state, and in the Activity page with who did it and a short checksum.
	st := e.state(t)
	pushed, _ := st["pushed"].(map[string]any)
	if pushed == nil || pushed["version"] != "9.9.9" || pushed["sha256"] != sum || pushed["hasPrevious"] != false {
		t.Fatalf("state = %v", st)
	}
	found := false
	for _, a := range getJSON[[]map[string]any](t, e.client, e.base+"/api/activity") {
		msg, _ := a["message"].(string)
		if a["eventType"] == "update" && strings.Contains(msg, "9.9.9") && strings.Contains(msg, "ryan") && strings.Contains(msg, sum[:12]) {
			found = true
			if strings.Contains(msg, sum) {
				t.Errorf("the whole checksum does not belong on the Activity page: %s", msg)
			}
		}
	}
	if !found {
		t.Fatal("the install is not recorded on the Activity page")
	}

	// A second, newer push keeps the first as the previous one.
	v3 := realProgram(t, "9.9.10")
	if code, body := e.push(t, v3, sumOf(v3), ""); code != http.StatusAccepted {
		t.Fatalf("second push: %d %v", code, body)
	}
	e.mustExit(t)
	st = e.state(t)
	pushed, _ = st["pushed"].(map[string]any)
	if pushed["version"] != "9.9.10" || pushed["hasPrevious"] != true || pushed["previousVersion"] != "9.9.9" {
		t.Fatalf("state after the second push = %v", st)
	}
	if prev, _ := os.ReadFile(filepath.Join(e.dir, "app.previous")); !bytes.Equal(prev, v2) {
		t.Fatal("the previous program was not kept")
	}

	// Removing it goes back to the image.
	req, _ := http.NewRequest(http.MethodDelete, e.base+"/api/system/update", nil)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var del map[string]any
	json.NewDecoder(resp.Body).Decode(&del)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || del["removed"] != "9.9.10" || del["image"] != "1.1.0" {
		t.Fatalf("DELETE: %d %v", resp.StatusCode, del)
	}
	e.nothingInstalled(t)
	if st := e.state(t); st["pushed"] != nil {
		t.Fatalf("still installed after DELETE: %v", st)
	}
	if code, _ := doStatus(t, e.client, http.MethodDelete, e.base+"/api/system/update"); code != http.StatusNotFound {
		t.Fatalf("DELETE with nothing installed: %d, want 404", code)
	}
}

func TestPushRollbackWithForce(t *testing.T) {
	e := newUpdateEnv(t)
	e.allowPush(t, true)
	v5 := realProgram(t, "1.5.0")
	v3 := realProgram(t, "1.3.0")
	if code, body := e.push(t, v5, sumOf(v5), ""); code != http.StatusAccepted {
		t.Fatalf("%d %v", code, body)
	}
	e.mustExit(t)
	// The running version is still 1.1.0 in this test, so simulate the restart into 1.5.0.
	e.server.TestSetUpdateEnv("1.5.0", "1.1.0")
	if code, body := e.push(t, v3, sumOf(v3), ""); code != http.StatusConflict {
		t.Fatalf("going back without force: %d %v", code, body)
	}
	code, body := e.push(t, v3, sumOf(v3), "?force=true")
	if code != http.StatusAccepted || body["version"] != "1.3.0" {
		t.Fatalf("going back with force: %d %v", code, body)
	}
	e.mustExit(t)
	if b, _ := os.ReadFile(filepath.Join(e.dir, "app")); !bytes.Equal(b, v3) {
		t.Fatal("the older program is not the installed one")
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "app.previous")); !bytes.Equal(b, v5) {
		t.Fatal("the program that was replaced was not kept")
	}
}

func TestPushSizeLimit(t *testing.T) {
	e := newUpdateEnv(t)
	e.allowPush(t, true)

	// A body that goes on for ever is cut at the limit and leaves nothing behind.
	e.server.TestSetPushLimit(4096)
	big := bytes.Repeat([]byte{1}, 100_000)
	code, body := e.push(t, big, sumOf(big), "")
	if code != http.StatusRequestEntityTooLarge || !strings.Contains(message(body), "too large") {
		t.Fatalf("%d %v", code, body)
	}
	e.mustNotExit(t)
	e.nothingInstalled(t)
	e.server.TestSetPushLimit(0)

	// A request that announces more than 200 MB is refused before a byte is read.
	u, _ := url.Parse(e.base)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cookies := e.client.Jar.Cookies(u)
	var cookieHeader string
	for _, c := range cookies {
		cookieHeader += c.Name + "=" + c.Value + "; "
	}
	fmt.Fprintf(conn, "POST /api/system/update HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\nX-Update-SHA256: %s\r\nContent-Length: %d\r\nContent-Type: application/octet-stream\r\n\r\n",
		u.Host, cookieHeader, strings.Repeat("a", 64), 300<<20)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("no answer: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", resp.StatusCode)
	}
	e.nothingInstalled(t)
}

func TestPushNeedsTheImageEntrypoint(t *testing.T) {
	e := newUpdateEnv(t)
	e.allowPush(t, true)
	e.server.TestSetUpdateEnv("1.1.0", "") // not started by the image's entrypoint
	prog := realProgram(t, "9.9.9")
	code, body := e.push(t, prog, sumOf(prog), "")
	if code != http.StatusConflict || !strings.Contains(message(body), "Docker image") {
		t.Fatalf("%d %v", code, body)
	}
	e.mustNotExit(t)
	e.nothingInstalled(t)
}

func TestPushOneAtATime(t *testing.T) {
	e := newUpdateEnv(t)
	e.allowPush(t, true)
	release := e.server.TestHoldUpdateLock()
	prog := realProgram(t, "9.9.9")
	code, body := e.push(t, prog, sumOf(prog), "")
	release()
	if code != http.StatusConflict || !strings.Contains(message(body), "already in progress") {
		t.Fatalf("%d %v", code, body)
	}
	e.nothingInstalled(t)
}

func TestRemovingAnUpdateWorksWhilePushingIsOff(t *testing.T) {
	e := newUpdateEnv(t)
	e.allowPush(t, true)
	prog := realProgram(t, "9.9.9")
	if code, _ := e.push(t, prog, sumOf(prog), ""); code != http.StatusAccepted {
		t.Fatal("push failed")
	}
	e.mustExit(t)
	e.allowPush(t, false)
	if code, body := doStatus(t, e.client, http.MethodDelete, e.base+"/api/system/update?restart=true"); code != http.StatusOK || body["restarting"] != true {
		t.Fatalf("%d %v", code, body)
	}
	e.mustExit(t)
	e.nothingInstalled(t)
}

func TestUpdateStateForAFailedProgram(t *testing.T) {
	e := newUpdateEnv(t)
	os.MkdirAll(e.dir, 0o755)
	os.WriteFile(filepath.Join(e.dir, "app.failed"), []byte("x"), 0o755)
	os.WriteFile(filepath.Join(e.dir, "VERSION.failed"), []byte("2.0.0\n"), 0o644)
	st := e.state(t)
	failed, _ := st["failed"].(map[string]any)
	if failed == nil || failed["version"] != "2.0.0" || st["pushed"] != nil {
		t.Fatalf("state = %v", st)
	}
	// Removing clears the failed mark too.
	if code, _ := doStatus(t, e.client, http.MethodDelete, e.base+"/api/system/update"); code != http.StatusOK {
		t.Fatalf("DELETE: %d", code)
	}
	e.nothingInstalled(t)
}
