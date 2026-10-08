package torrentclient

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/rdborg/mediarium/internal/netguard"
)

// applyNetworkPolicy decides where the engine's own web requests may go. The
// engine does more than talk to peers: it announces to HTTP trackers, fetches
// the metadata a magnet link names (xs=), and downloads from web seeds. All of
// those addresses come from torrents and magnet links, which strangers write.
//
//   - With a VPN tunnel, every one of them goes through the tunnel like the
//     peer connections do, so a tracker or web seed never sees the real
//     address. UDP trackers cannot be tunnelled (they need a socket on the host
//     network), so they are not used while the VPN is on.
//   - Without one, they are dialled through the same check as the rest of
//     Mediarium's outgoing connections (netguard): cloud metadata and other
//     link-local addresses are refused, even through a redirect or a host name
//     that resolves there. Local and LAN addresses stay reachable.
func applyNetworkPolicy(tcfg *torrent.ClientConfig, tunnel TunnelDialer) {
	if tunnel != nil {
		tcfg.HTTPDialContext = tunnel.DialContext
		tcfg.TrackerDialContext = tunnel.DialContext
		tcfg.HTTPProxy = func(*http.Request) (*url.URL, error) { return nil, nil }
		tcfg.WebTransport = &http.Transport{
			DialContext:         tunnel.DialContext,
			Proxy:               nil,
			MaxConnsPerHost:     10,
			TLSHandshakeTimeout: 20 * time.Second,
			IdleConnTimeout:     90 * time.Second,
		}
		// The engine panics when this returns an error (a magnet link with a UDP
		// tracker would end the whole program), so a UDP tracker gets a stand-in
		// that opens no socket and fails every announce instead.
		tcfg.TrackerListenPacket = func(network, addr string) (net.PacketConn, error) {
			return newUDPOffConn(), nil
		}
		return
	}
	dial := netguard.Dialer(30 * time.Second).DialContext
	tcfg.HTTPDialContext = dial
	tcfg.TrackerDialContext = dial
	tr := netguard.Transport()
	tr.MaxConnsPerHost = 10
	tcfg.WebTransport = tr
	tcfg.TrackerListenPacket = func(network, addr string) (net.PacketConn, error) {
		pc, err := net.ListenPacket(network, addr)
		if err != nil {
			return nil, err
		}
		return guardedPacketConn{pc}, nil
	}
}

var errUDPTrackersOffWithVPN = errors.New("UDP trackers are not used while the VPN is on (they cannot go through the tunnel); HTTP trackers still work")

// udpOffConn is the "socket" a UDP tracker gets while the VPN is on. It is not
// bound to anything on the host: sending fails with errUDPTrackersOffWithVPN,
// which the tracker shows as its error, and reading waits until it is closed.
type udpOffConn struct {
	done chan struct{}
	once sync.Once
}

func newUDPOffConn() *udpOffConn { return &udpOffConn{done: make(chan struct{})} }

func (c *udpOffConn) ReadFrom([]byte) (int, net.Addr, error) {
	<-c.done
	return 0, nil, net.ErrClosed
}
func (c *udpOffConn) WriteTo([]byte, net.Addr) (int, error) { return 0, errUDPTrackersOffWithVPN }
func (c *udpOffConn) Close() error                          { c.once.Do(func() { close(c.done) }); return nil }
func (c *udpOffConn) LocalAddr() net.Addr                   { return &net.UDPAddr{} }
func (c *udpOffConn) SetDeadline(time.Time) error           { return nil }
func (c *udpOffConn) SetReadDeadline(time.Time) error       { return nil }
func (c *udpOffConn) SetWriteDeadline(time.Time) error      { return nil }

// guardedPacketConn refuses to send to the addresses netguard blocks.
type guardedPacketConn struct{ net.PacketConn }

func (g guardedPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if ua, ok := addr.(*net.UDPAddr); ok {
		if a, ok := netip.AddrFromSlice(ua.IP); ok && netguard.Blocked(a) {
			return 0, netguard.ErrBlocked
		}
	}
	return g.PacketConn.WriteTo(p, addr)
}
