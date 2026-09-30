package api

import (
	"strings"
	"testing"
)

func TestCheckHostPort(t *testing.T) {
	runChecks(t, "checkHostPort", func(s string) string { return checkHostPort(s, "vpn.example.com:51820") }, []row{
		{"", true}, {"vpn.example.com:51820", true}, {"203.0.113.7:51820", true}, {"[2001:db8::1]:51820", true},
		{"localhost:1", true}, {"vpn.example.com", false}, {"vpn.example.com:0", false}, {"vpn.example.com:65536", false},
		{"vpn.example.com:abc", false}, {"udp://vpn.example.com:51820", false}, {"vpn.example.com:51820/x", false},
		{"vpn example.com:51820", false}, {"2001:db8::1:51820", false}, {":51820", false}, {"999.1.1.1:51820", false},
		{"bad_host!:51820", false},
	})
}

func TestCheckWireGuardKey(t *testing.T) {
	good := strings.Repeat("A", 43) + "="
	runChecks(t, "checkWireGuardKey", func(s string) string { return checkWireGuardKey(s, "The key") }, []row{
		{"", true}, {good, true}, {"  " + good + "  ", true}, {"YWJj", false}, {good[:43], false},
		{strings.Repeat("A", 44), false}, {strings.Repeat("A", 42) + "!=", false}, {good + good, false},
		{"PrivateKey = " + good, false},
	})
}

func TestCheckIPAddress(t *testing.T) {
	runChecks(t, "checkIPAddress(prefix ok)", func(s string) string { return checkIPAddress(s, "10.2.0.2/32", true) }, []row{
		{"", true}, {"10.2.0.2", true}, {"10.2.0.2/32", true}, {"10.2.0.2/0", true}, {"fd00::2/128", true}, {"::1", true},
		{"10.2.0.2/33", false}, {"fd00::2/129", false}, {"10.2.0.2/", false}, {"10.2.0.2/-1", false}, {"10.2.0.2/+8", false},
		{"10.2.0.256", false}, {"vpn.example.com", false}, {"10.2.0.2/8/8", false}, {"fe80::1%eth0", false},
	})
	runChecks(t, "checkIPAddress(no prefix)", func(s string) string { return checkIPAddress(s, "10.64.0.1", false) }, []row{
		{"10.64.0.1", true}, {"2606:4700::1111", true}, {"10.64.0.1/32", false}, {"one.one.one.one", false},
	})
}

func TestCheckCIDR(t *testing.T) {
	runChecks(t, "checkCIDR", func(s string) string { return checkCIDR(s, "0.0.0.0/0") }, []row{
		{"", true}, {"0.0.0.0/0", true}, {"::/0", true}, {"192.168.1.0/24", true},
		{"192.168.1.0", false}, {"192.168.1.0/40", false}, {"nope/24", false},
	})
}

func TestCheckDecimal(t *testing.T) {
	runChecks(t, "checkDecimal", func(s string) string { return checkDecimal(s, "The ratio", 0, 1000) }, []row{
		{"", true}, {"0", true}, {"2", true}, {"1.5", true}, {" 3 ", true}, {"1000", true}, {"1000.5", false},
		{"-1", false}, {"+1", false}, {"1e3", false}, {"NaN", false}, {"Inf", false}, {"abc", false}, {".5", false}, {"1,5", false},
	})
	if got := checkDecimal("-1", "The ratio", 0, 1000); got != "The ratio must be a number between 0 and 1000." {
		t.Errorf("message = %q", got)
	}
}

func TestCheckChoiceAndList(t *testing.T) {
	runChecks(t, "checkChoice", func(s string) string { return checkChoice(s, "Pick one.", "a", "b") }, []row{
		{"", true}, {"a", true}, {"b", true}, {"c", false}, {"A", false},
	})
	ok := func(s string) string { return checkCIDR(s, "0.0.0.0/0") }
	if checkList([]string{"0.0.0.0/0", "::/0"}, "Allowed IPs", 3, ok) != "" {
		t.Error("a good list should pass")
	}
	if checkList([]string{"0.0.0.0/0", "nope"}, "Allowed IPs", 3, ok) == "" {
		t.Error("a bad entry should fail")
	}
	if checkList([]string{"0.0.0.0/0,::/0"}, "Allowed IPs", 3, ok) == "" {
		t.Error("an entry with a comma should fail (lists are stored comma separated)")
	}
	if checkList([]string{"::/0", "::/0", "::/0", "::/0"}, "Allowed IPs", 3, ok) == "" {
		t.Error("too many entries should fail")
	}
}
