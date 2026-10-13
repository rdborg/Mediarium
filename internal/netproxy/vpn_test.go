package netproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocal(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:80", true},
		{"192.168.1.20:9696", true},
		{"10.0.0.5", true},
		{"100.101.1.2:80", true}, // shared space, e.g. Tailscale
		{"[::1]:80", true},
		{"[fd00::1]:80", true},
		{"localhost:9696", true},
		{"LOCALHOST.:80", true},
		{"jackett.localhost:9117", true},
		{"nas.local:80", true},
		{"box.lan", true},
		{"router.home.arpa:443", true},
		{"api.internal", true},
		{"prowlarr:9696", true},
		{"flaresolverr", true},
		{"indexer.example.com:443", false},
		{"localhost.example.com:80", false},
		{"8.8.8.8:53", false},
		{"[2001:4860::8888]:443", false},
	} {
		if got := local(tc.addr); got != tc.want {
			t.Errorf("local(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

// fakeTunnel stands in for the VPN tunnel: whatever address is asked for, it
// connects to one test server, and it counts the connections.
type fakeTunnel struct {
	to      string
	carried atomic.Int32
}

func (f *fakeTunnel) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	f.carried.Add(1)
	var d net.Dialer
	return d.DialContext(ctx, network, f.to)
}

// fakeVPN is a TunnelSource whose tunnel is up (tunnel set) or not.
type fakeVPN struct {
	tunnel        *fakeTunnel
	label, reason string
}

func (v *fakeVPN) Tunnel() ContextDialer {
	if v.tunnel == nil {
		return nil
	}
	return v.tunnel
}

func (v *fakeVPN) Selected() (string, string) { return v.label, v.reason }

func hostOf(srv *httptest.Server) string { return strings.TrimPrefix(srv.URL, "http://") }

func TestVPNForIndexersOnly(t *testing.T) {
	t.Cleanup(func() {
		SetVPN(nil, nil)
		Set(Config{})
	})
	tgt := target(t)
	tun := &fakeTunnel{to: hostOf(tgt)}
	vpn := &fakeVPN{tunnel: tun, label: "My VPN"}
	SetVPN(vpn, map[Use]bool{Indexers: true})

	if body, err := get(t, Client(5*time.Second, Indexers), "http://indexer.example.com/api"); err != nil || body != "hello" {
		t.Fatalf("through the VPN: %q %v", body, err)
	}
	if tun.carried.Load() != 1 {
		t.Fatalf("the tunnel carried %d connections, want 1", tun.carried.Load())
	}
	conn, err := DialContext(context.Background(), Indexers, 5*time.Second, "tcp", "irc.example.net:6697")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if tun.carried.Load() != 2 {
		t.Fatal("an indexer dial didn't go through the tunnel")
	}
	// Usenet isn't ticked: straight to the server.
	conn, err = DialContext(context.Background(), Usenet, 5*time.Second, "tcp", hostOf(tgt))
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if tun.carried.Load() != 2 {
		t.Fatal("Usenet went through the tunnel although it isn't ticked")
	}

	// Local addresses connect directly, also while the VPN is down.
	if body, err := get(t, Client(5*time.Second, Indexers), tgt.URL); err != nil || body != "hello" || tun.carried.Load() != 2 {
		t.Fatalf("a local indexer: %q %v, tunnel carried %d", body, err, tun.carried.Load())
	}
	SetVPN(&fakeVPN{label: "My VPN", reason: "Starting the connection."}, map[Use]bool{Indexers: true})
	if body, err := get(t, Client(5*time.Second, Indexers), tgt.URL); err != nil || body != "hello" {
		t.Fatalf("a local indexer with the VPN down: %q %v", body, err)
	}

	// The VPN comes before a proxy for the same traffic.
	socks := newSocksServer(t, "", "")
	Set(Config{URL: mustParse(t, "socks5://"+socks.ln.Addr().String()), For: map[Use]bool{Indexers: true}})
	tun2 := &fakeTunnel{to: hostOf(tgt)}
	SetVPN(&fakeVPN{tunnel: tun2, label: "My VPN"}, map[Use]bool{Indexers: true})
	if body, err := get(t, Client(5*time.Second, Indexers), "http://indexer.example.com/api"); err != nil || body != "hello" {
		t.Fatalf("with both: %q %v", body, err)
	}
	if tun2.carried.Load() != 1 || socks.carried.Load() != 0 {
		t.Fatalf("tunnel carried %d, proxy %d; the VPN should win", tun2.carried.Load(), socks.carried.Load())
	}
	// With indexers off the VPN, the proxy works as before.
	SetVPN(vpn, map[Use]bool{})
	if body, err := get(t, Client(5*time.Second, Indexers), tgt.URL); err != nil || body != "hello" || socks.carried.Load() != 1 {
		t.Fatalf("proxy only: %q %v, proxy carried %d", body, err, socks.carried.Load())
	}

	SetVPN(nil, nil)
	Set(Config{})
	if Routed(Indexers) {
		t.Fatal("nothing set up, yet indexers are routed")
	}
	SetVPN(vpn, map[Use]bool{Indexers: true})
	if !Routed(Indexers) || Routed(Usenet) {
		t.Fatal("the VPN should route indexers and only indexers")
	}
	SetVPN(nil, nil)
	Set(Config{URL: mustParse(t, "socks5://127.0.0.1:1080"), For: map[Use]bool{Indexers: true}})
	if !Routed(Indexers) {
		t.Fatal("a proxy for indexers routes them too")
	}
}

func TestVPNDownFailsClosed(t *testing.T) {
	t.Cleanup(func() { SetVPN(nil, nil) })
	vpn := &fakeVPN{label: "My VPN", reason: "Starting the connection."}
	SetVPN(vpn, map[Use]bool{Indexers: true})

	_, err := get(t, Client(5*time.Second, Indexers), "http://indexer.example.com/api")
	if !errors.Is(err, ErrWaitingForVPN) || !strings.Contains(err.Error(), "waiting for your VPN (My VPN). Starting the connection.") {
		t.Fatalf("a search with the VPN down: %v", err)
	}
	if _, err := DialContext(context.Background(), Indexers, time.Second, "tcp", "irc.example.net:6697"); !errors.Is(err, ErrWaitingForVPN) {
		t.Fatalf("a dial with the VPN down: %v", err)
	}

	vpn.label, vpn.reason = "", ""
	_, err = get(t, Client(5*time.Second, Indexers), "http://indexer.example.com/api")
	if !errors.Is(err, ErrWaitingForVPN) || !strings.Contains(err.Error(), "no VPN is switched on") {
		t.Fatalf("a search with no VPN switched on: %v", err)
	}
}

func TestVPNPerHopAndNewTunnel(t *testing.T) {
	t.Cleanup(func() { SetVPN(nil, nil) })
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "home") }))
	t.Cleanup(home.Close)
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/go" {
			http.Redirect(w, r, home.URL+"/file", http.StatusFound)
			return
		}
		io.WriteString(w, "hello")
	}))
	t.Cleanup(public.Close)
	first, second := &fakeTunnel{to: hostOf(public)}, &fakeTunnel{to: hostOf(public)}
	vpn := &fakeVPN{tunnel: first, label: "My VPN"}
	SetVPN(vpn, map[Use]bool{Indexers: true})
	c := Client(5*time.Second, Indexers)

	// A redirect to a local address leaves the tunnel for the next hop.
	if body, err := get(t, c, "http://indexer.example.com/go"); err != nil || body != "home" {
		t.Fatalf("redirect to a local address: %q %v", body, err)
	}
	if first.carried.Load() != 1 {
		t.Fatalf("the tunnel carried %d connections, want only the first hop", first.carried.Load())
	}

	// Another connection is switched on: a kept-alive connection through the
	// old tunnel must not be reused.
	vpn.tunnel = second
	if _, err := get(t, c, "http://indexer.example.com/api"); err != nil {
		t.Fatal(err)
	}
	if first.carried.Load() != 1 || second.carried.Load() != 1 {
		t.Fatalf("old tunnel carried %d, new one %d; want 1 and 1", first.carried.Load(), second.carried.Load())
	}
}
