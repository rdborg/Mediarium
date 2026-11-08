package vpn

import (
	"errors"
	"fmt"
	"reflect"
	"unsafe"

	"golang.zx2c4.com/wireguard/tun/netstack"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

// TCP through the tunnel runs on gVisor's userspace stack, which wireguard-go
// sets up with Reno congestion control and 4 MB buffers. Over a VPN server far
// away, a lost packet now and then made Reno halve its window again and
// again: a download ran at a fraction of what the line can do (discussion
// #126; a test with 100 ms and 0.1% loss went from 0.6 to several MB/s per
// connection). CUBIC, what Linux uses, recovers from a loss much faster, and
// bigger buffers let one connection fill a long, fast path.

// tunedBufferMax is the most a connection's send or receive buffer grows to.
const tunedBufferMax = 8 << 20

// netTunHead mirrors the first two fields of wireguard-go's netstack device
// (netstack.Net), which keeps its gVisor stack to itself. tuneStack checks
// the real layout before it reads through this.
type netTunHead struct {
	ep    *channel.Endpoint
	stack *stack.Stack
}

var errStackLayout = errors.New("the netstack device isn't laid out as expected")

// stackOf is the gVisor stack behind tnet, when wireguard-go's netstack.Net
// still starts with the fields netTunHead copies.
func stackOf(tnet *netstack.Net) (*stack.Stack, error) {
	t := reflect.TypeOf(netstack.Net{})
	if t.Kind() != reflect.Struct || t.NumField() < 2 ||
		t.Field(0).Name != "ep" || t.Field(0).Type != reflect.TypeOf((*channel.Endpoint)(nil)) ||
		t.Field(1).Name != "stack" || t.Field(1).Type != reflect.TypeOf((*stack.Stack)(nil)) ||
		t.Field(1).Offset != unsafe.Offsetof(netTunHead{}.stack) {
		return nil, errStackLayout
	}
	s := (*netTunHead)(unsafe.Pointer(tnet)).stack
	if s == nil {
		return nil, errStackLayout
	}
	return s, nil
}

// tuneStack sets CUBIC and bigger TCP buffers on the tunnel's stack. When the
// stack can't be reached (a wireguard-go update changed it) the tunnel still
// works, with gVisor's own settings.
func tuneStack(tnet *netstack.Net) error {
	s, err := stackOf(tnet)
	if err != nil {
		return err
	}
	cc := tcpip.CongestionControlOption("cubic")
	if e := s.SetTransportProtocolOption(tcp.ProtocolNumber, &cc); e != nil {
		return fmt.Errorf("set CUBIC: %v", e)
	}
	rcv := tcpip.TCPReceiveBufferSizeRangeOption{Min: tcp.MinBufferSize, Default: tcp.DefaultReceiveBufferSize, Max: tunedBufferMax}
	if e := s.SetTransportProtocolOption(tcp.ProtocolNumber, &rcv); e != nil {
		return fmt.Errorf("set the receive buffer: %v", e)
	}
	snd := tcpip.TCPSendBufferSizeRangeOption{Min: tcp.MinBufferSize, Default: tcp.DefaultSendBufferSize, Max: tunedBufferMax}
	if e := s.SetTransportProtocolOption(tcp.ProtocolNumber, &snd); e != nil {
		return fmt.Errorf("set the send buffer: %v", e)
	}
	return nil
}
