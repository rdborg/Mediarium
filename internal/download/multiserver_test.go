package download

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
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

// par2Extra is a repair file for an NZB: one article that the server has, and a
// declared size that says how much damage it could repair.
func par2Extra(declared int64) (NZBFile, []byte) {
	data := []byte("pretend recovery data")
	return NZBFile{
		Subject: `[2/2] "movie.vol0+1.par2" yEnc (1/1)`,
		Groups:  []string{"alt.binaries.test"},
		Segments: []NZBSegment{
			{Number: 1, Bytes: declared, MessageID: "par2@example"},
		},
	}, buildMultipartYenc("movie.vol0+1.par2", data, 1, len(data))
}

// Articles no server has are counted, not fatal, as long as the PAR2 files in
// the release could rebuild that much: the caller can still try repair.
func TestMissingEverywhereIsCountedWhenPar2CouldRepairIt(t *testing.T) {
	seg1, _, mid := multiArticles()
	par2, par2Article := par2Extra(1 << 20)
	a := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1, "par2@example": par2Article})
	b := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1, "par2@example": par2Article})
	nzb := twoSegmentNZB(mid, len(multiFixture))
	nzb.Files = append(nzb.Files, par2)

	res, err := DownloadFromServers(context.Background(), []ClientConfig{
		{Host: a.addr, Port: a.port, Connections: 1},
		{Host: b.addr, Port: b.port, Connections: 1},
	}, nzb, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("missing articles should not be a hard error: %v", err)
	}
	if res.MissingSegments != 1 || res.TotalSegments != 3 {
		t.Fatalf("expected 1 of 3 missing, got %+v", res)
	}
}

// A release with no PAR2 files can't be repaired, so the first article no
// server has ends the download at once, as the release's fault.
func TestMissingArticleWithoutPar2StopsAtOnce(t *testing.T) {
	seg1, _, mid := multiArticles()
	a := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1})
	_, err := DownloadFromServers(context.Background(), []ClientConfig{{Host: a.addr, Port: a.port, Connections: 1}},
		twoSegmentNZB(mid, len(multiFixture)), t.TempDir(), nil)
	var re *ReleaseError
	if !errors.As(err, &re) {
		t.Fatalf("want a ReleaseError, got %v", err)
	}
	if !strings.Contains(err.Error(), "couldn't be found on your Usenet servers") || !strings.Contains(err.Error(), "no PAR2") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// More is gone than the PAR2 files could ever rebuild: also stopped at once.
func TestMissingMoreThanPar2CouldRepairStopsAtOnce(t *testing.T) {
	seg1, _, mid := multiArticles()
	par2, par2Article := par2Extra(3) // tiny recovery data
	a := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1, "par2@example": par2Article})
	nzb := twoSegmentNZB(mid, len(multiFixture))
	nzb.Files = append(nzb.Files, par2)
	_, err := DownloadFromServers(context.Background(), []ClientConfig{{Host: a.addr, Port: a.port, Connections: 1}}, nzb, t.TempDir(), nil)
	var re *ReleaseError
	if !errors.As(err, &re) || !strings.Contains(err.Error(), "can't repair that much") {
		t.Fatalf("want a ReleaseError about too little repair data, got %v", err)
	}
}

func TestNoServersConfigured(t *testing.T) {
	_, _, mid := multiArticles()
	if _, err := DownloadFromServers(context.Background(), nil, twoSegmentNZB(mid, len(multiFixture)), t.TempDir(), nil); err == nil {
		t.Fatal("expected an error when no server is configured")
	}
}
