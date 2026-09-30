package selfupdate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// These tests run the real docker-entrypoint.sh against real Mediarium
// programs (built from this tree with different version numbers) in a fake
// container: a temporary config folder and a temporary "image" folder. They
// check which program the entrypoint chooses, and that a bad installed update
// can never keep Mediarium from starting.

var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

// program returns the path of a Mediarium program built with the version.
func program(t *testing.T, version string) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "mediarium-entrypoint-*")
		if buildErr == nil {
			buildErr = os.Chmod(buildDir, 0o755)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	out := filepath.Join(buildDir, "app-"+version)
	if _, err := os.Stat(out); err == nil {
		return out
	}
	cmd := exec.Command("go", "build", "-o", out, "-ldflags", "-X main.version="+version, "github.com/rdborg/mediarium/cmd/app")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build version %s: %v\n%s", version, err, b)
	}
	if err := os.Chmod(out, 0o755); err != nil {
		t.Fatal(err)
	}
	return out
}

type entry struct {
	t      *testing.T
	root   string // fake container: image/ and config/
	image  string // the image's program
	config string
	update string
}

// newEntry makes a fake container whose image holds imageVersion.
func newEntry(t *testing.T, imageVersion string) *entry {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the entrypoint runs on Linux")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh")
	}
	root, err := os.MkdirTemp("", "mediarium-container-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	os.Chmod(root, 0o777) // an ordinary user must be able to get in
	e := &entry{t: t, root: root, config: filepath.Join(root, "config"), update: filepath.Join(root, "config", "update")}
	for _, d := range []string{"image", "config", "config/update", "downloads", "movies", "tv"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o777); err != nil {
			t.Fatal(err)
		}
		os.Chmod(filepath.Join(root, d), 0o777)
	}
	e.image = filepath.Join(root, "image", "app")
	copyFile(t, program(t, imageVersion), e.image, 0o755)
	return e
}

func copyFile(t *testing.T, from, to string, mode os.FileMode) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, mode); err != nil {
		t.Fatal(err)
	}
	os.Chmod(to, mode)
}

// install places a program as the installed update, with its checksum file.
func (e *entry) install(from string) {
	e.t.Helper()
	app := filepath.Join(e.update, "app")
	copyFile(e.t, from, app, 0o755)
	e.writeSum(app)
}

func (e *entry) writeSum(app string) {
	b, _ := os.ReadFile(app)
	sum := sha256.Sum256(b)
	os.WriteFile(filepath.Join(e.update, "app.sha256"), []byte(hex.EncodeToString(sum[:])+"  app\n"), 0o666)
}

// script places a shell script as the installed update.
func (e *entry) script(body string, withSum bool) {
	e.t.Helper()
	app := filepath.Join(e.update, "app")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		e.t.Fatal(err)
	}
	os.Chmod(app, 0o755)
	if withSum {
		e.writeSum(app)
	}
}

// run starts the entrypoint (as an ordinary user, like a container started
// with a user: setting) with args; IMAGE is replaced by the image's program.
func (e *entry) run(env []string, args ...string) (stdout, stderr string, code int) {
	e.t.Helper()
	script, err := filepath.Abs("../../docker-entrypoint.sh")
	if err != nil {
		e.t.Fatal(err)
	}
	full := []string{}
	for _, a := range args {
		if a == "IMAGE" {
			a = e.image
		}
		full = append(full, a)
	}
	var cmd *exec.Cmd
	if os.Getuid() == 0 {
		// Run as user 1000 so the script takes its non-root path, as it does in
		// a container started with "user: 1000:1000".
		if _, err := exec.LookPath("setpriv"); err != nil {
			e.t.Skip("running as root needs setpriv to drop to an ordinary user")
		}
		cmd = exec.Command("setpriv", append([]string{"--reuid=1000", "--regid=1000", "--clear-groups", "sh", script}, full...)...)
	} else {
		cmd = exec.Command("sh", append([]string{script}, full...)...)
	}
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"CONFIG_DIR=" + e.config,
		"DOWNLOADS_DIR=" + filepath.Join(e.root, "downloads"),
		"MOVIES_DIR=" + filepath.Join(e.root, "movies"),
		"TV_DIR=" + filepath.Join(e.root, "tv"),
		"MEDIARIUM_IMAGE_BIN=" + e.image,
		"PUID=1000", "PGID=1000",
	}, env...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err = cmd.Run()
	code = 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		e.t.Fatalf("run entrypoint: %v", err)
	}
	return so.String(), se.String(), code
}

// version runs the chosen program with --version-check and returns its version.
func (e *entry) version(env ...string) (version, stderr string) {
	e.t.Helper()
	out, errText, code := e.run(env, "IMAGE", "--version-check")
	if code != 0 {
		e.t.Fatalf("entrypoint exited %d: %s", code, errText)
	}
	f := strings.Fields(out)
	if len(f) != 3 {
		e.t.Fatalf("unexpected output %q (stderr: %s)", out, errText)
	}
	return f[1], errText
}

func (e *entry) bootCount() string {
	b, _ := os.ReadFile(filepath.Join(e.update, "boot-count"))
	return strings.TrimSpace(string(b))
}

func TestEntrypointChoosesTheProgram(t *testing.T) {
	cases := []struct {
		name     string
		image    string
		install  string // version of the installed update, "" for none
		wantVer  string
		wantNote string // text in the log, "" for none
	}{
		{"nothing installed", "1.1.0", "", "1.1.0", ""},
		{"a newer update is used", "1.1.0", "1.2.0", "1.2.0", "starting the installed update 1.2.0"},
		{"1.10 is newer than 1.9", "1.9.0", "1.10.0", "1.10.0", "starting the installed update 1.10.0"},
		{"the same version is used", "1.1.0", "1.1.0", "1.1.0", "starting the installed update 1.1.0"},
		{"an older update is ignored", "1.2.0", "1.1.0", "1.2.0", "older than the version in the image"},
		{"1.9 is older than 1.10", "1.10.0", "1.9.0", "1.10.0", "older than the version in the image"},
		{"a pre-release of the same version is older", "1.1.0", "1.1.0-rc.1", "1.1.0", "older than the version in the image"},
		{"a newer pre-release is used", "1.1.0", "1.2.0-rc.1", "1.2.0-rc.1", "starting the installed update 1.2.0-rc.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEntry(t, tc.image)
			if tc.install != "" {
				e.install(program(t, tc.install))
			}
			got, log := e.version()
			if got != tc.wantVer {
				t.Fatalf("started version %s, want %s (log: %s)", got, tc.wantVer, log)
			}
			if tc.wantNote != "" && !strings.Contains(log, tc.wantNote) {
				t.Fatalf("log %q does not say %q", log, tc.wantNote)
			}
			if tc.wantNote == "" && strings.TrimSpace(log) != "" {
				t.Fatalf("unexpected log: %s", log)
			}
		})
	}
}

func TestEntrypointTellsTheAppItsImageVersion(t *testing.T) {
	e := newEntry(t, "1.1.0")
	out, _, code := e.run(nil, "env")
	if code != 0 || !strings.Contains(out, "MEDIARIUM_IMAGE_VERSION=1.1.0\n") {
		t.Fatalf("environment did not carry the image version: %q", out)
	}
}

func TestEntrypointIgnoresBadUpdates(t *testing.T) {
	arch := runtime.GOARCH
	cases := []struct {
		name string
		set  func(e *entry)
		note string
	}{
		{"checksum does not match", func(e *entry) {
			e.install(program(t, "1.2.0"))
			os.WriteFile(filepath.Join(e.update, "app.sha256"), []byte(strings.Repeat("0", 64)+"  app\n"), 0o666)
		}, "does not match its checksum"},
		{"the file changed after it was installed", func(e *entry) {
			e.install(program(t, "1.2.0"))
			f, _ := os.OpenFile(filepath.Join(e.update, "app"), os.O_APPEND|os.O_WRONLY, 0o755)
			f.WriteString("tampered")
			f.Close()
		}, "does not match its checksum"},
		{"not executable", func(e *entry) {
			e.install(program(t, "1.2.0"))
			os.Chmod(filepath.Join(e.update, "app"), 0o644)
		}, "not an executable file"},
		{"a folder instead of a file", func(e *entry) {
			os.Mkdir(filepath.Join(e.update, "app"), 0o777)
		}, "not an executable file"},
		{"does not answer as Mediarium", func(e *entry) { e.script("echo hello world\n", false) }, "does not answer as a Mediarium program"},
		{"says nothing", func(e *entry) { e.script("exit 0\n", false) }, "does not answer as a Mediarium program"},
		{"fails", func(e *entry) { e.script("exit 5\n", false) }, "does not answer as a Mediarium program"},
		{"another system", func(e *entry) {
			e.script("echo mediarium 9.9.9 linux/riscv64\n", false)
		}, "does not answer as a Mediarium program"},
		{"another program with a version line", func(e *entry) {
			e.script("echo radarr 9.9.9 linux/"+arch+"\n", false)
		}, "does not answer as a Mediarium program"},
		{"not a version number", func(e *entry) {
			e.script("echo mediarium banana linux/"+arch+"\n", false)
		}, "not a version number"},
		{"hangs", func(e *entry) { e.script("sleep 60\n", false) }, "does not answer as a Mediarium program"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "hangs" && testing.Short() {
				t.Skip("takes ten seconds")
			}
			if tc.name == "hangs" {
				if _, err := exec.LookPath("timeout"); err != nil {
					t.Skip("needs timeout")
				}
			}
			e := newEntry(t, "1.1.0")
			tc.set(e)
			got, log := e.version()
			if got != "1.1.0" {
				t.Fatalf("started version %s, want the image's 1.1.0 (log: %s)", got, log)
			}
			if !strings.Contains(log, tc.note) {
				t.Fatalf("log %q does not say %q", log, tc.note)
			}
			if lines := strings.Count(strings.TrimSpace(log), "\n") + 1; lines != 1 {
				t.Fatalf("log should be one line, got %d: %s", lines, log)
			}
		})
	}
}

func TestEntrypointSkipSwitch(t *testing.T) {
	e := newEntry(t, "1.1.0")
	e.install(program(t, "1.2.0"))
	got, log := e.version("MEDIARIUM_SKIP_INSTALLED_UPDATE=1")
	if got != "1.1.0" || !strings.Contains(log, "MEDIARIUM_SKIP_INSTALLED_UPDATE") {
		t.Fatalf("version %s, log %q", got, log)
	}
}

// The -full image starts "dumb-init -- run-full.sh /opt/mediarium/app": the
// program is an argument in the middle of a longer command, and it is
// replaced there.
func TestEntrypointReplacesTheProgramInsideALongerCommand(t *testing.T) {
	e := newEntry(t, "1.1.0")
	e.install(program(t, "1.2.0"))
	out, _, code := e.run(nil, "sh", "-c", `printf '%s|%s|%s\n' "$1" "$2" "$3"`, "_", "--first", "IMAGE", "--last")
	want := filepath.Join(e.update, "app")
	if code != 0 || strings.TrimSpace(out) != "--first|"+want+"|--last" {
		t.Fatalf("output %q (exit %d), want the installed program in the middle", out, code)
	}
	// Arguments with spaces and other odd characters survive untouched.
	out, _, _ = e.run(nil, "sh", "-c", `printf '[%s]' "$@"`, "_", "a b", "IMAGE", "*", "")
	if out != "[a b]["+want+"][*][]" {
		t.Fatalf("arguments were changed: %q", out)
	}
}

func TestEntrypointBootLoopProtection(t *testing.T) {
	arch := runtime.GOARCH
	// A "program" that identifies as version 9.9.9 but stops with an error
	// whenever it is really started.
	bad := `case "$1" in --version-check) echo "mediarium 9.9.9 linux/` + arch + `";; *) exit 1;; esac` + "\n"

	t.Run("three quick failures put the update aside", func(t *testing.T) {
		e := newEntry(t, "1.1.0")
		e.script(bad, true)
		os.WriteFile(filepath.Join(e.update, "VERSION"), []byte("9.9.9\n"), 0o666)
		for i := 1; i <= 3; i++ {
			_, log, code := e.run(nil, "IMAGE", "--accepts-update", "9.9.9")
			if code != 1 {
				t.Fatalf("start %d: exit %d, want the bad program's 1 (log: %s)", i, code, log)
			}
			if got := e.bootCount(); got != fmt.Sprint(i) {
				t.Fatalf("after start %d the counter is %q", i, got)
			}
		}
		// The fourth start falls back to the image's program, which succeeds.
		_, log, code := e.run(nil, "IMAGE", "--accepts-update", "9.9.9")
		if code != 0 {
			t.Fatalf("fourth start: exit %d (log: %s)", code, log)
		}
		if !strings.Contains(log, "stopped 3 times in a row") || strings.Count(strings.TrimSpace(log), "\n") != 0 {
			t.Fatalf("log = %q", log)
		}
		for name, wantThere := range map[string]bool{"app": false, "app.failed": true, "VERSION.failed": true, "boot-count": false, "app.sha256": false} {
			_, err := os.Stat(filepath.Join(e.update, name))
			if (err == nil) != wantThere {
				t.Errorf("%s exists = %v, want %v", name, err == nil, wantThere)
			}
		}
		// And it stays fixed: the next start is the image's, quietly.
		if got, log := e.version(); got != "1.1.0" || strings.TrimSpace(log) != "" {
			t.Fatalf("after the fallback: version %s, log %q", got, log)
		}
		// The state the app shows.
		if _, failed := State(e.update); failed == nil || failed.Version != "9.9.9" {
			t.Fatalf("failed state = %+v", failed)
		}
	})

	t.Run("a program that proves itself resets the count", func(t *testing.T) {
		e := newEntry(t, "1.1.0")
		e.script(bad, true)
		for i := 0; i < 10; i++ {
			if _, _, code := e.run(nil, "IMAGE", "--accepts-update", "9.9.9"); code != 1 {
				t.Fatalf("start %d: exit %d, want 1", i, code)
			}
			// The running program marks itself healthy after 20 seconds.
			MarkHealthy(e.update)
		}
		if _, err := os.Stat(filepath.Join(e.update, "app")); err != nil {
			t.Fatal("a program that keeps proving itself must stay installed")
		}
	})

	t.Run("a cleared counter is the only thing that reset it", func(t *testing.T) {
		e := newEntry(t, "1.1.0")
		e.script(bad, true)
		e.run(nil, "IMAGE", "--accepts-update", "9.9.9")
		e.run(nil, "IMAGE", "--accepts-update", "9.9.9")
		if e.bootCount() != "2" {
			t.Fatalf("counter = %q", e.bootCount())
		}
		// A garbage counter is treated as zero, never as a reason to crash.
		os.WriteFile(filepath.Join(e.update, "boot-count"), []byte("abc\n"), 0o666)
		if _, _, code := e.run(nil, "IMAGE", "--accepts-update", "9.9.9"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if e.bootCount() != "1" {
			t.Fatalf("counter after garbage = %q", e.bootCount())
		}
	})
}

func TestEntrypointNeedsAWritableUpdateFolder(t *testing.T) {
	e := newEntry(t, "1.1.0")
	e.install(program(t, "1.2.0"))
	if err := os.Chmod(e.update, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(e.update, 0o777)
	got, log := e.version()
	if got != "1.1.0" || !strings.Contains(log, "can't be written") {
		t.Fatalf("version %s, log %q: without a way to count starts, the update must not be used", got, log)
	}
}

// Going back: after the installed update is removed (DELETE /api/system/update)
// the image's own program starts again.
func TestEntrypointRollback(t *testing.T) {
	e := newEntry(t, "1.1.0")
	e.install(program(t, "1.2.0"))
	if got, _ := e.version(); got != "1.2.0" {
		t.Fatalf("version %s, want 1.2.0", got)
	}
	if err := Remove(e.update); err != nil {
		t.Fatal(err)
	}
	if got, log := e.version(); got != "1.1.0" || strings.TrimSpace(log) != "" {
		t.Fatalf("after removing: version %s, log %q", got, log)
	}
}

// Installing through the same code the API uses, then starting: the new
// program runs; installing a second one keeps the first as app.previous.
func TestEntrypointFollowsInstall(t *testing.T) {
	e := newEntry(t, "1.1.0")
	stage := func(version string) string {
		p := filepath.Join(e.update, ".upload-"+version)
		copyFile(t, program(t, version), p, 0o755)
		return p
	}
	sum := func(path string) string {
		b, _ := os.ReadFile(path)
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:])
	}
	p := stage("1.2.0")
	if err := Install(e.update, p, "1.2.0", sum(program(t, "1.2.0"))); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.version(); got != "1.2.0" {
		t.Fatalf("version %s", got)
	}
	MarkHealthy(e.update)
	p = stage("1.3.0")
	if err := Install(e.update, p, "1.3.0", sum(program(t, "1.3.0"))); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.version(); got != "1.3.0" {
		t.Fatalf("version %s", got)
	}
	pushed, _ := State(e.update)
	if pushed == nil || pushed.Version != "1.3.0" || !pushed.HasPrevious || pushed.PreviousVersion != "1.2.0" {
		t.Fatalf("state = %+v", pushed)
	}
	// Roll back to the previous program by installing it again (a forced push).
	MarkHealthy(e.update)
	p = stage("1.2.0")
	if err := Install(e.update, p, "1.2.0", sum(program(t, "1.2.0"))); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.version(); got != "1.2.0" {
		t.Fatalf("after rollback: version %s", got)
	}
}
