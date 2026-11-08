package vpn

import (
	"net/netip"
	"testing"

	"golang.zx2c4.com/wireguard/tun/netstack"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

// The tunnel's stack is found and gets CUBIC and the bigger buffers. If a
// wireguard-go update moves the stack, this fails instead of the tuning
// quietly stopping.
func TestTuneStack(t *testing.T) {
	_, tnet, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.99.0.1")}, nil, 1420)
	if err != nil {
		t.Fatal(err)
	}
	if err := tuneStack(tnet); err != nil {
		t.Fatalf("tuneStack: %v", err)
	}
	s, _ := stackOf(tnet)
	var cc tcpip.CongestionControlOption
	if e := s.TransportProtocolOption(tcp.ProtocolNumber, &cc); e != nil || cc != "cubic" {
		t.Errorf("congestion control = %q, %v", cc, e)
	}
	var rcv tcpip.TCPReceiveBufferSizeRangeOption
	if e := s.TransportProtocolOption(tcp.ProtocolNumber, &rcv); e != nil || rcv.Max != tunedBufferMax {
		t.Errorf("receive buffer = %+v, %v", rcv, e)
	}
}
