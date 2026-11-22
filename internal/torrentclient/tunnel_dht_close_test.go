package torrentclient

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/anacrolix/dht/v2"
)

// deadConn is a UDP socket whose tunnel has gone: reads answer io.EOF.
type deadConn struct {
	gone chan struct{}
}

func (c *deadConn) ReadFrom([]byte) (int, net.Addr, error) {
	<-c.gone
	return 0, nil, io.EOF
}
func (c *deadConn) WriteTo(b []byte, _ net.Addr) (int, error) { return len(b), nil }
func (c *deadConn) Close() error                              { return nil }
func (c *deadConn) LocalAddr() net.Addr                       { return &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 4000} }
func (c *deadConn) SetDeadline(time.Time) error               { return nil }
func (c *deadConn) SetReadDeadline(time.Time) error           { return nil }
func (c *deadConn) SetWriteDeadline(time.Time) error          { return nil }

// Deactivating the VPN closes the tunnel under a running DHT server. That
// used to panic ("panic: EOF") and take the app down.
func TestDHTServerSurvivesTheTunnelClosing(t *testing.T) {
	raw := &deadConn{gone: make(chan struct{})}
	conn := &dhtConn{PacketConn: raw}
	cfg := dht.NewDefaultServerConfig()
	cfg.Conn = conn
	cfg.NoSecurity = true
	srv, err := dht.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	conn.setOnClose(srv.Close)

	close(raw.gone) // the tunnel goes away
	time.Sleep(300 * time.Millisecond)
	// Reaching here means the server's goroutine did not panic.
}
