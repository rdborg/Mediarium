package mediaservers

import "testing"

// An address pasted from the browser's address bar often ends in the path of
// the server's web app. That path is not part of the server's address.
func TestNormalizeURLDropsTheWebClientPath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://host/web/#/home", "https://host"},
		{"https://host/web/index.html#!/home", "https://host"},
		{"http://192.168.1.10:8096/web/index.html", "http://192.168.1.10:8096"},
		{"https://host/web/", "https://host"},
		{"https://host/web", "https://host"},
		{"https://host/WEB/#/home", "https://host"},
		{"https://host/jellyfin/web/#/home", "https://host/jellyfin"},
		{"host:8096/web/#/home", "http://host:8096"},
		// left alone
		{"https://host", "https://host"},
		{"https://host/", "https://host"},
		{"https://host/jellyfin", "https://host/jellyfin"},
		{"https://host/websites", "https://host/websites"},
		{"https://host/web/api", "https://host/web/api"},
		{"https://host/myweb", "https://host/myweb"},
		{"http://192.168.1.10:32400", "http://192.168.1.10:32400"},
	} {
		got, err := NormalizeURL(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
