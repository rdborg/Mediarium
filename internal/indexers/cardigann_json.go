package indexers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// JSON responses (`response: type: json`) are navigated with a small path
// language modelled on what definitions use:
//
//	data.torrents        nested keys
//	$ / $.numFound       the document root
//	$[0].id / files[2]   array indexes
//	['odd key']          quoted keys
//
// followed by optional filters, as on row and field selectors:
//
//	data:has(id)                         keep items that have an id
//	data:not(info.free:contains(0))      drop items whose info.free contains "0"
//	release:not(:contains("a"))          test the value itself

func decodeJSON(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// jsonSelect follows a plain path (no filters) from v.
func jsonSelect(v any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	cur := v
	for path != "" {
		switch {
		case path[0] == '.':
			path = path[1:]
		case path[0] == '[':
			end := strings.IndexByte(path, ']')
			if end < 0 {
				return nil, false
			}
			tok := strings.TrimSpace(path[1:end])
			path = path[end+1:]
			if len(tok) >= 2 && (tok[0] == '\'' || tok[0] == '"') {
				m, ok := cur.(map[string]any)
				if !ok {
					return nil, false
				}
				cur, ok = m[tok[1:len(tok)-1]]
				if !ok {
					return nil, false
				}
				continue
			}
			i, err := strconv.Atoi(tok)
			arr, ok := cur.([]any)
			if err != nil || !ok {
				return nil, false
			}
			if i < 0 {
				i += len(arr)
			}
			if i < 0 || i >= len(arr) {
				return nil, false
			}
			cur = arr[i]
		default:
			end := strings.IndexAny(path, ".[")
			key := path
			if end >= 0 {
				key, path = path[:end], path[end:]
			} else {
				path = ""
			}
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			cur, ok = m[key]
			if !ok {
				return nil, false
			}
		}
	}
	return cur, true
}

// jsonScalar renders a JSON value the way definitions expect to see it:
// strings as-is, numbers exactly, booleans as True/False, null as "",
// objects and arrays as compact JSON.
func jsonScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "True"
		}
		return "False"
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

type jsonCond struct {
	negate   bool
	path     string
	contains *string
}

// parseJSONSelector splits "path:has(x):not(y:contains(z))" into the path
// and its conditions.
func parseJSONSelector(sel string) (string, []jsonCond, error) {
	sel = strings.TrimSpace(sel)
	i := strings.IndexByte(sel, ':')
	if i < 0 {
		return sel, nil, nil
	}
	path, rest := sel[:i], sel[i:]
	conds, err := parseJSONConds(rest)
	return path, conds, err
}

func parseJSONConds(rest string) ([]jsonCond, error) {
	var conds []jsonCond
	for rest != "" {
		name, arg, tail, err := nextPseudo(rest)
		if err != nil {
			return nil, err
		}
		rest = tail
		switch name {
		case "has", "not":
			inner := strings.TrimSpace(arg)
			c := jsonCond{negate: name == "not"}
			if j := strings.Index(inner, ":contains("); j >= 0 {
				c.path = inner[:j]
				n, a, t, err := nextPseudo(inner[j:])
				if err != nil || n != "contains" || strings.TrimSpace(t) != "" {
					return nil, fmt.Errorf("unsupported json condition %q", inner)
				}
				a = unquote(a)
				c.contains = &a
			} else {
				c.path = inner
			}
			conds = append(conds, c)
		case "contains":
			a := unquote(arg)
			conds = append(conds, jsonCond{contains: &a})
		default:
			return nil, fmt.Errorf("unsupported json selector filter :%s", name)
		}
	}
	return conds, nil
}

// nextPseudo reads ":name(arg)" from the start of s.
func nextPseudo(s string) (name, arg, rest string, err error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, ":") {
		return "", "", "", fmt.Errorf("unexpected %q in json selector", s)
	}
	open := strings.IndexByte(s, '(')
	if open < 0 {
		return "", "", "", fmt.Errorf("missing '(' in json selector %q", s)
	}
	name = s[1:open]
	depth := 0
	inQuote := byte(0)
	for j := open; j < len(s); j++ {
		c := s[j]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return name, s[open+1 : j], s[j+1:], nil
			}
		}
	}
	return "", "", "", fmt.Errorf("missing ')' in json selector %q", s)
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func (c jsonCond) holds(v any) bool {
	target := v
	found := true
	if strings.TrimSpace(c.path) != "" {
		target, found = jsonSelect(v, c.path)
		if found && target == nil {
			found = false
		}
	}
	ok := found
	if ok && c.contains != nil {
		ok = strings.Contains(jsonScalar(target), *c.contains)
	}
	if c.negate {
		return !ok
	}
	return ok
}

// jsonQuery selects path from v and applies the conditions to the result:
// for an array every item is filtered, for a single value the whole
// selection fails if a condition does not hold.
func jsonQuery(v any, sel string) (any, bool, error) {
	path, conds, err := parseJSONSelector(sel)
	if err != nil {
		return nil, false, err
	}
	got, ok := jsonSelect(v, path)
	if !ok {
		return nil, false, nil
	}
	if len(conds) == 0 {
		return got, true, nil
	}
	if arr, isArr := got.([]any); isArr && strings.TrimSpace(path) != "" {
		var kept []any
		for _, item := range arr {
			if allHold(conds, item) {
				kept = append(kept, item)
			}
		}
		return kept, true, nil
	}
	if !allHold(conds, got) {
		return nil, false, nil
	}
	return got, true, nil
}

func allHold(conds []jsonCond, v any) bool {
	for _, c := range conds {
		if !c.holds(v) {
			return false
		}
	}
	return true
}
