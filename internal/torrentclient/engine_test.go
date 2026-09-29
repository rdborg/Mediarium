package torrentclient_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/anacrolix/torrent"

	mediarium "github.com/ryanborg/mediarium/internal/torrentclient"
)

func TestSeedGoalMet(t *testing.T) {
	const gib = int64(1 << 30)
	tests := []struct {
		name     string
		goal     mediarium.SeedGoal
		uploaded int64
		size     int64
		seeding  time.Duration
		want     bool
	}{
		{"no limits never met", mediarium.SeedGoal{}, 10 * gib, gib, 1000 * time.Hour, false},
		{"ratio reached", mediarium.SeedGoal{Ratio: 2}, 2 * gib, gib, time.Minute, true},
		{"ratio not reached", mediarium.SeedGoal{Ratio: 2}, gib, gib, time.Minute, false},
		{"time reached", mediarium.SeedGoal{Time: time.Hour}, 0, gib, time.Hour, true},
		{"time not reached", mediarium.SeedGoal{Time: time.Hour}, 0, gib, 59 * time.Minute, false},
		{"either one is enough", mediarium.SeedGoal{Ratio: 5, Time: time.Hour}, gib, gib, 2 * time.Hour, true},
		{"unknown size never meets a ratio", mediarium.SeedGoal{Ratio: 1}, gib, 0, time.Minute, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.goal.Met(tc.uploaded, tc.size, tc.seeding); got != tc.want {
				t.Fatalf("Met = %v, want %v", got, tc.want)
			}
		})
	}
	if !(mediarium.SeedGoal{}).Unlimited() || (mediarium.SeedGoal{Ratio: 1}).Unlimited() {
		t.Fatal("Unlimited is wrong")
	}
}

// freePort finds a TCP and UDP port nobody listens on right now.
func freePort(t *testing.T) int {
	t.Helper()
	for i := 0; i < 20; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		if u, err := net.ListenPacket("udp", net.JoinHostPort("", strconv.Itoa(port))); err == nil {
			u.Close()
			return port
		}
	}
	t.Fatal("no free port")
	return 0
}

// TestEngineListensOnConfiguredPortAndSeesIncoming seeds a torrent from the
// engine, saved in a folder of its own, on a fixed port, and lets a plain
// client connect to it: the engine reports that port and the incoming
// connection, which is what the Downloaders page shows.
func TestEngineListensOnConfiguredPortAndSeesIncoming(t *testing.T) {
	content := []byte("incoming-connection fixture, repeated to fill a few pieces. ")
	for len(content) < 200_000 {
		content = append(content, content...)
	}
	itemDir := t.TempDir()
	file := filepath.Join(itemDir, "movie.mkv")
	if err := os.WriteFile(file, content, 0o644); err != nil {
		t.Fatal(err)
	}
	mi := buildMetaInfo(t, file)
	torrentFile := filepath.Join(t.TempDir(), "fixture.torrent")
	f, err := os.Create(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := mi.Write(f); err != nil {
		t.Fatal(err)
	}
	f.Close()

	port := freePort(t)
	engine, err := mediarium.New(mediarium.Config{DataDir: t.TempDir(), ListenPort: port})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()
	if engine.ListenPort() != port {
		t.Fatalf("engine listens on %d, want %d", engine.ListenPort(), port)
	}
	if !engine.LastIncoming().IsZero() {
		t.Fatal("no peer has connected yet")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	seeding, err := engine.AddTorrentFileIn(ctx, torrentFile, itemDir)
	if err != nil {
		t.Fatalf("add torrent: %v", err)
	}
	if err := seeding.VerifyDataContext(ctx); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if engine.Torrents() != 1 {
		t.Fatalf("engine runs %d torrents, want 1", engine.Torrents())
	}
	if _, err := engine.AddTorrentFileIn(ctx, torrentFile, t.TempDir()); err == nil {
		t.Fatal("the same torrent must not be added twice")
	}

	leecherCfg := torrent.NewDefaultClientConfig()
	leecherCfg.DataDir = t.TempDir()
	leecherCfg.NoDHT = true
	leecherCfg.DisableTrackers = true
	leecherCfg.ListenPort = 0
	leecher, err := torrent.NewClient(leecherCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer leecher.Close()
	lt, err := leecher.AddTorrent(mi)
	if err != nil {
		t.Fatal(err)
	}
	lt.AddPeers([]torrent.PeerInfo{{Addr: stringAddr(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))}})
	if err := mediarium.Download(ctx, lt, nil); err != nil {
		t.Fatalf("download from the engine: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for engine.LastIncoming().IsZero() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if engine.LastIncoming().IsZero() {
		t.Fatal("the engine did not notice the incoming connection")
	}

	engine.Remove(seeding)
	if engine.Torrents() != 0 {
		t.Fatalf("engine still runs %d torrents after Remove", engine.Torrents())
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("Remove must leave the files on disk: %v", err)
	}
}

// TestSeedUntilGoalStopsWhenCancelled proves an unlimited goal keeps a
// torrent seeding until it is stopped, rather than returning at once.
func TestSeedUntilGoalStopsWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(file, []byte("seed goal fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	mi := buildMetaInfo(t, file)
	torrentFile := filepath.Join(t.TempDir(), "a.torrent")
	f, _ := os.Create(torrentFile)
	_ = mi.Write(f)
	f.Close()

	engine, err := mediarium.New(mediarium.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	tr, err := engine.AddTorrentFileIn(context.Background(), torrentFile, dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- mediarium.SeedUntilGoal(ctx, tr, time.Now(), mediarium.SeedGoal{}, 10*time.Millisecond)
	}()
	select {
	case <-done:
		t.Fatal("an unlimited goal returned before the torrent was stopped")
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	if met := <-done; met {
		t.Fatal("stopping is not meeting the goal")
	}

	// A time goal that has already passed is met straight away.
	if !mediarium.SeedUntilGoal(context.Background(), tr, time.Now().Add(-time.Hour), mediarium.SeedGoal{Time: time.Minute}, time.Hour) {
		t.Fatal("a passed time goal should be met")
	}
}
