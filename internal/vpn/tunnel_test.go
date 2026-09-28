package vpn_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/vpn"
)

// TestTunnelBetweenTwoLocalPeers brings up two independent userspace
// WireGuard tunnels (no kernel TUN device, no elevated privileges — the
// whole point of PRD.md §4.7's embedded-WireGuard approach) and proves
// actual encrypted tunnel traffic flows between them: peer B listens on
// its tunnel-local address, peer A dials it through its own tunnel, and
// the bytes that arrive are exactly what A sent. This is the real
// WireGuard protocol (handshake, session keys, encrypted transport)
// running over a loopback UDP socket — not a mock — proving the
// mechanics PRD §4.7 actually depends on.
func TestTunnelBetweenTwoLocalPeers(t *testing.T) {
	keyA, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key A: %v", err)
	}
	keyB, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key B: %v", err)
	}

	// B comes up first, with no known endpoint for A yet — WireGuard
	// learns a peer's endpoint dynamically from its first valid
	// handshake ("roaming"), so this is realistic, not a simplification.
	tunnelB, err := vpn.Up(vpn.Config{
		PrivateKey:     keyB.PrivateKey,
		PeerPublicKey:  keyA.PublicKey,
		AllowedIPs:     []string{"10.99.0.1/32"},
		LocalAddresses: []string{"10.99.0.2/32"},
	})
	if err != nil {
		t.Fatalf("bring up tunnel B: %v", err)
	}
	defer tunnelB.Close()

	portB, err := tunnelB.ListenPort()
	if err != nil {
		t.Fatalf("get tunnel B listen port: %v", err)
	}

	tunnelA, err := vpn.Up(vpn.Config{
		PrivateKey:     keyA.PrivateKey,
		PeerPublicKey:  keyB.PublicKey,
		Endpoint:       "127.0.0.1:" + strconv.Itoa(portB),
		AllowedIPs:     []string{"10.99.0.2/32"},
		LocalAddresses: []string{"10.99.0.1/32"},
	})
	if err != nil {
		t.Fatalf("bring up tunnel A: %v", err)
	}
	defer tunnelA.Close()

	// Set up a TCP listener on B's tunnel-local address, reachable only
	// through the tunnel (it's a netstack address, not a real host
	// interface).
	ln, err := listenTCPOnNetstack(tunnelB, "10.99.0.2:9000")
	if err != nil {
		t.Fatalf("listen on tunnel B: %v", err)
	}
	defer ln.Close()

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	clientConn, err := tunnelA.DialContext(ctx, "tcp", "10.99.0.2:9000")
	if err != nil {
		t.Fatalf("dial through tunnel A: %v", err)
	}
	defer clientConn.Close()

	const message = "hello through the wireguard tunnel"
	if _, err := clientConn.Write([]byte(message)); err != nil {
		t.Fatalf("write to tunnel: %v", err)
	}

	var serverConn net.Conn
	select {
	case serverConn = <-accepted:
	case err := <-acceptErr:
		t.Fatalf("accept on tunnel B: %v", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for the tunneled connection to be accepted")
	}
	defer serverConn.Close()

	buf := make([]byte, len(message))
	if _, err := io.ReadFull(serverConn, buf); err != nil {
		t.Fatalf("read from tunnel: %v", err)
	}
	if string(buf) != message {
		t.Fatalf("expected %q, got %q", message, buf)
	}
}

// listenTCPOnNetstack is a small helper so the test above doesn't need to
// reach into netstack.Net's concrete type directly.
func listenTCPOnNetstack(t *vpn.Tunnel, addr string) (net.Listener, error) {
	return t.ListenTCP(addr)
}

// TestPublicIPThroughTunnel proves PublicIP actually dials out through the
// tunnel and parses a real HTTP response, using a fixture "IP echo" server
// reachable only via the tunnel's own netstack (CLAUDE.md: local fixtures
// over live third-party calls in tests) rather than hitting the real
// api.ipify.org.
func TestPublicIPThroughTunnel(t *testing.T) {
	keyA, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key A: %v", err)
	}
	keyB, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key B: %v", err)
	}

	tunnelB, err := vpn.Up(vpn.Config{
		PrivateKey:     keyB.PrivateKey,
		PeerPublicKey:  keyA.PublicKey,
		AllowedIPs:     []string{"10.99.1.1/32"},
		LocalAddresses: []string{"10.99.1.2/32"},
	})
	if err != nil {
		t.Fatalf("bring up tunnel B: %v", err)
	}
	defer tunnelB.Close()

	portB, err := tunnelB.ListenPort()
	if err != nil {
		t.Fatalf("get tunnel B listen port: %v", err)
	}

	tunnelA, err := vpn.Up(vpn.Config{
		PrivateKey:     keyA.PrivateKey,
		PeerPublicKey:  keyB.PublicKey,
		Endpoint:       "127.0.0.1:" + strconv.Itoa(portB),
		AllowedIPs:     []string{"10.99.1.2/32"},
		LocalAddresses: []string{"10.99.1.1/32"},
	})
	if err != nil {
		t.Fatalf("bring up tunnel A: %v", err)
	}
	defer tunnelA.Close()

	ln, err := listenTCPOnNetstack(tunnelB, "10.99.1.2:9001")
	if err != nil {
		t.Fatalf("listen on tunnel B: %v", err)
	}
	defer ln.Close()

	const fakeIP = "203.0.113.42"
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, fakeIP)
	})}
	go server.Serve(ln)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	got, err := tunnelA.TestPublicIPFrom(ctx, "http://10.99.1.2:9001")
	if err != nil {
		t.Fatalf("PublicIP: %v", err)
	}
	if got != fakeIP {
		t.Fatalf("expected %q, got %q", fakeIP, got)
	}
}

// TestPublicIPRejectsNonIPResponse guards against silently treating an
// error page (e.g. a captive portal or misbehaving proxy) as a real IP.
func TestPublicIPRejectsNonIPResponse(t *testing.T) {
	keyA, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key A: %v", err)
	}
	keyB, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key B: %v", err)
	}

	tunnelB, err := vpn.Up(vpn.Config{
		PrivateKey:     keyB.PrivateKey,
		PeerPublicKey:  keyA.PublicKey,
		AllowedIPs:     []string{"10.99.2.1/32"},
		LocalAddresses: []string{"10.99.2.2/32"},
	})
	if err != nil {
		t.Fatalf("bring up tunnel B: %v", err)
	}
	defer tunnelB.Close()

	portB, err := tunnelB.ListenPort()
	if err != nil {
		t.Fatalf("get tunnel B listen port: %v", err)
	}

	tunnelA, err := vpn.Up(vpn.Config{
		PrivateKey:     keyA.PrivateKey,
		PeerPublicKey:  keyB.PublicKey,
		Endpoint:       "127.0.0.1:" + strconv.Itoa(portB),
		AllowedIPs:     []string{"10.99.2.2/32"},
		LocalAddresses: []string{"10.99.2.1/32"},
	})
	if err != nil {
		t.Fatalf("bring up tunnel A: %v", err)
	}
	defer tunnelA.Close()

	ln, err := listenTCPOnNetstack(tunnelB, "10.99.2.2:9002")
	if err != nil {
		t.Fatalf("listen on tunnel B: %v", err)
	}
	defer ln.Close()

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<html>not an ip</html>")
	})}
	go server.Serve(ln)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := tunnelA.TestPublicIPFrom(ctx, "http://10.99.2.2:9002"); err == nil {
		t.Fatal("expected an error for a non-IP response, got nil")
	}
}
