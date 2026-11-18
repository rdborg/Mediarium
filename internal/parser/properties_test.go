package parser_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/parser"
)

// TestParseTimeBudget feeds the parser long, hostile names and holds each parse
// to a time budget, so a pattern that backtracks (or a loop that is quadratic in
// the name) shows up as a failure and not as a stuck search.
func TestParseTimeBudget(t *testing.T) {
	long := 10000
	unit := func(s string) string { return strings.Repeat(s, long/len(s)+1)[:long] }
	inputs := map[string]string{
		"dots":              unit("."),
		"dashes":            unit("-"),
		"letters":           unit("a"),
		"digits":            unit("1"),
		"year repeats":      unit("2019."),
		"episode repeats":   unit("S01E01."),
		"episode run":       "Show." + unit("E01"),
		"episode run dash":  "Show.S01E01" + unit("-E01"),
		"season run":        unit("S01-"),
		"x run":             unit("1x01-"),
		"bracket run":       unit("[a]"),
		"open brackets":     unit("["),
		"open parens":       unit("("),
		"group dashes":      "Show.2019.1080p" + unit("-A"),
		"tags run":          unit("1080p.WEB-DL.x264."),
		"language run":      "Movie.2020." + unit("GERMAN.DL."),
		"unicode":           unit("é"),
		"mixed separators":  unit("._- "),
		"anime dash":        "[G] Show" + unit(" - 01"),
		"anime batch":       "[G] Show - 0001-9999 " + unit("[x]"),
		"date run":          unit("2019.01.01."),
		"date run year end": unit("15.03.2024."),
		"half episode":      "[G] Show - 12" + unit(".5"),
		"junk suffix":       "Show.S01E01" + unit("-postbot"),
		"trailing tags":     "Show.S01E01-GRP" + unit("[rarbg]"),
		"words":             unit("Season.1.Episode.1."),
		"complete":          unit("Complete.Series."),
	}
	budget := 500 * time.Millisecond * slowdown
	for name, in := range inputs {
		start := time.Now()
		r := parser.Parse(in)
		if d := time.Since(start); d > budget {
			t.Errorf("%s: parsing %d bytes took %v (budget %v)", name, len(in), d, budget)
		}
		if len(r.Episodes) > 1000 || len(r.Absolute) > 2001 {
			t.Errorf("%s: %d episodes, %d absolute numbers", name, len(r.Episodes), len(r.Absolute))
		}
	}
}

// TestParseCorpusIsStable parses every name of the corpus again after writing
// the result back out as a plain name. The title, year and numbering must come
// out the same, so a library that renames from what it parsed does not drift.
func TestParseCorpusIsStable(t *testing.T) {
	// The corpus tables run first (files run in name order) and fill corpusInputs.
	if len(corpusInputs) == 0 {
		t.Skip("the corpus tests did not run")
	}
	checked := 0
	for _, in := range corpusInputs {
		first := parser.Parse(in)
		if first.Title == "" || first.AirDate != "" || len(first.Absolute) > 0 {
			continue
		}
		var canon string
		switch {
		case first.Season > 0 && first.Episode > 0 && first.Season < 100 && first.Episode < 1000:
			canon = fmt.Sprintf("%s S%02dE%02d", first.Title, first.Season, first.Episode)
		case first.Season > 0 || first.Special || len(first.Episodes) > 0 || first.Complete:
			continue
		case first.Year != 0:
			canon = fmt.Sprintf("%s (%d)", first.Title, first.Year)
		default:
			continue
		}
		second := parser.Parse(canon)
		if second.Title != first.Title || second.Season != first.Season || second.Episode != first.Episode || (first.Year != 0 && first.Season == 0 && second.Year != first.Year) {
			t.Errorf("%q gives %+v; %q gives title %q season %d episode %d year %d", in, first.Title, canon, second.Title, second.Season, second.Episode, second.Year)
		}
		// Parsing the same name twice gives the same answer.
		if again := parser.Parse(in); fmt.Sprintf("%+v", again) != fmt.Sprintf("%+v", first) {
			t.Errorf("%q parses differently the second time", in)
		}
		checked++
	}
	t.Logf("%d names written back out and parsed again", checked)
}
