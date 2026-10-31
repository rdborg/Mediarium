package api

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/torrentclient"
	"github.com/rdborg/mediarium/internal/vpn"
)

// torrentRoute says how a torrent's traffic left the machine, as far as a
// failure note needs to know.
type torrentRoute struct {
	// VPN is true when the torrent ran through the built-in VPN tunnel.
	VPN bool
	// VPNConnected is true when that VPN was answering at the time of the failure.
	VPNConnected bool
	// Handshake is when the VPN server last answered; zero if it never has.
	Handshake time.Time
	// Proxy is true when the torrent ran through a proxy for torrents.
	Proxy bool
	// Discover is true when UDP trackers and DHT were sent through the VPN.
	Discover bool
}

// torrentFailure is a torrent error with what the app knows about the network
// added in plain words. It still matches the error it wraps (errors.Is), so
// the blocklist and the "not a bad release" checks keep working.
type torrentFailure struct {
	err  error
	note string
}

func (e *torrentFailure) Error() string {
	msg := e.err.Error()
	if msg == "" {
		return e.note
	}
	runes := []rune(msg)
	runes[0] = unicode.ToUpper(runes[0])
	return strings.TrimRight(string(runes), ". ") + ". " + e.note
}

func (e *torrentFailure) Unwrap() error { return e.err }

// explainTorrentFailure adds the network context to a "found no one" error:
// whether a VPN or proxy was in the way, and what to try. tunnel is the dialer
// the torrent was started with (nil: the direct connection).
func (s *Server) explainTorrentFailure(err error, tunnel torrentclient.TunnelDialer) error {
	var de torrentclient.DiagError
	if !errors.As(err, &de) {
		return err
	}
	route := torrentRoute{}
	if vt, ok := tunnel.(*vpn.Tunnel); ok && vt != nil {
		st := s.VPNManager.Status()
		route = torrentRoute{VPN: true, VPNConnected: st.Connected, Handshake: st.LastHandshake, Discover: s.vpnUDPDiscovery()}
	} else if tunnel != nil {
		route.Proxy = true
	}
	return &torrentFailure{err: err, note: failureNote(route, de.Diagnosis())}
}

// failureNote is the sentences added after the counts: how the traffic was
// routed, and a hint that fits. The "try it with the VPN off" hint is only
// given when a VPN really was on.
func failureNote(route torrentRoute, d torrentclient.Diag) string {
	var parts []string
	switch {
	case route.VPN && route.VPNConnected:
		parts = append(parts, "The VPN was on and connected.")
	case route.VPN && route.Handshake.IsZero():
		parts = append(parts, "The VPN was on, but its server never answered.")
	case route.VPN:
		parts = append(parts, "The VPN was on, but its server had stopped answering.")
	case route.Proxy:
		parts = append(parts, "The torrent went through your proxy.")
	default:
		parts = append(parts, "The VPN was off.")
	}
	switch {
	case route.VPN && !route.VPNConnected:
		parts = append(parts, "Press Test on the VPN page (Settings > Downloading > VPN and proxy) to see what is wrong with the connection.")
	case route.VPN && d.TrackersTried == 0 && route.Discover:
		parts = append(parts, "This link lists no trackers. DHT ran through the VPN and still found no peers. Try another release.")
	case route.VPN && d.TrackersTried == 0:
		parts = append(parts, "This link lists no trackers, and DHT is off while the VPN is on, so there was no way to find peers. Try another release.")
	case route.VPN:
		parts = append(parts, "Try the same torrent with the VPN off, to see if the VPN is the problem. Some VPN servers block torrent traffic.")
	case route.Proxy && d.TrackersTried == 0:
		parts = append(parts, "This link lists no trackers, and DHT is off while a proxy is used, so there was no way to find peers. Try another release.")
	case d.TrackersTried == 0:
		parts = append(parts, "This link lists no trackers, so peers could only come from DHT. Try another release.")
	}
	return strings.Join(parts, " ")
}
