package indexers

import "testing"

// TestNewznabBase: a feed address works with or without "/api" on the end,
// as Prowlarr and Jackett show it.
func TestNewznabBase(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://api.example.com", "https://api.example.com"},
		{"https://api.example.com/", "https://api.example.com"},
		{"https://api.example.com/api", "https://api.example.com"},
		{"http://10.29.4.4:9696/1/api", "http://10.29.4.4:9696/1"},
		{"http://10.29.4.4:9696/1/api/", "http://10.29.4.4:9696/1"},
		{"http://prowlarr:9696/1/API?t=caps&apikey=x", "http://prowlarr:9696/1"},
		{"http://jackett:9117/api/v2.0/indexers/all/results/torznab/", "http://jackett:9117/api/v2.0/indexers/all/results/torznab"},
		{"http://jackett:9117/api/v2.0/indexers/all/results/torznab/api", "http://jackett:9117/api/v2.0/indexers/all/results/torznab"},
		{"  https://nzb.example.com/api  ", "https://nzb.example.com"},
	} {
		if got := newznabBase(tc.in); got != tc.want {
			t.Errorf("newznabBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
