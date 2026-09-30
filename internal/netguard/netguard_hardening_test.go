package netguard

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestBlockedMoreWaysToWriteTheSameAddress(t *testing.T) {
	tests := map[string]bool{
		"fe80::1%eth0":                       true, // link-local with a zone
		"::ffff:0:a9fe:a9fe":                 true, // IPv4-translated (SIIT) form of the metadata address
		"::ffff:0:808:808":                   false,
		"::ffff:169.254.169.254":             true,
		"::ffff:a9fe:a9fe":                   true,
		"::ffff:0.0.0.0":                     true,
		"::ffff:127.0.0.1":                   false,
		"::ffff:100.100.100.200":             true,
		"::ffff:192.0.0.192":                 true,
		"::ffff:255.255.255.255":             true,
		"fd00:ec2::254":                      true,
		"fd00:ec2::23":                       false, // other addresses in that network are ordinary
		"fc00::1":                            false, // unique local addresses are LAN addresses
		"fe80::":                             true,
		"febf::1":                            true, // last of fe80::/10
		"fec0::1":                            false,
		"ff02::1":                            true,
		"ff0e::1":                            true,
		"169.254.255.255":                    true,
		"169.253.255.255":                    false,
		"100.64.0.1":                         false,
		"100.127.255.254":                    false,
		"0.255.255.255":                      true,
		"1.0.0.0":                            false,
		"239.255.255.255":                    true,
		"64:ff9b::7f00:1":                    false, // NAT64 form of loopback stays reachable like loopback
		"64:ff9b::a00:1":                     false,
		"2002:a9fe:a9fe:1:2:3:4:5":           true,
		"::7f00:1":                           false,
		"::1":                                false,
		"2001:db8::1":                        false,
		"2002:0:0::1":                        true, // 6to4 form of 0.0.0.0
		"64:ff9b::":                          true, // NAT64 form of 0.0.0.0
		"::ffff:0:0:0":                       true,
		"::ffff:0:7f00:1":                    false,
		"64:ff9b::a9fe:a9fe%zone":            true,
		"2002:a9fe:a9fe::%eth0":              true,
		"fd00:ec2:0:0:0:0:0:254":             true,
		"0:0:0:0:0:ffff:a9fe:a9fe":           true,
		"0000:0000:0000:0000:0000:0000:0:0":  true,
		"::a9fe:a9ff":                        true,
		"::ffff:ffff:ffff":                   true,
		"::ffff:1.2.3.4":                     false,
		"255.255.255.254":                    false,
		"192.0.0.191":                        false,
		"192.0.0.193":                        false,
		"168.63.129.15":                      false,
		"168.63.129.17":                      false,
		"100.100.100.199":                    false,
		"100.100.100.201":                    false,
		"::ffff:100.100.100.201":             false,
		"64:ff9b::6464:64c8":                 true, // NAT64 form of 100.100.100.200
		"2002:6464:64c8::":                   true,
		"::6464:64c8":                        true,
		"::ffff:0:6464:64c8":                 true,
		"2001:4860:4860::8888":               false,
		"2a00:1450:4001:81b::200e":           false,
		"::ffff:c000:c0":                     true, // 192.0.0.192
		"::ffff:a83f:8110":                   true, // 168.63.129.16
		"::ffff:8.8.8.8":                     false,
		"fe80::a9fe:a9fe":                    true,
		"fd00:ec2::254%eth0":                 true,
		"[::1]":                              false, // (never produced: brackets are not part of an address)
		"::ffff:169.254.169.254%zone":        true,
		"1::":                                false,
		"3fff::":                             false,
		"ff00::":                             true,
		"fe7f::":                             false,
		"fe80::1%25":                         true,
		"::ffff:1:1":                         true, // maps to 0.1.0.1
		"::ffff:0:1:1":                       true, // translates to 0.1.0.1
		"::ffff:169.254.0.0":                 true,
		"::ffff:169.255.0.0":                 false,
		"2002:a9fe::":                        true,
		"2002:a9ff::":                        false,
		"2001:0:a9fe:a9fe::":                 false, // Teredo: the client address is obfuscated, not judged
		"64:ff9b:1::a9fe:a9fe":               false, // local-use NAT64 prefix is not translated by default
		"::ffff:0:0":                         true,  // maps to 0.0.0.0
		"::ffff:0.0.0.1":                     true,
		"::0.0.0.1":                          false, // that is ::1, loopback
		"::0.1.0.0":                          true,
		"::1:0:0:0":                          false,
		"::10.0.0.1":                         false,
		"::169.254.1.1":                      true,
		"::192.0.0.192":                      true,
		"::168.63.129.16":                    true,
		"::100.100.100.200":                  true,
		"::8.8.8.8":                          false,
		"::255.255.255.255":                  true,
		"::224.0.0.1":                        true,
		"::127.0.0.1":                        false,
		"::0.0.0.0":                          true,
		"::0.0.0.0%zone":                     true,
		"::1%zone":                           false,
		"127.0.0.1":                          false,
		"::ffff:127.0.0.1%zone":              false,
		"::ffff:10.1.2.3":                    false,
		"::ffff:192.168.0.1":                 false,
		"172.16.0.1":                         false,
		"192.0.2.1":                          false,
		"198.51.100.1":                       false,
		"203.0.113.1":                        false,
		"240.0.0.1":                          false,
		"::ffff:240.0.0.1":                   false,
		"fc00::":                             false,
		"fdff:ffff:ffff:ffff:ffff:ffff:ffff": false,
	}
	for ip, want := range tests {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			// a few keys are deliberately not addresses (see the comments)
			if want {
				t.Errorf("%q does not parse but is expected to be blocked", ip)
			}
			continue
		}
		if got := Blocked(addr); got != want {
			t.Errorf("Blocked(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestControlJudgesTheResolvedAddress(t *testing.T) {
	blocked := []string{
		"169.254.169.254:80", "[fe80::1%eth0]:443", "[fe80::1]:1", "[::ffff:169.254.169.254]:80", "[::ffff:a9fe:a9fe]:80",
		"[fd00:ec2::254]:80", "0.0.0.0:1", "[::]:1", "224.0.0.251:5353", "169.254.169.254", "[64:ff9b::a9fe:a9fe]:80",
		"[2002:a9fe:a9fe::]:80", "[::a9fe:a9fe]:80", "100.100.100.200:80", "192.0.0.192:80", "168.63.129.16:80",
	}
	for _, addr := range blocked {
		if err := Control("tcp", addr, nil); !errors.Is(err, ErrBlocked) {
			t.Errorf("Control(%q) = %v, want ErrBlocked", addr, err)
		}
	}
	allowed := []string{"127.0.0.1:80", "[::1]:80", "10.0.0.5:8989", "192.168.1.2:32400", "[fd12:3456::1]:80", "100.64.0.9:80", "93.184.216.34:443", "[2606:4700:4700::1111]:443"}
	for _, addr := range allowed {
		if err := Control("tcp", addr, nil); err != nil {
			t.Errorf("Control(%q) = %v, want no error", addr, err)
		}
	}
	// Control only ever sees resolved addresses: anything else is refused for
	// internet connections and left alone for the rest (unix sockets)
	for _, addr := range []string{"example.com:80", "", ":", "[", "localhost:1", "[fe80::1%[]:80"} {
		if err := Control("tcp", addr, nil); !errors.Is(err, ErrBlocked) {
			t.Errorf("Control(tcp, %q) = %v, want ErrBlocked", addr, err)
		}
	}
	if err := Control("unix", "/run/x.sock", nil); err != nil {
		t.Errorf("Control(unix) = %v", err)
	}
}

// Every way of writing the metadata address in a web address ends at a refused
// connection, including a user name that looks like a host.
func TestClientRefusesEveryFormOfTheMetadataAddress(t *testing.T) {
	c := Client(3 * time.Second)
	for _, target := range []string{
		"http://169.254.169.254/",
		"http://127.0.0.1@169.254.169.254/",
		"http://user:pass@169.254.169.254:80/latest",
		"http://[::ffff:169.254.169.254]/",
		"http://[::ffff:a9fe:a9fe]/",
		"http://[64:ff9b::a9fe:a9fe]/",
		"http://[2002:a9fe:a9fe::]/",
		"http://[::a9fe:a9fe]/",
		"http://[::ffff:0:a9fe:a9fe]/",
		"http://[fe80::1]/",
		"http://[::]/",
		"http://0.0.0.0/",
		"http://100.100.100.200/",
		"http://192.0.0.192/",
		"http://168.63.129.16/",
		"https://169.254.169.254/",
		"http://169.254.169.254:8080/?apikey=SECRET",
	} {
		_, err := c.Get(target)
		if !errors.Is(err, ErrBlocked) {
			t.Errorf("GET %s: err = %v, want ErrBlocked", target, err)
		}
		if err != nil && strings.Contains(CleanError(err).Error(), "SECRET") {
			t.Errorf("GET %s: the key is in %v", target, CleanError(err))
		}
	}
}

func TestRedactURLWhatGoCannotParse(t *testing.T) {
	const secret = "S3CR3T"
	for _, raw := range []string{
		"http://idx.example/%zz?apikey=" + secret,
		"http://user:" + secret + "@idx.example/%zz",
		"http://idx.example/a\x7fb?token=" + secret + "&x=1",
		"/api?apikey=" + secret,
		"api?apikey=" + secret,
		"idx.example:80/api?apikey=" + secret,
		"http://idx.example/api#access_token=" + secret,
		"http://idx.example/api?a=1#" + secret,
		"http://idx.example/api?a=1;apikey=" + secret,
		"http://[::1/api?apikey=" + secret,
		"http://idx.example:99999999999/api?apikey=" + secret,
		"HTTP://IDX.example/api?APIKEY=" + secret,
		"http://user@idx.example/api/" + strings.Repeat("a1", 15) + "?k=v",
		"http://idx.example/api?k=v&" + "%zz=" + secret,
	} {
		if got := RedactURL(raw); strings.Contains(got, secret) || strings.Contains(got, strings.Repeat("a1", 15)) {
			t.Errorf("RedactURL(%q) = %q, which still shows the secret", raw, got)
		}
	}
	// a plain word or a path is left alone
	for _, raw := range []string{"not a url", "", "/plain/path", "idx.example"} {
		if got := RedactURL(raw); got != raw {
			t.Errorf("RedactURL(%q) = %q", raw, got)
		}
	}
}

func TestCleanErrorHidesTheKeyOfABadAddress(t *testing.T) {
	_, err := Client(time.Second).Get("http://idx.example/%zz?apikey=SECRET123")
	if err == nil {
		t.Fatal("expected an error")
	}
	if msg := CleanError(err).Error(); strings.Contains(msg, "SECRET123") {
		t.Fatalf("key still in error: %s", msg)
	}
}

func FuzzBlocked(f *testing.F) {
	f.Add([]byte{169, 254, 169, 254}, "")
	f.Add([]byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, "eth0")
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 169, 254, 169, 254}, "")
	f.Add([]byte{0x20, 0x02, 0xa9, 0xfe, 0xa9, 0xfe, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, "z")
	f.Add([]byte{127, 0, 0, 1}, "")
	f.Fuzz(func(t *testing.T, raw []byte, zone string) {
		var addr netip.Addr
		switch {
		case len(raw) == 4:
			addr = netip.AddrFrom4([4]byte(raw))
		case len(raw) == 16:
			if strings.ContainsAny(zone, "[]%:/ \x00") {
				zone = "" // not a name an interface can have in an address
			}
			addr = netip.AddrFrom16([16]byte(raw)).WithZone(zone)
		default:
			return
		}
		got := Blocked(addr)
		// the zone and the IPv4-in-IPv6 wrapping never change the answer
		if Blocked(addr.WithZone("")) != got || Blocked(addr.Unmap()) != got {
			t.Fatalf("Blocked(%v) depends on the zone or the wrapping", addr)
		}
		// the dialer check agrees, however the address is written
		if err := Control("tcp", net.JoinHostPort(addr.String(), "80"), nil); errors.Is(err, ErrBlocked) != got {
			t.Fatalf("Control and Blocked disagree for %v: %v vs %v", addr, err, got)
		}
		// whatever the bytes are, these must be refused
		u := addr.Unmap().WithZone("")
		if u.Is4() {
			switch {
			case u.IsLinkLocalUnicast(), u.IsMulticast(), u.IsUnspecified(), u.As4()[0] == 0, u == netip.AddrFrom4([4]byte{255, 255, 255, 255}):
				if !got {
					t.Fatalf("%v should be blocked", addr)
				}
			}
		}
		if u.Is6() && (u.IsLinkLocalUnicast() || u.IsLinkLocalMulticast() || u.IsMulticast() || u.IsUnspecified()) && !got {
			t.Fatalf("%v should be blocked", addr)
		}
	})
}

// FuzzControl: a dialer address in any shape gets an answer, never a panic.
func FuzzControl(f *testing.F) {
	for _, s := range []string{"169.254.169.254:80", "[::1]:80", "", ":", "[", "]", "[fe80::1%eth0]:1", "a:b:c", "1.2.3.4:", "0x7f.1:80", "2130706433:80", "[::ffff:1.2.3.4]:80", strings.Repeat("1", 500)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, address string) {
		err := Control("tcp", address, nil)
		if err != nil && !errors.Is(err, ErrBlocked) {
			t.Fatalf("Control(%q) = %v", address, err)
		}
	})
}

// FuzzRedactURL: a key placed anywhere in a web address (query, user name,
// fragment, path) is never in the redacted text, and redacting twice changes
// nothing more.
func FuzzRedactURL(f *testing.F) {
	for _, s := range []string{
		"http://idx.example/api", "http://idx.example/%zz", "//h", "h", "", "http://[::1]/", "http://h:99999/", "http://h/a b", "ftp://h/x", "http://h/\x00",
		"http://h/?", "http://h/?&&&", "http://h/#", "http://a@b@c/", "http://h/;x=1", "http://h/?a=%zz",
	} {
		f.Add(s)
	}
	const secret = "S3CR3T-VALUE-9Q"
	f.Fuzz(func(t *testing.T, base string) {
		if strings.Contains(base, secret) || strings.Contains(base, "S3CR3T") {
			return
		}
		for _, raw := range []string{
			base + "?apikey=" + secret,
			base + "?x=1&token=" + secret,
			base + "#access_token=" + secret,
			"http://user:" + secret + "@" + base,
			"http://" + base + "?apikey=" + secret + "&x=1",
			"http://h.example/?a=b&password=" + secret + "#" + base,
		} {
			got := RedactURL(raw)
			if strings.Contains(got, secret) {
				t.Fatalf("RedactURL(%q) = %q, which still shows the secret", raw, got)
			}
			if again := RedactURL(got); strings.Contains(again, secret) {
				t.Fatalf("redacting twice shows it: %q", again)
			}
		}
	})
}
