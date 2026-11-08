// Command msgextract lists the sentences the server can send to the screen
// (error messages, validation messages, health and problem notes, entries of
// the activity feed), so the web app can translate them.
//
//	go run ./tools/msgextract          rewrite web/src/serverText.json
//	go run ./tools/msgextract -check   exit 1 if the file on disk is out of date (used by CI)
//
// It reads the Go code of the packages that talk to the screen and keeps the
// string literals that read like a sentence. A format verb (%s, %d, %v ...)
// becomes {0}, {1} ... in the order it appears, which is also how the
// translations name them. A message built by joining pieces with + is not
// found: write it as one format string to have it translated.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// dirs are the packages whose sentences reach the screen.
var dirs = []string{"internal/api", "internal/indexers", "internal/problems", "internal/health", "internal/plainerror", "internal/inputcheck", "internal/vpn", "internal/library", "internal/store", "internal/ytdlp"}

// out is the file the list is written to.
const out = "web/src/serverText.json"

// messageCalls are the functions whose message argument is always shown, with
// the position of that argument: whatever it holds is kept even when it does
// not end like a sentence.
var messageCalls = map[string]int{
	"writeError": 2, "LogActivity": 2, "LogSeriesActivity": 2, "LogItemActivity": 3, "renamedLine": 0,
}

// healthCalls are the functions that add a line to the Health list on the
// dashboard: the title, the note and the button label are all shown.
var healthCalls = map[string][]int{"addHealth": {2, 3, 4}}

// shownFields are the types written out as positional literals in a list
// whose fields are shown, with the positions of those fields: the problem
// catalog's heading, explanation, what to try and the button label.
var shownFields = map[string][]int{"Help": {3, 4, 5, 6}}

var (
	verb     = regexp.MustCompile(`%[-+# 0]*(\d+|\*)?(\.(\d+|\*))?[a-zA-Z]`)
	indexed  = regexp.MustCompile(`%\[\d+\]`)
	sqlWords = regexp.MustCompile(`(?i)\b(SELECT|INSERT INTO|UPDATE \w+ SET|DELETE FROM|CREATE TABLE|ALTER TABLE|PRAGMA)\b`)
	// A piece of a query added to another one (" AND s.id = ?").
	sqlClause = regexp.MustCompile(`^(AND|OR|WHERE|FROM|JOIN|LEFT JOIN|ORDER BY|GROUP BY|LIMIT|SET) `)
)

func main() {
	check := flag.Bool("check", false, "fail if the file on disk differs")
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	found := map[string]bool{}
	for _, d := range dirs {
		entries, err := os.ReadDir(filepath.Join(*root, d))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			// The MCP server (mcp*.go in internal/api) talks to AI assistants, not to
			// the screen: its sentences are not translated.
			// The same for the SABnzbd and qBittorrent look-alike addresses (compat_*.go).
			if d == "internal/api" && (strings.HasPrefix(name, "mcp") || strings.HasPrefix(name, "compat_")) {
				continue
			}
			scan(filepath.Join(*root, d, name), found)
		}
	}
	list := make([]string, 0, len(found))
	for s := range found {
		list = append(list, s)
	}
	sort.Strings(list)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(list); err != nil {
		fatal(err)
	}
	path := filepath.Join(*root, out)
	if *check {
		have, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(bytes.ReplaceAll(have, []byte("\r\n"), []byte("\n")), buf.Bytes()) {
			fmt.Fprintf(os.Stderr, "%s is out of date: run `go run ./tools/msgextract` and commit it\n", out)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s (%d sentences)\n", out, len(list))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func scan(path string, found map[string]bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		fatal(err)
	}
	skip := map[*ast.BasicLit]bool{}  // literals that are for the log, not for the screen
	shown := map[*ast.BasicLit]bool{} // literals that are always for the screen
	ast.Inspect(file, func(n ast.Node) bool {
		// A piece of a sentence joined with + is not a sentence of its own.
		if bin, ok := n.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
			for _, side := range []ast.Expr{bin.X, bin.Y} {
				if lit, ok := side.(*ast.BasicLit); ok {
					skip[lit] = true
				}
			}
		}
		// A list of a type in shownFields: []Help{{code, level, area, "Title", ...}}.
		if lit, ok := n.(*ast.CompositeLit); ok {
			if arr, ok := lit.Type.(*ast.ArrayType); ok {
				if id, ok := arr.Elt.(*ast.Ident); ok {
					for _, pos := range shownFields[id.Name] {
						for _, el := range lit.Elts {
							if row, ok := el.(*ast.CompositeLit); ok && pos < len(row.Elts) {
								markLiterals(row.Elts[pos], shown)
							}
						}
					}
				}
			}
		}
		// A Health line written out as a literal: its title, note and label.
		if lit, ok := n.(*ast.CompositeLit); ok {
			if id, ok := lit.Type.(*ast.Ident); ok && (id.Name == "healthItem" || id.Name == "healthAction") {
				for _, el := range lit.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if key, ok := kv.Key.(*ast.Ident); ok && (key.Name == "Title" || key.Name == "Impact" || key.Name == "Label") {
							markLiterals(kv.Value, shown)
						}
					}
				}
			}
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if id, ok := fn.X.(*ast.Ident); ok && (id.Name == "slog" || id.Name == "log") {
				for _, a := range call.Args {
					markLiterals(a, skip)
				}
			}
			if pos, ok := messageCalls[fn.Sel.Name]; ok && len(call.Args) >= pos {
				markLiterals(call.Args[pos], shown)
			}
		case *ast.Ident:
			if pos, ok := messageCalls[fn.Name]; ok && len(call.Args) > pos {
				markLiterals(call.Args[pos], shown)
			}
			for _, pos := range healthCalls[fn.Name] {
				if len(call.Args) > pos {
					markLiterals(call.Args[pos], shown)
				}
			}
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || skip[lit] {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if text, ok := template(s, shown[lit]); ok {
			found[text] = true
		}
		return true
	})
}

// markLiterals marks the string literals in e: the literal itself, or the
// format string of a fmt.Sprintf / fmt.Errorf call.
func markLiterals(e ast.Expr, set map[*ast.BasicLit]bool) {
	ast.Inspect(e, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			set[lit] = true
		}
		return true
	})
}

// template turns a Go string into a sentence with {0}, {1} ... for the
// format verbs, or says it is not one the screen shows.
func template(s string, shown bool) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 8 || !strings.Contains(s, " ") || indexed.MatchString(s) || sqlWords.MatchString(s) || sqlClause.MatchString(s) {
		return "", false
	}
	if strings.ContainsAny(s, "`<") || strings.Contains(s, "://") || strings.Contains(s, "\n") {
		return "", false
	}
	first := []rune(s)[0]
	// A line that is always shown may start with a number or a name that is
	// filled in ("%d downloads failed ...", "%s is paused").
	startsOK := unicode.IsUpper(first) || (shown && (first == '%' || unicode.IsDigit(first)))
	// Starting with a filled-in part, the sentence must still have enough words
	// of its own that it cannot match some other text by accident.
	if first == '%' && len(strings.TrimSpace(verb.ReplaceAllString(s, ""))) < 14 {
		return "", false
	}
	if !startsOK || len(s) > 400 {
		return "", false
	}
	if !shown {
		last := s[len(s)-1]
		if last != '.' && last != '?' && last != '!' {
			return "", false
		}
		if looksLikeCode(s) {
			return "", false
		}
	}
	n := 0
	text := strings.ReplaceAll(s, "%%", "\x00")
	text = verb.ReplaceAllStringFunc(text, func(string) string {
		v := fmt.Sprintf("{%d}", n)
		n++
		return v
	})
	text = strings.ReplaceAll(text, "\x00", "%")
	if strings.Contains(text, "%") {
		return "", false
	}
	return text, true
}

// looksLikeCode drops the sentences of a comment-like or developer text.
func looksLikeCode(s string) bool {
	return strings.Contains(s, ":=") || strings.Contains(s, "()") || strings.Contains(s, "&&") || strings.Contains(s, "=> ")
}
