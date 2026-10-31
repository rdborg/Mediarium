package torrentclient

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent"
)

// The engine looks a UDP tracker's hostname up with the host's own DNS and
// gives no way to change that. So while discovery goes through the tunnel,
// every UDP tracker address is looked up here, through the VPN's DNS, before
// the torrent is added, and the engine is handed the address itself. A name
// that can't be looked up that way is dropped: it is never tried on the home
// connection.

// resolveTrackers replaces the hostnames of udp:// trackers in spec with the
// addresses the tunnel's DNS gives. It does nothing without a tunnel that is
// carrying UDP.
func (c *Client) resolveTrackers(ctx context.Context, spec *torrent.TorrentSpec) {
	if !c.cfg.DiscoverThroughTunnel || c.cfg.Tunnel == nil {
		return
	}
	names, ok := c.cfg.Tunnel.(TunnelNames)
	if !ok {
		return
	}
	spec.Trackers = tunnelTrackers(ctx, names, spec.Trackers)
}

func tunnelTrackers(ctx context.Context, names TunnelNames, tiers [][]string) [][]string {
	out := make([][]string, 0, len(tiers))
	for _, tier := range tiers {
		kept := make([]string, 0, len(tier))
		for _, raw := range tier {
			if u, err := url.Parse(raw); err == nil && (u.Scheme == "udp" || u.Scheme == "udp4" || u.Scheme == "udp6") {
				resolved, ok := udpTrackerByAddress(ctx, names, u)
				if !ok {
					continue
				}
				raw = resolved
			}
			kept = append(kept, raw)
		}
		if len(kept) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

// udpTrackerByAddress returns u with its host replaced by an address from the
// tunnel's DNS.
func udpTrackerByAddress(ctx context.Context, names TunnelNames, u *url.URL) (string, bool) {
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		return u.String(), true
	}
	lctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ips, err := names.LookupHost(lctx, host)
	if err != nil {
		return "", false
	}
	for _, ip := range ips {
		if ip.To4() == nil && u.Scheme != "udp6" && u.Scheme != "udp" {
			continue
		}
		if v4 := ip.To4(); v4 != nil && u.Scheme == "udp6" {
			continue
		}
		cp := *u
		h := ip.String()
		if ip.To4() == nil {
			h = "[" + h + "]"
		}
		if port := u.Port(); port != "" {
			cp.Host = h + ":" + port
		} else {
			cp.Host = h
		}
		return cp.String(), true
	}
	return "", false
}

// tunnelBootstrap resolves the public DHT routers through the tunnel. It
// never calls the host resolver. If none of the names resolve, the error
// stops bootstrap instead of trying the home connection.
func tunnelBootstrap(names TunnelNames) func(string) dht.StartingNodesGetter {
	return func(network string) dht.StartingNodesGetter {
		return func() ([]dht.Addr, error) {
			return bootstrapThroughTunnel(network, names)
		}
	}
}

func bootstrapThroughTunnel(network string, names TunnelNames) ([]dht.Addr, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var addrs []dht.Addr
	for _, hp := range dht.DefaultGlobalBootstrapHostPorts {
		host, portStr, err := net.SplitHostPort(hp)
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			continue
		}
		ips, err := names.LookupHost(ctx, host)
		if err != nil {
			continue
		}
		for _, ip := range ips {
			v4 := ip.To4() != nil
			if network == "udp4" && !v4 {
				continue
			}
			if network == "udp6" && v4 {
				continue
			}
			addrs = append(addrs, dht.NewAddr(&net.UDPAddr{IP: ip, Port: port}))
		}
	}
	if len(addrs) == 0 {
		return nil, errors.New("DHT bootstrap names could not be looked up through the VPN")
	}
	return addrs, nil
}
