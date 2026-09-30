package inputcheck_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/inputcheck"
)

// FuzzChecks feeds every check odd text. None may panic, the answer must be
// the same each time, and a complaint must be a readable sentence.
func FuzzChecks(f *testing.F) {
	for _, s := range []string{
		"", " ", "name@example.com", "a@b", "@", "a@@b.c", "news.example.com", "news.example.com:563", "[::1]", "::1", "1.2.3.4", "999.1.1.1",
		"http://a.example", "https://[::1]:8080/x", "http://", "http://:80", "http://a b", "ftp://x", "//x", "a.example:99999", "http://a.example:0",
		"/media", `C:\Media`, `\\nas\share`, "media", "/a\x00b", "key", "a b", strings.Repeat("k", 600), "\xff\xfe", "日本語@例え.jp", "%zz",
		"http://user:pass@host/", "http://[fe80::1%25eth0]/", strings.Repeat("a.", 200) + "com",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		results := map[string]string{
			"Email":   inputcheck.Email(v),
			"Host":    inputcheck.Host(v, "news.example.com"),
			"HTTPURL": inputcheck.HTTPURL(v, "http://192.168.1.10:8080", false),
			"Strict":  inputcheck.HTTPURL(v, "https://a.example", true),
			"AbsPath": inputcheck.AbsPath(v, "/media"),
			"APIKey":  inputcheck.APIKey(v),
		}
		again := map[string]string{
			"Email":   inputcheck.Email(v),
			"Host":    inputcheck.Host(v, "news.example.com"),
			"HTTPURL": inputcheck.HTTPURL(v, "http://192.168.1.10:8080", false),
			"Strict":  inputcheck.HTTPURL(v, "https://a.example", true),
			"AbsPath": inputcheck.AbsPath(v, "/media"),
			"APIKey":  inputcheck.APIKey(v),
		}
		for name, msg := range results {
			if again[name] != msg {
				t.Fatalf("%s(%q) is not deterministic", name, v)
			}
			if msg != "" && (!utf8.ValidString(msg) || !strings.HasSuffix(msg, ".")) {
				t.Fatalf("%s(%q) = %q is not a sentence", name, v, msg)
			}
		}
		// Accepted values must agree with the plain predicates.
		if strings.TrimSpace(v) != "" && results["Email"] == "" && !inputcheck.ValidEmail(strings.TrimSpace(v)) {
			t.Fatalf("Email(%q) accepts what ValidEmail rejects", v)
		}
		if h := strings.Trim(strings.TrimSpace(v), "[]"); strings.TrimSpace(v) != "" && results["Host"] == "" && !inputcheck.LooksLikeHost(h) {
			t.Fatalf("Host(%q) accepts what LooksLikeHost rejects", v)
		}
	})
}
