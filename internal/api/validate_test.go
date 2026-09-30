package api

import (
	"strings"
	"testing"
)

// check runs fn over rows of (input, wantOK).
func runChecks(t *testing.T, name string, fn func(string) string, rows []struct {
	in string
	ok bool
}) {
	t.Helper()
	for _, r := range rows {
		got := fn(r.in)
		if (got == "") != r.ok {
			t.Errorf("%s(%q) = %q, want ok=%v", name, r.in, got, r.ok)
		}
	}
}

type row = struct {
	in string
	ok bool
}

func TestCheckEmail(t *testing.T) {
	runChecks(t, "checkEmail", checkEmail, []row{
		{"", true}, {"name@example.com", true}, {"first.last+tag@sub.example.co.uk", true}, {"  name@example.com ", true},
		{"name@example", false}, {"name@.com", false}, {"name@example.", false}, {"@example.com", false},
		{"name@@example.com", false}, {"na me@example.com", false}, {"a@b.com, c@d.com", false}, {"Name <name@example.com>", false},
	})
}

func TestCheckUsername(t *testing.T) {
	runChecks(t, "checkUsername", checkUsername, []row{
		{"", false}, {"ryan", true}, {"ryan.b-1_x", true}, {"abc", true}, {"ab", false},
		{strings.Repeat("a", 32), true}, {strings.Repeat("a", 33), false},
		{"has space", false}, {"semi;colon", false}, {"émile", false},
	})
}

func TestCheckPassword(t *testing.T) {
	runChecks(t, "checkPassword", func(s string) string { return checkPassword(s, "Password") }, []row{
		{"", false}, {"12345678", true}, {"short", false},
		{strings.Repeat("a", 72), true}, {strings.Repeat("a", 73), false}, {strings.Repeat("é", 37), false},
	})
}

func TestCheckHost(t *testing.T) {
	runChecks(t, "checkHost", func(s string) string { return checkHost(s, "news.example.com") }, []row{
		{"", true}, {"news.example.com", true}, {"localhost", true}, {"192.168.1.10", true}, {"::1", true},
		{"[fe80::1]", true}, {"my-host_1", true}, {"999.1.1.1", false}, {"1.2.3", false},
		{"http://news.example.com", false}, {"news.example.com:563", false}, {"news.example.com/path", false},
		{"-bad.example.com", false}, {"bad host", false}, {"a..b", false},
	})
}

func TestCheckHTTPURL(t *testing.T) {
	runChecks(t, "checkHTTPURL", func(s string) string { return checkHTTPURL(s, "http://192.168.1.10:8080", false) }, []row{
		{"", true}, {"http://192.168.1.10:32400", true}, {"https://plex.example.com/web", true}, {"http://[::1]:8080", true},
		{"http://localhost", true}, {"192.168.1.10:8080", true}, {"plex.local", true},
		{"ftp://example.com", false}, {"javascript:alert(1)", false}, {"http://", false}, {"http://exa mple.com", false},
		{"http://host:99999", false}, {"http://300.1.1.1", false}, {"//example.com", false},
	})
	runChecks(t, "checkHTTPURL(requireScheme)", func(s string) string { return checkHTTPURL(s, "http://a.example", true) }, []row{
		{"", true}, {"http://a.example", true}, {"a.example", false}, {"192.168.1.10:8080", false},
	})
}

func TestCheckAbsPath(t *testing.T) {
	runChecks(t, "checkAbsPath", func(s string) string { return checkAbsPath(s, "/media/movies") }, []row{
		{"", true}, {"/media/movies", true}, {"/", true}, {`D:\Media\Movies`, true}, {"d:/media", true},
		{`\\nas\share\tv`, true}, {"media/movies", false}, {"./movies", false}, {"D:", false},
		{"/media/mov\x00ies", false}, {"~/movies", false},
	})
}

func TestCheckAPIKey(t *testing.T) {
	runChecks(t, "checkAPIKey", checkAPIKey, []row{
		{"", true}, {"abc123DEF", true}, {"  abc123  ", true}, {"abc 123", false}, {"abc\n123", false},
		{strings.Repeat("x", 513), false},
	})
}

func TestCheckNumbers(t *testing.T) {
	ports := []struct {
		n  int
		ok bool
	}{{0, false}, {1, true}, {563, true}, {65535, true}, {65536, false}, {-1, false}}
	for _, p := range ports {
		if got := checkPort(p.n, "Port"); (got == "") != p.ok {
			t.Errorf("checkPort(%d) = %q", p.n, got)
		}
	}
	if got := checkPort(0, "Port"); got != "Port must be a number between 1 and 65535." {
		t.Errorf("port message = %q", got)
	}
	if checkIntRange(5, "Connections", 1, 100) != "" || checkIntRange(0, "Connections", 1, 100) == "" || checkIntRange(101, "Connections", 1, 100) == "" {
		t.Error("checkIntRange")
	}
	if checkAtLeast(1, "Limit", 1) != "" || checkAtLeast(0, "Limit", 1) == "" || checkAtLeast(-1, "Limit", 0) == "" || checkAtLeast(0, "Limit", 0) != "" {
		t.Error("checkAtLeast")
	}
}

func TestCheckTextHelpers(t *testing.T) {
	if checkRequired("  ", "Add a name.") != "Add a name." || checkRequired("x", "m") != "" {
		t.Error("checkRequired")
	}
	if checkMaxLen(strings.Repeat("é", 10), "Name", 10) != "" || checkMaxLen(strings.Repeat("é", 11), "Name", 10) == "" {
		t.Error("checkMaxLen")
	}
	if checkNoControl("a\nb", "Name") == "" || checkNoControl("ab", "Name") != "" {
		t.Error("checkNoControl")
	}
	if firstProblem("", "", "x", "y") != "x" || firstProblem() != "" {
		t.Error("firstProblem")
	}
}
