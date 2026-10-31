package torrentclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/rdborg/mediarium/internal/netguard"
)

type recordingTunnel struct {
	mu    sync.Mutex
	dials []string
}

func (r *recordingTunnel) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	r.mu.Lock()
	r.dials = append(r.dials, network+" "+addr)
	r.mu.Unlock()
	return nil, errors.New("tunnel test: no connection")
}

// Without a VPN, the engine's web requests, tracker announces and web seed
// downloads are refused when they would go to a cloud metadata address.
func TestNetworkPolicyWithoutVPNRefusesMetadataAddresses(t *testing.T) {
	tcfg := torrent.NewDefaultClientConfig()
	applyNetworkPolicy(tcfg, nil, false, false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for name, dial := range map[string]func(context.Context, string, string) (net.Conn, error){
		"HTTPDialContext": tcfg.HTTPDialContext, "TrackerDialContext": tcfg.TrackerDialContext,
	} {
		if dial == nil {
			t.Fatalf("%s is not set", name)
		}
		if _, err := dial(ctx, "tcp", "169.254.169.254:80"); !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("%s to the metadata address: %v, want ErrBlocked", name, err)
		}
	}

	// Web seeds and metadata sources use WebTransport.
	if tcfg.WebTransport == nil {
		t.Fatal("WebTransport is not set")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	if _, err := tcfg.WebTransport.RoundTrip(req); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("web seed request to the metadata address: %v, want ErrBlocked", err)
	}

	// UDP trackers may not be written to a blocked address either.
	pc, err := tcfg.TrackerListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if _, err := pc.WriteTo([]byte("x"), &net.UDPAddr{IP: net.ParseIP("169.254.169.254"), Port: 6969}); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("UDP announce to the metadata address: %v, want ErrBlocked", err)
	}
}

// With a VPN, every web request goes into the tunnel and UDP trackers are off,
// so the real address never reaches a tracker or web seed.
func TestNetworkPolicyWithVPNUsesOnlyTheTunnel(t *testing.T) {
	tun := &recordingTunnel{}
	tcfg := torrent.NewDefaultClientConfig()
	applyNetworkPolicy(tcfg, tun, false, false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = tcfg.TrackerDialContext(ctx, "tcp", "tracker.example:80")
	_, _ = tcfg.HTTPDialContext(ctx, "tcp", "seed.example:443")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://webseed.example/file", nil)
	_, _ = tcfg.WebTransport.RoundTrip(req)

	tun.mu.Lock()
	got := append([]string(nil), tun.dials...)
	tun.mu.Unlock()
	want := map[string]bool{"tcp tracker.example:80": false, "tcp seed.example:443": false, "tcp webseed.example:80": false}
	for _, d := range got {
		if _, ok := want[d]; ok {
			want[d] = true
		}
	}
	for d, seen := range want {
		if !seen {
			t.Errorf("no dial through the tunnel for %q; dials: %v", d, got)
		}
	}
	if u, err := tcfg.HTTPProxy(req); u != nil || err != nil {
		t.Errorf("HTTPProxy = %v, %v; a proxy from the environment would carry traffic outside the tunnel", u, err)
	}
	// A UDP tracker gets a stand-in that opens nothing on the host: asking the
	// engine for a socket must not fail (it would panic), sending fails, and
	// reading ends when the stand-in is closed.
	pc, err := tcfg.TrackerListenPacket("udp4", ":0")
	if err != nil {
		t.Fatalf("TrackerListenPacket returned %v; an error here makes the engine panic", err)
	}
	if _, ok := pc.(*udpOffConn); !ok {
		t.Fatalf("UDP trackers got %T, want the stand-in that opens no host socket", pc)
	}
	if _, err := pc.WriteTo([]byte("x"), &net.UDPAddr{IP: net.ParseIP("203.0.113.5"), Port: 6969}); !errors.Is(err, errUDPTrackersOffWithVPN) {
		t.Errorf("UDP announce while the VPN is on: %v, want errUDPTrackersOffWithVPN", err)
	}
	read := make(chan error, 1)
	go func() { _, _, err := pc.ReadFrom(make([]byte, 16)); read <- err }()
	select {
	case err := <-read:
		t.Fatalf("ReadFrom returned %v before the stand-in was closed", err)
	case <-time.After(50 * time.Millisecond):
	}
	_ = pc.Close()
	_ = pc.Close() // closing twice is fine
	select {
	case err := <-read:
		if !errors.Is(err, net.ErrClosed) {
			t.Errorf("ReadFrom after Close: %v, want net.ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("ReadFrom did not return after Close")
	}
}

// Trackers, web seeds and magnet addresses on this machine or the home
// network are refused unless the person allowed them.
func TestNetworkPolicyRefusesLocalAddressesUnlessAllowed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	strict := torrent.NewDefaultClientConfig()
	applyNetworkPolicy(strict, nil, false, false)
	for name, dial := range map[string]func(context.Context, string, string) (net.Conn, error){
		"HTTPDialContext": strict.HTTPDialContext, "TrackerDialContext": strict.TrackerDialContext,
	} {
		if _, err := dial(ctx, "tcp", ln.Addr().String()); !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("%s to this machine: %v, want ErrBlocked", name, err)
		}
	}
	pc, err := strict.TrackerListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if _, err := pc.WriteTo([]byte("x"), &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 6969}); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("UDP announce to a home address: %v, want ErrBlocked", err)
	}

	open := torrent.NewDefaultClientConfig()
	applyNetworkPolicy(open, nil, true, false)
	c, err := open.TrackerDialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("allowed local tracker: %v", err)
	}
	c.Close()
}

// A tracker connection stops delivering after the cap, so a hostile tracker
// can't send the engine an endless nested answer.
func TestTrackerReplyIsCapped(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go func() {
		defer b.Close()
		buf := make([]byte, 64<<10)
		for {
			if _, err := b.Write(buf); err != nil {
				return
			}
		}
	}()
	c := &cappedConn{Conn: a, left: 100 << 10}
	var got int64
	buf := make([]byte, 32<<10)
	for {
		n, err := c.Read(buf)
		got += int64(n)
		if err != nil {
			break
		}
	}
	if got != 100<<10 {
		t.Fatalf("read %d bytes, want exactly the cap %d", got, 100<<10)
	}
}
