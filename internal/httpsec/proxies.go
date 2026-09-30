// Package httpsec holds the HTTP-level security pieces that depend on how
// Mediarium is reached: which reverse proxies are trusted to say who the
// caller is (client IP, HTTPS or not, public host name), the cross-site
// request check, and the security response headers.
//
// The rule behind all of it: a header such as X-Forwarded-For is only
// believed when the TCP connection it arrived on comes from a proxy the
// owner trusts (TRUSTED_PROXIES). From anyone else it is ignored, so a
// direct visitor cannot pretend to be another address, or to be on HTTPS.
package httpsec

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// privateRanges are what "private" (the default) trusts: the loopback
// interface, the private IPv4 ranges (RFC 1918), carrier-grade NAT space
// (Tailscale and friends), link-local addresses and IPv6 unique-local
// addresses. That covers a proxy on the same machine, in the same Docker
// network or on the same LAN, and never a visitor coming straight from the
// internet.
var privateRanges = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"100.64.0.0/10",
	"169.254.0.0/16", "fe80::/10",
	"fc00::/7",
}

// Proxies is the set of reverse proxies whose forwarding headers are
// believed. The zero value trusts nobody.
type Proxies struct {
	prefixes []netip.Prefix
}

// ParseProxies reads a TRUSTED_PROXIES value: a comma-separated list of
// addresses, CIDR ranges and the keywords "private" (loopback and private
// ranges, the default) and "none" (trust no proxy: forwarding headers are
// ignored). An empty value means "private".
func ParseProxies(spec string) (*Proxies, error) {
	p := &Proxies{}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = "private"
	}
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		switch strings.ToLower(item) {
		case "":
			continue
		case "none":
			continue
		case "private":
			for _, c := range privateRanges {
				p.prefixes = append(p.prefixes, netip.MustParsePrefix(c))
			}
			continue
		}
		if strings.Contains(item, "/") {
			pre, err := netip.ParsePrefix(item)
			if err != nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not a CIDR range: %w", item, err)
			}
			p.prefixes = append(p.prefixes, pre.Masked())
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not an IP address or CIDR range", item)
		}
		addr = addr.Unmap()
		p.prefixes = append(p.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return p, nil
}

// Trusts reports whether addr is one of the trusted proxies.
func (p *Proxies) Trusts(addr netip.Addr) bool {
	if p == nil {
		return false
	}
	addr = addr.Unmap().WithZone("")
	for _, pre := range p.prefixes {
		if pre.Contains(addr) {
			return true
		}
	}
	return false
}

// peer is the address of the TCP peer of r (the proxy, if there is one).
func peer(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap().WithZone(""), true
}

// FromTrustedProxy reports whether the connection r arrived on comes from a
// trusted proxy, i.e. whether its X-Forwarded-* headers may be believed.
func (p *Proxies) FromTrustedProxy(r *http.Request) bool {
	a, ok := peer(r)
	return ok && p.Trusts(a)
}

// ClientIP is the address of the real caller. Without a trusted proxy in
// front it is the TCP peer, whatever headers say. Behind one it is taken
// from X-Forwarded-For by walking the list from the right (the end each
// proxy appends to) and stopping at the first address that is not itself a
// trusted proxy: entries further left were supplied by the caller and are
// never believed. X-Real-IP is used only when there is no
// X-Forwarded-For. The result is "" only if the peer address is unreadable.
func (p *Proxies) ClientIP(r *http.Request) string {
	pa, ok := peer(r)
	if !ok {
		return r.RemoteAddr
	}
	if !p.Trusts(pa) {
		return pa.String()
	}
	var chain []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				chain = append(chain, part)
			}
		}
	}
	if len(chain) == 0 {
		if a, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
			return a.Unmap().WithZone("").String()
		}
		return pa.String()
	}
	for i := len(chain) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(stripPort(chain[i]))
		if err != nil {
			return pa.String() // a proxy passed on something that is not an address: do not guess
		}
		a = a.Unmap().WithZone("")
		if !p.Trusts(a) || i == 0 {
			return a.String()
		}
	}
	return pa.String()
}

// stripPort removes a :port (and the brackets of an IPv6 literal) that some
// proxies add to the address.
func stripPort(s string) string {
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return strings.Trim(s, "[]")
}

// LimitKey is what rate limits are counted against for ip: the address
// itself for IPv4, the whole /64 for IPv6 (one visitor owns a whole /64, so
// counting single addresses would let it rotate through billions).
func LimitKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	a = a.Unmap()
	if a.Is6() {
		if pre, err := a.Prefix(64); err == nil {
			return pre.String()
		}
	}
	return a.String()
}

// IsHTTPS reports whether the caller reached Mediarium over HTTPS: this
// connection is TLS, or a trusted proxy says the caller's was
// (X-Forwarded-Proto, or the proto of the Forwarded header).
func (p *Proxies) IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !p.FromTrustedProxy(r) {
		return false
	}
	if vals := r.Header.Values("X-Forwarded-Proto"); len(vals) > 0 {
		parts := strings.Split(vals[len(vals)-1], ",")
		return strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "https")
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Ssl")), "on") {
		return true
	}
	for _, v := range r.Header.Values("Forwarded") {
		for _, elem := range strings.Split(v, ",") {
			for _, kv := range strings.Split(elem, ";") {
				k, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
				if ok && strings.EqualFold(k, "proto") && strings.EqualFold(strings.Trim(val, `"`), "https") {
					return true
				}
			}
		}
	}
	return false
}

// PublicHosts lists the host names (with port when not a default one) the
// caller may have used to reach Mediarium: the Host header of the request,
// and the X-Forwarded-Host a trusted proxy adds. Lower case.
func (p *Proxies) PublicHosts(r *http.Request) []string {
	hosts := []string{strings.ToLower(r.Host)}
	if p.FromTrustedProxy(r) {
		for _, v := range r.Header.Values("X-Forwarded-Host") {
			for _, part := range strings.Split(v, ",") {
				if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
					hosts = append(hosts, part)
				}
			}
		}
	}
	return hosts
}
