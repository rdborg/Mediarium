package api

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/rdborg/mediarium/internal/inputcheck"
)

// More shared checks, in the same style as validate.go: each returns "" when
// the value is fine (a blank value is fine too) or a friendly message that
// goes straight into a 400 response.

// checkHostPort wants an address with its port in one box: vpn.example.com:51820,
// 203.0.113.7:51820 or [2001:db8::1]:51820.
func checkHostPort(v, example string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.Contains(v, "://") || strings.ContainsAny(v, "/\\?#") || strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		return fmt.Sprintf("Enter the address and port only, like %s.", example)
	}
	host, port, err := net.SplitHostPort(v)
	if err != nil {
		return fmt.Sprintf("Add the port after the address, like %s.", example)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "The port must be a number between 1 and 65535."
	}
	if !inputcheck.LooksLikeHost(host) {
		return fmt.Sprintf("That doesn't look like a server name or IP address. It should look like %s.", example)
	}
	return ""
}

var wireguardKeyShape = regexp.MustCompile(`^[A-Za-z0-9+/]{43}=$`)

// checkWireGuardKey wants a WireGuard key: 32 bytes written in base64, which is
// always 44 characters ending in =. label starts the sentence, e.g. "The private key".
func checkWireGuardKey(v, label string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if raw, err := base64.StdEncoding.DecodeString(v); !wireguardKeyShape.MatchString(v) || err != nil || len(raw) != 32 {
		return fmt.Sprintf("%s should be 44 characters ending in =, exactly as it appears in your provider's config. Copy just the key, with nothing around it.", label)
	}
	return ""
}

// checkIPAddress wants an IPv4 or IPv6 address, with an optional /prefix
// length when prefixOK (10.2.0.2/32).
func checkIPAddress(v, example string, prefixOK bool) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	bad := fmt.Sprintf("That doesn't look like an IP address. It should look like %s.", example)
	addr, prefix, hasPrefix := strings.Cut(v, "/")
	ip, err := netip.ParseAddr(addr)
	if err != nil || strings.Contains(addr, "%") {
		return bad
	}
	if !hasPrefix {
		return ""
	}
	if !prefixOK {
		return bad
	}
	max := 32
	if ip.Is6() && !ip.Is4In6() {
		max = 128
	}
	if n, err := strconv.Atoi(prefix); err != nil || n < 0 || n > max || prefix == "" || strings.ContainsAny(prefix, "+-") {
		return fmt.Sprintf("The number after the / should be between 0 and %d, like %s.", max, example)
	}
	return ""
}

// checkCIDR wants a network range with its prefix length, such as 0.0.0.0/0.
func checkCIDR(v, example string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.Contains(v, "/") {
		return fmt.Sprintf("Add the range size after a /, like %s.", example)
	}
	return checkIPAddress(v, example, true)
}

var plainDecimal = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// checkDecimal wants a number written in plain digits (2 or 1.5, no sign or
// exponent) from min to max.
func checkDecimal(v, label string, min, max float64) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	f, err := strconv.ParseFloat(v, 64)
	if !plainDecimal.MatchString(v) || err != nil || f < min || f > max {
		return fmt.Sprintf("%s must be a number between %s and %s.", label, formatNumber(min), formatNumber(max))
	}
	return ""
}

func formatNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// checkChoice complains with message when v is not one of the allowed values.
// A blank value passes, like every other check.
func checkChoice(v, message string, allowed ...string) string {
	if v == "" {
		return ""
	}
	for _, a := range allowed {
		if v == a {
			return ""
		}
	}
	return message
}

// checkList runs check over each entry of a list and stops at the first
// problem. It also keeps the list to max entries.
func checkList(items []string, label string, max int, check func(string) string) string {
	if len(items) > max {
		return fmt.Sprintf("%s can have at most %d entries.", label, max)
	}
	for _, it := range items {
		if strings.Contains(it, ",") {
			return fmt.Sprintf("%s can't have a comma inside an entry. Put each one on its own.", label)
		}
		if m := check(it); m != "" {
			return m
		}
	}
	return ""
}
