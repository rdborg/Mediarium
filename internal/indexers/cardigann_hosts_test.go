package indexers

import (
	"net/url"
	"strings"
	"testing"
)

// The definitions come from a public repository. A path in one that is a full
// address must lead to the site, or the login, cookie or passkey would be sent
// to whoever the address names.
func TestResolvePathOnlyLeadsToTheSite(t *testing.T) {
	mk := func(base string, links ...string) *cgSession {
		b, _ := url.Parse(base)
		return &cgSession{name: "Fixture", base: b, def: &Definition{Links: links}}
	}
	tests := []struct {
		name string
		s    *cgSession
		path string
		want string // "" = allowed; otherwise a part of the refusal
	}{
		{"relative path", mk("https://www.private.example/"), "login.php", ""},
		{"absolute path on the same host", mk("https://www.private.example/"), "https://www.private.example/login", ""},
		{"an api host of the same site", mk("https://www.private.example/"), "https://api.private.example/v1/login", ""},
		{"the bare domain of the site", mk("https://www.private.example/"), "https://private.example/login", ""},
		{"a mirror the definition lists", mk("https://private.example/", "https://private.example/", "https://mirror.example.net/"), "https://mirror.example.net/login", ""},
		{"a host on another domain", mk("https://www.private.example/"), "https://evil.example.org/collect", "not one of the site's own addresses"},
		{"a look-alike domain", mk("https://www.private.example/"), "https://private.example.evil.org/login", "not one of the site's own addresses"},
		{"the same second-level name under a shared suffix", mk("https://www.private.co.uk/"), "https://evil.co.uk/login", "not one of the site's own addresses"},
		{"another user of a shared hosting suffix", mk("https://victim.github.io/"), "https://attacker.github.io/login", "not one of the site's own addresses"},
		{"a different IP address", mk("http://192.168.1.10:9117/"), "http://192.168.1.11:9117/login", "not one of the site's own addresses"},
		{"the same IP address", mk("http://192.168.1.10:9117/"), "http://192.168.1.10:9117/login", ""},
		{"localhost only matches itself", mk("http://localhost:9117/"), "http://evil.localhost:9117/login", "not one of the site's own addresses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.s.resolvePath(tt.path)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("resolvePath(%q): %v", tt.path, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("resolvePath(%q) = %v, want an error containing %q", tt.path, err, tt.want)
			}
		})
	}
}

// A download of definitions cannot fill memory with matching files.
func TestExtractDefinitionsStopsAtATotalSize(t *testing.T) {
	files := map[string][]byte{}
	big := []byte(strings.Repeat("a", maxDefinitionBytes-10))
	for i := 0; i < int(maxDefinitionsTotal/int64(len(big)))+3; i++ {
		files["definitions/v11/site"+string(rune('a'+i%26))+string(rune('a'+i/26))+".yml"] = big
	}
	if _, _, err := extractDefinitions(tarGz(t, files), 11); err == nil || !strings.Contains(err.Error(), "in total") {
		t.Fatalf("err = %v, want the total-size refusal", err)
	}
}
