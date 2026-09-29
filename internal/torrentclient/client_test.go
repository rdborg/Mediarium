package torrentclient_test

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	mediarium "github.com/ryanborg/mediarium/internal/torrentclient"
)

// TestDownloadBetweenTwoLocalPeers builds a torrent from a local fixture
// file, seeds it from one in-process torrent.Client, and downloads it with
// a second — real BitTorrent protocol traffic over localhost, no internet
// access, tracker, or DHT involved (tests use local fixtures, not live
// network calls). This proves internal/torrentclient actually moves bytes
// over the wire, not just that it links against the library.
func TestDownloadBetweenTwoLocalPeers(t *testing.T) {
	fixtureContent := []byte("Fixture movie content for a real BitTorrent transfer test. " +
		"Repeated to give it more than one small piece worth of data. ")
	for len(fixtureContent) < 300_000 {
		fixtureContent = append(fixtureContent, fixtureContent...)
	}

	seederDir := t.TempDir()
	seederFile := filepath.Join(seederDir, "movie.mkv")
	if err := os.WriteFile(seederFile, fixtureContent, 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	mi := buildMetaInfo(t, seederFile)

	seederCfg := torrent.NewDefaultClientConfig()
	seederCfg.DataDir = seederDir
	seederCfg.Seed = true
	seederCfg.NoDHT = true
	seederCfg.DisableTrackers = true
	seederCfg.ListenPort = 0
	seederClient, err := torrent.NewClient(seederCfg)
	if err != nil {
		t.Fatalf("new seeder client: %v", err)
	}

	seederTorrent, err := seederClient.AddTorrent(mi)
	if err != nil {
		t.Fatalf("seeder add torrent: %v", err)
	}
	if err := seederTorrent.VerifyDataContext(context.Background()); err != nil {
		t.Fatalf("seeder verify data: %v", err)
	}

	leecherDir := t.TempDir()
	leecherCfg := torrent.NewDefaultClientConfig()
	leecherCfg.DataDir = leecherDir
	leecherCfg.NoDHT = true
	leecherCfg.DisableTrackers = true
	leecherCfg.ListenPort = 0
	leecherClient, err := torrent.NewClient(leecherCfg)
	if err != nil {
		t.Fatalf("new leecher client: %v", err)
	}

	leecherTorrent, err := leecherClient.AddTorrent(mi)
	if err != nil {
		t.Fatalf("leecher add torrent: %v", err)
	}

	seederAddrs := seederClient.ListenAddrs()
	if len(seederAddrs) == 0 {
		t.Fatal("seeder has no listen addresses")
	}
	added := leecherTorrent.AddPeers([]torrent.PeerInfo{{Addr: addrString(seederAddrs[0])}})
	if added == 0 {
		t.Fatal("expected at least 1 peer to be added")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var lastDone, lastTotal int64
	if err := mediarium.Download(ctx, leecherTorrent, func(done, total int64) {
		lastDone, lastTotal = done, total
	}); err != nil {
		t.Fatalf("download: %v", err)
	}
	if lastTotal != int64(len(fixtureContent)) {
		t.Fatalf("expected total %d, got %d", len(fixtureContent), lastTotal)
	}
	if lastDone != lastTotal {
		t.Fatalf("expected done==total at completion, got done=%d total=%d", lastDone, lastTotal)
	}

	got, err := os.ReadFile(filepath.Join(leecherDir, "movie.mkv"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(fixtureContent) {
		t.Fatalf("downloaded content mismatch (len want=%d got=%d)", len(fixtureContent), len(got))
	}

	// Close explicitly (rather than via defer) and give Windows a moment to
	// release its file handles before t.TempDir()'s own cleanup tries to
	// remove them — on Windows, unlike POSIX, a file can't be deleted while
	// a process still holds it open.
	leecherClient.Close()
	seederClient.Close()
	time.Sleep(200 * time.Millisecond)
}

// countingDialer proxies DialContext straight to net.Dial, but records how
// many times it was actually used — standing in for a real *vpn.Tunnel to
// prove the wiring routes connections through whatever's given, without
// needing a live WireGuard handshake in this package's tests (that's
// already proven for real in internal/vpn's own tests).
type countingDialer struct {
	dials atomic.Int64
}

func (d *countingDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.dials.Add(1)
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, addr)
}

// deadDialer simulates a tunnel that's down: every dial fails, and nothing
// else should ever succeed in its place — proving there's no direct-dial
// fallback path once tunnel mode is on.
type deadDialer struct{}

func (deadDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return nil, errors.New("simulated tunnel down")
}

// TestTunnelModeRoutesThroughGivenDialer proves two things about
// Config.Tunnel (the kill switch): the client binds no real host socket for
// peer traffic (ListenAddrs is empty), and peer connections actually flow
// through the supplied dialer rather than any host-network path.
func TestTunnelModeRoutesThroughGivenDialer(t *testing.T) {
	fixtureContent := []byte("Tunnel-mode fixture content, repeated to fill more than one piece. ")
	for len(fixtureContent) < 300_000 {
		fixtureContent = append(fixtureContent, fixtureContent...)
	}

	seederDir := t.TempDir()
	seederFile := filepath.Join(seederDir, "movie.mkv")
	if err := os.WriteFile(seederFile, fixtureContent, 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	mi := buildMetaInfo(t, seederFile)

	seederCfg := torrent.NewDefaultClientConfig()
	seederCfg.DataDir = seederDir
	seederCfg.Seed = true
	seederCfg.NoDHT = true
	seederCfg.DisableTrackers = true
	seederCfg.ListenPort = 0
	seederClient, err := torrent.NewClient(seederCfg)
	if err != nil {
		t.Fatalf("new seeder client: %v", err)
	}

	seederTorrent, err := seederClient.AddTorrent(mi)
	if err != nil {
		t.Fatalf("seeder add torrent: %v", err)
	}
	if err := seederTorrent.VerifyDataContext(context.Background()); err != nil {
		t.Fatalf("seeder verify data: %v", err)
	}

	torrentFilePath := filepath.Join(t.TempDir(), "fixture.torrent")
	f, err := os.Create(torrentFilePath)
	if err != nil {
		t.Fatalf("create .torrent file: %v", err)
	}
	if err := mi.Write(f); err != nil {
		f.Close()
		t.Fatalf("write .torrent file: %v", err)
	}
	f.Close()

	dialer := &countingDialer{}
	leecherDir := t.TempDir()
	leecher, err := mediarium.New(mediarium.Config{DataDir: leecherDir, Tunnel: dialer})
	if err != nil {
		t.Fatalf("new tunnel-mode client: %v", err)
	}

	if addrs := leecher.ListenAddrs(); len(addrs) != 0 {
		t.Fatalf("expected no host listen addresses in tunnel mode, got %v", addrs)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	leecherTorrent, err := leecher.AddTorrentFile(ctx, torrentFilePath)
	if err != nil {
		t.Fatalf("leecher add torrent file: %v", err)
	}

	seederAddrs := seederClient.ListenAddrs()
	if len(seederAddrs) == 0 {
		t.Fatal("seeder has no listen addresses")
	}
	if added := leecherTorrent.AddPeers([]torrent.PeerInfo{{Addr: addrString(seederAddrs[0])}}); added == 0 {
		t.Fatal("expected at least 1 peer to be added")
	}

	if err := mediarium.Download(ctx, leecherTorrent, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	if dialer.dials.Load() == 0 {
		t.Fatal("expected the injected tunnel dialer to have been used at least once")
	}

	got, err := os.ReadFile(filepath.Join(leecherDir, "movie.mkv"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(fixtureContent) {
		t.Fatalf("downloaded content mismatch (len want=%d got=%d)", len(fixtureContent), len(got))
	}

	// Close explicitly (rather than relying on the deferred Close calls) and
	// give Windows a moment to release its file handles before t.TempDir()'s
	// own cleanup tries to remove them — see TestDownloadBetweenTwoLocalPeers
	// for the same precaution and why it's needed on Windows specifically.
	// This is a known pre-existing flake in this package on Windows dev
	// machines (anacrolix/torrent doesn't release its mmap'd file handle
	// synchronously on Close). The real deployment target
	// is Linux, which doesn't have Windows' mandatory-file-locking
	// semantics, so this is a dev-box nuisance rather than a real bug.
	leecher.Close()
	seederClient.Close()
	time.Sleep(200 * time.Millisecond)
}

// TestTunnelModeHasNoFallbackWhenDialerFails proves the kill-switch half of
// the VPN protection: with a dialer that always fails (simulating a
// dropped tunnel),
// a peer connection to a real, otherwise-reachable seeder on localhost still
// never succeeds — there's no direct-connection path being tried instead.
func TestTunnelModeHasNoFallbackWhenDialerFails(t *testing.T) {
	fixtureContent := []byte("fallback-check fixture content")
	seederDir := t.TempDir()
	seederFile := filepath.Join(seederDir, "movie.mkv")
	if err := os.WriteFile(seederFile, fixtureContent, 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	mi := buildMetaInfo(t, seederFile)

	seederCfg := torrent.NewDefaultClientConfig()
	seederCfg.DataDir = seederDir
	seederCfg.Seed = true
	seederCfg.NoDHT = true
	seederCfg.DisableTrackers = true
	seederCfg.ListenPort = 0
	seederClient, err := torrent.NewClient(seederCfg)
	if err != nil {
		t.Fatalf("new seeder client: %v", err)
	}
	defer seederClient.Close()

	seederTorrent, err := seederClient.AddTorrent(mi)
	if err != nil {
		t.Fatalf("seeder add torrent: %v", err)
	}
	if err := seederTorrent.VerifyDataContext(context.Background()); err != nil {
		t.Fatalf("seeder verify data: %v", err)
	}

	torrentFilePath := filepath.Join(t.TempDir(), "fixture.torrent")
	f, err := os.Create(torrentFilePath)
	if err != nil {
		t.Fatalf("create .torrent file: %v", err)
	}
	if err := mi.Write(f); err != nil {
		f.Close()
		t.Fatalf("write .torrent file: %v", err)
	}
	f.Close()

	leecherDir := t.TempDir()
	leecher, err := mediarium.New(mediarium.Config{DataDir: leecherDir, Tunnel: deadDialer{}})
	if err != nil {
		t.Fatalf("new tunnel-mode client: %v", err)
	}
	defer leecher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	leecherTorrent, err := leecher.AddTorrentFile(ctx, torrentFilePath)
	if err != nil {
		t.Fatalf("leecher add torrent file: %v", err)
	}

	seederAddrs := seederClient.ListenAddrs()
	if len(seederAddrs) == 0 {
		t.Fatal("seeder has no listen addresses")
	}
	leecherTorrent.AddPeers([]torrent.PeerInfo{{Addr: addrString(seederAddrs[0])}})

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shortCancel()
	err = mediarium.Download(shortCtx, leecherTorrent, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the download to hang (no fallback path) until the context deadline, got %v", err)
	}
}

type stringAddr string

func (s stringAddr) String() string  { return string(s) }
func (s stringAddr) Network() string { return "tcp" }

func addrString(a interface{ String() string }) torrent.PeerRemoteAddr {
	return stringAddr(a.String())
}

func buildMetaInfo(t *testing.T, filePath string) *metainfo.MetaInfo {
	t.Helper()
	info := metainfo.Info{PieceLength: 64 * 1024}
	if err := info.BuildFromFilePath(filePath); err != nil {
		t.Fatalf("build info from file path: %v", err)
	}
	if err := info.GeneratePieces(func(metainfo.FileInfo) (io.ReadCloser, error) {
		return os.Open(filePath)
	}); err != nil {
		t.Fatalf("generate pieces: %v", err)
	}

	mi := &metainfo.MetaInfo{}
	mi.SetDefaults()
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	mi.InfoBytes = infoBytes
	return mi
}
