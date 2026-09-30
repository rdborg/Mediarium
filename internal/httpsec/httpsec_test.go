package httpsec

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustProxies(t *testing.T, spec string) *Proxies {
	t.Helper()
	p, err := ParseProxies(spec)
	if err != nil {
		t.Fatalf("ParseProxies(%q): %v", spec, err)
	}
	return p
}

func req(method, remote string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(method, "http://media.example.com/api/x", nil)
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		remote  string
		headers map[string]string
		want    string
	}{
		{"direct visitor cannot spoof X-Forwarded-For", "private", "203.0.113.9:5000", map[string]string{"X-Forwarded-For": "1.2.3.4"}, "203.0.113.9"},
		{"direct visitor cannot spoof X-Real-IP", "private", "203.0.113.9:5000", map[string]string{"X-Real-IP": "1.2.3.4"}, "203.0.113.9"},
		{"proxy on private range: client from XFF", "private", "172.18.0.2:5000", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
		{"caller-supplied left entries are ignored", "private", "172.18.0.2:5000", map[string]string{"X-Forwarded-For": "6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		{"chain of trusted proxies is skipped", "private", "172.18.0.2:5000", map[string]string{"X-Forwarded-For": "198.51.100.7, 10.0.0.5, 192.168.1.1"}, "198.51.100.7"},
		{"X-Real-IP used when no XFF", "private", "127.0.0.1:5000", map[string]string{"X-Real-IP": "198.51.100.7"}, "198.51.100.7"},
		{"no headers: peer", "private", "127.0.0.1:5000", nil, "127.0.0.1"},
		{"garbage in XFF falls back to peer", "private", "172.18.0.2:5000", map[string]string{"X-Forwarded-For": "not-an-ip"}, "172.18.0.2"},
		{"LAN client through proxy", "private", "172.18.0.2:5000", map[string]string{"X-Forwarded-For": "192.168.1.50"}, "192.168.1.50"},
		{"none trusts no proxy", "none", "172.18.0.2:5000", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "172.18.0.2"},
		{"explicit public proxy", "203.0.113.0/24", "203.0.113.9:5000", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
		{"private peer not trusted when list is explicit", "203.0.113.0/24", "172.18.0.2:5000", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "172.18.0.2"},
		{"port in XFF entry", "private", "172.18.0.2:5000", map[string]string{"X-Forwarded-For": "198.51.100.7:4433"}, "198.51.100.7"},
		{"IPv4-mapped IPv6 peer", "private", "[::ffff:10.0.0.2]:5000", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustProxies(t, tt.spec).ClientIP(req("GET", tt.remote, tt.headers))
			if got != tt.want {
				t.Errorf("ClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseProxiesRejectsGarbage(t *testing.T) {
	for _, spec := range []string{"banana", "10.0.0.0/33", "10.0.0.0/8,what"} {
		if _, err := ParseProxies(spec); err == nil {
			t.Errorf("ParseProxies(%q) succeeded, want an error", spec)
		}
	}
}

func TestLimitKeyGroupsIPv6ByPrefix(t *testing.T) {
	if a, b := LimitKey("2001:db8:1:2::1"), LimitKey("2001:db8:1:2:ffff::9"); a != b {
		t.Errorf("same /64 gave different keys: %q %q", a, b)
	}
	if a, b := LimitKey("2001:db8:1:2::1"), LimitKey("2001:db8:1:3::1"); a == b {
		t.Errorf("different /64s share a key: %q", a)
	}
	if LimitKey("198.51.100.7") != "198.51.100.7" {
		t.Error("IPv4 key changed")
	}
}

func TestIsHTTPS(t *testing.T) {
	p := mustProxies(t, "private")
	if p.IsHTTPS(req("GET", "203.0.113.9:1", map[string]string{"X-Forwarded-Proto": "https"})) {
		t.Error("X-Forwarded-Proto from an untrusted peer was believed")
	}
	if !p.IsHTTPS(req("GET", "127.0.0.1:1", map[string]string{"X-Forwarded-Proto": "https"})) {
		t.Error("X-Forwarded-Proto: https from a trusted proxy was not believed")
	}
	if p.IsHTTPS(req("GET", "127.0.0.1:1", map[string]string{"X-Forwarded-Proto": "http"})) {
		t.Error("http reported as https")
	}
	if !p.IsHTTPS(req("GET", "127.0.0.1:1", map[string]string{"Forwarded": "for=1.2.3.4;proto=https"})) {
		t.Error("Forwarded proto=https not understood")
	}
	direct := req("GET", "203.0.113.9:1", nil)
	direct.TLS = &tls.ConnectionState{}
	if !p.IsHTTPS(direct) {
		t.Error("direct TLS not detected")
	}
}

func TestCSRF(t *testing.T) {
	p := mustProxies(t, "private")
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	tests := []struct {
		name    string
		method  string
		remote  string
		host    string
		headers map[string]string
		extra   []string
		want    int
	}{
		{"GET is never blocked", "GET", "127.0.0.1:1", "media.example.com", map[string]string{"Origin": "https://evil.example"}, nil, 204},
		{"POST same origin", "POST", "127.0.0.1:1", "media.example.com", map[string]string{"Origin": "https://media.example.com"}, nil, 204},
		{"POST cross origin", "POST", "127.0.0.1:1", "media.example.com", map[string]string{"Origin": "https://evil.example"}, nil, 403},
		{"POST null origin", "POST", "127.0.0.1:1", "media.example.com", map[string]string{"Origin": "null"}, nil, 403},
		{"POST cross-site by Sec-Fetch-Site, no Origin", "POST", "127.0.0.1:1", "media.example.com", map[string]string{"Sec-Fetch-Site": "cross-site"}, nil, 403},
		{"POST sibling subdomain", "POST", "127.0.0.1:1", "media.example.com", map[string]string{"Origin": "https://evil.example.com", "Sec-Fetch-Site": "same-site"}, nil, 403},
		{"POST no Origin (curl)", "POST", "127.0.0.1:1", "media.example.com", nil, nil, 204},
		{"POST Referer only, cross-site", "POST", "127.0.0.1:1", "media.example.com", map[string]string{"Referer": "https://evil.example/page"}, nil, 403},
		{"POST Referer only, same host", "POST", "127.0.0.1:1", "media.example.com", map[string]string{"Referer": "https://media.example.com/settings"}, nil, 204},
		{"proxy rewrites Host, browser says same-origin", "POST", "172.18.0.2:1", "mediarium:8264", map[string]string{"Origin": "https://media.example.com", "Sec-Fetch-Site": "same-origin"}, nil, 204},
		{"proxy rewrites Host, X-Forwarded-Host passed", "POST", "172.18.0.2:1", "mediarium:8264", map[string]string{"Origin": "https://media.example.com", "X-Forwarded-Host": "media.example.com"}, nil, 204},
		{"X-Forwarded-Host from an untrusted peer is ignored", "POST", "203.0.113.9:1", "media.example.com", map[string]string{"Origin": "https://evil.example", "X-Forwarded-Host": "evil.example"}, nil, 403},
		{"proxy rewrites Host, old browser, ALLOWED_ORIGINS", "POST", "172.18.0.2:1", "mediarium:8264", map[string]string{"Origin": "https://media.example.com"}, []string{"media.example.com"}, 204},
		{"proxy rewrites Host, old browser, nothing configured", "POST", "172.18.0.2:1", "mediarium:8264", map[string]string{"Origin": "https://media.example.com"}, nil, 403},
		{"API key client", "POST", "203.0.113.9:1", "media.example.com", map[string]string{"X-API-Key": "k", "Origin": "https://tool.example"}, nil, 204},
		{"default port equals none", "POST", "127.0.0.1:1", "media.example.com:443", map[string]string{"Origin": "https://media.example.com"}, nil, 204},
		{"DELETE cross origin", "DELETE", "127.0.0.1:1", "media.example.com", map[string]string{"Origin": "https://evil.example"}, nil, 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := req(tt.method, tt.remote, tt.headers)
			r.Host = tt.host
			w := httptest.NewRecorder()
			p.CSRF(tt.extra, ok).ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestHeaders(t *testing.T) {
	p := mustProxies(t, "private")
	h := p.Headers(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	plain := httptest.NewRecorder()
	h.ServeHTTP(plain, req("GET", "203.0.113.9:1", nil))
	for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Permissions-Policy", "X-Robots-Tag"} {
		if plain.Header().Get(name) == "" {
			t.Errorf("missing %s", name)
		}
	}
	if plain.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent over plain HTTP")
	}

	secure := httptest.NewRecorder()
	h.ServeHTTP(secure, req("GET", "127.0.0.1:1", map[string]string{"X-Forwarded-Proto": "https"}))
	if secure.Header().Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS behind an HTTPS proxy")
	}
}
