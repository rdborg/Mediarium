package download

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func subjectFor(name string) string { return `[1/1] "` + name + `" yEnc (1/1)` }

func TestOutputFileNamesAreSafeAndDistinct(t *testing.T) {
	long := strings.Repeat("a", 300) + ".mkv"
	longUnicode := strings.Repeat("é", 200) + ".mkv"
	tests := []struct {
		name     string
		subjects []string
		check    func(t *testing.T, got []string)
	}{
		{
			name:     "plain names are kept",
			subjects: []string{subjectFor("movie.mkv"), subjectFor("movie.nfo")},
			check: func(t *testing.T, got []string) {
				if got[0] != "movie.mkv" || got[1] != "movie.nfo" {
					t.Fatalf("got %v", got)
				}
			},
		},
		{
			name:     "dot names would be the folder itself or the folder above",
			subjects: []string{subjectFor(".."), subjectFor("."), subjectFor("..."), subjectFor(" ")},
			check: func(t *testing.T, got []string) {
				for i, n := range got {
					if strings.Trim(n, ". ") == "" {
						t.Errorf("file %d got the name %q", i, n)
					}
				}
			},
		},
		{
			name:     "separators and drive letters never survive",
			subjects: []string{subjectFor("../../etc/passwd"), subjectFor(`..\..\boot.ini`), subjectFor("/abs/path.mkv"), subjectFor(`C:\x.mkv`)},
			check: func(t *testing.T, got []string) {
				for i, n := range got {
					if strings.ContainsAny(n, `/\:`) || n == ".." {
						t.Errorf("file %d got the name %q", i, n)
					}
				}
			},
		},
		{
			name:     "the download's own bookkeeping files are not overwritten by a release",
			subjects: []string{subjectFor(ResumeFile), subjectFor(ResumeFile + ".tmp"), subjectFor(".mediarium-downloaded")},
			check: func(t *testing.T, got []string) {
				for i, n := range got {
					if strings.HasPrefix(n, ".mediarium-") {
						t.Errorf("file %d got the name %q", i, n)
					}
				}
			},
		},
		{
			name:     "two files with one name get two names",
			subjects: []string{subjectFor("part.rar"), subjectFor("part.rar"), subjectFor("PART.RAR")},
			check: func(t *testing.T, got []string) {
				seen := map[string]bool{}
				for _, n := range got {
					if seen[strings.ToLower(n)] {
						t.Fatalf("names repeat: %v", got)
					}
					seen[strings.ToLower(n)] = true
				}
				if got[0] != "part.rar" {
					t.Errorf("the first file lost its name: %v", got)
				}
				for _, n := range got {
					if filepath.Ext(n) != filepath.Ext("part.rar") && !strings.EqualFold(filepath.Ext(n), ".rar") {
						t.Errorf("extension lost: %v", got)
					}
				}
			},
		},
		{
			name:     "long names are cut but keep their extension and stay valid text",
			subjects: []string{subjectFor(long), subjectFor(longUnicode)},
			check: func(t *testing.T, got []string) {
				for i, n := range got {
					if len(n) > maxFileNameBytes || !strings.HasSuffix(n, ".mkv") || !utf8.ValidString(n) {
						t.Errorf("file %d got the name %q (%d bytes)", i, n, len(n))
					}
				}
			},
		},
		{
			name:     "no quoted name falls back to a numbered one",
			subjects: []string{"just some words", "more words"},
			check: func(t *testing.T, got []string) {
				if got[0] != "file_0.bin" || got[1] != "file_1.bin" {
					t.Fatalf("got %v", got)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := make([]NZBFile, len(tc.subjects))
			for i, s := range tc.subjects {
				files[i] = NZBFile{Subject: s}
			}
			got := outputFileNames(files)
			if len(got) != len(files) {
				t.Fatalf("got %d names for %d files", len(got), len(files))
			}
			tc.check(t, got)
		})
	}
}

// nzbOfFiles is one single-article file per name, with its own content.
func nzbOfFiles(names []string, contents [][]byte) (*NZB, map[string][]byte) {
	nzb := &NZB{}
	articles := map[string][]byte{}
	for i, name := range names {
		id := fmt.Sprintf("file%d@example", i)
		articles[id] = EncodeYenc(name, contents[i])
		nzb.Files = append(nzb.Files, NZBFile{
			Subject:  subjectFor(name),
			Groups:   []string{"alt.binaries.test"},
			Segments: []NZBSegment{{Number: 1, Bytes: int64(len(contents[i])), MessageID: id}},
		})
	}
	return nzb, articles
}

func TestDownloadKeepsFilesWithTheSameNameApart(t *testing.T) {
	contents := [][]byte{[]byte("first file, one kind of content"), []byte("second file with different content and length")}
	nzb, articles := nzbOfFiles([]string{"sample.mkv", "sample.mkv"}, contents)
	srv := newFakeNNTPServer(t, articles)
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Connections: 2}

	res, err, dir := downloadTo(t, context.Background(), []ClientConfig{cfg}, nzb)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(res.Paths) != 2 || res.Paths[0] == res.Paths[1] {
		t.Fatalf("paths = %v", res.Paths)
	}
	for i, p := range res.Paths {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, contents[i]) {
			t.Errorf("file %d holds %q, want %q", i, got, contents[i])
		}
		if filepath.Dir(p) != dir {
			t.Errorf("file %d was saved outside the download folder: %s", i, p)
		}
	}
}

func TestDownloadWithHostileFileNamesStaysInItsFolder(t *testing.T) {
	names := []string{"..", ".", "../../escape.mkv", ResumeFile, strings.Repeat("x", 400) + ".mkv"}
	contents := make([][]byte, len(names))
	for i := range names {
		contents[i] = []byte(fmt.Sprintf("content of file number %d", i))
	}
	nzb, articles := nzbOfFiles(names, contents)
	srv := newFakeNNTPServer(t, articles)
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Connections: 1}

	parent := t.TempDir()
	dir := filepath.Join(parent, "queue-1")
	res, err := DownloadFromServers(context.Background(), []ClientConfig{cfg}, nzb, dir, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	for i, p := range res.Paths {
		if filepath.Dir(p) != dir {
			t.Errorf("file %d was saved outside the download folder: %s", i, p)
		}
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, contents[i]) {
			t.Errorf("file %d: %q, %v", i, got, err)
		}
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 1 {
		t.Errorf("something was written next to the download folder: %v", entries)
	}
}

// A full disk fails every write. The download must stop with that error
// straight away instead of fetching the rest of the release for nothing.
func TestDownloadStopsWhenTheDiskIsFull(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /dev/full")
	}
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full here")
	}
	const segments = 200
	nzb, articles, _ := bigNZB(segments)
	srv := newFakeNNTPServer(t, articles)
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Connections: 1}

	dir := t.TempDir()
	// The output file is /dev/full: opening it works, every write fails with
	// "no space left on device", like a disk with no room.
	if err := os.Symlink("/dev/full", filepath.Join(dir, "big.mkv")); err != nil {
		t.Skip("cannot make a link here")
	}
	_, err := DownloadFromServers(context.Background(), []ClientConfig{cfg}, nzb, dir, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "no space") {
		t.Errorf("error = %v, want it to say the disk is full", err)
	}
	if hits := srv.totalHits(); hits > segments/4 {
		t.Errorf("%d of %d articles were fetched after the disk was full", hits, segments)
	}
}
