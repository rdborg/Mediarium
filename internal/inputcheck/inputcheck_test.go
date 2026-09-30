package inputcheck_test

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/inputcheck"
)

func TestChecks(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) string
		rows []struct {
			in string
			ok bool
		}
	}{
		{"Email", inputcheck.Email, []struct {
			in string
			ok bool
		}{
			{"", true}, {"name@example.com", true}, {"a.b+c@sub.example.co.uk", true},
			{"name@example", false}, {"name@.com", false}, {"@example.com", false}, {"a b@example.com", false},
			{"a@b.com, c@d.com", false}, {"Name <n@example.com>", false},
		}},
		{"Host", func(s string) string { return inputcheck.Host(s, "news.example.com") }, []struct {
			in string
			ok bool
		}{
			{"", true}, {"news.example.com", true}, {"localhost", true}, {"10.0.0.5", true}, {"::1", true},
			{"999.0.0.1", false}, {"news.example.com:563", false}, {"http://x", false}, {"a b", false}, {"-x.example.com", false},
		}},
		{"HTTPURL", func(s string) string { return inputcheck.HTTPURL(s, "http://192.168.1.10:8080", false) }, []struct {
			in string
			ok bool
		}{
			{"", true}, {"http://192.168.1.10:8080", true}, {"https://a.example/path?x=1", true}, {"192.168.1.10:8080", true},
			{"ftp://a.example", false}, {"http://", false}, {"http://a.example:0", false}, {"a b", false},
		}},
		{"HTTPURL with scheme required", func(s string) string { return inputcheck.HTTPURL(s, "https://a.example", true) }, []struct {
			in string
			ok bool
		}{
			{"https://a.example", true}, {"a.example", false},
		}},
		{"AbsPath", func(s string) string { return inputcheck.AbsPath(s, "/media") }, []struct {
			in string
			ok bool
		}{
			{"", true}, {"/media", true}, {`C:\Media`, true}, {`\\nas\share`, true}, {"media", false}, {"/a\x00b", false},
		}},
		{"APIKey", inputcheck.APIKey, []struct {
			in string
			ok bool
		}{
			{"", true}, {"abc123", true}, {"a b", false}, {"a\nb", false}, {strings.Repeat("k", 600), false},
		}},
	}
	for _, c := range cases {
		for _, r := range c.rows {
			if got := c.fn(r.in); (got == "") != r.ok {
				t.Errorf("%s(%q) = %q, want ok=%v", c.name, r.in, got, r.ok)
			}
		}
	}
}
