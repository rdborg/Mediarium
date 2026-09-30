package indexers

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// FilterBlock is one entry of a definition's `filters:` list.
type FilterBlock struct {
	Name string `yaml:"name"`
	Args any    `yaml:"args"`
}

// knownFilters lists every filter the engine implements; validation rejects
// anything else up front.
var knownFilters = map[string]bool{
	"querystring": true, "regexp": true, "re_replace": true, "replace": true, "split": true,
	"trim": true, "prepend": true, "append": true, "tolower": true, "toupper": true,
	"dateparse": true, "timeparse": true, "timeago": true, "reltime": true, "fuzzytime": true,
	"urldecode": true, "urlencode": true, "htmldecode": true, "htmlencode": true,
	"validfilename": true, "diacritics": true, "jsonjoinarray": true, "validate": true,
	"andmatch": true, "strdump": true, "hexdump": true,
}

func filterArgs(a any) []string {
	switch v := a.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, len(v))
		for i, x := range v {
			out[i] = scalarString(x)
		}
		return out
	case []string:
		return v
	default:
		return []string{scalarString(v)}
	}
}

func scalarString(x any) string {
	switch v := x.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

// applyFilters runs a filter chain over value. vars may be nil when no
// template variables are available.
func applyFilters(value string, filters []FilterBlock, vars *tmplVars) (string, error) {
	for _, f := range filters {
		out, err := applyFilter(value, f, vars)
		if err != nil {
			return "", fmt.Errorf("filter %s: %w", f.Name, err)
		}
		if len(out) > maxFilterBytes {
			return "", fmt.Errorf("filter %s: the text became too large", f.Name)
		}
		value = out
	}
	return value, nil
}

func tmplArg(s string, vars *tmplVars) (string, error) {
	if vars == nil || !strings.Contains(s, "{{") {
		return s, nil
	}
	return applyTemplate(s, vars, nil)
}

func applyFilter(data string, f FilterBlock, vars *tmplVars) (string, error) {
	args := filterArgs(f.Args)
	switch f.Name {
	case "querystring":
		name := argAt(args, 0)
		qs := data
		if i := strings.IndexByte(qs, '?'); i >= 0 {
			qs = qs[i+1:]
		}
		if i := strings.IndexByte(qs, '#'); i >= 0 {
			qs = qs[:i]
		}
		v, err := url.ParseQuery(qs)
		if err != nil {
			// ParseQuery keeps what it could parse
			if v == nil {
				return "", nil
			}
		}
		return v.Get(name), nil
	case "regexp":
		re, err := compileDotNetRegex(argAt(args, 0))
		if err != nil {
			return "", err
		}
		m := re.FindStringSubmatch(data)
		if len(m) < 2 {
			return "", nil
		}
		return m[1], nil
	case "re_replace":
		re, err := compileDotNetRegex(argAt(args, 0))
		if err != nil {
			warnSkippedRegex(err)
			return data, nil
		}
		rep, err := tmplArg(argAt(args, 1), vars)
		if err != nil {
			return "", err
		}
		return regexReplace(re, data, rep)
	case "replace":
		rep, err := tmplArg(argAt(args, 1), vars)
		if err != nil {
			return "", err
		}
		from := argAt(args, 0)
		if from == "" {
			return data, nil
		}
		if len(rep) > len(from) && len(data)+strings.Count(data, from)*(len(rep)-len(from)) > maxFilterBytes {
			return "", fmt.Errorf("the text would become too large")
		}
		return strings.ReplaceAll(data, from, rep), nil
	case "split":
		sep := argAt(args, 0)
		if sep == "" {
			return "", fmt.Errorf("missing separator")
		}
		pos, err := strconv.Atoi(strings.TrimSpace(argAt(args, 1)))
		if err != nil {
			return "", fmt.Errorf("bad index %q", argAt(args, 1))
		}
		parts := strings.Split(data, firstRune(sep))
		if pos < 0 {
			pos += len(parts)
		}
		if pos < 0 || pos >= len(parts) {
			return "", fmt.Errorf("index %s out of range for %q", argAt(args, 1), data)
		}
		return parts[pos], nil
	case "trim":
		if cut := argAt(args, 0); cut != "" {
			return strings.Trim(data, firstRune(cut)), nil
		}
		return strings.TrimSpace(data), nil
	case "prepend":
		s, err := tmplArg(argAt(args, 0), vars)
		return s + data, err
	case "append":
		s, err := tmplArg(argAt(args, 0), vars)
		return data + s, err
	case "tolower":
		return strings.ToLower(data), nil
	case "toupper":
		return strings.ToUpper(data), nil
	case "urldecode":
		if s, err := url.QueryUnescape(data); err == nil {
			return s, nil
		}
		return data, nil
	case "urlencode":
		return url.QueryEscape(data), nil
	case "htmldecode":
		return html.UnescapeString(data), nil
	case "htmlencode":
		return html.EscapeString(data), nil
	case "dateparse", "timeparse":
		t, err := parseDateLayout(strings.TrimSpace(data), argAt(args, 0), time.Now())
		if err != nil {
			return "", err
		}
		return t.Format(time.RFC1123Z), nil
	case "timeago", "reltime":
		t, err := fromTimeAgo(data, time.Now())
		if err != nil {
			return "", err
		}
		return t.Format(time.RFC1123Z), nil
	case "fuzzytime":
		t, err := fromUnknownDate(data, time.Now())
		if err != nil {
			return "", err
		}
		return t.Format(time.RFC1123Z), nil
	case "validfilename":
		return validFilename(data), nil
	case "diacritics":
		if argAt(args, 0) != "replace" {
			return "", fmt.Errorf("unsupported diacritics mode %q", argAt(args, 0))
		}
		return removeDiacritics(data), nil
	case "jsonjoinarray":
		return jsonJoinArray(data, argAt(args, 0), argAt(args, 1))
	case "validate":
		return validateWords(data, argAt(args, 0)), nil
	case "andmatch", "strdump", "hexdump":
		return data, nil // row-level or debugging filters: no effect on a value
	}
	return "", fmt.Errorf("unsupported filter %q", f.Name)
}

// maxFilterBytes is the most text a filter may produce. Pages are read up to
// 16 MB, so this leaves room for a replacement that lengthens them, and stops
// a chain of replacements from growing a value without end.
const maxFilterBytes = 96 << 20

// regexReplace replaces every match of re in data, refusing a replacement that
// could make the text larger than maxFilterBytes. Each match can add at most
// the replacement's own length, plus the text of the whole match for each group
// reference in it.
func regexReplace(re *regexp.Regexp, data, replacement string) (string, error) {
	rep := dotNetReplacement(replacement)
	if (len(data)+1)*(len(rep)+1)+len(data) > maxFilterBytes {
		return "", fmt.Errorf("the replacement could make the text too large")
	}
	return re.ReplaceAllString(data, rep), nil
}

// firstRune is the first character of s (all of it, when it is one character).
func firstRune(s string) string {
	_, size := utf8.DecodeRuneInString(s)
	return s[:size]
}

var invalidFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func validFilename(s string) string {
	return invalidFilenameChars.ReplaceAllString(s, "_")
}

var diacriticMap = func() map[rune]string {
	m := map[rune]string{}
	add := func(to string, from string) {
		for _, r := range from {
			m[r] = to
		}
	}
	add("a", "àáâãäåāăą")
	add("A", "ÀÁÂÃÄÅĀĂĄ")
	add("c", "çćĉċč")
	add("C", "ÇĆĈĊČ")
	add("d", "ďđ")
	add("D", "ĎĐ")
	add("e", "èéêëēĕėęě")
	add("E", "ÈÉÊËĒĔĖĘĚ")
	add("g", "ĝğġģ")
	add("G", "ĜĞĠĢ")
	add("h", "ĥħ")
	add("i", "ìíîïĩīĭįı")
	add("I", "ÌÍÎÏĨĪĬĮİ")
	add("j", "ĵ")
	add("k", "ķ")
	add("l", "ĺļľŀł")
	add("L", "ĹĻĽĿŁ")
	add("n", "ñńņňŉ")
	add("N", "ÑŃŅŇ")
	add("o", "òóôõöøōŏő")
	add("O", "ÒÓÔÕÖØŌŎŐ")
	add("r", "ŕŗř")
	add("R", "ŔŖŘ")
	add("s", "śŝşšș")
	add("S", "ŚŜŞŠȘ")
	add("t", "ţťŧț")
	add("T", "ŢŤŦȚ")
	add("u", "ùúûüũūŭůűų")
	add("U", "ÙÚÛÜŨŪŬŮŰŲ")
	add("y", "ýÿŷ")
	add("Y", "ÝŸŶ")
	add("z", "źżž")
	add("Z", "ŹŻŽ")
	add("ss", "ß")
	add("ae", "æ")
	add("AE", "Æ")
	add("oe", "œ")
	add("OE", "Œ")
	return m
}()

func removeDiacritics(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if rep, ok := diacriticMap[r]; ok {
			sb.WriteString(rep)
			continue
		}
		if unicode.Is(unicode.Mn, r) { // stray combining marks
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func jsonJoinArray(data, path, sep string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return "", fmt.Errorf("value is not JSON: %w", err)
	}
	sel, ok := jsonSelect(v, path)
	if !ok {
		return "", fmt.Errorf("json path %q not found", path)
	}
	arr, ok := sel.([]any)
	if !ok {
		return jsonScalar(sel), nil
	}
	parts := make([]string, 0, len(arr))
	for _, x := range arr {
		parts = append(parts, jsonScalar(x))
	}
	return strings.Join(parts, sep), nil
}

var validateDelims = func(r rune) bool { return strings.ContainsRune(", /)(.;[]\"|:", r) }

func validateWords(data, allowed string) string {
	valid := strings.FieldsFunc(strings.ToLower(allowed), validateDelims)
	have := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(data), validateDelims) {
		have[w] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, w := range valid {
		if have[w] && !seen[w] {
			out = append(out, w)
			seen[w] = true
		}
	}
	return strings.Join(out, ",")
}

// --- numbers and sizes ---

// parseSize turns "1.4 GB", "700 MiB", "1,234.5 MB" or a plain byte count
// into bytes (binary multiples, as Cardigann does).
func parseSize(s string) int64 {
	lower := strings.ToLower(s)
	var num strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) || r == '.' || r == ',' {
			num.WriteRune(r)
		}
	}
	v := strings.ReplaceAll(num.String(), ",", ".")
	if v == "" {
		return 0
	}
	if strings.Count(v, ".") > 1 {
		last := strings.LastIndexByte(v, '.')
		v = strings.ReplaceAll(v[:last], ".", "") + v[last:]
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	mult := 1.0
	switch {
	case strings.Contains(lower, "kb") || strings.Contains(lower, "kib"):
		mult = 1 << 10
	case strings.Contains(lower, "mb") || strings.Contains(lower, "mib"):
		mult = 1 << 20
	case strings.Contains(lower, "gb") || strings.Contains(lower, "gib"):
		mult = 1 << 30
	case strings.Contains(lower, "tb") || strings.Contains(lower, "tib"):
		mult = 1 << 40
	case strings.Contains(lower, "pb") || strings.Contains(lower, "pib"):
		mult = 1 << 50
	}
	return clampInt64(f * mult)
}

// clampInt64 converts a float to int64, staying inside the range: a plain
// conversion of a value that is too large gives an unspecified number.
func clampInt64(f float64) int64 {
	switch {
	case f != f: // NaN
		return 0
	case f >= math.MaxInt64:
		return math.MaxInt64
	case f <= math.MinInt64:
		return math.MinInt64
	}
	return int64(f)
}

var firstNumberRe = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// parseCount reads a seeders/leechers/grabs style number: "1,234" -> 1234,
// "-" or garbage -> 0.
func parseCount(s string) int64 {
	s = strings.NewReplacer(",", "", " ", "", " ", "").Replace(s)
	m := firstNumberRe.FindString(s)
	if m == "" {
		return 0
	}
	f, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0
	}
	return clampInt64(f)
}
