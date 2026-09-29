package indexers

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestDefinitionCorpus validates a local folder of real Cardigann
// definitions (e.g. a checkout of Prowlarr/Indexers definitions/v11) and
// reports which ones the engine can't run. It reads local files only and is
// skipped unless MEDIARIUM_CARDIGANN_DEFS points at such a folder:
//
//	MEDIARIUM_CARDIGANN_DEFS=/path/to/definitions/v11 go test ./internal/indexers -run Corpus -v
func TestDefinitionCorpus(t *testing.T) {
	dir := os.Getenv("MEDIARIUM_CARDIGANN_DEFS")
	if dir == "" {
		t.Skip("set MEDIARIUM_CARDIGANN_DEFS to a folder of definitions to run this check")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no definitions in %s: %v", dir, err)
	}
	reasons := map[string]int{}
	bad := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		def, err := ParseDefinition(data)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		p := def.Problems()
		if len(p) == 0 {
			p = renderProblems(def)
		}
		if len(p) > 0 {
			bad++
			t.Logf("%s: %s", def.ID, strings.Join(p, "; "))
			for _, r := range p {
				if i := strings.Index(r, ": "); i >= 0 {
					r = r[i+2:]
				}
				if len(r) > 60 {
					r = r[:60]
				}
				reasons[r]++
			}
		}
	}
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return reasons[keys[i]] > reasons[keys[j]] })
	for _, k := range keys {
		t.Logf("%4d  %s", reasons[k], k)
	}
	t.Logf("supported: %d of %d definitions", len(files)-bad, len(files))
}

// renderProblems runs the definition's search templates with default
// settings and a sample query, catching errors that only show when a
// template is executed.
func renderProblems(def *Definition) []string {
	s, err := newSession(&CardigannManager{}, def, Instance{Name: def.ID})
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for _, q := range []string{"", "the matrix 1999"} {
		vars := s.vars()
		vars.Query["Keywords"] = q
		kw, err := applyFilters(q, def.Search.KeywordsFilters, vars)
		if err != nil {
			out = append(out, "keywords: "+err.Error())
		}
		vars.Keywords = kw
		vars.Categories = def.siteCategories([]int{2000, 5000})
		for _, p := range def.Search.Paths {
			if _, err := applyTemplate(p.Path, vars, urlPathEscape); err != nil {
				out = append(out, "path: "+err.Error())
			}
			for _, k := range p.Inputs.Keys {
				if _, err := applyTemplate(p.Inputs.Values[k], vars, nil); err != nil {
					out = append(out, "input: "+err.Error())
				}
			}
		}
		for _, k := range def.Search.Inputs.Keys {
			if _, err := applyTemplate(def.Search.Inputs.Values[k], vars, nil); err != nil {
				out = append(out, "input: "+err.Error())
			}
		}
		sel, err := applyTemplate(def.Search.Rows.Selector, vars, nil)
		if err != nil {
			out = append(out, "rows: "+err.Error())
		} else if !def.jsonSearch() {
			if _, err := CompileSelector(sel); err != nil {
				out = append(out, "rows: "+err.Error())
			}
		}
	}
	return out
}
