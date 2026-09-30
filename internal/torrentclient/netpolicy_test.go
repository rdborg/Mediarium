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
	applyNetworkPolicy(tcfg, nil)

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
	applyNetworkPolicy(tcfg, tun)

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
	if pc, err := tcfg.TrackerListenPacket("udp4", ":0"); err == nil {
		pc.Close()
		t.Error("UDP trackers must not open a host socket while the VPN is on")
	}
}
