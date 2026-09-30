package organizer_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/organizer"
)

// fake7z writes a stand-in for the 7z tool. "7z l" prints listing, "7z x"
// runs unpack inside the folder it was asked to unpack into. That lets the
// tests hand the extractor exactly the archives (and the misbehaviour) a
// hostile release could produce, without needing 7z on the test machine.
func fake7z(t *testing.T, listing, unpack string) *organizer.Extractor {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in 7z is a shell script")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "listing.txt"), []byte(listing))
	writeTestFile(t, filepath.Join(dir, "unpack.sh"), []byte(unpack))
	script := `#!/bin/sh
here="$(dirname "$0")"
case "$1" in
  l) cat "$here/listing.txt" ;;
  x) for a in "$@"; do case "$a" in -o*) out="${a#-o}";; esac; done
     cd "$out" && sh "$here/unpack.sh" ;;
esac
`
	bin := filepath.Join(dir, "7z")
	writeTestFile(t, bin, []byte(script))
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	return &organizer.Extractor{BinaryPath: bin}
}

// listing7z is what `7z l -slt` prints for the given entries, each
// "path|size|attributes".
func listing7z(entries ...string) string {
	var b strings.Builder
	b.WriteString("\n7-Zip 26.01\n\nListing archive: release.7z\n\n--\nPath = release.7z\nType = 7z\n\n----------\n")
	for _, e := range entries {
		parts := strings.Split(e, "|")
		b.WriteString("Path = " + parts[0] + "\nSize = " + parts[1] + "\nModified = 2026-09-30 09:42:14\nAttributes = " + parts[2] + "\nEncrypted = -\n\n")
	}
	return b.String()
}

func fresh7zDest(t *testing.T) (archive, dest string) {
	t.Helper()
	root := t.TempDir()
	archive = filepath.Join(root, "release.7z")
	writeTestFile(t, archive, []byte("fake"))
	return archive, filepath.Join(root, "dest")
}

func entriesIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

func TestExtract7zRefusesHostileArchives(t *testing.T) {
	tests := []struct {
		name    string
		listing string
		unpack  string
		want    error
	}{
		{
			name:    "a link named in the listing",
			listing: listing7z("Movie.en.srt|11|A lrwxrwxrwx"),
			unpack:  "touch SHOULD-NOT-RUN",
			want:    organizer.ErrUnsafeArchive,
		},
		{
			name:    "a link written by an older 7z",
			listing: "----------\n" + "Path = evil.srt\nSize = 5\nSymbolic Link = /etc/passwd\n\n",
			unpack:  "touch SHOULD-NOT-RUN",
			want:    organizer.ErrUnsafeArchive,
		},
		{
			name:    "a device file",
			listing: listing7z("dev|0|A crw-rw-rw-"),
			unpack:  "touch SHOULD-NOT-RUN",
			want:    organizer.ErrUnsafeArchive,
		},
		{
			name:    "a path that climbs out",
			listing: listing7z("../../config/app.db|10|A -rw-r--r--"),
			unpack:  "touch SHOULD-NOT-RUN",
			want:    organizer.ErrUnsafeArchive,
		},
		{
			name:    "an absolute path",
			listing: listing7z("/etc/cron.d/x|10|A -rw-r--r--"),
			unpack:  "touch SHOULD-NOT-RUN",
			want:    organizer.ErrUnsafeArchive,
		},
		{
			name:    "more data than allowed",
			listing: listing7z("big.mkv|5000000000000|A -rw-r--r--"),
			unpack:  "touch SHOULD-NOT-RUN",
			want:    organizer.ErrArchiveTooLarge,
		},
		{
			name:    "encrypted files",
			listing: strings.Replace(listing7z("secret.mkv|10|A -rw-r--r--"), "Encrypted = -", "Encrypted = +", 1),
			unpack:  "touch SHOULD-NOT-RUN",
			want:    organizer.ErrPasswordProtected,
		},
		{
			name:    "a link the listing did not show",
			listing: listing7z("Movie.en.srt|11|A -rw-r--r--"),
			unpack:  "ln -s /etc/passwd Movie.en.srt",
			want:    organizer.ErrUnsafeArchive,
		},
		{
			name:    "a link to a folder, with a file behind it",
			listing: listing7z("esc|0|D drwxr-xr-x", "esc/app.db|10|A -rw-r--r--"),
			unpack:  "ln -s /tmp esc",
			want:    organizer.ErrUnsafeArchive,
		},
		{
			name:    "a named pipe",
			listing: listing7z("x.mkv|1|A -rw-r--r--"),
			unpack:  "mkfifo x.mkv",
			want:    organizer.ErrUnsafeArchive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := fake7z(t, tt.listing, tt.unpack)
			ex.MaxTotalBytes = 1 << 30
			archive, dest := fresh7zDest(t)
			err := ex.Extract(archive, dest)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if left := entriesIn(t, dest); len(left) != 0 {
				t.Errorf("a refused archive must leave nothing behind, found %v", left)
			}
		})
	}
}

func TestExtract7zUnpacksRegularFiles(t *testing.T) {
	ex := fake7z(t,
		listing7z("Movie (2020)|4|A -rw-r--r--", "Movie (2020)/movie.mkv|4|A -rw-r--r--", "Movie (2020)/sub|0|D drwxr-xr-x", "Movie (2020)/sub/movie.en.srt|3|A -rw-r--r--"),
		"mkdir -p 'Movie (2020)/sub' && printf data > 'Movie (2020)/movie.mkv' && printf srt > 'Movie (2020)/sub/movie.en.srt'")
	archive, dest := fresh7zDest(t)
	if err := ex.Extract(archive, dest); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, filepath.Join(dest, "Movie (2020)", "movie.mkv")); got != "data" {
		t.Errorf("movie.mkv = %q", got)
	}
	if got := readTestFile(t, filepath.Join(dest, "Movie (2020)", "sub", "movie.en.srt")); got != "srt" {
		t.Errorf("movie.en.srt = %q", got)
	}
	for _, e := range entriesIn(t, dest) {
		if strings.Contains(e, ".unpack-7z") {
			t.Errorf("the working folder was left behind: %s", e)
		}
	}
}

// A slow or hung 7z is stopped.
func TestExtract7zStopsAfterTheTimeout(t *testing.T) {
	ex := fake7z(t, listing7z("a.mkv|1|A -rw-r--r--"), "sleep 30")
	ex.SevenZipTimeout = 300 * time.Millisecond
	archive, dest := fresh7zDest(t)
	start := time.Now()
	err := ex.Extract(archive, dest)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("the tool was not stopped: %v", time.Since(start))
	}
}

// With a real 7z, a link stored in the archive is refused.
func TestExtractReal7zRefusesSymlinks(t *testing.T) {
	ex := organizer.NewExtractor()
	if !ex.Available() {
		t.Skip("7z binary not available on this machine")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(src, "movie.mkv"), []byte("movie"))
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "movie.en.srt")); err != nil {
		t.Skip("cannot make a symlink here")
	}
	archive := filepath.Join(dir, "release.7z")
	cmd := exec.Command("7z", "a", "-snl", archive, "movie.mkv", "movie.en.srt")
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the fixture: %v: %s", err, out)
	}
	dest := filepath.Join(dir, "dest")
	err := ex.Extract(archive, dest)
	if !errors.Is(err, organizer.ErrUnsafeArchive) {
		t.Fatalf("err = %v, want ErrUnsafeArchive", err)
	}
	if left := entriesIn(t, dest); len(left) != 0 {
		t.Errorf("found %v", left)
	}
}

// A link that is already in the destination (planted by an earlier archive of
// the same release) is not written through.
func TestExtractDoesNotWriteThroughAnExistingLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	dest := filepath.Join(root, "dest")
	for _, d := range []string{outside, dest} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(dest, "esc")); err != nil {
		t.Skip("cannot make a symlink here")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("esc/app.db")
	w.Write([]byte("overwritten"))
	zw.Close()
	archive := filepath.Join(root, "release.zip")
	writeTestFile(t, archive, buf.Bytes())

	err := organizer.NewExtractor().Extract(archive, dest)
	if !errors.Is(err, organizer.ErrUnsafeArchive) {
		t.Fatalf("err = %v, want ErrUnsafeArchive", err)
	}
	if got := entriesIn(t, outside); len(got) != 0 {
		t.Fatalf("a file was written outside the destination: %v", got)
	}
}
