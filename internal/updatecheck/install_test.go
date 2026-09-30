package updatecheck

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

func targz(t *testing.T, files map[string][]byte, types map[string]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		typ := byte(tar.TypeReg)
		if v, ok := types[name]; ok {
			typ = v
		}
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: typ}
		if typ == tar.TypeSymlink {
			hdr.Size = 0
			hdr.Linkname = "/etc/passwd"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			tw.Write(data)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// release builds the assets a real release has, signed with priv.
type fakeRelease struct {
	sums, sig, archive []byte
}

func makeRelease(t *testing.T, version string, priv ed25519.PrivateKey, program []byte) fakeRelease {
	t.Helper()
	name := ArchiveName(version, "linux", "amd64")
	archive := targz(t, map[string][]byte{
		"mediarium_" + version + "_linux_amd64/LICENSE":   []byte("licence"),
		"mediarium_" + version + "_linux_amd64/mediarium": program,
	}, nil)
	sum := sha256.Sum256(archive)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n" + strings.Repeat("0", 64) + "  other.zip\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums))
	return fakeRelease{sums: sums, sig: []byte(sig), archive: archive}
}

// fakeDownloads serves /rdborg/Mediarium/releases/download/<tag>/<file>.
type fakeDownloads struct {
	srv   *httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	paths []string
}

func newFakeDownloads(t *testing.T, tag string, r fakeRelease, version string) *fakeDownloads {
	d := &fakeDownloads{files: map[string][]byte{
		SumsName:                               r.sums,
		SigName:                                r.sig,
		ArchiveName(version, "linux", "amd64"): r.archive,
	}}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.paths = append(d.paths, req.URL.Path)
		prefix := "/rdborg/Mediarium/releases/download/" + tag + "/"
		if !strings.HasPrefix(req.URL.Path, prefix) {
			http.NotFound(w, req)
			return
		}
		data, ok := d.files[strings.TrimPrefix(req.URL.Path, prefix)]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func testFetcher(d *fakeDownloads) *Fetcher {
	f := &Fetcher{Repo: "rdborg/Mediarium", Base: d.srv.URL, Version: "1.1.0"}
	f.setHosts([]string{"127.0.0.1"}, true)
	return f
}

func releaseWithAssets(version string, names ...string) Release {
	rel := Release{Version: version, Tag: "v" + version}
	for _, n := range names {
		rel.Assets = append(rel.Assets, Asset{Name: n})
	}
	return rel
}

func fullRelease(version string) Release {
	return releaseWithAssets(version, SumsName, SigName, ArchiveName(version, "linux", "amd64"))
}

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestFetchHappyPath(t *testing.T) {
	pub, priv := keys(t)
	program := []byte("#!/bin/sh\necho a program\n")
	d := newFakeDownloads(t, "v1.2.0", makeRelease(t, "1.2.0", priv, program), "1.2.0")
	dir := t.TempDir()
	got, err := testFetcher(d).Fetch(context.Background(), fullRelease("1.2.0"), "linux", "amd64", pub, dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	data, err := os.ReadFile(got.Path)
	if err != nil || !bytes.Equal(data, program) {
		t.Fatalf("program file = %q, %v", data, err)
	}
	sum := sha256.Sum256(program)
	if got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("SHA256 = %s", got.SHA256)
	}
	if fi, _ := os.Stat(got.Path); fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("program is not executable: %v", fi.Mode())
	}
	// Only the archive and the program are left behind in the folder.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d files left in the folder, want 1 (the program)", len(entries))
	}
}

func TestFetchRefusals(t *testing.T) {
	pub, priv := keys(t)
	otherPub, otherPriv := keys(t)
	program := []byte("program")
	good := makeRelease(t, "1.2.0", priv, program)

	tampered := good
	tampered.archive = append(append([]byte(nil), good.archive...), 0)
	badSig := good
	badSig.sig = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(otherPriv, good.sums)))
	tamperedSums := good
	tamperedSums.sums = append([]byte("0000  something\n"), good.sums...)
	junkSig := good
	junkSig.sig = []byte("not base64 !!")
	shortSig := good
	shortSig.sig = []byte(base64.StdEncoding.EncodeToString([]byte("short")))

	cases := []struct {
		name string
		r    fakeRelease
		pub  ed25519.PublicKey
		rel  Release
		want error
	}{
		{"archive changed after signing", tampered, pub, fullRelease("1.2.0"), ErrBadChecksum},
		{"signed by another key", badSig, pub, fullRelease("1.2.0"), ErrBadSignature},
		{"good release, wrong public key", good, otherPub, fullRelease("1.2.0"), ErrBadSignature},
		{"checksum list changed after signing", tamperedSums, pub, fullRelease("1.2.0"), ErrBadSignature},
		{"signature is not base64", junkSig, pub, fullRelease("1.2.0"), ErrBadSignature},
		{"signature is too short", shortSig, pub, fullRelease("1.2.0"), ErrBadSignature},
		{"no signature file on the release", good, pub, releaseWithAssets("1.2.0", SumsName, ArchiveName("1.2.0", "linux", "amd64")), ErrNoSignature},
		{"no checksum file on the release", good, pub, releaseWithAssets("1.2.0", SigName, ArchiveName("1.2.0", "linux", "amd64")), ErrNoSignature},
		{"no program file for this system", good, pub, releaseWithAssets("1.2.0", SumsName, SigName), ErrNoArchive},
		{"no public key built in", good, nil, fullRelease("1.2.0"), ErrKeyMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDownloads(t, "v1.2.0", tc.r, "1.2.0")
			dir := t.TempDir()
			_, err := testFetcher(d).Fetch(context.Background(), tc.rel, "linux", "amd64", tc.pub, dir)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("a failed download left %d files behind", len(entries))
			}
		})
	}
}

func TestFetchArchiveNotInSignedList(t *testing.T) {
	pub, priv := keys(t)
	r := makeRelease(t, "1.2.0", priv, []byte("p"))
	r.sums = []byte(strings.Repeat("a", 64) + "  other.tar.gz\n")
	r.sig = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, r.sums)))
	d := newFakeDownloads(t, "v1.2.0", r, "1.2.0")
	_, err := testFetcher(d).Fetch(context.Background(), fullRelease("1.2.0"), "linux", "amd64", pub, t.TempDir())
	if !errors.Is(err, ErrNoArchive) {
		t.Fatalf("error = %v", err)
	}
}

func TestFetchNeverAsksForUnsignedFilesFirst(t *testing.T) {
	// The archive is downloaded only after the signature has been checked.
	pub, _ := keys(t)
	_, priv := keys(t) // signed by someone else
	d := newFakeDownloads(t, "v1.2.0", makeRelease(t, "1.2.0", priv, []byte("p")), "1.2.0")
	_, err := testFetcher(d).Fetch(context.Background(), fullRelease("1.2.0"), "linux", "amd64", pub, t.TempDir())
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("error = %v", err)
	}
	for _, p := range d.paths {
		if strings.HasSuffix(p, ".tar.gz") {
			t.Fatalf("the archive was downloaded although the signature was bad: %v", d.paths)
		}
	}
}

func TestDownloadsOnlyFromOfficialRepo(t *testing.T) {
	pub, priv := keys(t)
	d := newFakeDownloads(t, "v1.2.0", makeRelease(t, "1.2.0", priv, []byte("p")), "1.2.0")
	if _, err := testFetcher(d).Fetch(context.Background(), fullRelease("1.2.0"), "linux", "amd64", pub, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, p := range d.paths {
		if !strings.HasPrefix(p, "/rdborg/Mediarium/releases/download/v1.2.0/") {
			t.Errorf("asked for %s", p)
		}
	}
}

func TestRedirectToOtherHostIsRefused(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("evil")) }))
	defer evil.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(evil.URL, "127.0.0.1", "localhost", 1)+"/x", http.StatusFound)
	}))
	defer origin.Close()
	f := &Fetcher{Repo: "rdborg/Mediarium", Base: origin.URL, Version: "1.1.0"}
	f.setHosts([]string{"127.0.0.1"}, true)
	pub, _ := keys(t)
	_, err := f.Fetch(context.Background(), fullRelease("1.2.0"), "linux", "amd64", pub, t.TempDir())
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("error = %v", err)
	}
}

func TestRedirectWithinAllowedHostsWorks(t *testing.T) {
	pub, priv := keys(t)
	real := newFakeDownloads(t, "v1.2.0", makeRelease(t, "1.2.0", priv, []byte("p")), "1.2.0")
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, real.srv.URL+r.URL.Path, http.StatusFound)
	}))
	defer origin.Close()
	f := &Fetcher{Repo: "rdborg/Mediarium", Base: origin.URL, Version: "1.1.0"}
	f.setHosts([]string{"127.0.0.1"}, true)
	if _, err := f.Fetch(context.Background(), fullRelease("1.2.0"), "linux", "amd64", pub, t.TempDir()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestHTTPIsRefusedInProduction(t *testing.T) {
	f := NewFetcher("1.1.0")
	u := mustURL(t, "http://github.com/x")
	if err := f.checkURL(u); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("http github.com: %v", err)
	}
	for _, host := range []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"} {
		if err := f.checkURL(mustURL(t, "https://"+host+"/x")); err != nil {
			t.Errorf("%s refused: %v", host, err)
		}
	}
	for _, host := range []string{"evil.example", "github.com.evil.example", "githubusercontent.com", "api.github.com", "169.254.169.254"} {
		if err := f.checkURL(mustURL(t, "https://"+host+"/x")); !errors.Is(err, ErrHostNotAllowed) {
			t.Errorf("%s allowed", host)
		}
	}
}

func TestOversizeDownloadsAreCut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), 2<<20))
	}))
	defer srv.Close()
	f := &Fetcher{Repo: "rdborg/Mediarium", Base: srv.URL, Version: "1.1.0"}
	f.setHosts([]string{"127.0.0.1"}, true)
	_, err := f.getBytes(context.Background(), "v1.2.0", SumsName, maxSums)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("error = %v", err)
	}
}

func TestExtractProgramIsCareful(t *testing.T) {
	prog := []byte("the program")
	cases := []struct {
		name  string
		files map[string][]byte
		types map[string]byte
		want  bool
	}{
		{"in a folder", map[string][]byte{"mediarium_1.2.0_linux_amd64/mediarium": prog}, nil, true},
		{"at the top", map[string][]byte{"mediarium": prog}, nil, true},
		{"with dot slash", map[string][]byte{"./mediarium": prog}, nil, true},
		{"a symlink is not a program", map[string][]byte{"x/mediarium": nil}, map[string]byte{"x/mediarium": tar.TypeSymlink}, false},
		{"too deep", map[string][]byte{"a/b/c/mediarium": prog}, nil, false},
		{"escaping path", map[string][]byte{"../mediarium": prog}, nil, false},
		{"absolute path", map[string][]byte{"/tmp/mediarium": prog}, nil, false},
		{"other names", map[string][]byte{"x/mediarium.exe": prog, "x/README": prog}, nil, false},
		{"empty file", map[string][]byte{"x/mediarium": {}}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			arc := dir + "/a.tar.gz"
			os.WriteFile(arc, targz(t, tc.files, tc.types), 0o644)
			out := t.TempDir()
			got, err := ExtractProgram(arc, out)
			if !tc.want {
				if !errors.Is(err, ErrNoProgram) {
					t.Fatalf("error = %v, want ErrNoProgram", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractProgram: %v", err)
			}
			data, _ := os.ReadFile(got.Path)
			if !bytes.Equal(data, prog) {
				t.Fatalf("program = %q", data)
			}
		})
	}
	t.Run("not a gzip file", func(t *testing.T) {
		arc := t.TempDir() + "/a.tar.gz"
		os.WriteFile(arc, []byte("hello"), 0o644)
		if _, err := ExtractProgram(arc, t.TempDir()); !errors.Is(err, ErrNoProgram) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestSumFor(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("B", 64)
	sums := []byte(a + "  one.tar.gz\n" + b + " *two.zip\n" + "garbage line\n" + strings.Repeat("c", 63) + "  short.zip\n" + strings.Repeat("z", 64) + "  nothex.zip\n")
	cases := []struct {
		name string
		want string
		err  error
	}{
		{"one.tar.gz", a, nil},
		{"two.zip", strings.ToLower(b), nil},
		{"short.zip", "", ErrBadChecksum},
		{"nothex.zip", "", ErrBadChecksum},
		{"missing.zip", "", ErrNoArchive},
	}
	for _, tc := range cases {
		got, err := SumFor(sums, tc.name)
		if !errors.Is(err, tc.err) || got != tc.want {
			t.Errorf("SumFor(%s) = %q, %v; want %q, %v", tc.name, got, err, tc.want, tc.err)
		}
	}
}

func TestInstallable(t *testing.T) {
	cases := []struct {
		name string
		rel  Release
		goos string
		ok   bool
		part string
	}{
		{"complete", fullRelease("1.2.0"), "linux", true, ""},
		{"unsigned", releaseWithAssets("1.2.0", SumsName, ArchiveName("1.2.0", "linux", "amd64")), "linux", false, "isn't signed"},
		{"no program for this system", releaseWithAssets("1.2.0", SumsName, SigName), "linux", false, "no program file"},
		{"windows installs by hand", fullRelease("1.2.0"), "windows", false, "Docker image"},
		{"no assets", Release{Version: "1.2.0"}, "linux", false, "isn't signed"},
	}
	for _, tc := range cases {
		ok, why := Installable(tc.rel, tc.goos, "amd64")
		if ok != tc.ok || (!ok && !strings.Contains(why, tc.part)) {
			t.Errorf("%s: Installable = %v, %q", tc.name, ok, why)
		}
	}
}

func TestParsePublicKey(t *testing.T) {
	pub, _ := keys(t)
	good := base64.StdEncoding.EncodeToString(pub)
	if got, err := ParsePublicKey(" " + good + "\n"); err != nil || !bytes.Equal(got, pub) {
		t.Fatalf("good key: %v", err)
	}
	for _, bad := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("short")), base64.StdEncoding.EncodeToString(make([]byte, 64))} {
		if _, err := ParsePublicKey(bad); !errors.Is(err, ErrKeyMissing) {
			t.Errorf("ParsePublicKey(%q) = %v", bad, err)
		}
	}
	// The key the project ships is a valid ed25519 key.
	if _, err := ParsePublicKey("HM7xZn1frF+sxwYVWD/tyt+53pKr3vMtQ813g0Ccn6M="); err != nil {
		t.Errorf("the shipped key does not parse: %v", err)
	}
}

func TestArchiveName(t *testing.T) {
	for _, c := range [][4]string{
		{"1.2.0", "linux", "amd64", "mediarium_1.2.0_linux_amd64.tar.gz"},
		{"1.2.0-rc.1", "linux", "arm64", "mediarium_1.2.0-rc.1_linux_arm64.tar.gz"},
		{"1.2.0", "windows", "amd64", "mediarium_1.2.0_windows_amd64.zip"},
	} {
		if got := ArchiveName(c[0], c[1], c[2]); got != c[3] {
			t.Errorf("ArchiveName = %q, want %q", got, c[3])
		}
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

var _ = fmt.Sprint
