package organizer_test

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanborg/mediarium/internal/organizer"
)

// The rardecode module ships no .rar fixtures and no RAR compressor is
// available offline, so the RAR archives below are built in code: they are
// genuine RAR 4.x archives using the "stored" (method 0x30, no compression)
// method, with correct block CRCs and file CRCs, decoded by the real
// rardecode reader. They cover the container logic (volumes, paths, flags);
// the compression codecs themselves are rardecode's own responsibility.

type rarEntry struct {
	name      string
	data      []byte
	dir       bool
	attr      uint32
	encrypted bool
}

func le16(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
func le32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }

// rarBlock builds one RAR 4 block: crc16, type, flags, size, then body.
func rarBlock(typ byte, flags uint16, body []byte) []byte {
	h := make([]byte, 7, 7+len(body))
	h[2] = typ
	le16(h[3:], flags)
	le16(h[5:], uint16(7+len(body)))
	h = append(h, body...)
	le16(h[0:], uint16(crc32.ChecksumIEEE(h[2:])))
	return h
}

const (
	rarSplitBefore = 0x0001
	rarSplitAfter  = 0x0002
)

// rarFileBlock builds a file header plus its (stored) data chunk. fullData is
// the whole file (its CRC goes in every part), chunk is this volume's slice.
func rarFileBlock(e rarEntry, fullData, chunk []byte, split uint16) []byte {
	flags := uint16(0x8000) | split
	name := []byte(e.name)
	if e.dir {
		flags |= 0xe0
	}
	if e.encrypted {
		flags |= 0x04 | 0x0400
	}
	body := make([]byte, 25, 25+len(name)+8)
	le32(body[0:], uint32(len(chunk)))    // packed size
	le32(body[4:], uint32(len(fullData))) // unpacked size
	body[8] = 3                           // host OS: unix
	le32(body[9:], crc32.ChecksumIEEE(fullData))
	le32(body[13:], 0) // dos time
	body[17] = 20      // unpack version
	body[18] = 0x30    // method: store
	le16(body[19:], uint16(len(name)))
	le32(body[21:], e.attr)
	if e.encrypted {
		body = append(body, 1, 2, 3, 4, 5, 6, 7, 8) // salt
	}
	body = append(body, name...)
	return append(rarBlock(0x74, flags, body), chunk...)
}

func rarVolume(arcFlags uint16, files [][]byte, notLast bool) []byte {
	out := []byte("Rar!\x1a\x07\x00")
	out = append(out, rarBlock(0x73, arcFlags, make([]byte, 6))...)
	for _, f := range files {
		out = append(out, f...)
	}
	endFlags := uint16(0x4000)
	if notLast {
		endFlags |= 0x0001
	}
	return append(out, rarBlock(0x7b, endFlags, nil)...)
}

// rarSingle builds a one-volume archive holding entries.
func rarSingle(entries ...rarEntry) []byte {
	var blocks [][]byte
	for _, e := range entries {
		blocks = append(blocks, rarFileBlock(e, e.data, e.data, 0))
	}
	return rarVolume(0x0000, blocks, false)
}

// rarSplit builds one file spread over len(names) volumes and writes them.
func rarSplit(t *testing.T, dir string, names []string, newNaming bool, e rarEntry) {
	t.Helper()
	n := len(names)
	chunk := (len(e.data) + n - 1) / n
	for i, name := range names {
		lo, hi := i*chunk, (i+1)*chunk
		if hi > len(e.data) {
			hi = len(e.data)
		}
		var split uint16
		if i > 0 {
			split |= rarSplitBefore
		}
		if i < n-1 {
			split |= rarSplitAfter
		}
		arc := uint16(0x0001) // volume
		if newNaming {
			arc |= 0x0010
		}
		if i == 0 {
			arc |= 0x0100
		}
		vol := rarVolume(arc, [][]byte{rarFileBlock(e, e.data, e.data[lo:hi], split)}, i < n-1)
		writeTestFile(t, filepath.Join(dir, name), vol)
	}
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func assertEscapedNothing(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "dest" && e.Name() != "release.rar" && e.Name() != "release.zip" {
			t.Errorf("unexpected file outside destination: %s", e.Name())
		}
	}
}

func TestExtractRARSingleVolume(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "release.rar")
	writeTestFile(t, archive, rarSingle(
		rarEntry{name: "Movie.2001", dir: true, attr: 0x10},
		// RAR 4 writes Windows-style separators.
		rarEntry{name: `Movie.2001\Sample\sample.mkv`, data: []byte("sample bytes")},
		rarEntry{name: "Movie.2001/movie.mkv", data: bytes.Repeat([]byte("movie"), 1000)},
	))

	dest := filepath.Join(root, "dest")
	if err := organizer.NewExtractor().Extract(archive, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got := readTestFile(t, filepath.Join(dest, "Movie.2001", "Sample", "sample.mkv")); got != "sample bytes" {
		t.Errorf("sample.mkv = %q", got)
	}
	if got := readTestFile(t, filepath.Join(dest, "Movie.2001", "movie.mkv")); got != strings.Repeat("movie", 1000) {
		t.Errorf("movie.mkv has wrong content (len %d)", len(got))
	}
}

func TestExtractRARMultiVolume(t *testing.T) {
	payload := []byte(strings.Repeat("0123456789abcdef", 3000))
	tests := []struct {
		name      string
		volumes   []string
		newNaming bool
		primary   string
	}{
		{"new style part01", []string{"movie.part01.rar", "movie.part02.rar", "movie.part03.rar"}, true, "movie.part01.rar"},
		{"new style part1", []string{"movie.part1.rar", "movie.part2.rar"}, true, "movie.part1.rar"},
		{"old style r00", []string{"movie.rar", "movie.r00", "movie.r01"}, false, "movie.rar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			rarSplit(t, dir, tt.volumes, tt.newNaming, rarEntry{name: "movie.mkv", data: payload})

			primaries, err := organizer.FindPrimaryArchives(dir)
			if err != nil || len(primaries) != 1 || filepath.Base(primaries[0]) != tt.primary {
				t.Fatalf("primary archives = %v (err %v), want [%s]", primaries, err, tt.primary)
			}

			dest := filepath.Join(dir, "out")
			if err := organizer.NewExtractor().Extract(primaries[0], dest); err != nil {
				t.Fatalf("extract: %v", err)
			}
			if got := readTestFile(t, filepath.Join(dest, "movie.mkv")); got != string(payload) {
				t.Fatalf("reassembled file differs (len %d, want %d)", len(got), len(payload))
			}
		})
	}
}

func TestExtractRARMissingVolumeFailsAndLeavesNoPartialFile(t *testing.T) {
	dir := t.TempDir()
	rarSplit(t, dir, []string{"movie.part1.rar", "movie.part2.rar", "movie.part3.rar"}, true,
		rarEntry{name: "movie.mkv", data: bytes.Repeat([]byte("x"), 9000)})
	if err := os.Remove(filepath.Join(dir, "movie.part3.rar")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	err := organizer.NewExtractor().Extract(filepath.Join(dir, "movie.part1.rar"), dest)
	if err == nil {
		t.Fatal("expected an error for a missing volume")
	}
	if _, statErr := os.Stat(filepath.Join(dest, "movie.mkv")); !os.IsNotExist(statErr) {
		t.Errorf("partial file left behind (stat err %v)", statErr)
	}
}

func TestExtractRARPasswordProtected(t *testing.T) {
	root := t.TempDir()

	fileEncrypted := rarSingle(rarEntry{name: "movie.mkv", data: []byte("cipher"), encrypted: true})
	// Archive-level (header) encryption flag 0x0080 on the main header.
	headerEncrypted := rarVolume(0x0080, nil, false)

	for name, data := range map[string][]byte{"file": fileEncrypted, "headers": headerEncrypted} {
		t.Run(name, func(t *testing.T) {
			archive := filepath.Join(root, name+".rar")
			writeTestFile(t, archive, data)
			err := organizer.NewExtractor().Extract(archive, filepath.Join(root, "out-"+name))
			if !errors.Is(err, organizer.ErrPasswordProtected) {
				t.Fatalf("err = %v, want ErrPasswordProtected", err)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "password") {
				t.Errorf("error should mention the password: %v", err)
			}
		})
	}
}

func TestExtractRARCorruptHeader(t *testing.T) {
	root := t.TempDir()
	data := rarSingle(rarEntry{name: "movie.mkv", data: []byte("abc")})
	data[len(data)-30] ^= 0xff // flip a byte inside a block header
	archive := filepath.Join(root, "release.rar")
	writeTestFile(t, archive, data)
	if err := organizer.NewExtractor().Extract(archive, filepath.Join(root, "dest")); err == nil {
		t.Fatal("expected an error for a corrupt archive")
	}
}

func TestExtractRARRefusesUnsafeEntries(t *testing.T) {
	tests := []struct {
		name  string
		entry rarEntry
	}{
		{"parent traversal backslash", rarEntry{name: `..\..\evil.txt`, data: []byte("x")}},
		{"parent traversal slash", rarEntry{name: "../evil.txt", data: []byte("x")}},
		{"nested traversal", rarEntry{name: "a/b/../../../evil.txt", data: []byte("x")}},
		{"absolute unix", rarEntry{name: "/etc/evil.txt", data: []byte("x")}},
		{"absolute windows", rarEntry{name: `C:\evil.txt`, data: []byte("x")}},
		{"symlink absolute", rarEntry{name: "link", data: []byte("/etc/passwd"), attr: 0xA1FF}},
		{"symlink escapes", rarEntry{name: "sub/link", data: []byte("../../outside"), attr: 0xA1FF}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "release.rar")
			writeTestFile(t, archive, rarSingle(tt.entry))
			err := organizer.NewExtractor().Extract(archive, filepath.Join(root, "dest"))
			if !errors.Is(err, organizer.ErrUnsafeArchive) {
				t.Fatalf("err = %v, want ErrUnsafeArchive", err)
			}
			assertEscapedNothing(t, root)
		})
	}
}

func TestExtractRARSafeSymlinkIsSkippedNotCreated(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "release.rar")
	writeTestFile(t, archive, rarSingle(
		rarEntry{name: "movie.mkv", data: []byte("real")},
		rarEntry{name: "alias.mkv", data: []byte("movie.mkv"), attr: 0xA1FF},
	))
	dest := filepath.Join(root, "dest")
	if err := organizer.NewExtractor().Extract(archive, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got := readTestFile(t, filepath.Join(dest, "movie.mkv")); got != "real" {
		t.Errorf("movie.mkv = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dest, "alias.mkv")); !os.IsNotExist(err) {
		t.Errorf("links must not be materialised (lstat err %v)", err)
	}
}

func TestExtractSizeAndSpaceLimits(t *testing.T) {
	tests := []struct {
		name string
		ext  organizer.Extractor
		want error
	}{
		{"single file over the cap", organizer.Extractor{MaxTotalBytes: 50}, organizer.ErrArchiveTooLarge},
		{"cumulative size over the cap", organizer.Extractor{MaxTotalBytes: 150}, organizer.ErrArchiveTooLarge},
		{"disk full", organizer.Extractor{FreeSpace: func(string) (uint64, bool) { return 10, true }}, organizer.ErrInsufficientSpace},
		{"unknown free space is not an error", organizer.Extractor{FreeSpace: func(string) (uint64, bool) { return 0, false }}, nil},
		{"within limits", organizer.Extractor{MaxTotalBytes: 1000}, nil},
	}
	payload := bytes.Repeat([]byte("z"), 100)

	rarArchive := rarSingle(rarEntry{name: "a.bin", data: payload}, rarEntry{name: "b.bin", data: payload})
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	for _, n := range []string{"a.bin", "b.bin"} {
		w, _ := zw.Create(n)
		w.Write(payload)
	}
	zw.Close()

	for _, format := range []struct {
		ext  string
		data []byte
	}{{"rar", rarArchive}, {"zip", zipBuf.Bytes()}} {
		for _, tt := range tests {
			t.Run(format.ext+"/"+tt.name, func(t *testing.T) {
				root := t.TempDir()
				archive := filepath.Join(root, "release."+format.ext)
				writeTestFile(t, archive, format.data)
				ext := tt.ext
				err := ext.Extract(archive, filepath.Join(root, "dest"))
				if tt.want == nil {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					return
				}
				if !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
			})
		}
	}
}

func TestExtractZip(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "release.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("Show.S01/"); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"Show.S01/e01.mkv":       "episode one",
		`Show.S01\sub\e02.mkv`:   "episode two", // Windows-made zips use backslashes
		"Show.S01/notes/nfo.txt": "nfo",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	zw.Close()
	writeTestFile(t, archive, buf.Bytes())

	dest := filepath.Join(root, "dest")
	if err := organizer.NewExtractor().Extract(archive, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for path, want := range map[string]string{
		"Show.S01/e01.mkv":       "episode one",
		"Show.S01/sub/e02.mkv":   "episode two",
		"Show.S01/notes/nfo.txt": "nfo",
	} {
		if got := readTestFile(t, filepath.Join(dest, filepath.FromSlash(path))); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

func TestExtractZipRefusesUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		entry   string
		content string
		symlink bool
	}{
		{"parent traversal", "../evil.txt", "x", false},
		{"nested traversal", "a/../../evil.txt", "x", false},
		{"backslash traversal", `..\evil.txt`, "x", false},
		{"absolute unix", "/tmp/evil.txt", "x", false},
		{"absolute windows", `C:\evil.txt`, "x", false},
		{"symlink absolute", "link", "/etc/passwd", true},
		{"symlink escapes", "a/link", "../../outside", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "release.zip")
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			hdr := &zip.FileHeader{Name: tt.entry, Method: zip.Store}
			if tt.symlink {
				hdr.SetMode(os.ModeSymlink | 0o777)
			}
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				t.Fatal(err)
			}
			w.Write([]byte(tt.content))
			zw.Close()
			writeTestFile(t, archive, buf.Bytes())

			err = organizer.NewExtractor().Extract(archive, filepath.Join(root, "dest"))
			if !errors.Is(err, organizer.ErrUnsafeArchive) {
				t.Fatalf("err = %v, want ErrUnsafeArchive", err)
			}
			assertEscapedNothing(t, root)
		})
	}
}

func TestExtractZipPasswordProtected(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "release.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Flag bit 0 marks an encrypted entry; the payload is irrelevant because
	// the extractor must refuse before reading it.
	w, err := zw.CreateRaw(&zip.FileHeader{Name: "movie.mkv", Method: zip.Store, Flags: 0x1, CompressedSize64: 4, UncompressedSize64: 4})
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("abcd"))
	zw.Close()
	writeTestFile(t, archive, buf.Bytes())

	err = organizer.NewExtractor().Extract(archive, filepath.Join(root, "dest"))
	if !errors.Is(err, organizer.ErrPasswordProtected) {
		t.Fatalf("err = %v, want ErrPasswordProtected", err)
	}
}

func TestExtractUnsupportedAndSevenZipMissing(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.txt"), []byte("x"))
	writeTestFile(t, filepath.Join(root, "release.7z"), []byte("not really 7z"))

	if err := organizer.NewExtractor().Extract(filepath.Join(root, "notes.txt"), filepath.Join(root, "out")); !errors.Is(err, organizer.ErrUnsupportedArchive) {
		t.Errorf("txt: err = %v, want ErrUnsupportedArchive", err)
	}

	missing := &organizer.Extractor{BinaryPath: "definitely-not-an-installed-7z-binary"}
	err := missing.Extract(filepath.Join(root, "release.7z"), filepath.Join(root, "out"))
	if !errors.Is(err, organizer.ErrSevenZipMissing) {
		t.Fatalf("7z: err = %v, want ErrSevenZipMissing", err)
	}
	if !strings.Contains(err.Error(), "7z") {
		t.Errorf("error should name the missing tool: %v", err)
	}
}

func TestExtractReal7z(t *testing.T) {
	extractor := organizer.NewExtractor()
	if !extractor.Available() {
		t.Skip("7z binary not available on this machine, skipping .7z extraction test")
	}
	dir := t.TempDir()
	original := filepath.Join(dir, "movie.mkv")
	writeTestFile(t, original, []byte("fixture movie content for archive round-trip"))
	archive := filepath.Join(dir, "release.7z")
	if out, err := exec.Command("7z", "a", archive, original).CombinedOutput(); err != nil {
		t.Fatalf("build 7z fixture: %v: %s", err, fmt.Sprint(string(out)))
	}
	dest := filepath.Join(dir, "extracted")
	if err := extractor.Extract(archive, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got := readTestFile(t, filepath.Join(dest, "movie.mkv")); got != "fixture movie content for archive round-trip" {
		t.Fatalf("unexpected extracted content: %q", got)
	}
}
