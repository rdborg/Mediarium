package vpn

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestResolveEndpoint(t *testing.T) {
	old := lookupIP
	defer func() { lookupIP = old }()
	lookupIP = func(_ context.Context, network, host string) ([]netip.Addr, error) {
		switch host {
		case "us-slc.prod.surfshark.com":
			return []netip.Addr{netip.MustParseAddr("2001:db8::7"), netip.MustParseAddr("172.216.252.7")}, nil
		case "v6only.example":
			return []netip.Addr{netip.MustParseAddr("2001:db8::9")}, nil
		case "empty.example":
			return nil, nil
		}
		return nil, errors.New("lookup " + host + ": no such host")
	}

	tests := []struct {
		name, in, want, wantErr string
	}{
		{"an IPv4 address is left alone", "203.0.113.5:51820", "203.0.113.5:51820", ""},
		{"an IPv6 address is left alone", "[2001:db8::1]:51820", "[2001:db8::1]:51820", ""},
		{"a name becomes its IPv4 address", "us-slc.prod.surfshark.com:51820", "172.216.252.7:51820", ""},
		{"a name with only IPv6 keeps it", "v6only.example:1234", "[2001:db8::9]:1234", ""},
		{"a name that finds nothing", "empty.example:51820", "", "no address found"},
		{"a name that does not exist", "nope.example:51820", "", "no such host"},
		{"no port", "us-slc.prod.surfshark.com", "", "missing port"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveEndpoint(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("resolveEndpoint(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
}

// A config with a host name reaches wireguard-go as an address, which is what
// its config protocol accepts, and a name that can't be found says so in words
// a person can act on.
func TestUAPIConfigResolvesHostName(t *testing.T) {
	old := lookupIP
	defer func() { lookupIP = old }()
	lookupIP = func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host == "vpn.example.test" {
			return []netip.Addr{netip.MustParseAddr("198.51.100.8")}, nil
		}
		return nil, errors.New("lookup " + host + ": no such host")
	}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	cfg := Config{PrivateKey: key, PeerPublicKey: key, Endpoint: "vpn.example.test:51820"}
	got, err := cfg.uapiConfig()
	if err != nil || !strings.Contains(got, "endpoint=198.51.100.8:51820\n") {
		t.Fatalf("uapiConfig = %q, %v; want the resolved address", got, err)
	}
	cfg.Endpoint = "gone.example.test:51820"
	_, err = cfg.uapiConfig()
	if err == nil {
		t.Fatal("a name that can't be found must fail")
	}
	if msg := Explain(err); !strings.Contains(msg, "Couldn't find the VPN server") {
		t.Fatalf("Explain = %q, want the 'couldn't find the VPN server' message", msg)
	}
}
