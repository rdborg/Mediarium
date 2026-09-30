// Package inputcheck holds the format checks for values people type into
// forms: email addresses, host names, web addresses, folder paths and keys.
//
// Each check returns "" when the value is fine (a blank value is fine too, so
// optional fields can use the same call) or a short, friendly sentence that
// can be shown to the person as it is. The same rules are written out in
// web/src/validate.ts so the page and the server agree.
//
// The package depends on nothing else in the repo, so any package can use it.
package inputcheck

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ValidEmail is deliberately loose (one @, something on both sides, a dot in
// the domain, no spaces): it catches typos, not every RFC quirk.
func ValidEmail(email string) bool {
	if email == "" || strings.ContainsAny(email, " ,;<>") || strings.IndexFunc(email, unicode.IsSpace) >= 0 {
		return false
	}
	at := strings.IndexByte(email, '@')
	if at < 1 || at != strings.LastIndexByte(email, '@') {
		return false
	}
	domain := email[at+1:]
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

// Email checks one address.
func Email(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !ValidEmail(v) || len(v) > 254 {
		return "That email address doesn't look right. It should look like name@example.com."
	}
	return ""
}

var hostLabel = regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?$`)

// LooksLikeHost reports whether h is a host name, an IPv4 address or an IPv6
// address, with no scheme, port or path (IPv6 without brackets).
func LooksLikeHost(h string) bool {
	if h == "" {
		return false
	}
	if strings.Contains(h, ":") {
		return net.ParseIP(h) != nil
	}
	allDigits := true
	for _, r := range h {
		if r != '.' && (r < '0' || r > '9') {
			allDigits = false
			break
		}
	}
	if allDigits && strings.Contains(h, ".") {
		ip := net.ParseIP(h)
		return ip != nil && ip.To4() != nil
	}
	h = strings.TrimSuffix(h, ".")
	if h == "" || len(h) > 253 {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if !hostLabel.MatchString(l) {
			return false
		}
	}
	return true
}

// Host wants a bare host name or IP address: no scheme, path or port. example
// is shown in the message, for instance "news.example.com".
func Host(v, example string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.Contains(v, "://") || strings.ContainsAny(v, "/\\?#") {
		return fmt.Sprintf("Enter just the server name, without http:// or a path. For example %s.", example)
	}
	if strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		return "The server name can't contain spaces."
	}
	if strings.Count(v, ":") == 1 && !strings.HasPrefix(v, "[") {
		return fmt.Sprintf("Put the port in the Port box, not in the address. The address should look like %s.", example)
	}
	if !LooksLikeHost(strings.Trim(v, "[]")) {
		return fmt.Sprintf("That doesn't look like a server name or IP address. It should look like %s or 192.168.1.10.", example)
	}
	return ""
}

// HTTPURL wants an http or https web address with a server name in it. With
// requireScheme false, an address typed without http:// is accepted, for the
// forms that add the scheme themselves.
func HTTPURL(v, example string, requireScheme bool) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		return "The address can't contain spaces."
	}
	bad := fmt.Sprintf("That doesn't look like a web address. It should look like %s.", example)
	if strings.HasPrefix(v, "/") {
		return bad
	}
	hasScheme := strings.Contains(v, "://")
	if !hasScheme && requireScheme {
		return fmt.Sprintf("Start the address with http:// or https://, for example %s.", example)
	}
	if hasScheme {
		low := strings.ToLower(v)
		if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
			return fmt.Sprintf("The address has to start with http:// or https://, for example %s.", example)
		}
	} else {
		v = "http://" + v
	}
	u, err := url.Parse(v)
	if err != nil {
		return bad
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Sprintf("That address is missing a server name. It should look like %s.", example)
	}
	if !LooksLikeHost(host) {
		return bad
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "The port in that address must be a number between 1 and 65535."
		}
	}
	return ""
}

var winDrive = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// AbsPath wants an absolute folder path: /..., D:\..., or \\server\share.
func AbsPath(v, example string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "The folder path can't contain hidden or control characters."
	}
	if !strings.HasPrefix(v, "/") && !winDrive.MatchString(v) && !strings.HasPrefix(v, `\\`) {
		return fmt.Sprintf("That folder path isn't complete. It should start with / or a drive letter, for example %s.", example)
	}
	return ""
}

// APIKey wants one unbroken string, the way keys and tokens are.
func APIKey(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		return "That key has a space or line break in it. Copy just the key itself, with nothing around it."
	}
	if len(v) > 512 {
		return "That key is much longer than a real one. Check that you copied only the key."
	}
	return ""
}
