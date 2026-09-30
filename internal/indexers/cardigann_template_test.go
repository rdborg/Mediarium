package indexers

import (
	"strings"
	"testing"
	"time"
)

func sampleVars() *tmplVars {
	v := newTmplVars()
	v.Config["sitelink"] = "https://site.test/"
	v.Config["sort"] = "seeders"
	v.Config["freeleech"] = checkboxTrue
	v.Config["disablesort"] = ""
	v.Config["2facode"] = "123456"
	v.Config["sort-by"] = "size"
	v.Keywords = "the matrix"
	v.Query["Keywords"] = "The Matrix"
	v.Query["IMDBID"] = ""
	v.Categories = []string{"1", "42"}
	v.Result["title_default"] = "Default Title"
	v.Result["title_optional"] = ""
	v.Result["_id"] = "77"
	v.now = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	return v
}

func TestApplyTemplate(t *testing.T) {
	tests := []struct {
		name, src, want string
		encode          bool
	}{
		{"plain", "no actions", "no actions", false},
		{"keywords", "q={{ .Keywords }}", "q=the matrix", false},
		{"config", "{{ .Config.sitelink }}details.php?id={{ .Result._id }}", "https://site.test/details.php?id=77", false},
		{"if true", "{{ if .Config.freeleech }}1{{ else }}0{{ end }}", "1", false},
		{"if false", "{{ if .Config.disablesort }}1{{ else }}0{{ end }}", "0", false},
		{"eq False", "{{ if eq .Config.disablesort .False }}sort{{ end }}", "sort", false},
		{"eq True", "{{ if eq .Config.freeleech .True }}yes{{ end }}", "yes", false},
		{"and/or", "{{ if and .Keywords (eq .Config.disablesort .False) }}a{{ end }}{{ if or .Query.IMDBID .Keywords }}b{{ end }}", "ab", false},
		{"or value", "{{ or .Result.title_optional .Result.title_default }}", "Default Title", false},
		{"missing result", "[{{ .Result.nothing }}]", "[]", false},
		{"missing top-level", "[{{ .Nothing }}]", "[]", false},
		{"range", "{{ range .Categories }}cat[]={{.}}&{{end}}", "cat[]=1&cat[]=42&", false},
		{"join", `{{ join .Categories "," }}`, "1,42", false},
		{"re_replace raw string", `{{ re_replace .Keywords "\s+" "." }}`, "the.matrix", false},
		{"re_replace group", `{{ re_replace .Keywords "(\w+) (\w+)" "$2-$1" }}`, "matrix-the", false},
		{"digit name", "{{ .Config.2facode }}", "123456", false},
		{"hyphen name", "{{ .Config.sort-by }}", "size", false},
		{"stray paren", "{{ if and (.Keywords) (eq .Config.disablesort .False)) }}x{{ else }}y{{ end }}", "x", false},
		{"today", "{{ .Today.Year }}", "2026", false},
		{"encoded path", "search/{{ .Keywords }}/1/", "search/the+matrix/1/", true},
		{"encoded keeps literal", "a b/{{ .Config.sort }}", "a b/seeders", true},
		{"trim markers", "a {{- .Config.sort -}} b", "aseedersb", false},
		{"else if", "{{ if .Query.IMDBID }}imdb{{ else if .Keywords }}kw{{ else }}none{{ end }}", "kw", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var enc func(string) string
			if tt.encode {
				enc = urlPathEscape
			}
			got, err := applyTemplate(tt.src, sampleVars(), enc)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyTemplateErrors(t *testing.T) {
	for _, src := range []string{"{{ if .Keywords }}unterminated", "{{ .Keywords ", "{{ nosuchfunc .Keywords }}"} {
		if _, err := applyTemplate(src, sampleVars(), nil); err == nil {
			t.Errorf("expected an error for %q", src)
		}
		if err := checkTemplate(src); err == nil {
			t.Errorf("checkTemplate should reject %q", src)
		}
	}
}

func TestDotNetRegex(t *testing.T) {
	tests := []struct {
		pattern, in, rep, want string
		ok                     bool
	}{
		{`\bS(20\d{2})\b`, "Show S2023 E01", "$1", "Show 2023 E01", true},
		{`(\w+)`, "ab", "$1x", "abx", true},
		{`[\p{IsCyrillic}]+`, "Матрица Matrix", "", " Matrix", true},
		{`\u00a0`, "a\u00a0b", " ", "a b", true},
		{"a\\ b", "a b", "-", "-", true},
		{`MULTI(?!FRENCH)`, "MULTI", "", "", false},
		{`(?<=x)y`, "xy", "", "", false},
	}
	for _, tt := range tests {
		re, err := compileDotNetRegex(tt.pattern)
		if (err == nil) != tt.ok {
			t.Errorf("%q: compile err=%v, want ok=%v", tt.pattern, err, tt.ok)
			continue
		}
		if err != nil {
			continue
		}
		if got := re.ReplaceAllString(tt.in, dotNetReplacement(tt.rep)); got != tt.want {
			t.Errorf("%q on %q: got %q, want %q", tt.pattern, tt.in, got, tt.want)
		}
	}
}

func TestFilters(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		filter FilterBlock
		want   string
	}{
		{"querystring", "/download.php?id=42&name=x", FilterBlock{"querystring", "id"}, "42"},
		{"querystring missing", "/download.php?name=x", FilterBlock{"querystring", "id"}, ""},
		{"regexp group", "Size: 1.5 GB", FilterBlock{"regexp", `([\d.]+ \w+)`}, "1.5 GB"},
		{"regexp no match", "nothing", FilterBlock{"regexp", `(\d+)`}, ""},
		{"re_replace", "a.b.c", FilterBlock{"re_replace", []any{`\.`, " "}}, "a b c"},
		{"re_replace unsupported leaves value", "MULTI", FilterBlock{"re_replace", []any{`MULTI(?!x)`, "y"}}, "MULTI"},
		{"replace", "Hello World", FilterBlock{"replace", []any{"World", "There"}}, "Hello There"},
		{"split", "/torrent/123/name/", FilterBlock{"split", []any{"/", 2}}, "123"},
		{"split negative", "a-b-c", FilterBlock{"split", []any{"-", -1}}, "c"},
		{"split string index", "a-b-c", FilterBlock{"split", []any{"-", "1"}}, "b"},
		{"trim", "  x  ", FilterBlock{"trim", nil}, "x"},
		{"trim cutset", "--x--", FilterBlock{"trim", "-"}, "x"},
		{"prepend", "b", FilterBlock{"prepend", "a"}, "ab"},
		{"append template", "id=", FilterBlock{"append", "{{ .Result._id }}"}, "id=77"},
		{"tolower", "ABC", FilterBlock{"tolower", nil}, "abc"},
		{"toupper", "abc", FilterBlock{"toupper", nil}, "ABC"},
		{"urldecode", "a%20b+c", FilterBlock{"urldecode", nil}, "a b c"},
		{"urlencode", "a b&c", FilterBlock{"urlencode", nil}, "a+b%26c"},
		{"htmldecode", "a &amp; b &#39;c&#39;", FilterBlock{"htmldecode", nil}, "a & b 'c'"},
		{"htmlencode", "<b>", FilterBlock{"htmlencode", nil}, "&lt;b&gt;"},
		{"validfilename", `a:b/c?`, FilterBlock{"validfilename", nil}, "a_b_c_"},
		{"diacritics", "Amélie Straße", FilterBlock{"diacritics", "replace"}, "Amelie Strasse"},
		{"jsonjoinarray", `{"genres":["Action","Drama"]}`, FilterBlock{"jsonjoinarray", []any{"$.genres", ", "}}, "Action, Drama"},
		{"validate", "Action, Comedy, Unknown", FilterBlock{"validate", "action, drama, comedy"}, "action,comedy"},
		{"dateparse dotnet", "2026-01-15 16:24:34 +0000", FilterBlock{"dateparse", "yyyy-MM-dd HH:mm:ss zzz"}, "Thu, 15 Jan 2026 16:24:34 +0000"},
		{"dateparse go layout", "15/01/2026", FilterBlock{"dateparse", "02/01/2006"}, time.Date(2026, 1, 15, 0, 0, 0, 0, time.Local).Format(time.RFC1123Z)},
		{"andmatch no-op", "x", FilterBlock{"andmatch", nil}, "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyFilters(tt.in, []FilterBlock{tt.filter}, sampleVars())
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilterErrors(t *testing.T) {
	for _, f := range []FilterBlock{
		{"nosuchfilter", nil}, {"split", []any{"/", 9}}, {"regexp", `(?<=a)b`}, {"dateparse", "yyyy-MM-dd"},
		{"diacritics", "keep"}, {"jsonjoinarray", []any{"$.x", ","}},
	} {
		if _, err := applyFilters("not-a-date/x", []FilterBlock{f}, nil); err == nil {
			t.Errorf("filter %s(%v): expected an error", f.Name, f.Args)
		}
	}
}

func TestDotNetLayoutToGo(t *testing.T) {
	tests := map[string]string{
		"yyyy-MM-dd HH:mm:ss zzz":   "2006-01-02 15:04:05 -07:00",
		"dd.MM.yyyy HH:mm":          "02.01.2006 15:04",
		"ddd, dd MMM yyyy HH:mm:ss": "Mon, 02 Jan 2006 15:04:05",
		"MMM d yyyy hh:mm tt":       "Jan 2 2006 03:04 PM",
		"htt MMM. d":                "3PM Jan. 2",
		"d MMMM yy":                 "2 January 06",
		"yyyy-MM-dd'T'HH:mm":        "2006-01-02T15:04",
	}
	for in, want := range tests {
		if got := dotNetLayoutToGo(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestParseDateLayout(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		value, layout string
		want          time.Time
	}{
		{"2026-01-15 16:24:34 +08:00", "yyyy-MM-dd HH:mm:ss zzz", time.Date(2026, 1, 15, 8, 24, 34, 0, time.UTC)},
		{"7am Sep. 14", "htt MMM. d", time.Date(2026, 9, 14, 7, 0, 0, 0, time.UTC)},
		{"Apr. 18 11", "MMM. d yy", time.Date(2011, 4, 18, 0, 0, 0, 0, time.UTC)},
		{"05/03/2025 10:30:00 +0000", "MM/dd/yyyy HH:mm:ss zzz", time.Date(2025, 5, 3, 10, 30, 0, 0, time.UTC)},
		{"Mar 4 2024  9:05 pm", "MMM d yyyy h:mm tt", time.Date(2024, 3, 4, 21, 5, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		got, err := parseDateLayout(tt.value, tt.layout, now)
		if err != nil {
			t.Errorf("%q with %q: %v", tt.value, tt.layout, err)
			continue
		}
		if !got.Equal(tt.want) {
			t.Errorf("%q with %q: got %v, want %v", tt.value, tt.layout, got.UTC(), tt.want)
		}
	}
}

func TestFromUnknownDate(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Time
	}{
		{"now", now},
		{"3 hours ago", now.Add(-3 * time.Hour)},
		{"1 day, 2 hours ago", now.Add(-26 * time.Hour)},
		{"an hour ago", now.Add(-time.Hour)},
		{"2 weeks ago", now.Add(-14 * 24 * time.Hour)},
		{"5 mins", now.Add(-5 * time.Minute)},
		{"Today 10:15", time.Date(2026, 9, 28, 10, 15, 0, 0, time.UTC)},
		{"Yesterday at 11:30 pm", time.Date(2026, 9, 27, 23, 30, 0, 0, time.UTC)},
		{"12:25am", time.Date(2026, 9, 28, 0, 25, 0, 0, time.UTC)},
		{"1700000000", time.Unix(1700000000, 0)},
		{"2024-03-01 08:00:00", time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC)},
		{"2024-03-01T08:00:00Z", time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC)},
		{"Mon, 02 Jan 2006 15:04:05 -0700", time.Date(2006, 1, 2, 22, 4, 5, 0, time.UTC)},
	}
	for _, tt := range tests {
		got, err := fromUnknownDate(tt.in, now)
		if err != nil {
			t.Errorf("%q: %v", tt.in, err)
			continue
		}
		if !got.Equal(tt.want) {
			t.Errorf("%q: got %v, want %v", tt.in, got.UTC(), tt.want)
		}
	}
	if _, err := fromUnknownDate("gibberish", now); err == nil {
		t.Error("expected an error for gibberish")
	}
}

func TestParseSizeAndCount(t *testing.T) {
	sizes := map[string]int64{
		"1.5 GB": 1610612736, "700 MB": 734003200, "700 MiB": 734003200, "1,234.5 MB": 1294467072,
		"1,5 GB": 1610612736, "512 KB": 524288, "2 TB": 2199023255552, "123456": 123456, "": 0, "n/a": 0,
	}
	for in, want := range sizes {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
	counts := map[string]int64{"1,234": 1234, "-": 0, " 17 ": 17, "5.0": 5, "": 0}
	for in, want := range counts {
		if got := parseCount(in); got != want {
			t.Errorf("parseCount(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestJSONQuery(t *testing.T) {
	doc, err := decodeJSON([]byte(`{"data":{"torrents":[
		{"id":1,"name":"A","free":true,"info":{"size":100},"files":[{"n":"x"}]},
		{"id":2,"name":"B","free":false,"info":{"size":200}},
		{"id":3,"name":"C"}]},"total":3}`))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		sel  string
		want string
	}{
		{"total", "3"},
		{"$.total", "3"},
		{"data.torrents[0].name", "A"},
		{"data.torrents[-1].id", "3"},
		{"data.torrents[0].files[0].n", "x"},
		{"data.torrents[1].free", "False"},
		{"data['torrents'][1]['info'].size", "200"},
	}
	for _, tt := range tests {
		v, ok, err := jsonQuery(doc, tt.sel)
		if err != nil || !ok {
			t.Errorf("%q: ok=%v err=%v", tt.sel, ok, err)
			continue
		}
		if got := jsonScalar(v); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.sel, got, tt.want)
		}
	}
	filters := map[string]int{
		"data.torrents":                       3,
		"data.torrents:has(free)":             2,
		"data.torrents:not(free)":             1,
		"data.torrents:has(free:contains(T))": 1,
		"data.torrents:not(name:contains(B))": 2,
	}
	for sel, want := range filters {
		v, ok, err := jsonQuery(doc, sel)
		if err != nil || !ok {
			t.Errorf("%q: ok=%v err=%v", sel, ok, err)
			continue
		}
		if got := len(v.([]any)); got != want {
			t.Errorf("%q: %d rows, want %d", sel, got, want)
		}
	}
	if _, ok, _ := jsonQuery(doc, "data.missing"); ok {
		t.Error("missing path should not be found")
	}
	if _, _, err := jsonQuery(doc, "data:bogus(x)"); err == nil {
		t.Error("unknown json filter should be an error")
	}
	if v, ok, _ := jsonQuery("hello", `:not(:contains("x"))`); !ok || jsonScalar(v) != "hello" {
		t.Error("condition on the value itself failed")
	}
	if !strings.Contains(jsonScalar(map[string]any{"a": 1}), `"a"`) {
		t.Error("objects should render as JSON")
	}
}
