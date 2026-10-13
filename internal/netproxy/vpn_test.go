package netproxy

import "testing"

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
