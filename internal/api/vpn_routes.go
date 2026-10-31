package api

import (
	"github.com/rdborg/mediarium/internal/netproxy"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/vpn"
)

// vpnRoute lets netproxy send traffic through the VPN without knowing about
// the VPN package.
type vpnRoute struct{ m *vpn.Manager }

func (r vpnRoute) Tunnel() netproxy.ContextDialer {
	// Check for a nil *Tunnel before converting to the interface, or a nil
	// pointer would look like a real tunnel.
	if t := r.m.Tunnel(); t != nil {
		return t
	}
	return nil
}

func (r vpnRoute) Selected() (label, reason string) {
	st := r.m.Status()
	return st.Label, st.Reason
}

// vpnForIndexers reports whether indexer traffic is set to go through the VPN.
// vpnNoticeAcknowledged reports that the person said their torrent client is
// already behind a VPN.
func (s *Server) vpnNoticeAcknowledged() bool {
	v, _ := s.Settings.GetBool(settings.KeyVPNNoticeAcknowledged)
	return v
}

func (s *Server) vpnForIndexers() bool {
	v, _ := s.Settings.GetBool(settings.KeyVPNRouteIndexers)
	return v
}

// vpnUDPDiscovery reports whether UDP trackers and DHT go through the VPN.
func (s *Server) vpnUDPDiscovery() bool {
	v, _ := s.Settings.GetBool(settings.KeyVPNDiscoverThroughTunnel)
	return v
}

// applyVPNRoutes tells netproxy which traffic goes through the VPN. Torrents
// aren't part of it: the torrent engine is handed the tunnel itself (see
// torrentTunnel).
func (s *Server) applyVPNRoutes() {
	netproxy.SetVPN(vpnRoute{s.VPNManager}, map[netproxy.Use]bool{netproxy.Indexers: s.vpnForIndexers()})
}
