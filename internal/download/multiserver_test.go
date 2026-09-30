package download

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
)

// twoSegmentNZB describes one file split over two articles.
func twoSegmentNZB(mid, total int) *NZB {
	return &NZB{Files: []NZBFile{{
		Subject: `[1/1] "movie.mkv" yEnc (1/2)`,
		Groups:  []string{"alt.binaries.test"},
		Segments: []NZBSegment{
			{Number: 1, Bytes: int64(mid), MessageID: "seg1@example"},
			{Number: 2, Bytes: int64(total - mid), MessageID: "seg2@example"},
		},
	}}}
}

var multiFixture = []byte("The quick brown fox jumps over the lazy dog. This is fixture movie content.")

func multiArticles() (seg1, seg2 []byte, mid int) {
	mid = len(multiFixture) / 2
	return buildMultipartYenc("movie.mkv", multiFixture, 1, mid), buildMultipartYenc("movie.mkv", multiFixture, mid+1, len(multiFixture)), mid
}

func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// An article the primary server lacks is fetched from the backup, and the
// assembled file is complete.
func TestBackupServerSuppliesArticlesThePrimaryLacks(t *testing.T) {
	seg1, seg2, mid := multiArticles()
	primary := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1}) // no seg2
	backup := newFakeNNTPServer(t, map[string][]byte{"seg2@example": seg2})

	res, err := DownloadFromServers(context.Background(), []ClientConfig{
		{Host: primary.addr, Port: primary.port, Connections: 1},
		{Host: backup.addr, Port: backup.port, Connections: 1},
	}, twoSegmentNZB(mid, len(multiFixture)), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if res.MissingSegments != 0 {
		t.Fatalf("nothing should be missing, got %d", res.MissingSegments)
	}
	got, _ := os.ReadFile(res.Paths[0])
	if string(got) != string(multiFixture) {
		t.Fatalf("assembled file mismatch: %q", got)
	}
}

// A backup server that is not needed is never even connected to.
func TestBackupServerIsNotContactedWhenPrimaryHasEverything(t *testing.T) {
	seg1, seg2, mid := multiArticles()
	primary := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1, "seg2@example": seg2})
	backup := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1, "seg2@example": seg2})

	if _, err := DownloadFromServers(context.Background(), []ClientConfig{
		{Host: primary.addr, Port: primary.port, Connections: 2},
		{Host: backup.addr, Port: backup.port, Connections: 2},
	}, twoSegmentNZB(mid, len(multiFixture)), t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
	if n := backup.conns.Load(); n != 0 {
		t.Fatalf("the backup server got %d connection(s) although the primary served everything", n)
	}
}

// An unreachable primary falls back to the backup instead of failing.
func TestDeadPrimaryFallsBackToBackup(t *testing.T) {
	seg1, seg2, mid := multiArticles()
	backup := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1, "seg2@example": seg2})

	res, err := DownloadFromServers(context.Background(), []ClientConfig{
		{Host: "127.0.0.1", Port: closedPort(t), Connections: 2},
		{Host: backup.addr, Port: backup.port, Connections: 2},
	}, twoSegmentNZB(mid, len(multiFixture)), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("a dead primary should not fail the download: %v", err)
	}
	got, _ := os.ReadFile(res.Paths[0])
	if string(got) != string(multiFixture) {
		t.Fatalf("assembled file mismatch: %q", got)
	}
}

// With every server unreachable the problem is our setup, not the release: a
// plain error, not a ReleaseError, so nothing gets blocklisted.
func TestAllServersDownIsNotAReleaseFault(t *testing.T) {
	_, _, mid := multiArticles()
	_, err := DownloadFromServers(context.Background(), []ClientConfig{
		{Host: "127.0.0.1", Port: closedPort(t), Connections: 1},
	}, twoSegmentNZB(mid, len(multiFixture)), t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected an error with no reachable server")
	}
	var re *ReleaseError
	if errors.As(err, &re) {
		t.Fatalf("an unreachable server must not look like a bad release: %v", err)
	}
}

// Articles no server has are counted, not fatal: the caller can still try
// PAR2 repair.
func TestMissingEverywhereIsCountedNotFatal(t *testing.T) {
	seg1, _, mid := multiArticles()
	a := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1})
	b := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1})

	res, err := DownloadFromServers(context.Background(), []ClientConfig{
		{Host: a.addr, Port: a.port, Connections: 1},
		{Host: b.addr, Port: b.port, Connections: 1},
	}, twoSegmentNZB(mid, len(multiFixture)), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("missing articles should not be a hard error: %v", err)
	}
	if res.MissingSegments != 1 || res.TotalSegments != 2 {
		t.Fatalf("expected 1 of 2 missing, got %+v", res)
	}
}

func TestNoServersConfigured(t *testing.T) {
	_, _, mid := multiArticles()
	if _, err := DownloadFromServers(context.Background(), nil, twoSegmentNZB(mid, len(multiFixture)), t.TempDir(), nil); err == nil {
		t.Fatal("expected an error when no server is configured")
	}
}
