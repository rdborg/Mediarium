package api_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/updatecheck"
)

// releaseKit is one published release: the signed files GitHub would serve.
type releaseKit struct {
	version string
	files   map[string][]byte
	assets  []string // names listed in the release, in the release list
}

func archiveNameFor(version string) string {
	return updatecheck.ArchiveName(version, runtime.GOOS, runtime.GOARCH)
}

func tarGz(t *testing.T, program []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "mediarium_x/LICENSE", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg})
	tw.Write([]byte("MIT"))
	tw.WriteHeader(&tar.Header{Name: "mediarium_x/mediarium", Mode: 0o755, Size: int64(len(program)), Typeflag: tar.TypeReg})
	tw.Write(program)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// newRelease packs program as release version, signed by priv.
func newRelease(t *testing.T, priv ed25519.PrivateKey, version string, program []byte) *releaseKit {
	t.Helper()
	archive := tarGz(t, program)
	sums := []byte(sumOf(archive) + "  " + archiveNameFor(version) + "\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)))
	return &releaseKit{
		version: version,
		files:   map[string][]byte{updatecheck.SumsName: sums, updatecheck.SigName: sig, archiveNameFor(version): archive},
		assets:  []string{updatecheck.SumsName, updatecheck.SigName, archiveNameFor(version)},
	}
}

func (k *releaseKit) listJSON() string {
	var assets []map[string]any
	for _, n := range k.assets {
		assets = append(assets, map[string]any{"name": n, "size": 100})
	}
	b, _ := json.Marshal(map[string]any{"tag_name": "v" + k.version, "name": "Mediarium " + k.version, "body": "notes", "assets": assets, "published_at": "2026-10-01T10:00:00Z"})
	return "[" + string(b) + "]"
}

// installEnv is an updateEnv wired to a fake GitHub (API and file downloads)
// and a signing key.
type installEnv struct {
	*updateEnv
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	gh   *fakeGitHub
	dl   *httptest.Server
	kit  *releaseKit
}

func newInstallEnv(t *testing.T, kitFor func(priv ed25519.PrivateKey) *releaseKit) *installEnv {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e := &installEnv{updateEnv: newUpdateEnv(t), pub: pub, priv: priv}
	e.kit = kitFor(priv)
	e.gh = newFakeGitHub(t, e.kit.listJSON())
	e.dl = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/rdborg/Mediarium/releases/download/v" + e.kit.version + "/"
		name := strings.TrimPrefix(r.URL.Path, prefix)
		data, ok := e.kit.files[name]
		if !strings.HasPrefix(r.URL.Path, prefix) || !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(e.dl.Close)
	e.server.TestSetGitHub(e.gh.srv.URL)
	e.server.TestSetFetcher(updatecheck.NewFetcherForTest(e.dl.URL, "127.0.0.1"))
	if err := e.server.SetUpdatePublicKey(base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	return e
}

// job polls the install until it is no longer "working" and returns it.
func (e *installEnv) waitJob(t *testing.T, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		n := getJSON[map[string]any](t, e.client, e.base+"/api/system/update/install")
		if job, _ := n["job"].(map[string]any); job != nil && (job["state"] == want || job["state"] == "failed" || job["state"] == "restarting") {
			if job["state"] != want {
				t.Fatalf("job ended as %v, want %s: %v", job["state"], want, job)
			}
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the job never reached %s", want)
	return nil
}

func TestUpdateNowInstallsASignedRelease(t *testing.T) {
	program := realProgram(t, "9.9.9")
	e := newInstallEnv(t, func(priv ed25519.PrivateKey) *releaseKit { return newRelease(t, priv, "9.9.9", program) })

	n := check(t, e.updateEnv)
	if n["available"] != true || n["canInstall"] != true {
		t.Fatalf("notice = %v", n)
	}
	out := postJSON[map[string]any](t, e.client, e.base+"/api/system/update/install", nil, http.StatusAccepted)
	if job, _ := out["job"].(map[string]any); job == nil {
		t.Fatalf("no job in %v", out)
	}
	e.waitJob(t, "restarting")
	e.mustExit(t)

	got, err := os.ReadFile(filepath.Join(e.dir, "app"))
	if err != nil || !bytes.Equal(got, program) {
		t.Fatalf("the installed program is not the released one: %v", err)
	}
	if v, _ := os.ReadFile(filepath.Join(e.dir, "VERSION")); strings.TrimSpace(string(v)) != "9.9.9" {
		t.Fatalf("VERSION = %q", v)
	}
	entries, _ := os.ReadDir(e.dir)
	for _, f := range entries {
		if strings.HasPrefix(f.Name(), ".release") {
			t.Errorf("leftover %s", f.Name())
		}
	}
	found := false
	for _, a := range getJSON[[]map[string]any](t, e.client, e.base+"/api/activity") {
		if a["eventType"] == "update" && strings.Contains(fmt.Sprint(a["message"]), "9.9.9") && strings.Contains(fmt.Sprint(a["message"]), "signed release") {
			found = true
		}
	}
	if !found {
		t.Fatal("the install is not on the Activity page")
	}
	// It did not need the "allow pushed updates" switch.
	if e.state(t)["allowPush"] != false {
		t.Fatal("installing a signed release must not depend on the push switch")
	}
}

func TestUpdateNowRefusals(t *testing.T) {
	program := realProgram(t, "9.9.9")
	older := realProgram(t, "9.9.8")
	other := func(t *testing.T) ed25519.PrivateKey {
		_, k, _ := ed25519.GenerateKey(rand.Reader)
		return k
	}

	cases := []struct {
		name string
		kit  func(t *testing.T, priv ed25519.PrivateKey) *releaseKit
		// what should happen
		notOffered string // text in installNote: the button is not offered at all
		failsWith  string // text in the job error: it starts and then fails
		tweak      func(e *installEnv)
	}{
		{"no signature file", func(t *testing.T, priv ed25519.PrivateKey) *releaseKit {
			k := newRelease(t, priv, "9.9.9", program)
			k.assets = []string{updatecheck.SumsName, archiveNameFor("9.9.9")}
			delete(k.files, updatecheck.SigName)
			return k
		}, "isn't signed", "", nil},
		{"no program for this system", func(t *testing.T, priv ed25519.PrivateKey) *releaseKit {
			k := newRelease(t, priv, "9.9.9", program)
			k.assets = []string{updatecheck.SumsName, updatecheck.SigName}
			return k
		}, "no program file", "", nil},
		{"signed by another key", func(t *testing.T, _ ed25519.PrivateKey) *releaseKit {
			return newRelease(t, other(t), "9.9.9", program)
		}, "", "signature isn't valid", nil},
		{"archive changed after signing", func(t *testing.T, priv ed25519.PrivateKey) *releaseKit {
			k := newRelease(t, priv, "9.9.9", program)
			k.files[archiveNameFor("9.9.9")] = append(k.files[archiveNameFor("9.9.9")], 1)
			return k
		}, "", "doesn't match its signed checksum", nil},
		{"the program is another version than the release says", func(t *testing.T, priv ed25519.PrivateKey) *releaseKit {
			return newRelease(t, priv, "9.9.9", older)
		}, "", "says it is version 9.9.8, not 9.9.9", nil},
		{"the archive holds no program", func(t *testing.T, priv ed25519.PrivateKey) *releaseKit {
			k := newRelease(t, priv, "9.9.9", program)
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			tw := tar.NewWriter(gz)
			tw.WriteHeader(&tar.Header{Name: "readme", Size: 1, Mode: 0o644, Typeflag: tar.TypeReg})
			tw.Write([]byte("x"))
			tw.Close()
			gz.Close()
			k.files[archiveNameFor("9.9.9")] = buf.Bytes()
			k.files[updatecheck.SumsName] = []byte(sumOf(buf.Bytes()) + "  " + archiveNameFor("9.9.9") + "\n")
			k.files[updatecheck.SigName] = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, k.files[updatecheck.SumsName])))
			return k
		}, "", "doesn't contain Mediarium", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newInstallEnv(t, func(priv ed25519.PrivateKey) *releaseKit { return tc.kit(t, priv) })
			n := check(t, e.updateEnv)
			if tc.notOffered != "" {
				if n["canInstall"] != false || !strings.Contains(fmt.Sprint(n["installNote"]), tc.notOffered) {
					t.Fatalf("notice = %v", n)
				}
				code, body := doStatus(t, e.client, http.MethodPost, e.base+"/api/system/update/install")
				if code != http.StatusConflict || !strings.Contains(message(body), tc.notOffered) {
					t.Fatalf("POST install: %d %v", code, body)
				}
			} else {
				postJSON[map[string]any](t, e.client, e.base+"/api/system/update/install", nil, http.StatusAccepted)
				deadline := time.Now().Add(20 * time.Second)
				var job map[string]any
				for time.Now().Before(deadline) {
					job, _ = getJSON[map[string]any](t, e.client, e.base+"/api/system/update/install")["job"].(map[string]any)
					if job != nil && job["state"] == "failed" {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
				if job == nil || job["state"] != "failed" || !strings.Contains(fmt.Sprint(job["error"]), tc.failsWith) {
					t.Fatalf("job = %v, want a failure containing %q", job, tc.failsWith)
				}
			}
			e.mustNotExit(t)
			e.nothingInstalled(t)
		})
	}
}

func TestUpdateNowNeedsTheKeyTheImageAndANewerVersion(t *testing.T) {
	program := realProgram(t, "9.9.9")
	mk := func(priv ed25519.PrivateKey) *releaseKit { return newRelease(t, priv, "9.9.9", program) }

	t.Run("no update key built in", func(t *testing.T) {
		e := newUpdateEnv(t)
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		kit := mk(priv)
		gh := newFakeGitHub(t, kit.listJSON())
		e.server.TestSetGitHub(gh.srv.URL)
		if err := e.server.SetUpdatePublicKey(""); err != nil {
			t.Fatal(err)
		}
		n := check(t, e)
		if n["canInstall"] != false || !strings.Contains(fmt.Sprint(n["installNote"]), "no update key") {
			t.Fatalf("notice = %v", n)
		}
		if code, _ := doStatus(t, e.client, http.MethodPost, e.base+"/api/system/update/install"); code != http.StatusConflict {
			t.Fatalf("POST install: %d", code)
		}
	})
	t.Run("a bad key is refused at start", func(t *testing.T) {
		e := newUpdateEnv(t)
		if err := e.server.SetUpdatePublicKey("not a key"); err == nil {
			t.Fatal("a bad key was accepted")
		}
	})
	t.Run("not started by the image", func(t *testing.T) {
		e := newInstallEnv(t, mk)
		e.server.TestSetUpdateEnv("1.1.0", "")
		check(t, e.updateEnv)
		code, body := doStatus(t, e.client, http.MethodPost, e.base+"/api/system/update/install")
		if code != http.StatusConflict || !strings.Contains(message(body), "Docker image") {
			t.Fatalf("%d %v", code, body)
		}
	})
	t.Run("nothing newer", func(t *testing.T) {
		e := newInstallEnv(t, mk)
		e.server.TestSetUpdateEnv("9.9.9", "1.1.0")
		e.server.TestSetGitHub(e.gh.srv.URL)
		check(t, e.updateEnv)
		code, body := doStatus(t, e.client, http.MethodPost, e.base+"/api/system/update/install")
		if code != http.StatusConflict || !strings.Contains(message(body), "no newer version") {
			t.Fatalf("%d %v", code, body)
		}
	})
	t.Run("one at a time", func(t *testing.T) {
		e := newInstallEnv(t, mk)
		check(t, e.updateEnv)
		release := e.server.TestHoldUpdateLock()
		code, body := doStatus(t, e.client, http.MethodPost, e.base+"/api/system/update/install")
		release()
		if code != http.StatusConflict || !strings.Contains(message(body), "already in progress") {
			t.Fatalf("%d %v", code, body)
		}
	})
}

func TestOvernightInstall(t *testing.T) {
	program := realProgram(t, "9.9.9")
	mk := func(priv ed25519.PrivateKey) *releaseKit { return newRelease(t, priv, "9.9.9", program) }
	night := time.Date(2026, 10, 2, 3, 0, 0, 0, time.Local)
	day := time.Date(2026, 10, 2, 14, 0, 0, 0, time.Local)

	t.Run("off by default", func(t *testing.T) {
		e := newInstallEnv(t, mk)
		e.server.TestUpdateTick(t.Context(), night)
		e.mustNotExit(t)
		e.nothingInstalled(t)
	})
	t.Run("on: installs at night, not by day", func(t *testing.T) {
		e := newInstallEnv(t, mk)
		postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/system/options", map[string]any{"autoInstall": true}, http.StatusOK)
		e.server.TestUpdateTick(t.Context(), day) // checks, finds it, but it is daytime
		e.mustNotExit(t)
		e.nothingInstalled(t)
		e.server.TestUpdateTick(t.Context(), night)
		e.waitJob(t, "restarting")
		e.mustExit(t)
		if b, _ := os.ReadFile(filepath.Join(e.dir, "app")); !bytes.Equal(b, program) {
			t.Fatal("the release was not installed")
		}
		found := false
		for _, a := range getJSON[[]map[string]any](t, e.client, e.base+"/api/activity") {
			if strings.Contains(fmt.Sprint(a["message"]), "automatic update") {
				found = true
			}
		}
		if !found {
			t.Fatal("the automatic install is not on the Activity page")
		}
	})
	t.Run("on: a release that fails is tried once", func(t *testing.T) {
		e := newInstallEnv(t, func(priv ed25519.PrivateKey) *releaseKit {
			_, wrong, _ := ed25519.GenerateKey(rand.Reader)
			return newRelease(t, wrong, "9.9.9", program)
		})
		postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/system/options", map[string]any{"autoInstall": true}, http.StatusOK)
		e.server.TestUpdateTick(t.Context(), night)
		e.waitJob(t, "failed")
		e.server.TestUpdateTick(t.Context(), night.Add(time.Hour))
		time.Sleep(200 * time.Millisecond)
		e.mustNotExit(t)
		e.nothingInstalled(t)
	})
}
