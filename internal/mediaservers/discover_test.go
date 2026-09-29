package mediaservers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// testAllow lets the tests' fakes, which listen on 127.0.0.1, be contacted.
func testAllow(ip net.IP) bool { return ip.IsLoopback() || PrivateIPv4(ip) }

func portOf(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := strconv.Atoi(u.Port())
	return p
}

func TestParseSubnets(t *testing.T) {
	var d Discoverer
	nine := []string{}
	for i := 0; i < 9; i++ {
		nine = append(nine, fmt.Sprintf("192.168.%d.0/24", i))
	}
	cases := []struct {
		name    string
		in      []string
		want    []string
		wantErr string
	}{
		{"home network", []string{"192.168.1.0/24"}, []string{"192.168.1.0/24"}, ""},
		{"host bits dropped, duplicates merged", []string{" 192.168.1.77/24", "192.168.1.0/24", ""}, []string{"192.168.1.0/24"}, ""},
		{"one address", []string{"10.1.2.3"}, []string{"10.1.2.3/32"}, ""},
		{"smaller network", []string{"172.20.5.64/26"}, []string{"172.20.5.64/26"}, ""},
		{"tailscale range", []string{"100.100.1.0/24"}, []string{"100.100.1.0/24"}, ""},
		{"nothing", nil, nil, ""},
		{"public network", []string{"8.8.8.0/24"}, nil, "not a private network"},
		{"public address", []string{"1.1.1.1"}, nil, "not a private network"},
		{"just outside 172.16/12", []string{"172.32.0.0/24"}, nil, "not a private network"},
		{"loopback", []string{"127.0.0.0/24"}, nil, "not a private network"},
		{"link-local", []string{"169.254.1.0/24"}, nil, "not a private network"},
		{"too large", []string{"192.168.0.0/16"}, nil, "too large"},
		{"huge public", []string{"0.0.0.0/0"}, nil, "too large"},
		{"ipv6", []string{"fd00::/120"}, nil, "not a network"},
		{"garbage", []string{"home"}, nil, "not a network"},
		{"too many", nine, nil, "at most 8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := d.ParseSubnets(tc.in)
			if tc.wantErr != "" {
				var ue *UserError
				if !errors.As(err, &ue) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var s []string
			for _, n := range got {
				s = append(s, n.String())
			}
			if fmt.Sprint(s) != fmt.Sprint(tc.want) {
				t.Fatalf("got %v, want %v", s, tc.want)
			}
		})
	}
}

func TestDefaultSubnets(t *testing.T) {
	d := Discoverer{interfaces: func() ([]*net.IPNet, error) {
		mk := func(ip string, ones int) *net.IPNet {
			return &net.IPNet{IP: net.ParseIP(ip).To4(), Mask: net.CIDRMask(ones, 32)}
		}
		// Docker's bridge network, a public address, and the home network.
		return []*net.IPNet{mk("172.17.0.5", 16), mk("81.2.3.4", 24), mk("192.168.1.20", 24), mk("10.9.9.9", 28)}, nil
	}}
	var got []string
	for _, n := range d.DefaultSubnets() {
		got = append(got, n.String())
	}
	want := []string{"172.17.0.0/24", "192.168.1.0/24", "10.9.9.0/28", "192.168.0.0/24", "10.0.0.0/24", "10.0.1.0/24", "172.16.0.0/24"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestHosts(t *testing.T) {
	cases := map[string][2]string{ // first and last address tried
		"192.168.1.0/24":  {"192.168.1.1", "192.168.1.254"},
		"10.0.0.8/30":     {"10.0.0.9", "10.0.0.10"},
		"10.0.0.8/31":     {"10.0.0.8", "10.0.0.9"},
		"10.0.0.8/32":     {"10.0.0.8", "10.0.0.8"},
		"172.16.5.128/25": {"172.16.5.129", "172.16.5.254"},
	}
	for cidr, want := range cases {
		_, n, _ := net.ParseCIDR(cidr)
		h := hosts(n)
		if h[0].String() != want[0] || h[len(h)-1].String() != want[1] {
			t.Errorf("%s: %s..%s, want %s..%s", cidr, h[0], h[len(h)-1], want[0], want[1])
		}
	}
}

func TestParseReplies(t *testing.T) {
	src := net.ParseIP("192.168.1.9")
	embyCases := []struct {
		name string
		data string
		ok   bool
		want Found
	}{
		{"jellyfin", `{"Address":"http://192.168.1.9:8096","Id":"j1","Name":"Den","EndpointAddress":null}`, true,
			Found{Kind: KindJellyfin, Name: "Den", Address: "http://192.168.1.9:8096", ID: "j1"}},
		{"public address replaced by source", `{"Address":"https://jf.example.com:8920","Id":"j1","Name":"Den"}`, true,
			Found{Kind: KindJellyfin, Name: "Den", Address: "https://192.168.1.9:8920", ID: "j1"}},
		{"no address", `{"Id":"j1"}`, true, Found{Kind: KindJellyfin, Name: "Jellyfin at 192.168.1.9", Address: "http://192.168.1.9:8096", ID: "j1"}},
		{"no id", `{"Address":"http://192.168.1.9:8096"}`, false, Found{}},
		{"not json", `hello`, false, Found{}},
	}
	for _, tc := range embyCases {
		got, ok := parseEmbyReply([]byte(tc.data), src, KindJellyfin, PrivateIPv4)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: %+v %v, want %+v %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	gdmCases := []struct {
		name string
		data string
		ok   bool
		want Found
	}{
		{"plex", "HTTP/1.0 200 OK\r\nContent-Type: plex/media-server\r\nResource-Identifier: m1\r\nName: Living Room\r\nPort: 32401\r\nVersion: 1.41.0\r\n\r\n", true,
			Found{Kind: KindPlex, Name: "Living Room", Address: "http://192.168.1.9:32401", ID: "m1", Version: "1.41.0"}},
		{"bad port keeps default", "HTTP/1.0 200 OK\r\nResource-Identifier: m1\r\nPort: 99999\r\n", true,
			Found{Kind: KindPlex, Name: "Plex at 192.168.1.9", Address: "http://192.168.1.9:32400", ID: "m1"}},
		{"a Plex player, not a server", "HTTP/1.0 200 OK\r\nContent-Type: plex/media-player\r\nResource-Identifier: p1\r\n", false, Found{}},
		{"no identifier", "HTTP/1.0 200 OK\r\nName: x\r\n", false, Found{}},
		{"error", "HTTP/1.0 404 Not Found\r\nResource-Identifier: m1\r\n", false, Found{}},
	}
	for _, tc := range gdmCases {
		got, ok := parseGDM([]byte(tc.data), src)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: %+v %v, want %+v %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// udpResponder answers every datagram containing want with reply.
func udpResponder(t *testing.T, want, reply string) string {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if strings.Contains(string(buf[:n]), want) {
				_, _ = conn.WriteTo([]byte(reply), from)
			}
		}
	}()
	return conn.LocalAddr().String()
}

func TestDiscover(t *testing.T) {
	plex := newFakePlex(t, "tok")
	jelly := newFakeAuthServer(t, KindJellyfin)
	emby := newFakeAuthServer(t, KindEmby)

	jfReply := fmt.Sprintf(`{"Address":%q,"Id":"srv-jellyfin","Name":"Den Jellyfin"}`, jelly.srv.URL)
	gdmReply := fmt.Sprintf("HTTP/1.0 200 OK\r\nContent-Type: plex/media-server\r\nResource-Identifier: abc123machine\r\nName: Living Room\r\nPort: %d\r\n\r\n", portOf(t, plex.srv.URL))

	var mu sync.Mutex
	var dialed []string
	d := &Discoverer{
		Budget: 5 * time.Second, BroadcastWait: 300 * time.Millisecond, Workers: 4,
		Probes: []Probe{
			{Port: portOf(t, plex.srv.URL), Scheme: "http", Plex: true},
			{Port: portOf(t, jelly.srv.URL), Scheme: "http"},
			{Port: portOf(t, emby.srv.URL), Scheme: "http"},
		},
		EmbyTargets: []string{udpResponder(t, "who is JellyfinServer?", jfReply)},
		PlexTargets: []string{udpResponder(t, "M-SEARCH * HTTP/1.1", gdmReply)},
		allow:       testAllow,
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	_, lo, _ := net.ParseCIDR("127.0.0.1/32")
	res := d.Discover(context.Background(), []*net.IPNet{lo})

	if fmt.Sprint(res.Scanned) != "[127.0.0.1/32]" || res.Note != "" {
		t.Fatalf("scanned %v note %q", res.Scanned, res.Note)
	}
	byKind := map[Kind]Found{}
	for _, f := range res.Found {
		if _, dup := byKind[f.Kind]; dup {
			t.Fatalf("duplicate %s in %+v", f.Kind, res.Found)
		}
		byKind[f.Kind] = f
	}
	want := map[Kind]Found{
		KindPlex:     {Kind: KindPlex, Name: "Living Room", Address: plex.srv.URL, Version: "1.41.0", ID: "abc123machine", Via: "broadcast"},
		KindJellyfin: {Kind: KindJellyfin, Name: "Den Jellyfin", Address: jelly.srv.URL, Version: "10.10.3", ID: "srv-jellyfin", Via: "broadcast"},
		KindEmby:     {Kind: KindEmby, Name: "Den Emby", Address: emby.srv.URL, Version: "4.8.0", ID: "srv-emby", Via: "scan"},
	}
	if len(byKind) != 3 {
		t.Fatalf("found %+v", res.Found)
	}
	for k, w := range want {
		if byKind[k] != w {
			t.Errorf("%s: got %+v\nwant %+v", k, byKind[k], w)
		}
	}
	if len(dialed) != 3 {
		t.Errorf("dialed %v", dialed)
	}

	// Saved servers are marked, by server id or by address.
	MarkAdded(res.Found, []Server{{ServerID: "abc123machine", BaseURL: "http://elsewhere:32400"}, {BaseURL: emby.srv.URL + "/"}})
	for _, f := range res.Found {
		if f.AlreadyAdded != (f.Kind != KindJellyfin) {
			t.Errorf("%s alreadyAdded = %v", f.Kind, f.AlreadyAdded)
		}
	}
}

func TestDiscoverTimeBudget(t *testing.T) {
	var mu sync.Mutex
	var dialed []string
	d := &Discoverer{
		Budget: 150 * time.Millisecond, BroadcastWait: -1, Workers: 1, DialTimeout: 40 * time.Millisecond,
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			<-ctx.Done() // nothing answers
			return nil, ctx.Err()
		},
	}
	subnets, err := d.ParseSubnets([]string{"10.20.30.0/29"}) // 6 hosts x 2 ports
	if err != nil {
		t.Fatal(err)
	}
	res := d.Discover(context.Background(), subnets)
	if len(res.Found) != 0 || !strings.Contains(res.Note, "addresses not checked") {
		t.Fatalf("result %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dialed) == 0 || len(dialed) >= 12 {
		t.Fatalf("dialed %d addresses", len(dialed))
	}
	for _, a := range dialed {
		host, _, _ := net.SplitHostPort(a)
		if !PrivateIPv4(net.ParseIP(host)) {
			t.Fatalf("dialed a non-private address %s", a)
		}
	}
}

func TestDiscoverNothingFound(t *testing.T) {
	d := &Discoverer{
		BroadcastWait: -1,
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, errors.New("connection refused")
		},
	}
	subnets, _ := d.ParseSubnets([]string{"192.168.50.0/24"})
	res := d.Discover(context.Background(), subnets)
	if len(res.Found) != 0 || !strings.Contains(res.Note, "Docker") || fmt.Sprint(res.Scanned) != "[192.168.50.0/24]" {
		t.Fatalf("result %+v", res)
	}
}

func TestProbeClientRefusesPublicAddresses(t *testing.T) {
	d := &Discoverer{}
	_, err := d.probeClient().Get("http://8.8.8.8:32400/identity")
	if err == nil || !strings.Contains(err.Error(), "not a private network address") {
		t.Fatalf("err = %v", err)
	}
}
