package origin

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	long := strings.Repeat("é", 300)
	cases := []struct {
		name string
		in   Origin
		want Origin
	}{
		{"empty stays empty", Origin{}, Origin{}},
		{"a manual origin keeps the person", Origin{Kind: Manual, UserID: 4, User: "Anna"}, Origin{Kind: Manual, UserID: 4, User: "Anna"}},
		{"a list keeps its name, trimmed", Origin{Kind: List, Name: "  Trending  "}, Origin{Kind: List, Name: "Trending"}},
		{"an unknown kind is dropped, with everything else", Origin{Kind: "teleport", Name: "x", UserID: 1}, Origin{}},
		{"a negative id becomes none", Origin{Kind: Search, UserID: -3}, Origin{Kind: Search}},
		{"a long name is cut by letters, not bytes", Origin{Kind: List, Name: long}, Origin{Kind: List, Name: strings.Repeat("é", 120)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.Clean(); got != c.want {
				t.Fatalf("Clean() = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestKnown(t *testing.T) {
	if (Origin{}).Known() {
		t.Fatal("the zero origin is not known")
	}
	if !(Origin{Kind: Auto, Name: JobRSS}).Known() {
		t.Fatal("an auto origin is known")
	}
}
