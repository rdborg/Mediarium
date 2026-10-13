package netproxy

import (
	"net"
	"net/netip"
	"strings"

	"github.com/rdborg/mediarium/internal/netguard"
)

// localSuffixes are name endings that only exist on a home network.
var localSuffixes = []string{".localhost", ".local", ".lan", ".home.arpa", ".internal"}

// local reports whether addr is on this machine or the home network, which the
// VPN can't reach: a private, loopback or shared-space address, localhost, a
// home-network name, or a name without a dot (a container or a computer on the
// network). Names are judged by how they look, because looking them up here
// would tell the home DNS server which sites are being used.
func local(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		return netguard.NotPublic(ip) || ip.IsLinkLocalUnicast()
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true
	}
	for _, s := range localSuffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}
