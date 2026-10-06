package mediaservers

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseGateway(t *testing.T) {
	const head = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"
	tests := []struct {
		name  string
		table string
		want  string
	}{
		{"docker bridge", head + "eth0\t00000000\t0100A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n", "192.168.0.1"},
		{"docker default network", head + "eth0\t00000000\t010011AC\t0003\t0\t0\t0\t00000000\t0\t0\t0\neth0\t000011AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n", "172.17.0.1"},
		{"only a local route", head + "eth0\t000011AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n", ""},
		{"default route with no gateway", head + "eth0\t00000000\t00000000\t0001\t0\t0\t0\t00000000\t0\t0\t0\n", ""},
		{"empty", "", ""},
		{"garbage", head + "eth0\t00000000\tnope\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGateway(tc.table)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("got %v, want none", got)
				}
				return
			}
			if got.String() != tc.want {
				t.Fatalf("got %v, want %s", got, tc.want)
			}
		})
	}
}

func TestExtraHosts(t *testing.T) {
	_, own, _ := net.ParseCIDR("172.17.0.0/24")
	d := &Discoverer{
		gateway: func() net.IP { return net.ParseIP("172.17.5.1") },
		lookup: func(_ context.Context, name string) []net.IP {
			switch name {
			case "host.docker.internal":
				return []net.IP{net.ParseIP("192.168.65.254")}
			case "jellyfin":
				return []net.IP{net.ParseIP("172.17.0.4"), net.ParseIP("192.168.65.254")} // one inside the searched network, one twice
			case "plex":
				return []net.IP{net.ParseIP("8.8.8.8")} // a search domain sending us to the internet
			}
			return nil
		},
	}
	var got []string
	for _, ip := range d.extraHosts(context.Background(), []*net.IPNet{own}) {
		got = append(got, ip.String())
	}
	want := "172.17.5.1 192.168.65.254"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestLooksLikeDockerBridge(t *testing.T) {
	nets := func(cidrs ...string) func() ([]*net.IPNet, error) {
		return func() ([]*net.IPNet, error) {
			var out []*net.IPNet
			for _, c := range cidrs {
				ip, n, _ := net.ParseCIDR(c)
				out = append(out, &net.IPNet{IP: ip, Mask: n.Mask})
			}
			return out, nil
		}
	}
	tests := []struct {
		name      string
		container bool
		nets      func() ([]*net.IPNet, error)
		want      bool
	}{
		{"bridge network", true, nets("172.17.0.2/16"), true},
		{"compose network", true, nets("172.29.0.5/16"), true},
		{"host network in a container", true, nets("192.168.1.20/24", "172.17.0.1/16"), false},
		{"container on a home network (macvlan)", true, nets("192.168.1.50/24"), false},
		{"not a container", false, nets("172.17.0.2/16"), false},
		{"no network at all", true, nets(), false},
		{"cannot list networks", true, func() ([]*net.IPNet, error) { return nil, errors.New("no") }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := &Discoverer{inDocker: func() bool { return tc.container }, interfaces: tc.nets}
			if got := d.looksLikeDockerBridge(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// A search with no networks of its own also tries the gateway and the
// neighbour names; a search with typed networks does not.
func TestDiscoverTriesGatewayAndNamesOnlyWithoutTypedNetworks(t *testing.T) {
	jelly := newFakeAuthServer(t, KindJellyfin)
	var lookups atomic.Int32 // the lookups run side by side
	d := &Discoverer{
		Budget: 5 * time.Second, BroadcastWait: -1, Workers: 8,
		Probes:     []Probe{{Port: portOf(t, jelly.srv.URL), Scheme: "http"}},
		allow:      testAllow,
		interfaces: func() ([]*net.IPNet, error) { return nil, nil },
		gateway:    func() net.IP { return net.ParseIP("127.0.0.1") },
		lookup:     func(context.Context, string) []net.IP { lookups.Add(1); return nil },
		inDocker:   func() bool { return true },
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			if host != "127.0.0.1" {
				return nil, errors.New("connection refused")
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}

	res := d.Discover(context.Background(), nil)
	if len(res.Found) != 1 || res.Found[0].Kind != KindJellyfin || res.Found[0].Via != "scan" {
		t.Fatalf("the gateway should have been tried and found: %+v", res)
	}
	if int(lookups.Load()) != len(NeighbourNames) {
		t.Fatalf("looked up %d names, want %d", lookups.Load(), len(NeighbourNames))
	}

	_, lan, _ := net.ParseCIDR("192.168.50.0/24")
	res = d.Discover(context.Background(), []*net.IPNet{lan})
	if len(res.Found) != 0 || int(lookups.Load()) != len(NeighbourNames) {
		t.Fatalf("typed networks must be the only thing searched: %+v (lookups %d)", res, lookups.Load())
	}
}

func TestDiscoverNothingFoundSaysWhy(t *testing.T) {
	refuse := func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("connection refused") }
	_, lan, _ := net.ParseCIDR("192.168.50.0/24")
	tests := []struct {
		name string
		d    *Discoverer
		want []string
	}{
		{
			"docker bridge",
			&Discoverer{BroadcastWait: -1, dial: refuse, inDocker: func() bool { return true }, interfaces: func() ([]*net.IPNet, error) {
				return []*net.IPNet{{IP: net.ParseIP("172.17.0.2").To4(), Mask: net.CIDRMask(16, 32)}}, nil
			}},
			[]string{"Docker's own network", "home network", "by its address", "host networking"},
		},
		{
			"ordinary network",
			&Discoverer{BroadcastWait: -1, dial: refuse, inDocker: func() bool { return false }},
			[]string{"Nothing found on 192.168.50.0/24", "another network", "by its address"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.d.Discover(context.Background(), []*net.IPNet{lan})
			for _, w := range tc.want {
				if !strings.Contains(res.Note, w) {
					t.Errorf("note %q does not say %q", res.Note, w)
				}
			}
		})
	}
}
