package api

import (
	"slices"
	"testing"
)

// A main title with umlauts is also searched written out (#148), first among
// the other titles, also when English is the only language wanted.
func TestMainTitleUmlautSpelling(t *testing.T) {
	tests := []struct {
		title string
		in    []string
		want  []string
	}{
		{"Sams im Glück", nil, []string{"Sams im Glueck"}},
		{"Sams im Glück", []string{"Sams in Luck"}, []string{"Sams im Glueck", "Sams in Luck"}},
		{"Die Weiße Rose", nil, []string{"Die Weisse Rose"}},
		{"Sams im Glück", []string{"sams im glueck"}, []string{"sams im glueck"}}, // already there
		{"Inception", []string{"Origen"}, []string{"Origen"}},
		{"Inception", nil, nil},
	}
	for _, tt := range tests {
		if got := withMainSpelling(tt.title, tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("%q %v: got %v, want %v", tt.title, tt.in, got, tt.want)
		}
	}

	s := newBareServer(t)
	// No TMDB key and only English wanted: the written-out spelling still comes.
	if got := s.altTitlesFor(t.Context(), "movie", 1, "Sams im Glück", nil); !slices.Equal(got, []string{"Sams im Glueck"}) {
		t.Errorf("altTitlesFor = %v", got)
	}
}
