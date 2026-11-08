package api

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestForeignShowHint(t *testing.T) {
	tests := []struct {
		name      string
		countries []string
		want      string // a word the hint must contain, empty for no hint
	}{
		{"french show", []string{"FR"}, "France"},
		{"lower case code", []string{"jp"}, "Japan"},
		{"co-production with the US", []string{"FR", "US"}, ""},
		{"british show", []string{"GB"}, ""},
		{"no country known", nil, ""},
		{"country without a name", []string{"ZZ"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := foreignShowHint(library.Series{OriginCountry: tc.countries})
			if tc.want == "" && got != "" {
				t.Fatalf("hint %q, want none", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("hint %q does not name %s", got, tc.want)
			}
		})
	}
}
