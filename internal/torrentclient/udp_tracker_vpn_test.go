package torrentclient_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mediarium "github.com/rdborg/mediarium/internal/torrentclient"
)

// packetTunnel carries UDP inside the test double. It never opens a host socket.
type packetTunnel struct {
	refusingTunnel
	listens atomic.Int32
}

func (p *packetTunnel) ListenPacket(string, string) (net.PacketConn, error) {
	p.listens.Add(1)
	return &idlePacket{addr: &net.UDPAddr{IP: net.IPv4(10, 2, 0, 2), Port: 1}, done: make(chan struct{})}, nil
}

func (p *packetTunnel) LookupHost(context.Context, string) ([]net.IP, error) {
	return nil, errors.New("no dns in this test")
}

type idlePacket struct {
	addr net.Addr
	done chan struct{}
	once sync.Once
}

func (p *idlePacket) ReadFrom([]byte) (int, net.Addr, error) {
	<-p.done
	return 0, nil, net.ErrClosed
}
func (p *idlePacket) WriteTo(b []byte, _ net.Addr) (int, error) { return len(b), nil }
func (p *idlePacket) Close() error                              { p.once.Do(func() { close(p.done) }); return nil }
func (p *idlePacket) LocalAddr() net.Addr                       { return p.addr }
func (p *idlePacket) SetDeadline(time.Time) error               { return nil }
func (p *idlePacket) SetReadDeadline(time.Time) error           { return nil }
func (p *idlePacket) SetWriteDeadline(time.Time) error          { return nil }

// Discovery through the VPN must use the tunnel's UDP socket and must not
// listen on the host. The home address stays unused.
func TestDiscoverThroughTunnelDoesNotListenOnTheHost(t *testing.T) {
	tun := &packetTunnel{}
	engine, err := mediarium.New(mediarium.Config{DataDir: t.TempDir(), Tunnel: tun, DiscoverThroughTunnel: true})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()
	if tun.listens.Load() < 1 {
		t.Fatal("DHT was not opened on the tunnel")
	}
	if addrs := engine.ListenAddrs(); len(addrs) != 0 {
		t.Fatalf("host listen addresses = %v, want none", addrs)
	}
}

type refusingTunnel struct{}

func (refusingTunnel) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("tunnel test: no connection")
}

// A magnet link that lists a UDP tracker must not take the download down while
// the VPN is on. UDP trackers can't go through the tunnel, so they are
// skipped; the engine used to panic when the torrent tried to use one.
func TestMagnetWithUDPTrackerDoesNotCrashWithVPN(t *testing.T) {
	engine, err := mediarium.New(mediarium.Config{DataDir: t.TempDir(), Tunnel: refusingTunnel{}})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=fixture" +
		"&tr=udp%3A%2F%2Ftracker.example%3A6969%2Fannounce" +
		"&tr=http%3A%2F%2Ftracker.example%3A80%2Fannounce"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tor, err := engine.AddMagnetIn(ctx, magnet, t.TempDir())
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("add magnet: %v", err)
	}
	// Give the announcer time to try every tracker; a panic ends the test run.
	time.Sleep(1500 * time.Millisecond)
	if tor != nil {
		engine.Remove(tor)
	}
}
