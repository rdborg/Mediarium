package indexers

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const selectorFixture = `<!DOCTYPE html>
<html><head><title>Fixture</title></head>
<body>
<div id="main" class="content wide" data-kind="list">
  <h1 class="title">Results</h1>
  <table id="results" class="lista">
    <tr class="header"><th>Name</th><th>Size</th></tr>
    <tr class="row odd" data-id="1"><td class="coll-1"><a href="/torrent/1/alpha/">Alpha 1080p</a></td><td class="coll-4">1.2 GB</td><td><span>seed</span></td></tr>
    <tr class="row even" data-id="2"><td class="coll-1"><a href="/torrent/2/beta/">Beta 720p</a><img src="/img/free.png"></td><td class="coll-4">700 MB</td><td></td></tr>
    <tr class="row odd" data-id="3"><td class="coll-1"><a href="https://other.test/x">Gamma (2160p)</a></td><td class="coll-4">20 GB</td><td><b>x</b></td></tr>
  </table>
  <p lang="en-US">English</p>
  <ul><li>one</li><li class="x">two</li><li>three</li><li>four</li></ul>
  <span>first span</span><em>emphasis</em><span>second span</span>
</div>
</body></html>`

func mustDoc(t *testing.T, s string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// describe renders matched elements compactly: tag + text/data-id.
func describe(nodes []*html.Node) string {
	var parts []string
	for _, n := range nodes {
		label := n.Data
		if v, ok := attr(n, "data-id"); ok {
			label += "#" + v
		} else if txt := strings.TrimSpace(textContent(n)); txt != "" && len(txt) < 20 {
			label += "(" + txt + ")"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, ",")
}

func TestSelectorMatching(t *testing.T) {
	doc := mustDoc(t, selectorFixture)
	tests := []struct {
		sel  string
		want string
	}{
		{"h1", "h1(Results)"},
		{"#results tr.row", "tr#1,tr#2,tr#3"},
		{".row.even", "tr#2"},
		{"tr[data-id]", "tr#1,tr#2,tr#3"},
		{`tr[data-id="2"]`, "tr#2"},
		{`tr[data-id='3']`, "tr#3"},
		{`tr[data-id=3]`, "tr#3"},
		{`a[href^="/torrent/"]`, "a(Alpha 1080p),a(Beta 720p)"},
		{`a[href$="/beta/"]`, "a(Beta 720p)"},
		{`a[href*="other"]`, "a(Gamma (2160p))"},
		{`tr[class~="odd"]`, "tr#1,tr#3"},
		{`p[lang|="en"]`, "p(English)"},
		{`tr[data-id!="1"].row`, "tr#2,tr#3"},
		{`a[HREF^="/TORRENT" i]`, "a(Alpha 1080p),a(Beta 720p)"},
		{"table > tbody > tr.row > td > a", "a(Alpha 1080p),a(Beta 720p),a(Gamma (2160p))"},
		{"div > a", ""},
		{"tr.header + tr", "tr#1"},
		{"tr.header ~ tr.odd", "tr#1,tr#3"},
		{"h1, p", "h1(Results),p(English)"},
		{"li:first-child", "li(one)"},
		{"li:last-child", "li(four)"},
		{"li:nth-child(2)", "li(two)"},
		{"li:nth-child(odd)", "li(one),li(three)"},
		{"li:nth-child(even)", "li(two),li(four)"},
		{"li:nth-child(2n+1)", "li(one),li(three)"},
		{"li:nth-child(-n+2)", "li(one),li(two)"},
		{"li:nth-child(n+3)", "li(three),li(four)"},
		{"li:nth-last-child(1)", "li(four)"},
		{"span:nth-of-type(2)", "span(second span)"},
		{"div > span:first-of-type", "span(first span)"},
		{"div > span:last-of-type", "span(second span)"},
		{"span:nth-last-of-type(2)", "span(first span)"},
		{"li:not(.x)", "li(one),li(three),li(four)"},
		{"li:not(:first-child, :last-child)", "li(two),li(three)"},
		{"tr.row:has(img)", "tr#2"},
		{`tr:has(a[href^="/torrent/"])`, "tr#1,tr#2"},
		{"tr.row:has(> td > b)", "tr#3"},
		{"tr.header:has(+ tr.odd)", "tr(NameSize)"},
		{`td:contains("Beta")`, "td(Beta 720p)"},
		{`td:contains('2160p')`, "td(Gamma (2160p))"},
		{`td:contains(700 MB)`, "td(700 MB)"},
		{`tr.row:not(:contains("Gamma"))`, "tr#1,tr#2"},
		{"td:empty", "td"},
		{"tr.row td:nth-child(3):empty", "td"},
		{":root", ""},
		{"*.title", "h1(Results)"},
		{":is(h1, p)", "h1(Results),p(English)"},
	}
	for _, tt := range tests {
		t.Run(tt.sel, func(t *testing.T) {
			sel, err := CompileSelector(tt.sel)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			got := describe(sel.All(doc))
			if tt.sel == ":root" {
				if n := sel.All(doc); len(n) != 1 || n[0].Data != "html" {
					t.Fatalf(":root should be the html element, got %s", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectorScopeAndSelf(t *testing.T) {
	doc := mustDoc(t, selectorFixture)
	rowSel, _ := CompileSelector("tr.row")
	rows := rowSel.All(doc)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	cell, _ := CompileSelector("td.coll-4")
	if got := strings.TrimSpace(textContent(cell.First(rows[1]))); got != "700 MB" {
		t.Errorf("query within row: got %q", got)
	}
	// :scope refers to the element the query runs on
	scoped, _ := CompileSelector(":scope > td:nth-child(2)")
	if got := describe(scoped.All(rows[0])); got != "td(1.2 GB)" {
		t.Errorf(":scope query: got %q", got)
	}
	// ancestors outside the row still count for descendant combinators
	anc, _ := CompileSelector("#results td.coll-1 a")
	if got := describe(anc.All(rows[2])); got != "a(Gamma (2160p))" {
		t.Errorf("ancestor outside scope: got %q", got)
	}
	// Matches tests the element itself
	odd, _ := CompileSelector(".odd")
	if !odd.Matches(rows[0]) || odd.Matches(rows[1]) {
		t.Error("Matches on the element itself is wrong")
	}
	star, _ := CompileSelector("*")
	if !star.Matches(rows[1]) {
		t.Error("* should match any element")
	}
}

func TestSelectorErrors(t *testing.T) {
	bad := []string{
		"", "tr[", "tr[data-id=", `a[href^="x"`, "li:nth-child(", "li:nth-child(x)",
		"li:unknown-pseudo", "p::before", "div >", ":not(", `:contains("x`, "a b)", "#", ".",
		"a:first-child(2)", "li:has()",
	}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			if _, err := CompileSelector(s); err == nil {
				t.Errorf("expected an error for %q", s)
			}
		})
	}
}

func TestSelectorNeverPanics(t *testing.T) {
	doc := mustDoc(t, selectorFixture)
	inputs := []string{
		":has(:has(:has(a)))", ":not(:not(:not(li)))", "tr:has(~ tr:has(+ tr))",
		strings.Repeat(":not(", 40) + "a" + strings.Repeat(")", 40),
		"a" + strings.Repeat(" > a", 200), "[a=\\", "\\", ":nth-child(99999999999999999999)",
	}
	for _, s := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %q: %v", s, r)
				}
			}()
			if sel, err := CompileSelector(s); err == nil {
				sel.All(doc)
			}
		}()
	}
}

func TestParseNth(t *testing.T) {
	tests := []struct {
		in   string
		a, b int
		ok   bool
	}{
		{"3", 0, 3, true}, {"odd", 2, 1, true}, {"even", 2, 0, true}, {"2n+1", 2, 1, true},
		{"-n+3", -1, 3, true}, {"n", 1, 0, true}, {"+n+2", 1, 2, true}, {" 3n - 1 ", 3, -1, true},
		{"x", 0, 0, false}, {"", 0, 0, false},
	}
	for _, tt := range tests {
		a, b, err := parseNth(tt.in)
		if (err == nil) != tt.ok || (tt.ok && (a != tt.a || b != tt.b)) {
			t.Errorf("parseNth(%q) = %d,%d,%v; want %d,%d ok=%v", tt.in, a, b, err, tt.a, tt.b, tt.ok)
		}
	}
}
