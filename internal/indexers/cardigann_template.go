package indexers

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
)

// Cardigann definitions embed a small Go-template-like language
// ({{ .Keywords }}, {{ if .Config.x }}...{{ else }}...{{ end }}, {{ range }},
// {{ join }}, {{ re_replace }}...). Go's text/template runs nearly all of it;
// rewriteTemplate papers over the differences:
//
//   - string literals in Cardigann are raw ("\d" means backslash-d), so every
//     "..." inside an action becomes an equivalent Go literal;
//   - names Go can't lex (.Config.2facode, .Config.sort-by) become index calls;
//   - stray unbalanced parentheses, which Cardigann's own engine ignores, are
//     dropped;
//   - every printing action is piped through a modifier, so values placed in
//     a URL path can be URL-encoded (Cardigann encodes variables there).

// tmplVars is the variable set a template sees.
type tmplVars struct {
	Config      map[string]string
	Query       map[string]string
	Result      map[string]string
	Keywords    string
	Categories  []string
	DownloadURI map[string]any
	now         time.Time
}

func newTmplVars() *tmplVars {
	return &tmplVars{
		Config: map[string]string{},
		Query:  map[string]string{},
		Result: map[string]string{},
	}
}

func (v *tmplVars) data() map[string]any {
	now := v.now
	if now.IsZero() {
		now = time.Now()
	}
	d := map[string]any{
		"Config":     v.Config,
		"Query":      v.Query,
		"Result":     v.Result,
		"Keywords":   v.Keywords,
		"Categories": v.Categories,
		"Today":      map[string]string{"Year": strconv.Itoa(now.Year())},
		"True":       checkboxTrue,
		"False":      "",
	}
	if v.DownloadURI != nil {
		d["DownloadUri"] = v.DownloadURI
	} else {
		d["DownloadUri"] = map[string]any{"Query": map[string]string{}}
	}
	return d
}

// checkboxTrue is what a ticked checkbox setting reads as, and what .True
// is; an unticked one is "", like .False, so {{ if .Config.x }} and
// {{ eq .Config.x .False }} both behave as definitions expect.
const checkboxTrue = ".True"

var (
	tmplCacheMu sync.Mutex
	tmplCache   = map[string]*template.Template{}
)

var tmplFuncs = template.FuncMap{
	"join":       tmplJoin,
	"re_replace": tmplReReplace,
	"__mod":      func(s any) any { return s }, // replaced per execution
}

// applyTemplate renders a Cardigann template string. encode, if set, is
// applied to every printed value (used for URL paths).
func applyTemplate(src string, vars *tmplVars, encode func(string) string) (string, error) {
	if !strings.Contains(src, "{{") {
		return src, nil
	}
	rewritten, err := rewriteTemplate(src)
	if err != nil {
		return "", fmt.Errorf("template %q: %w", src, err)
	}
	t, err := compileTemplate(rewritten)
	if err != nil {
		return "", fmt.Errorf("template %q: %w", src, err)
	}
	mod := func(s any) any { return s }
	if encode != nil {
		mod = func(s any) any { return encode(fmt.Sprint(s)) }
	}
	t, err = t.Clone()
	if err != nil {
		return "", fmt.Errorf("template %q: %w", src, err)
	}
	t.Funcs(template.FuncMap{"__mod": mod})
	w := &limitedWriter{max: maxTemplateOutput}
	if err := t.Execute(w, vars.data()); err != nil {
		return "", fmt.Errorf("template %q: %w", src, err)
	}
	return strings.ReplaceAll(w.sb.String(), "<no value>", ""), nil
}

// checkTemplate reports whether src can be compiled, without running it.
func checkTemplate(src string) error {
	if !strings.Contains(src, "{{") {
		return nil
	}
	rewritten, err := rewriteTemplate(src)
	if err != nil {
		return err
	}
	_, err = compileTemplate(rewritten)
	return err
}

func compileTemplate(src string) (*template.Template, error) {
	tmplCacheMu.Lock()
	t, ok := tmplCache[src]
	tmplCacheMu.Unlock()
	if ok {
		return t, nil
	}
	t, err := template.New("cardigann").Funcs(tmplFuncs).Option("missingkey=zero").Parse(src)
	if err != nil {
		return nil, err
	}
	tmplCacheMu.Lock()
	if len(tmplCache) > 4096 {
		tmplCache = map[string]*template.Template{}
	}
	tmplCache[src] = t
	tmplCacheMu.Unlock()
	return t, nil
}

var controlWords = map[string]bool{
	"if": true, "else": true, "end": true, "range": true, "with": true, "define": true,
	"template": true, "block": true, "break": true, "continue": true,
}

var badFieldRe = regexp.MustCompile(`\.(Config|Result|Query)\.([A-Za-z0-9_\-]+)`)
var goIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// rewriteTemplate converts Cardigann template syntax into Go template syntax.
func rewriteTemplate(src string) (string, error) {
	var out strings.Builder
	rest := src
	var blocks []string // kinds of the blocks that are open: "if", "with", "range"
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			out.WriteString(rest)
			return out.String(), nil
		}
		out.WriteString(rest[:i])
		rest = rest[i+2:]
		// find the closing }} outside string literals
		end := -1
		inStr := false
		for j := 0; j < len(rest)-1; j++ {
			if rest[j] == '"' {
				inStr = !inStr
				continue
			}
			if !inStr && rest[j] == '}' && rest[j+1] == '}' {
				end = j
				break
			}
		}
		if end < 0 {
			return "", fmt.Errorf("unterminated {{ action")
		}
		action := rest[:end]
		rest = rest[end+2:]
		text, keyword, err := rewriteAction(action)
		if err != nil {
			return "", err
		}
		switch keyword {
		case "if", "with", "range":
			blocks = append(blocks, keyword)
			if n := countKind(blocks, "range"); n > maxRangeNesting {
				return "", fmt.Errorf("loops are nested more than %d deep", maxRangeNesting)
			}
		case "end":
			if len(blocks) > 0 {
				blocks = blocks[:len(blocks)-1]
			}
		}
		out.WriteString("{{")
		out.WriteString(text)
		out.WriteString("}}")
	}
}

// maxRangeNesting is how many loops a template may nest. Definitions loop over
// the categories being searched and nothing else; the limit keeps a crafted
// template from running for ever.
const maxRangeNesting = 2

// maxTemplateOutput is the most text one template may produce.
const maxTemplateOutput = 1 << 20

func countKind(list []string, kind string) int {
	n := 0
	for _, k := range list {
		if k == kind {
			n++
		}
	}
	return n
}

// limitedWriter stops a template that produces too much text.
type limitedWriter struct {
	sb  strings.Builder
	max int
}

var errTemplateTooLong = errors.New("the template produces too much text")

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.sb.Len()+len(p) > w.max {
		return 0, errTemplateTooLong
	}
	return w.sb.Write(p)
}

// rangeOverNumber reports whether the code of a range action holds a number or
// character literal: Go templates loop that many times ("range 1000000000").
func rangeOverNumber(code string) bool {
	inStr := false
	prev := byte(' ')
	for i := 0; i < len(code); i++ {
		c := code[i]
		if inStr {
			switch c {
			case '\\':
				i++
			case '"':
				inStr = false
			}
			prev = c
			continue
		}
		switch {
		case c == '"':
			inStr = true
		case c == '\'' || c == '`':
			return true
		case c >= '0' && c <= '9':
			isName := prev == '.' || prev == '$' || prev == '_' || (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9')
			if !isName {
				return true
			}
		}
		prev = c
	}
	return false
}

func rewriteAction(action string) (text, keyword string, err error) {
	leftTrim, rightTrim := "", ""
	body := action
	if strings.HasPrefix(body, "-") && len(body) > 1 && (body[1] == ' ' || body[1] == '\t' || body[1] == '\n') {
		leftTrim = "- "
		body = body[2:]
	}
	if strings.HasSuffix(body, "-") && len(body) > 1 {
		if c := body[len(body)-2]; c == ' ' || c == '\t' || c == '\n' {
			rightTrim = " -"
			body = body[:len(body)-2]
		}
	}

	// Split into string literals and code, rewriting only code.
	var sb strings.Builder
	depth := 0
	for len(body) > 0 {
		q := strings.IndexByte(body, '"')
		code := body
		if q >= 0 {
			code = body[:q]
		}
		code = badFieldRe.ReplaceAllStringFunc(code, func(m string) string {
			parts := badFieldRe.FindStringSubmatch(m)
			if goIdentRe.MatchString(parts[2]) {
				return m
			}
			return fmt.Sprintf("(index .%s %s)", parts[1], strconv.Quote(parts[2]))
		})
		// drop unbalanced closing parentheses
		for _, r := range code {
			switch r {
			case '(':
				depth++
			case ')':
				if depth == 0 {
					continue
				}
				depth--
			}
			sb.WriteRune(r)
		}
		if q < 0 {
			break
		}
		body = body[q+1:]
		e := strings.IndexByte(body, '"')
		if e < 0 { // unterminated literal: keep as is
			sb.WriteString(strconv.Quote(body))
			body = ""
			break
		}
		sb.WriteString(strconv.Quote(body[:e]))
		body = body[e+1:]
	}
	code := sb.String() + strings.Repeat(")", depth)

	trimmed := strings.TrimSpace(code)
	first := trimmed
	// the first word ends at anything that is not part of a name, so
	// "range.Categories" is the word "range" followed by a field
	if k := strings.IndexFunc(first, func(r rune) bool {
		return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	}); k >= 0 {
		first = first[:k]
	}
	switch first {
	case "define", "template", "block":
		return "", "", fmt.Errorf("{{ %s }} is not supported", first)
	case "range":
		if rangeOverNumber(strings.TrimPrefix(trimmed, "range")) {
			return "", "", fmt.Errorf("{{ range }} can only loop over a list, not a number")
		}
	}
	if controlWords[first] {
		keyword = first
	}
	isOutput := trimmed != "" && !controlWords[first] && !strings.HasPrefix(trimmed, "/*") && !declRe.MatchString(trimmed)
	if isOutput {
		code = " " + trimmed + " | __mod "
	}
	return leftTrim + code + rightTrim, keyword, nil
}

var declRe = regexp.MustCompile(`^\$[A-Za-z0-9_]*\s*(:=|=)`)

func tmplJoin(list any, sep string) string {
	switch l := list.(type) {
	case []string:
		return strings.Join(l, sep)
	case []any:
		parts := make([]string, len(l))
		for i, x := range l {
			parts[i] = fmt.Sprint(x)
		}
		return strings.Join(parts, sep)
	case string:
		return l
	case nil:
		return ""
	}
	return fmt.Sprint(list)
}

func tmplReReplace(s any, pattern, replacement string) (string, error) {
	re, err := compileDotNetRegex(pattern)
	if err != nil {
		warnSkippedRegex(err)
		return fmt.Sprint(s), nil
	}
	return regexReplace(re, fmt.Sprint(s), replacement)
}

var (
	skippedRegexMu sync.Mutex
	skippedRegex   = map[string]bool{}
)

// warnSkippedRegex logs (once per pattern) that a text replacement was
// skipped because Go's regular expressions can't express it; the value is
// left unchanged rather than failing the whole result.
func warnSkippedRegex(err error) {
	skippedRegexMu.Lock()
	defer skippedRegexMu.Unlock()
	if skippedRegex[err.Error()] || len(skippedRegex) > 256 {
		return
	}
	skippedRegex[err.Error()] = true
	log.Printf("cardigann: skipping a text replacement: %v", err)
}

// unicodeBlocks maps .NET named blocks to the closest Go script.
var unicodeBlocks = map[string]string{
	"IsCyrillic": "Cyrillic", "IsCyrillicSupplement": "Cyrillic", "IsGreek": "Greek",
	"IsGreekandCoptic": "Greek", "IsArabic": "Arabic", "IsHebrew": "Hebrew", "IsThai": "Thai",
	"IsCJKUnifiedIdeographs": "Han", "IsHiragana": "Hiragana", "IsKatakana": "Katakana",
	"IsHangulSyllables": "Hangul", "IsArmenian": "Armenian", "IsGeorgian": "Georgian",
	"IsDevanagari": "Devanagari",
}

// translateDotNetRegex rewrites the .NET-only escapes definitions use into
// RE2 syntax: a \u followed by four hex digits, and \p{IsBlock} classes.
func translateDotNetRegex(p string) string {
	if !strings.Contains(p, `\`) {
		return p
	}
	var sb strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c != '\\' || i+1 >= len(p) {
			sb.WriteByte(c)
			continue
		}
		n := p[i+1]
		switch {
		case n >= 0x80:
			// .NET lets any character be escaped; RE2 only punctuation.
			continue
		case n == 'u' && i+5 < len(p) && isHex(p[i+2:i+6]):
			sb.WriteString(`\x{` + p[i+2:i+6] + `}`)
			i += 5
		case (n == 'p' || n == 'P') && i+2 < len(p) && p[i+2] == '{':
			if end := strings.IndexByte(p[i:], '}'); end > 0 {
				if script, ok := unicodeBlocks[p[i+3:i+end]]; ok {
					sb.WriteString(`\` + string(n) + `{` + script + `}`)
					i += end
					continue
				}
			}
			sb.WriteString(p[i : i+2])
			i++
		default:
			sb.WriteString(p[i : i+2])
			i++
		}
	}
	return sb.String()
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// --- regular expressions ---

var (
	regexCacheMu sync.Mutex
	regexCache   = map[string]*regexp.Regexp{}
)

// maxRegexSteps is the largest compiled pattern accepted. Real definitions use
// patterns of a few hundred steps.
const maxRegexSteps = 10000

// compileDotNetRegex compiles a regular expression written for .NET (the
// dialect Cardigann definitions target) with Go's RE2 engine. Constructs RE2
// lacks (look-around, back-references, atomic groups) fail with a clear
// error.
func compileDotNetRegex(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.Lock()
	re, ok := regexCache[pattern]
	regexCacheMu.Unlock()
	if ok {
		return re, nil
	}
	for _, bad := range []struct{ tok, what string }{
		{"(?=", "look-ahead"}, {"(?!", "negative look-ahead"}, {"(?<=", "look-behind"},
		{"(?<!", "negative look-behind"}, {"(?>", "atomic group"},
	} {
		if strings.Contains(pattern, bad.tok) {
			return nil, fmt.Errorf("regular expression %q uses a %s, which is not supported", pattern, bad.what)
		}
	}
	translated := translateDotNetRegex(pattern)
	re, err := regexp.Compile(translated)
	if err != nil {
		return nil, fmt.Errorf("regular expression %q is not supported: %w", pattern, err)
	}
	// Matching takes time in proportion to the pattern's size times the text's:
	// a pattern of a million steps run over a page of 16 MB never finishes.
	if parsed, perr := syntax.Parse(translated, syntax.Perl); perr == nil {
		if prog, cerr := syntax.Compile(parsed.Simplify()); cerr == nil && len(prog.Inst) > maxRegexSteps {
			return nil, fmt.Errorf("regular expression %q is too complex", shortenText(pattern, 60))
		}
	}
	regexCacheMu.Lock()
	if len(regexCache) > 4096 {
		regexCache = map[string]*regexp.Regexp{}
	}
	regexCache[pattern] = re
	regexCacheMu.Unlock()
	return re, nil
}

var dotNetGroupRef = regexp.MustCompile(`\$(\d+)`)

// dotNetReplacement turns .NET's $1 into Go's ${1} so "$1a" keeps meaning
// group 1 followed by "a".
func dotNetReplacement(s string) string {
	return dotNetGroupRef.ReplaceAllString(s, "$${$1}")
}

// urlPathEscape encodes a value placed into a URL path the way Cardigann
// does (form encoding: spaces become '+').
func urlPathEscape(s string) string { return url.QueryEscape(s) }
