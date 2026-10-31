package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/netproxy"
)

func TestRouteIndexersThroughVPNSetting(t *testing.T) {
	t.Cleanup(func() { netproxy.SetVPN(nil, nil) })
	_, base, client := loginNewServer(t)

	got := getJSON[map[string]any](t, client, base+"/api/settings")
	if got["routeIndexersThroughVpn"] != false {
		t.Fatalf("off by default: %v", got["routeIndexersThroughVpn"])
	}
	if got["discoverThroughTunnel"] != false {
		t.Fatalf("udp discovery off by default: %v", got["discoverThroughTunnel"])
	}
	saved := putJSONStatus(t, client, base+"/api/settings", map[string]any{"routeIndexersThroughVpn": true}, http.StatusOK)
	if saved["routeIndexersThroughVpn"] != true || !netproxy.Routed(netproxy.Indexers) {
		t.Fatalf("switching it on should route indexers: %v", saved["routeIndexersThroughVpn"])
	}

	// No VPN is switched on, so testing a public indexer fails instead of
	// going direct. The address is never contacted.
	res := postJSON[map[string]any](t, client, base+"/api/indexers/test", map[string]any{
		"name": "N", "baseUrl": "https://indexer.example.com", "apiKey": "k", "protocol": "usenet",
	}, http.StatusOK)
	if res["ok"] != false || !strings.Contains(res["message"].(string), "no VPN is switched on") {
		t.Fatalf("test with no VPN: %v", res)
	}

	putJSONStatus(t, client, base+"/api/settings", map[string]any{"routeIndexersThroughVpn": false}, http.StatusOK)
	if netproxy.Routed(netproxy.Indexers) {
		t.Fatal("switching it off should leave indexers direct")
	}
}

func TestHealthWhenIndexersWaitForTheVPN(t *testing.T) {
	t.Cleanup(func() { netproxy.SetVPN(nil, nil) })
	_, base, client := loginNewServer(t)
	// Torrents off: the VPN items must still show for indexers.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"torrentEnabled": false, "routeIndexersThroughVpn": true}, http.StatusOK)

	h := healthByID(t, client, base)
	if h["vpn-indexers-none"] == nil || h["vpn-indexers-none"]["level"] != "error" {
		t.Fatalf("routing on with no VPN should be an error: %v", keysOf(h))
	}
}
