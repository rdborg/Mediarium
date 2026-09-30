package indexers

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// Site definitions are written by strangers and the pages, feeds and JSON they
// read come from sites that may be hostile. These fuzz targets check that odd
// input never panics, never runs for long and never makes anything unbounded.

func fuzzSeedFiles(f *testing.F, glob string) {
	files, _ := filepath.Glob(glob)
	for _, name := range files {
		if data, err := os.ReadFile(name); err == nil {
			f.Add(data)
		}
	}
}

// slow fails a test whose body took longer than limit.
func slow(t *testing.T, start time.Time, limit time.Duration, what string) {
	t.Helper()
	if d := time.Since(start); d > limit {
		t.Fatalf("%s took %v", what, d)
	}
}

func FuzzApplyTemplate(f *testing.F) {
	for _, s := range []string{
		"", "plain", "{{ .Keywords }}", `{{ if .Config.x }}a{{ else }}b{{ end }}`, `{{ range .Categories }}c{{.}}=1&{{end}}`,
		`{{ join .Categories "," }}`, `{{ re_replace .Keywords "\d+" "N" }}`, `{{ .Config.2fa }}`, `{{ .Config.sort-by }}`,
		"{{", "{{ }}", `{{ "unterminated }}`, `{{)}}`, `{{ ((( }}`, `{{- .Keywords -}}`, `{{ $x := "a" }}{{ $x }}`,
		`{{range 1000000000}}{{end}}`, `{{range 'a'}}{{end}}`, `{{define "a"}}{{template "a"}}{{end}}{{template "a"}}`,
		`{{block "a" .}}{{template "a" .}}{{end}}`, `{{range.Categories}}{{range.Categories}}{{range.Categories}}x{{end}}{{end}}{{end}}`,
		`{{ printf "%1000000s" "x" }}`, `{{ printf "%*d" 99999999 1 }}`, `{{ .Today.Year }}`, `{{ call .Keywords }}`,
		`{{ index .Config "a" 1 2 3 }}`, `{{ len .Categories }}`, `{{ slice .Keywords 5 2 }}`, `{{ html .Keywords }}{{ js .Keywords }}`,
		`{{ if and .Config.a (or .Config.b) }}x{{ end }}`, `{{ with .Config.a }}{{ . }}{{ end }}`, `{{/* comment */}}`,
		`{{ re_replace .Keywords "(a+)+$" "$1$1$1$1$1$1$1$1" }}`, `{{ re_replace .Keywords "(" "x" }}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		vars := newTmplVars()
		vars.Config["a"] = "aaaa"
		vars.Config["x"] = ".True"
		vars.Keywords = "The Movie 2024"
		vars.Categories = []string{"2000", "2010", "5000"}
		for _, enc := range []func(string) string{nil, urlPathEscape} {
			start := time.Now()
			out, err := applyTemplate(src, vars, enc)
			slow(t, start, 5*time.Second, "template")
			if err == nil && len(out) > maxTemplateOutput+maxTemplateOutput/2 {
				// each printed value is limited, so the whole cannot pass the limit by much
				t.Fatalf("template made %d bytes", len(out))
			}
		}
		_ = checkTemplate(src)
	})
}

func FuzzApplyFilter(f *testing.F) {
	names := make([]string, 0, len(knownFilters))
	for name := range knownFilters {
		names = append(names, name)
	}
	sort.Strings(names)
	for i := range names {
		f.Add(uint8(i), "page?a=1&b=2#x", "1", "", "1.5 GB 3 hours ago")
		f.Add(uint8(i), "", "(", "$1$1", "{{ range .Categories }}x{{ end }}")
		f.Add(uint8(i), `{"a":[1,2,{"b":null}]}`, "a", ",", "2024-01-02 03:04")
	}
	f.Fuzz(func(t *testing.T, pick uint8, data, arg0, arg1, arg2 string) {
		name := names[int(pick)%len(names)]
		vars := newTmplVars()
		vars.Keywords = "movie"
		start := time.Now()
		out, err := applyFilters(data, []FilterBlock{{Name: name, Args: []any{arg0, arg1, arg2}}}, vars)
		slow(t, start, 5*time.Second, "filter "+name)
		if err == nil && len(out) > maxFilterBytes {
			t.Fatalf("filter %s made %d bytes", name, len(out))
		}
		// the same filter with a single plain argument
		_, _ = applyFilters(data, []FilterBlock{{Name: name, Args: arg0}}, vars)
	})
}

// FuzzSizesAndCounts: numbers read off a page are never negative sizes or
// wrapped-around values.
func FuzzSizesAndCounts(f *testing.F) {
	for _, s := range []string{"1.5 GB", "700 MiB", "1,234.5 MB", "99999999999999999999999999 PB", "1e400", "", "-5", "1.2.3.4 KB", strings.Repeat("9", 400), "٣ GB"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if n := parseSize(s); n < 0 {
			t.Fatalf("parseSize(%q) = %d", s, n)
		}
		_ = parseCount(s)
		_ = sane(parseCount(s))
	})
}

func FuzzDates(f *testing.F) {
	for _, s := range []string{
		"3 hours ago", "1 day 2 hours", "an hour ago", "yesterday", "today 12:30", "2024-05-01 10:00", "1700000000", "1700000000000",
		"Mon, 02 Jan 2006 15:04:05 -0700", "99999999999999999999 years ago", "1e400 seconds ago", "12:30 pm", "", "  ", "now",
		"-5 days ago", "3 months ago", "0.5 weeks ago", "1,5 days ago",
	} {
		f.Add(s, "yyyy-MM-dd HH:mm:ss zzz")
		f.Add(s, "2006-01-02 15:04")
	}
	f.Add("x", "'unterminated")
	f.Add("x", `\`)
	f.Add("x", strings.Repeat("f", 100))
	f.Fuzz(func(t *testing.T, value, layout string) {
		now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
		start := time.Now()
		_, _ = fromUnknownDate(value, now)
		_, _ = fromTimeAgo(value, now)
		_, _ = parseDateLayout(value, layout, now)
		_ = dotNetLayoutToGo(layout)
		slow(t, start, 5*time.Second, "date parsing")
	})
}

func FuzzCompileRegex(f *testing.F) {
	for _, s := range []string{`\d+`, `(?<=a)b`, `é`, `\p{IsCyrillic}`, `\p{`, `\p{IsX`, `\u12`, `(`, `[`, `a{1000}{1000}`, `(a*)*b`, "\\", `\é`, strings.Repeat("(", 300), `(?i)x`, `\k<a>`, `(?<n>a)\k<n>`} {
		f.Add(s, "aaaaaaaaaaaaaaaaaaaaaaaa!")
	}
	f.Fuzz(func(t *testing.T, pattern, input string) {
		start := time.Now()
		re, err := compileDotNetRegex(pattern)
		if err == nil {
			_ = re.FindStringSubmatch(input)
			_, _ = regexReplace(re, input, "$1-$2-${3}")
		}
		slow(t, start, 5*time.Second, "regular expression")
	})
}

// FuzzSelector: any selector compiles or is refused, and matching it against
// any page finishes quickly.
func FuzzSelector(f *testing.F) {
	page := `<html><body><table id="t"><tr class="a b"><td>1</td><td><a href="/x">x</a></td></tr><tr><td>2</td></tr></table><ul><li>a<li>b<li>c</ul><p>text<p>more</body></html>`
	for _, s := range []string{
		"tr", "tr.a", "#t td:nth-child(2)", "td:first-child", "a[href^='/']", "a[href$=x i]", "li:nth-of-type(2n+1)", "tr:has(> td a)",
		"td:not(:first-child)", "p:contains('more')", "ul > li + li", "ul ~ p", "*", "a,b,c", ":scope > *", "", " ", ",", "(", "[", "[a", "a[b=", ":nth-child(", ":nth-child(99999999999999999999)",
		":not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(:not(a)))))))))))))))))))))))))))))))))))",
		"\\", "a\\", "#\\", ":has(", ":is()", "tr td td td td td td td td td td td td td td td td td td td td",
	} {
		f.Add(s, page)
	}
	f.Add("div div div div div div div div span", strings.Repeat("<div>", 400)+"<i>"+strings.Repeat("</div>", 400))
	f.Add("i:nth-child(2)", strings.Repeat("<i>", 3000))
	f.Add("i:nth-last-of-type(2n)", strings.Repeat("<i></i>", 3000))
	f.Add("div:contains(x)", strings.Repeat("<div>", 300)+"x")
	f.Fuzz(func(t *testing.T, sel, page string) {
		start := time.Now()
		s, err := CompileSelector(sel)
		if err != nil {
			return
		}
		doc, err := html.Parse(strings.NewReader(page))
		if err != nil {
			return
		}
		budget := &workBudget{left: 5_000_000}
		found := s.AllWith(doc, budget)
		if first := s.FirstWith(doc, &workBudget{left: 5_000_000}); first != nil && len(found) == 0 && budget.left >= 0 {
			t.Fatal("First found an element that All did not")
		}
		for _, n := range found {
			if n.Type != html.ElementNode {
				t.Fatalf("selector matched a node of type %v", n.Type)
			}
			_ = textContent(n)
		}
		slow(t, start, 5*time.Second, "selector "+sel)
	})
}

func FuzzParseDefinition(f *testing.F) {
	fuzzSeedFiles(f, "testdata/cardigann/*.yml")
	f.Add([]byte("id: a\nlinks: [http://x]\nsearch:\n  path: /\n  rows: {selector: tr}\n  fields: {title: {selector: a}}\n"))
	f.Add([]byte("id: a\nx: &a [1,2,3,4,5,6,7,8,9]\ny: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]\nz: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]\n"))
	f.Add([]byte("id: \"\\/\"\n"))
	f.Add([]byte(strings.Repeat("[", 20000)))
	f.Add([]byte("id: a\nsearch:\n  rows: {after: -1}\n  paths: [{path: '{{range 1000000}}{{end}}'}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		start := time.Now()
		def, err := ParseDefinition(data)
		if err != nil {
			return
		}
		_ = def.Validate()
		sum := summarize(def)
		_ = sum.IsSecret("password")
		_ = def.siteCategories([]int{2000, 5000})
		_ = def.newznabForSite("1")
		_ = def.newznabForDesc("Movies")
		slow(t, start, 10*time.Second, "definition")
	})
}

func FuzzJSONQuery(f *testing.F) {
	body := `{"data":{"torrents":[{"id":1,"name":"a","info":{"free":0},"files":[1,2]},{"id":2,"name":null}]},"n":12345678901234567890,"s":"x"}`
	for _, sel := range []string{"data.torrents", "$", "$.n", "data.torrents[0].name", "data.torrents[-1]", "data['torrents']", "data.torrents:has(id)", "data.torrents:not(info.free:contains(0))", "release:not(:contains(\"a\"))", "a[", "[", ":", ":has(", ":x(y)", "..a", "data.torrents[99999999999999999999]"} {
		f.Add([]byte(body), sel)
	}
	f.Add([]byte(strings.Repeat("[", 9999)+strings.Repeat("]", 9999)), "$")
	f.Add([]byte(strings.Repeat("[", 20000)), "$")
	f.Fuzz(func(t *testing.T, data []byte, sel string) {
		start := time.Now()
		root, err := decodeJSON(data)
		if err != nil {
			return
		}
		v, _, _ := jsonQuery(root, sel)
		_ = jsonScalar(v)
		_, _ = jsonSelect(root, strings.TrimLeft(sel, "."))
		_, _ = jsonJoinArray(string(data), sel, ",")
		slow(t, start, 5*time.Second, "json")
	})
}

func FuzzParseXMLDoc(f *testing.F) {
	f.Add([]byte(`<?xml version="1.0"?><rss><channel><item><title>a</title><torznab:attr name="seeders" value="3"/></item></channel></rss>`))
	f.Add([]byte(`<!DOCTYPE x [<!ENTITY a "aaaa"><!ENTITY b "&a;&a;&a;&a;">]><x>&b;&b;</x>`))
	f.Add([]byte(strings.Repeat("<a>", 100000)))
	f.Add([]byte(`<?xml version="1.0" encoding="shift_jis"?><a>x</a>`))
	f.Add([]byte(`<?xml version="1.0" encoding="nonsense"?><a>x</a>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		start := time.Now()
		doc, err := parseXMLDoc(data)
		if err != nil {
			return
		}
		if sel, err := CompileSelector("item > title, *"); err == nil {
			_ = sel.AllWith(doc, &workBudget{left: 5_000_000})
		}
		_ = textContent(doc)
		slow(t, start, 5*time.Second, "xml")
	})
}

func FuzzLinkRef(f *testing.F) {
	f.Add(int64(7), "https://example.org/download.php?id=1&key=a b")
	f.Add(int64(-1), "magnet:?xt=urn:btih:abc")
	f.Add(int64(0), "javascript:alert(1)")
	f.Add(int64(1), "")
	f.Fuzz(func(t *testing.T, id int64, link string) {
		ref := EncodeLinkRef(id, link)
		if !IsLinkRef(ref) {
			t.Fatalf("%q is not a link reference", ref)
		}
		gotID, gotLink, ok := DecodeLinkRef(ref)
		if ok && (gotID != id || gotLink != link) {
			t.Fatalf("round trip gave %d %q, want %d %q", gotID, gotLink, id, link)
		}
		// whatever comes back must be a web or magnet address
		if ok && !strings.HasPrefix(gotLink, "magnet:") {
			if !strings.HasPrefix(strings.ToLower(gotLink), "http://") && !strings.HasPrefix(strings.ToLower(gotLink), "https://") {
				t.Fatalf("decoded link %q is not a web address", gotLink)
			}
		}
		_, _, _ = DecodeLinkRef(link)
	})
}

// FuzzNewznabFeed: any reply is a list of releases or an error, and an error
// never repeats the API key.
func FuzzNewznabFeed(f *testing.F) {
	f.Add([]byte(`<?xml version="1.0"?><rss xmlns:newznab="x"><channel><item><title>A.Movie.2024.1080p</title><guid>g</guid><link>http://x/y.nzb</link><pubDate>Mon, 02 Jan 2006 15:04:05 +0000</pubDate><enclosure url="http://x/y.nzb" length="123" type="application/x-nzb"/><newznab:attr name="size" value="99"/><newznab:attr name="category" value="2040"/></item></channel></rss>`))
	f.Add([]byte(`<error code="100" description="Incorrect user credentials (key=SECRETKEY1)"/>`))
	f.Add([]byte(`<rss><channel><error description="SECRETKEY1 is over its limit"/></channel></rss>`))
	f.Add([]byte(`<rss><channel><item><enclosure length="-9223372036854775808"/><attr name="seeders" value="-5"/><attr name="size" value="-1"/></item></channel></rss>`))
	f.Add([]byte(`<rss><channel>` + strings.Repeat(`<item><title> a  b </title></item>`, 2000) + `</channel></rss>`))
	f.Add([]byte(strings.Repeat("<a>", 50000)))
	f.Fuzz(func(t *testing.T, body []byte) {
		start := time.Now()
		results, err := parseNewznabFeed(body, "Idx", "SECRETKEY1")
		slow(t, start, 5*time.Second, "newznab feed")
		if err != nil && strings.Contains(err.Error(), "SECRETKEY1") {
			t.Fatalf("error repeats the key: %v", err)
		}
		if len(results) > maxNewznabItems {
			t.Fatalf("%d results", len(results))
		}
		for _, r := range results {
			if r.SizeBytes < 0 || r.Seeders < 0 || r.Peers < 0 {
				t.Fatalf("negative number in %+v", r)
			}
			if len(r.Title) > maxTitleBytes {
				t.Fatalf("title of %d bytes", len(r.Title))
			}
			if y := r.PublishDate.Year(); y < 0 || y > 9999 {
				t.Fatalf("date %v", r.PublishDate)
			}
		}
	})
}
