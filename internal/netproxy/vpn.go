package netproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rdborg/mediarium/internal/netguard"
)

// localSuffixes are name endings that only exist on a home network.
var localSuffixes = []string{".localhost", ".local", ".lan", ".home.arpa", ".internal"}

// local reports whether addr is on this machine or the home network, which the
// VPN can't reach: a private, loopback or shared-space address, localhost, a
// home-network name, or a name without a dot (a container or a computer on the
// network). Names are judged by how they look, because looking them up here
// would tell the home DNS server which sites are being used.
func local(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		return netguard.NotPublic(ip) || ip.IsLinkLocalUnicast()
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true
	}
	for _, s := range localSuffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}

// ContextDialer opens connections, as the VPN tunnel does.
type ContextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// TunnelSource is the VPN as this package sees it, so it doesn't depend on
// the VPN package.
type TunnelSource interface {
	// Tunnel returns the live tunnel, or nil while none is up.
	Tunnel() ContextDialer
	// Selected returns the connection that is switched on ("" for none) and,
	// while it isn't up, why, in plain words.
	Selected() (label, reason string)
}

// ErrWaitingForVPN is what a connection fails with while its traffic has to go
// through the VPN and the VPN isn't up. There is no direct fallback.
var ErrWaitingForVPN = errors.New("waiting for the VPN")

type waitError struct{ msg string }

func (e *waitError) Error() string        { return e.msg }
func (e *waitError) Is(target error) bool { return target == ErrWaitingForVPN }

// WaitMessage is the sentence a search shows when err is a wait for the VPN,
// or "" when it isn't.
func WaitMessage(err error) string {
	var w *waitError
	if !errors.As(err, &w) {
		return ""
	}
	return strings.ToUpper(w.msg[:1]) + w.msg[1:] + "."
}

type vpnState struct {
	src  TunnelSource
	uses map[Use]bool
}

var currentVPN atomic.Pointer[vpnState]

// SetVPN says which kinds of traffic go through the VPN. Like the proxy
// setting, it's read on every new connection.
func SetVPN(src TunnelSource, uses map[Use]bool) {
	currentVPN.Store(&vpnState{src: src, uses: uses})
}

// viaVPN returns the VPN when use goes through it, or nil.
func viaVPN(use Use) TunnelSource {
	st := currentVPN.Load()
	if st == nil || st.src == nil || !st.uses[use] {
		return nil
	}
	return st.src
}

// Routed reports whether use goes through the VPN or a proxy instead of
// straight to the server.
func Routed(use Use) bool { return viaVPN(use) != nil || For(use) != nil }

// tunnelFor picks the way to addr for traffic that goes through the VPN: the
// tunnel, nil for a local address (which connects directly), or the error to
// fail with while the VPN isn't up.
func tunnelFor(src TunnelSource, use Use, addr string) (ContextDialer, error) {
	if local(addr) {
		return nil, nil
	}
	if t := src.Tunnel(); t != nil {
		return t, nil
	}
	label, reason := src.Selected()
	if label == "" {
		return nil, &waitError{fmt.Sprintf("no VPN is switched on, but %s are set to go through one. Switch one on in Settings > Downloading > VPN and proxy, or turn off Send indexer traffic through the VPN there", use)}
	}
	return nil, &waitError{fmt.Sprintf("waiting for your VPN (%s). %s Check Settings > Downloading > VPN and proxy", label, reason)}
}

// tunnelHTTP is the HTTP transport for the current tunnel. A new tunnel gets a
// new transport, and the old one's idle connections are closed, so nothing
// keeps using a tunnel that's gone.
var tunnelHTTP struct {
	mu        sync.Mutex
	tunnel    ContextDialer
	transport *http.Transport
}

func tunnelTransport(tunnel ContextDialer) *http.Transport {
	tunnelHTTP.mu.Lock()
	defer tunnelHTTP.mu.Unlock()
	if tunnelHTTP.tunnel != tunnel {
		if tunnelHTTP.transport != nil {
			tunnelHTTP.transport.CloseIdleConnections()
		}
		t := netguard.Transport()
		t.Proxy = nil
		t.DialContext = tunnel.DialContext
		tunnelHTTP.tunnel, tunnelHTTP.transport = tunnel, t
	}
	return tunnelHTTP.transport
}

func closeTunnelIdle() {
	tunnelHTTP.mu.Lock()
	defer tunnelHTTP.mu.Unlock()
	if tunnelHTTP.transport != nil {
		tunnelHTTP.transport.CloseIdleConnections()
	}
}
