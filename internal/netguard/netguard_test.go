package netguard

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestBlocked(t *testing.T) {
	tests := map[string]bool{
		"169.254.169.254":          true, // AWS, Azure, GCP, DigitalOcean metadata
		"169.254.0.1":              true,
		"fe80::1":                  true,
		"fd00:ec2::254":            true, // AWS IPv6 metadata
		"100.100.100.200":          true, // Alibaba
		"192.0.0.192":              true, // Oracle
		"0.0.0.0":                  true,
		"::":                       true,
		"0.1.2.3":                  true,
		"224.0.0.1":                true,
		"255.255.255.255":          true,
		"::ffff:169.254.169.254":   true, // IPv4-mapped form of the metadata address
		"168.63.129.16":            true, // Azure's host service
		"64:ff9b::a9fe:a9fe":       true, // NAT64 form of the metadata address
		"64:ff9b::169.254.169.254": true,
		"2002:a9fe:a9fe::1":        true, // 6to4 form of it
		"::a9fe:a9fe":              true, // IPv4-compatible form of it
		"::ffff:168.63.129.16":     true,
		"64:ff9b::808:808":         false, // NAT64 form of a public address
		"2002:c000:204::1":         false, // 6to4 form of 192.0.2.4
		"127.0.0.1":                false, // local services stay reachable
		"::1":                      false,
		"192.168.1.10":             false,
		"10.0.0.5":                 false,
		"172.17.0.2":               false,
		"100.64.0.1":               false, // Tailscale / CGNAT
		"93.184.216.34":            false,
		"2606:4700:4700::1111":     false,
	}
	for ip, want := range tests {
		if got := Blocked(netip.MustParseAddr(ip)); got != want {
			t.Errorf("Blocked(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestClientRefusesMetadataButReachesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	c := Client(3 * time.Second)

	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("loopback should stay reachable: %v", err)
	}
	resp.Body.Close()

	for _, target := range []string{"http://169.254.169.254/latest/meta-data/", "http://[fd00:ec2::254]/", "http://0.0.0.0:1/"} {
		_, err := c.Get(target)
		if !errors.Is(err, ErrBlocked) {
			t.Errorf("GET %s: err = %v, want ErrBlocked", target, err)
		}
	}
}

func TestRedirectToMetadataIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()
	_, err := Client(3 * time.Second).Get(srv.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("redirect to the metadata address: err = %v, want ErrBlocked", err)
	}
}

func TestRedactURL(t *testing.T) {
	tests := map[string]string{
		"https://idx.example/api?t=search&apikey=SECRET123&q=x":           "https://idx.example/api?apikey=REDACTED&q=REDACTED&t=REDACTED",
		"https://user:pw@idx.example/dl":                                  "https://idx.example/dl",
		"https://idx.example/getnzb/0123456789abcdef0123456789abcdef.nzb": "https://idx.example/getnzb/0123456789abcdef0123456789abcdef.nzb",
		"https://idx.example/api/abcdefghij0123456789abcdefghij/get":      "https://idx.example/api/REDACTED/get",
		"not a url": "not a url",
	}
	for in, want := range tests {
		got := RedactURL(in)
		if strings.Contains(got, "SECRET123") || strings.Contains(got, "pw@") {
			t.Errorf("RedactURL(%q) leaked: %q", in, got)
		}
		if got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanErrorHidesKeyInClientError(t *testing.T) {
	_, err := Client(time.Second).Get("http://169.254.169.254/x?apikey=SECRET123")
	if err == nil {
		t.Fatal("expected an error")
	}
	if msg := CleanError(err).Error(); strings.Contains(msg, "SECRET123") {
		t.Fatalf("key still in error: %s", msg)
	}
}

// A token sent in a header must not follow a redirect to another host.
func TestRedirectToAnotherHostDropsCredentialHeaders(t *testing.T) {
	var got http.Header
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		io.WriteString(w, "ok")
	}))
	defer other.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			io.WriteString(w, "ok")
		case "/next":
			http.Redirect(w, r, "/same", http.StatusFound)
		default:
			http.Redirect(w, r, other.URL+"/landing", http.StatusFound)
		}
	}))
	defer first.Close()

	do := func(path string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, first.URL+path, nil)
		req.Header.Set("X-Plex-Token", "SECRETTOKEN")
		req.Header.Set("X-Gotify-Key", "SECRETKEY")
		req.Header.Set("Api-Key", "SECRETAPIKEY")
		req.Header.Set("User-Agent", "Mediarium-test")
		req.Header.Set("Accept", "application/json")
		resp, err := Client(3 * time.Second).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	do("/away")
	for _, h := range []string{"X-Plex-Token", "X-Gotify-Key", "Api-Key"} {
		if v := got.Get(h); v != "" {
			t.Errorf("%s followed the redirect to another host: %q", h, v)
		}
	}
	if got.Get("User-Agent") != "Mediarium-test" || got.Get("Accept") != "application/json" {
		t.Errorf("the plain headers should still be sent, got %v", got)
	}
}

// Redirects that stay on the same host keep working with the token.
func TestRedirectOnTheSameHostKeepsHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		got = r.Header.Clone()
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/start", nil)
	req.Header.Set("X-Plex-Token", "SECRETTOKEN")
	resp, err := Client(3 * time.Second).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got.Get("X-Plex-Token") != "SECRETTOKEN" {
		t.Errorf("the token was lost on a redirect within the same host: %v", got)
	}
}
