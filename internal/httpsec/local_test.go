package httpsec

import "testing"

func TestIsHomeAddress(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true}, {"::1", true}, {"10.1.2.3", true}, {"172.16.0.9", true}, {"172.31.255.1", true},
		{"192.168.1.20", true}, {"100.100.1.1", true}, {"169.254.1.1", true}, {"fe80::1", true}, {"fd12:3456::1", true},
		{"::ffff:192.168.1.5", true},
		{"172.32.0.1", false}, {"8.8.8.8", false}, {"198.51.100.7", false}, {"2001:db8::1", false},
		{"::ffff:8.8.8.8", false}, {"", false}, {"not-an-ip", false}, {"192.168.1.5.evil.com", false},
	}
	for _, tt := range tests {
		if got := IsHomeAddress(tt.ip); got != tt.want {
			t.Errorf("IsHomeAddress(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestIsHomeHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true}, {"localhost:8264", true}, {"127.0.0.1:8264", true}, {"192.168.1.10", true},
		{"[::1]:8264", true}, {"[fd00::5]", true}, {"nas", true}, {"nas:8264", true}, {"mediarium.local", true},
		{"nas.lan:8264", true}, {"box.home.arpa", true}, {"thing.internal", true}, {"Server.LOCAL.", true},
		{"media.example.com", false}, {"media.example.com:443", false}, {"evil.com", false},
		{"192.168.1.10.evil.com", false}, {"local.evil.com", false}, {"", false},
	}
	for _, tt := range tests {
		if got := IsHomeHost(tt.host); got != tt.want {
			t.Errorf("IsHomeHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestFromHome(t *testing.T) {
	const localHost = "192.168.1.10:8264"
	tests := []struct {
		name    string
		spec    string
		remote  string
		host    string
		headers map[string]string
		want    bool
	}{
		{"a LAN device connecting directly", "private", "192.168.1.50:4000", localHost, nil, true},
		{"the machine itself", "private", "127.0.0.1:4000", "localhost:8264", nil, true},
		{"a stranger connecting directly", "private", "203.0.113.9:4000", localHost, nil, false},
		{"a stranger claiming to be private", "private", "203.0.113.9:4000", localHost, map[string]string{"X-Forwarded-For": "192.168.1.5"}, false},
		{"a LAN device through a proxy on the LAN", "private", "172.18.0.2:4000", localHost, map[string]string{"X-Forwarded-For": "192.168.1.50"}, true},
		{"a visitor from the internet through a proxy", "private", "172.18.0.2:4000", localHost, map[string]string{"X-Forwarded-For": "198.51.100.7"}, false},
		{"a proxy that hides the visitor", "private", "172.18.0.2:4000", localHost, map[string]string{"X-Forwarded-Proto": "https"}, false},
		{"a proxy that hides the visitor with a real host", "private", "172.18.0.2:4000", localHost, map[string]string{"X-Forwarded-Host": "media.example.com"}, false},
		{"forged private XFF, real X-Real-IP", "private", "172.18.0.2:4000", localHost, map[string]string{"X-Forwarded-For": "192.168.1.5", "X-Real-IP": "198.51.100.7"}, false},
		{"a public name pointing home (DNS rebinding)", "private", "192.168.1.50:4000", "evil.example.com:8264", nil, false},
		{"no trusted proxy: headers are noise", "none", "192.168.1.50:4000", localHost, map[string]string{"X-Forwarded-For": "198.51.100.7"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := req("POST", tt.remote, tt.headers)
			r.Host = tt.host
			if got := mustProxies(t, tt.spec).FromHome(r); got != tt.want {
				t.Errorf("FromHome = %v, want %v", got, tt.want)
			}
		})
	}
}
