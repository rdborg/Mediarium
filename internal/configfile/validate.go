package configfile

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Checks that need nothing from the server: is a value the kind of value the
// setting holds, and is an address an address. Ranges and the rest are the
// server's own rules, which run when the file is applied (and the apply is
// undone if the server refuses something).

func isNumber(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	}
	return false
}

// kindWord says what a value should be, in a few words.
func kindWord(have any) string {
	switch have.(type) {
	case bool:
		return "true or false"
	case string:
		return "text (in quotes when it looks like a number)"
	case []any:
		return "a list"
	case map[string]any:
		return "a group of name: value lines"
	}
	return "a number"
}

// valueProblem is "" when want can stand where have is, else a sentence.
// have is what Mediarium shows now; a value Mediarium shows as empty (null)
// can't tell what kind it is, so anything goes.
func valueProblem(label, key string, have, want any) string {
	if want == nil {
		return ""
	}
	if isURLKey(key) {
		if s, ok := want.(string); ok {
			if p := urlProblem(label, s); p != "" {
				return p
			}
		}
	}
	if have == nil {
		return ""
	}
	// An indexer's priority may be a number or one of a few words.
	if key == "priority" {
		return ""
	}
	ok := true
	switch have.(type) {
	case bool:
		_, ok = want.(bool)
	case float64:
		ok = isNumber(want)
	case string:
		_, ok = want.(string)
	case []any:
		_, ok = want.([]any)
	case map[string]any:
		_, ok = want.(map[string]any)
	}
	if !ok {
		return fmt.Sprintf("%s should be %s.", label, kindWord(have))
	}
	return ""
}

// isURLKey is true for the names that hold a web address.
func isURLKey(key string) bool {
	return key == "url" || key == "baseUrl" || key == "publicUrl"
}

// urlProblem checks a web address: http or https, with a host.
func urlProblem(label, s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Sprintf("%s (%s) isn't a web address. It should start with http:// or https://.", label, s)
	}
	return ""
}

// valueProblems checks the values of one list item. have is the item as
// Mediarium has it, nil when the file adds it.
func valueProblems(label string, have, item map[string]any, listName string) []string {
	var out []string
	keys := make([]string, 0, len(item))
	for k := range item {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "name" {
			continue
		}
		var hv any
		if have != nil {
			hv = have[k]
		}
		// A notification's settings are all text, whatever a file writes.
		if k == "config" && listName == "notifications" {
			continue
		}
		if p := valueProblem(label+"."+k, k, hv, item[k]); p != "" {
			out = append(out, p)
		}
	}
	return out
}
