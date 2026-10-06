package indexers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func hardeningSession(t *testing.T, base string) *cgSession {
	t.Helper()
	def := &Definition{ID: "x", Name: "X", Links: []string{base}}
	s, err := newSession(NewCardigannManager(nil, nil), def, Instance{Name: "X", BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTemplateRefusesRunawayConstructs(t *testing.T) {
	vars := newTmplVars()
	vars.Categories = []string{"2000", "2010", "5000"}
	vars.Keywords = "movie"
	cases := []struct {
		name    string
		src     string
		wantErr string // "" = must work
		want    string
	}{
		{"loop over categories", `{{ range .Categories }}c{{.}}=1&{{end}}`, "", "c2000=1&c2010=1&c5000=1&"},
		{"loop with variables", `{{ range $i, $e := .Categories }}{{ $e }};{{ end }}`, "", "2000;2010;5000;"},
		{"two loops nested", `{{ range .Categories }}{{ range $.Categories }}.{{ end }}{{ end }}`, "", "........."},
		{"loop over a number", `{{ range 1000000000 }}{{ end }}`, "number", ""},
		{"loop over a character", `{{ range 'a' }}{{ end }}`, "number", ""},
		{"loop over a number, no space", `{{ range(1000000000) }}{{ end }}`, "number", ""},
		{"three loops nested", `{{ range .Categories }}{{ range .Categories }}{{ range .Categories }}x{{ end }}{{ end }}{{ end }}`, "nested", ""},
		{"three loops nested, no spaces", `{{range.Categories}}{{range.Categories}}{{range.Categories}}x{{end}}{{end}}{{end}}`, "nested", ""},
		{"loops inside conditions still count", `{{ range .Categories }}{{ if .Keywords }}{{ range .Categories }}{{ range .Categories }}x{{ end }}{{ end }}{{ end }}{{ end }}`, "nested", ""},
		{"a template that calls itself", `{{ define "a" }}{{ template "a" }}{{ end }}{{ template "a" }}`, "not supported", ""},
		{"a block", `{{ block "a" . }}x{{ end }}`, "not supported", ""},
		{"too much text", `{{ range .Categories }}{{ range $.Categories }}{{ printf "%200000s" "x" }}{{ end }}{{ end }}`, "too much text", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			out, err := applyTemplate(tc.src, vars, nil)
			if time.Since(start) > 3*time.Second {
				t.Fatalf("took %v", time.Since(start))
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tc.want != "" && out != tc.want {
					t.Fatalf("got %q, want %q", out, tc.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want an error mentioning %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestFilterCannotGrowWithoutLimit(t *testing.T) {
	big := strings.Repeat("a", 20<<20)
	cases := []struct {
		name string
		f    FilterBlock
	}{
		{"replace", FilterBlock{Name: "replace", Args: []any{"a", strings.Repeat("b", 1000)}}},
		{"re_replace", FilterBlock{Name: "re_replace", Args: []any{".", strings.Repeat("b", 1000)}}},
		{"re_replace with group", FilterBlock{Name: "re_replace", Args: []any{"(a*)", "$1$1$1$1$1$1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			out, err := applyFilters(big, []FilterBlock{tc.f}, nil)
			if err == nil || len(out) != 0 {
				t.Fatalf("a %d MB value was allowed to grow: err=%v len=%d", len(big)>>20, err, len(out))
			}
			if time.Since(start) > 3*time.Second {
				t.Fatalf("took %v", time.Since(start))
			}
		})
	}
	// ordinary use is untouched
	got, err := applyFilters("a-b-c", []FilterBlock{{Name: "replace", Args: []any{"-", "--"}}, {Name: "re_replace", Args: []any{"(b)", "[$1]"}}}, nil)
	if err != nil || got != "a--[b]--c" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestSplitAndTrimTakeWholeCharacters(t *testing.T) {
	got, err := applyFilter("a·b·c", FilterBlock{Name: "split", Args: []any{"·", "1"}}, nil)
	if err != nil || got != "b" {
		t.Errorf("split on a multi-byte separator gave %q, %v", got, err)
	}
	got, err = applyFilter("··a··", FilterBlock{Name: "trim", Args: []any{"·"}}, nil)
	if err != nil || got != "a" {
		t.Errorf("trim of a multi-byte character gave %q, %v", got, err)
	}
}

func TestParseSizeAndCountStayInRange(t *testing.T) {
	for _, s := range []string{"99999999999999999999999 PB", strings.Repeat("9", 300) + " GB", "1e999 TB", "9223372036854775807 PB", "18446744073709551615"} {
		if n := parseSize(s); n < 0 {
			t.Errorf("parseSize(%q) = %d", s, n)
		}
		if n := parseCount(s); n < 0 {
			t.Errorf("parseCount(%q) = %d", s, n)
		}
	}
	if got := parseSize("1.5 GB"); got != 1610612736 {
		t.Errorf("parseSize(1.5 GB) = %d", got)
	}
}

func TestResolvePathStaysOnTheSite(t *testing.T) {
	s := hardeningSession(t, "https://www.example.org/")
	cases := []struct {
		path string
		ok   bool
	}{
		{"/search?q=a", true},
		{"search/a b", true},
		{"https://www.example.org/x", true},
		{"https://api.example.org/x", true},
		{"HTTPS://API.EXAMPLE.ORG/x", true},
		{"//api.example.org/x", true},
		{"https://www.example.org./x", true}, // a trailing dot is the same host
		{"https://WWW.EXAMPLE.ORG:8443/x", true},
		{"https://www.example。org/x", false}, // an ideographic full stop is not a dot here
		{"https://xn--www-example-9d0b.org/x", false},
		{"//evil.example.net/x", false},
		{"HTTPS://evil.example.net/x", false},
		{"Http://evil.example.net", false},
		{"https://evil.example.net/x", false},
		{"https:evil.example.net", false},
		{"https://www.example.org.evil.net/", false},
		{"https://www.example.org@evil.net/", false},
		{"https://evil.net\\@www.example.org/", false},
		{"javascript:alert(1)", false},
		{"file:///etc/passwd", false},
		{"ftp://www.example.org/x", false},
		{"magnet:?xt=urn:btih:abc", false},
		{"http://127.0.0.1:8080/x", false},
		{"http://169.254.169.254/latest/meta-data/", false},
	}
	for _, tc := range cases {
		got, err := s.resolvePath(tc.path)
		if tc.ok && err != nil {
			t.Errorf("resolvePath(%q) refused: %v", tc.path, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("resolvePath(%q) = %q, want a refusal", tc.path, got)
		}
	}
}

// A login page whose form sends the password to another site must not be
// obeyed.
func TestLoginFormCannotSendCredentialsElsewhere(t *testing.T) {
	var stolen string
	thief := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		stolen = string(body)
	}))
	t.Cleanup(thief.Close)
	// same machine, different name: 127.0.0.1 is the site, localhost the thief
	thiefURL := strings.Replace(thief.URL, "127.0.0.1", "localhost", 1)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><form id="f" method="post" action="%s/collect"><input name="username"><input name="password" type="password"></form></body></html>`, thiefURL)
	}))
	t.Cleanup(site.Close)

	def := &Definition{
		ID: "phish", Name: "Phish", Links: []string{site.URL + "/"},
		Login:  &LoginBlock{Path: "login", Method: "form", Form: "form#f", Inputs: OrderedMap[string]{Keys: []string{"password"}, Values: map[string]string{"password": "hunter2"}}},
		Search: SearchBlock{Paths: []SearchPath{{Path: "s"}}},
	}
	m := NewCardigannManager(nil, nil)
	m.DefaultDelay = 0
	s, err := newSession(m, def, Instance{Name: "Phish", BaseURL: site.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	err = s.login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if stolen != "" {
		t.Fatalf("the password reached the other site: %q", stolen)
	}
}

func TestRequestDelayIsLimited(t *testing.T) {
	m := NewCardigannManager(nil, nil)
	m.DefaultDelay = 0
	for _, delay := range []float64{1e300, -5} {
		s := &cgSession{m: m, def: &Definition{RequestDelay: delay}, lastRequest: time.Now()}
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		err := s.wait(ctx)
		cancel()
		if delay < 0 && (err != nil || time.Since(start) > 200*time.Millisecond) {
			t.Errorf("a negative delay waited: %v after %v", err, time.Since(start))
		}
		if delay > 0 && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("a huge delay should wait (up to the limit), got %v", err)
		}
	}
}

func TestSelectorGivesUpOnAHostilePage(t *testing.T) {
	page := "<html><body>" + strings.Repeat("<i></i>", 150000) + "</body></html>"
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	sel, err := CompileSelector("i:nth-child(2n+1)")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	found := sel.AllWith(doc, &workBudget{left: 5_000_000})
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if len(found) >= 150000/2 {
		t.Fatalf("the whole page was read: %d matches", len(found))
	}
	// a sane page is read in full
	small, _ := html.Parse(strings.NewReader("<ul><li>a<li>b<li>c<li>d</ul>"))
	if n := len(sel.AllWith(small, newWorkBudget())); n != 0 {
		t.Errorf("unexpected matches %d", n)
	}
	odd, _ := CompileSelector("li:nth-child(odd)")
	if n := len(odd.AllWith(small, newWorkBudget())); n != 2 {
		t.Errorf("li:nth-child(odd) matched %d, want 2", n)
	}
}

func TestSelectorLongChainOnDeepPage(t *testing.T) {
	page := strings.Repeat("<div>", 500) + "<b>x</b>" + strings.Repeat("</div>", 500)
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	sel, err := CompileSelector("div div div div div div div div div span")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	sel.All(doc)
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestXMLReplyNestedTooDeep(t *testing.T) {
	if _, err := parseXMLDoc([]byte(strings.Repeat("<a>", 100000))); err == nil {
		t.Fatal("a reply nested 100000 deep was accepted")
	}
	doc, err := parseXMLDoc([]byte("<rss><channel><item><title>x</title></item></channel></rss>"))
	if err != nil || textContent(doc) != "x" {
		t.Fatalf("plain feed: %v", err)
	}
}

func TestFinishDropsAbsurdDatesAndTitles(t *testing.T) {
	s := hardeningSession(t, "https://www.example.org/")
	rb := &releaseBuilder{title: strings.Repeat("é", 2000), link: "https://www.example.org/d/1", date: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)}
	res, err := s.finish(rb)
	if err != nil {
		t.Fatal(err)
	}
	if !res.PublishDate.IsZero() {
		t.Errorf("date %v kept", res.PublishDate)
	}
	if len(res.Title) > maxTitleBytes || !strings.HasPrefix(res.Title, "é") {
		t.Errorf("title of %d bytes", len(res.Title))
	}
	rb = &releaseBuilder{title: "ok", link: "https://www.example.org/d/1", date: time.Now().Add(-time.Hour)}
	if res, _ = s.finish(rb); res.PublishDate.IsZero() {
		t.Error("a normal date was dropped")
	}
}

func TestNewznabReplyHandling(t *testing.T) {
	const key = "0123456789abcdef"
	respond := func(status int, body string) *NewznabClient {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, body)
		}))
		t.Cleanup(srv.Close)
		return NewNewznabClient("Idx", srv.URL, key)
	}
	ctx := context.Background()

	// an indexer that repeats the request (with the key) in its error page
	_, err := respond(403, "forbidden: /api?t=search&apikey="+key+"&q=x").Search(ctx, "x", nil)
	if err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("error shows the key: %v", err)
	}
	_, err = respond(200, `<error code="100" description="bad key `+key+`"/>`).Search(ctx, "x", nil)
	if err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("api error shows the key: %v", err)
	}

	// a reply that is far too big is refused, not read into memory
	huge := "<rss><channel>" + strings.Repeat("<item><title>a</title></item>", 700000) + "</channel></rss>"
	if _, err = respond(200, huge).Search(ctx, "x", nil); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("huge reply: %v", err)
	}

	// dates in the forms indexers really write, and numbers that cannot be right
	feed := `<rss><channel>
<item><title>A</title><pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate><enclosure url="http://x/a.nzb" length="-5"/></item>
<item><title>B</title><pubDate>Mon, 2 Jan 2006 15:04:05 +0100</pubDate><attr name="seeders" value="-9"/><attr name="peers" value="-1"/></item>
</channel></rss>`
	got, err := respond(200, feed).Search(ctx, "x", nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("feed: %v %v", got, err)
	}
	for _, r := range got {
		if r.PublishDate.Year() != 2006 {
			t.Errorf("%s: date %v not read", r.Title, r.PublishDate)
		}
		if r.SizeBytes < 0 || r.Seeders < 0 || r.Peers < 0 {
			t.Errorf("%s: negative number %+v", r.Title, r)
		}
	}
}

func TestBadAddressErrorsDoNotShowTheKey(t *testing.T) {
	const key = "KEYKEYKEY123456"
	// an address net/http cannot parse: its error would quote the whole URL
	_, err := NewNewznabClient("Idx", "http://host:99999", key).Search(context.Background(), "x", nil)
	if err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("newznab: %v", err)
	}
	_, err = NewNewznabClient("Idx", "http://host/%zz", key).Search(context.Background(), "x", nil)
	if err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("newznab with a bad escape: %v", err)
	}
	// a definition that builds an address with a bad escape while the person
	// keys are in the query
	s := hardeningSession(t, "https://www.example.org/")
	_, err = s.doOnce(context.Background(), cgRequest{url: "https://www.example.org/%zz?passkey=" + key})
	if err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("cardigann: %v", err)
	}
}

func TestLinkRefNeedsAHost(t *testing.T) {
	for _, link := range []string{"https:0", "http:", "https://", "https:///x"} {
		if _, _, ok := DecodeLinkRef(EncodeLinkRef(1, link)); ok {
			t.Errorf("%q was accepted", link)
		}
	}
	if id, link, ok := DecodeLinkRef(EncodeLinkRef(9, "https://example.org/d?id=1")); !ok || id != 9 || link != "https://example.org/d?id=1" {
		t.Errorf("a normal link was refused: %v %q", ok, link)
	}
}

func TestNewznabParseErrorDoesNotEchoTheKey(t *testing.T) {
	_, err := parseNewznabFeed([]byte("<rss>&SECRETKEY1000"), "Idx", "SECRETKEY1")
	if err == nil || strings.Contains(err.Error(), "SECRETKEY1") {
		t.Fatalf("error = %v", err)
	}
}

func TestSiteCategoriesComeInTheSameOrderEveryTime(t *testing.T) {
	def := &Definition{Caps: CapsBlock{Categories: map[string]string{}}}
	for i := 0; i < 30; i++ {
		def.Caps.Categories[fmt.Sprintf("c%02d", i)] = "Movies/HD"
	}
	want := strings.Join(def.siteCategories([]int{2000}), ",")
	if strings.Count(want, ",") != 29 {
		t.Fatalf("categories = %q", want)
	}
	for i := 0; i < 50; i++ {
		if got := strings.Join(def.siteCategories([]int{2000}), ","); got != want {
			t.Fatalf("order changed: %q then %q", want, got)
		}
	}
}

func TestLonePageFieldReadIsBounded(t *testing.T) {
	doc, err := html.Parse(strings.NewReader("<html><body>" + strings.Repeat("<i></i>", 120000) + "</body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, found, _ := htmlRow{n: doc}.value(SelectorField{Selector: "i:nth-child(120000)"}, newTmplVars())
	if found {
		t.Error("the last sibling was found within the budget")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("took %v", d)
	}
}

func TestBadLinkErrorsDoNotShowThePasskey(t *testing.T) {
	s := hardeningSession(t, "https://www.example.org/")
	for _, path := range []string{"se%zzarch?passkey=SECRETPASS", "/a%zz/b?x=1&passkey=SECRETPASS", "https://www.example.org/%zz?passkey=SECRETPASS"} {
		_, err := s.resolvePath(path)
		if err == nil || strings.Contains(err.Error(), "SECRETPASS") {
			t.Errorf("resolvePath(%q): %v", path, err)
		}
	}
}

func TestHugeRegularExpressionIsRefused(t *testing.T) {
	huge := strings.Repeat(`\pL{1000}`, 12) + "x"
	start := time.Now()
	if _, err := compileDotNetRegex(huge); err == nil || !strings.Contains(err.Error(), "too complex") {
		t.Errorf("a 12000-step pattern was accepted: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
	for _, ok := range []string{`\d+`, `(?i)(?:1080p|720p|2160p|4k)[. ]?(?:web|bluray)`, `^(\d{4})-(\d{2})-(\d{2})$`, strings.Repeat(`[a-z]{20}`, 20)} {
		if _, err := compileDotNetRegex(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

// TestFeedThatIsAWebPage: an address that gives back a web page (Prowlarr's
// home page instead of an indexer feed) gets a message saying what to use.
func TestFeedThatIsAWebPage(t *testing.T) {
	for _, body := range []string{"<!DOCTYPE html><html><head><title>Prowlarr</title></head></html>", "\n  <html lang=\"en\"><body>login</body></html>"} {
		_, err := parseNewznabFeed([]byte(body), "Prowlarr", "SECRETKEY1")
		if err == nil || !strings.Contains(err.Error(), "web page") || !strings.Contains(err.Error(), "/api") {
			t.Fatalf("%q: %v", body, err)
		}
	}
}
