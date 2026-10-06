package notify

import "testing"

func TestFromHeader(t *testing.T) {
	cases := []struct{ from, name, want string }{
		{"nas@alphatech.ws", "", `"Mediarium" <nas@alphatech.ws>`},
		{"<nas@alphatech.ws>", "", `"Mediarium" <nas@alphatech.ws>`},
		{"nas@alphatech.ws", "Mediarium App", `"Mediarium App" <nas@alphatech.ws>`},
		{"Home Server <nas@example.com>", "", `"Home Server" <nas@example.com>`},
		{"Home Server <nas@example.com>", "Family Movies", `"Family Movies" <nas@example.com>`},
		{"you@example.com", "  ", `"Mediarium" <you@example.com>`},
		{"you@example.com", "Café Média", "=?utf-8?q?Caf=C3=A9_M=C3=A9dia?= <you@example.com>"},
		{"you@example.com", "Evil\r\nBcc: x@example.com", `"Evil Bcc: x@example.com" <you@example.com>`},
		{"not an address", "Mediarium", "not an address"},
		{"x@example.com\r\nBcc: evil@example.com", "", "x@example.com Bcc: evil@example.com"},
	}
	for _, tc := range cases {
		if got := fromHeader(tc.from, tc.name); got != tc.want {
			t.Errorf("fromHeader(%q, %q) = %q, want %q", tc.from, tc.name, got, tc.want)
		}
	}
}
