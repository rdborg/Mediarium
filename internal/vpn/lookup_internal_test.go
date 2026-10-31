package vpn

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestLookupHostFailsClosedWithoutDNS(t *testing.T) {
	tun := &Tunnel{}
	if _, err := tun.LookupHost(context.Background(), "tracker.example"); err == nil {
		t.Fatal("a name was looked up with no VPN DNS servers")
	}
}

func TestLookupHostReturnsALiteral(t *testing.T) {
	tun := &Tunnel{}
	ips, err := tun.LookupHost(context.Background(), "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("192.0.2.1")) {
		t.Fatalf("addresses = %v", ips)
	}
	if _, err := tun.LookupHost(context.Background(), "tracker.example"); err == nil || !strings.Contains(err.Error(), "no DNS") {
		t.Fatalf("hostname error = %v", err)
	}
}
