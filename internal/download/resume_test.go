package download

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// resumeFixture is a one-file NZB of n articles and the server that has them.
func resumeFixture(t *testing.T, n int) (*NZB, *fakeNNTPServer, []byte, ClientConfig) {
	t.Helper()
	full := []byte(strings.Repeat("The quick brown fox jumps over the lazy dog. ", n*4))
	per := len(full) / n
	bodies := map[string][]byte{}
	nzb := &NZB{Files: []NZBFile{{Subject: `[1/1] "movie.mkv" yEnc (1/` + fmt.Sprint(n) + `)`, Groups: []string{"alt.binaries.test"}}}}
	for i := 0; i < n; i++ {
		begin, end := i*per+1, (i+1)*per
		if i == n-1 {
			end = len(full)
		}
		id := fmt.Sprintf("seg%d@example", i+1)
		bodies[id] = buildMultipartYenc("movie.mkv", full, begin, end)
		nzb.Files[0].Segments = append(nzb.Files[0].Segments, NZBSegment{Number: i + 1, Bytes: int64(end - begin + 1), MessageID: id})
	}
	srv := newFakeNNTPServer(t, bodies)
	return nzb, srv, full, ClientConfig{Host: srv.addr, Port: srv.port, Username: "u", Password: "p", Connections: 1}
}

// interrupt runs a download and cancels it once about a third of it is saved.
func interrupt(t *testing.T, cfg ClientConfig, nzb *NZB, dir string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stopped atomic.Bool
	_, err := DownloadFromServers(ctx, []ClientConfig{cfg}, nzb, dir, func(done, total int64) {
		if done*3 >= total && stopped.CompareAndSwap(false, true) {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("the first run should have been stopped part way")
	}
}

func TestResumeFetchesOnlyWhatIsMissing(t *testing.T) {
	const articles = 12
	nzb, srv, full, cfg := resumeFixture(t, articles)
	dir := t.TempDir()

	interrupt(t, cfg, nzb, dir)
	if _, err := os.Stat(filepath.Join(dir, ResumeFile)); err != nil {
		t.Fatalf("a stopped download should leave its progress behind: %v", err)
	}
	fetched := srv.totalHits()
	if fetched == 0 || fetched >= articles {
		t.Fatalf("expected the first run to fetch part of the release, got %d of %d", fetched, articles)
	}

	res, err := DownloadFromServers(context.Background(), []ClientConfig{cfg}, nzb, dir, nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if res.MissingSegments != 0 {
		t.Fatalf("nothing should be missing, got %d", res.MissingSegments)
	}
	got, err := os.ReadFile(res.Paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(full) {
		t.Fatalf("the resumed file differs from the original (%d bytes, want %d)", len(got), len(full))
	}
	// The second run asked for the articles the first did not save, plus at
	// most the one that was in flight when it was cancelled.
	if extra := srv.totalHits() - fetched; extra > articles-fetched+1 {
		t.Fatalf("resume fetched %d articles again; only %d were missing", extra, articles-fetched)
	}
	if _, err := os.Stat(filepath.Join(dir, ResumeFile)); err == nil {
		t.Fatal("the progress file should be gone once the download is complete")
	}
}

func TestResumeIsIgnoredWhenItCannotBeTrusted(t *testing.T) {
	const articles = 6
	tests := []struct {
		name   string
		damage func(t *testing.T, dir string)
	}{
		{"progress file is not readable", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, ResumeFile), []byte("not json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"the file it describes was deleted", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "movie.mkv")); err != nil {
				t.Fatal(err)
			}
		}},
		{"the file it describes is empty", func(t *testing.T, dir string) {
			if err := os.Truncate(filepath.Join(dir, "movie.mkv"), 0); err != nil {
				t.Fatal(err)
			}
		}},
		{"it was written for another release", func(t *testing.T, dir string) {
			b, err := os.ReadFile(filepath.Join(dir, ResumeFile))
			if err != nil {
				t.Fatal(err)
			}
			other := strings.Replace(string(b), `"release":"`, `"release":"00`, 1)
			if err := os.WriteFile(filepath.Join(dir, ResumeFile), []byte(other), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nzb, srv, full, cfg := resumeFixture(t, articles)
			dir := t.TempDir()
			interrupt(t, cfg, nzb, dir)
			tc.damage(t, dir)
			before := srv.totalHits()

			res, err := DownloadFromServers(context.Background(), []ClientConfig{cfg}, nzb, dir, nil)
			if err != nil {
				t.Fatalf("download: %v", err)
			}
			got, _ := os.ReadFile(res.Paths[0])
			if string(got) != string(full) {
				t.Fatalf("the file must be complete and correct whatever the progress file said")
			}
			if fetched := srv.totalHits() - before; fetched < articles {
				t.Fatalf("with progress that cannot be trusted every article must be fetched again, got %d of %d", fetched, articles)
			}
		})
	}
}

func TestFreshFolderIsNotAffectedByResume(t *testing.T) {
	nzb, srv, full, cfg := resumeFixture(t, 4)
	dir := t.TempDir()
	res, err := DownloadFromServers(context.Background(), []ClientConfig{cfg}, nzb, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Paths[0])
	if string(got) != string(full) || srv.totalHits() != 4 {
		t.Fatalf("a normal download fetches every article once, got %d", srv.totalHits())
	}
}
