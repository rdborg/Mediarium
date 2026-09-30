package httpsec

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// homeNetworks are the addresses that count as "the home network" when
// deciding who may create the first administrator without a setup code:
// this machine, the private ranges, link-local addresses, carrier-grade NAT
// space (Tailscale and friends) and IPv6 unique-local addresses. It is the
// same list as the default of TRUSTED_PROXIES.
var homeNetworks = func() []netip.Prefix {
	out := make([]netip.Prefix, 0, len(privateRanges))
	for _, c := range privateRanges {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}()

// IsHomeAddress reports whether ip (as text) is on the home network: loopback,
// private, link-local, carrier-grade NAT or unique-local. Anything that is
// not an address returns false.
func IsHomeAddress(ip string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	a = a.Unmap().WithZone("")
	for _, p := range homeNetworks {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// homeHostSuffixes are the endings of host names that only make sense inside a
// home network (mDNS, router-assigned names, the reserved private names).
var homeHostSuffixes = []string{".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain", ".localhost"}

// IsHomeHost reports whether the host a visitor typed (the Host header, with
// or without a port) is an address or a name that belongs to a home network:
// an IP address, "localhost", a name without a dot (a NAS called "nas") or one
// ending in .local, .lan, .home, .home.arpa or .internal. A public host name
// such as media.example.com is not, even when it points at a private address
// on the home network: a web page on another site can make a visitor's
// browser send requests to a private address under a name of its own
// ("DNS rebinding"), and such a request must not pass for someone at home.
func IsHomeHost(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(strings.TrimSuffix(host, ".")), "[]")
	if host == "" {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	if !strings.Contains(host, ".") {
		return true
	}
	for _, s := range homeHostSuffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}

// forwardingHeaders are the headers a reverse proxy adds to say where a
// request really came from.
var forwardingHeaders = []string{"X-Forwarded-For", "X-Real-IP", "Forwarded", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Ssl"}

// FromHome reports whether r plainly comes from someone on the home network:
// the caller's address (see ClientIP) is a home address and the request was
// made to a home address or name (IsHomeHost).
//
// It is deliberately strict where it cannot tell. A request that a trusted
// proxy passed on without saying who the caller is (forwarding headers, but no
// readable X-Forwarded-For or X-Real-IP) looks like the proxy's own address,
// which is a home address, yet the visitor behind it could be anyone. That is
// not "from home". The first-run setup uses this to decide who needs the setup
// code.
func (p *Proxies) FromHome(r *http.Request) bool {
	if !IsHomeHost(r.Host) {
		return false
	}
	pa, ok := peer(r)
	if !ok {
		return false
	}
	ip := p.ClientIP(r)
	if !IsHomeAddress(ip) {
		return false
	}
	if p.Trusts(pa) {
		// Every address the proxy passed on has to be a home address, not only
		// the one ClientIP settled on: a proxy that sets X-Real-IP but leaves a
		// visitor's own X-Forwarded-For untouched would otherwise let the
		// visitor pick the address that counts.
		for _, a := range forwardedAddresses(r) {
			if !IsHomeAddress(a) {
				return false
			}
		}
		for _, h := range forwardingHeaders {
			if len(r.Header.Values(h)) == 0 {
				continue
			}
			if ip == pa.String() {
				return false // proxied, but the caller's own address was not passed on
			}
			break
		}
	}
	return true
}

// forwardedAddresses lists every address named in X-Forwarded-For and
// X-Real-IP (ports removed).
func forwardedAddresses(r *http.Request) []string {
	var out []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, stripPort(part))
			}
		}
	}
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		out = append(out, stripPort(v))
	}
	return out
}
