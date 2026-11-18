package parser_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/parser"
)

// corpusCase is one release name and the fields the parser must read out of
// it. want is a list of key=value pairs split by "; ": only the keys named are
// checked, so a case states what matters and nothing else.
type corpusCase struct {
	in   string
	want string
}

// check compares the parsed release with the key=value list.
func checkCorpus(r parser.Release, want string) []string {
	var bad []string
	for _, kv := range strings.Split(want, "; ") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		var got string
		switch k {
		case "title":
			got = r.Title
		case "year":
			got = strconv.Itoa(r.Year)
		case "s":
			got = strconv.Itoa(r.Season)
		case "e":
			got = strconv.Itoa(r.Episode)
		case "eps":
			parts := make([]string, len(r.Episodes))
			for i, e := range r.Episodes {
				parts[i] = strconv.Itoa(e)
			}
			got = strings.Join(parts, ",")
		case "sto":
			got = strconv.Itoa(r.SeasonTo)
		case "complete":
			got = strconv.FormatBool(r.Complete)
		case "abs":
			parts := make([]string, len(r.Absolute))
			for i, e := range r.Absolute {
				parts[i] = strconv.Itoa(e)
			}
			got = strings.Join(parts, ",")
		case "air":
			got = r.AirDate
		case "res":
			got = r.Resolution
		case "src":
			got = r.Source
		case "codec":
			got = r.Codec
		case "audio":
			got = r.AudioCodec
		case "hdr":
			got = r.HDR
		case "ed":
			got = r.Edition
		case "group":
			got = r.Group
		case "proper":
			got = strconv.FormatBool(r.Proper)
		case "repack":
			got = strconv.FormatBool(r.Repack)
		case "langs":
			got = strings.Join(r.Languages, ",")
		case "multi":
			got = strconv.FormatBool(r.Multi)
		case "subbed":
			got = strconv.FormatBool(r.Subbed)
		case "special":
			got = strconv.FormatBool(r.Special)
		case "3d":
			got = strconv.FormatBool(r.Is3D)
		case "late":
			got = strconv.FormatBool(r.YearLate)
		default:
			bad = append(bad, "unknown key "+k)
			continue
		}
		if got != v {
			bad = append(bad, fmt.Sprintf("%s: got %q want %q", k, got, v))
		}
	}
	return bad
}

// corpusInputs collects every name the corpus tests ran, for the property tests.
var corpusInputs []string

func runCorpus(t *testing.T, cases []corpusCase) {
	t.Helper()
	for _, c := range cases {
		corpusInputs = append(corpusInputs, c.in)
	}
	failed := 0
	for _, c := range cases {
		r := parser.Parse(c.in)
		if bad := checkCorpus(r, c.want); len(bad) > 0 {
			failed++
			t.Errorf("%q\n    %s", c.in, strings.Join(bad, " | "))
		}
	}
	t.Logf("%d cases, %d wrong", len(cases), failed)
}
