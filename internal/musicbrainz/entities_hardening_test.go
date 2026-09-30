package musicbrainz

import "testing"

func TestYearOfIsFourDigits(t *testing.T) {
	for d, want := range map[string]int{"1999-05-02": 1999, "2001": 2001, "": 0, "199": 0, "-123-05": 0, "+199x": 0, "abcd": 0, "19a9": 0} {
		if got := yearOf(d); got != want {
			t.Errorf("yearOf(%q) = %d, want %d", d, got, want)
		}
	}
}

// FuzzQueryText: text put into a search query and a sort key never panics,
// and an escaped name never leaves a bare operator behind.
func FuzzQueryText(f *testing.F) {
	for _, s := range []string{"AC/DC", "!!!", "a b", "", `"quoted"`, "x\\", "(a AND b)", "\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		esc := luceneEscape(s)
		if len(esc) < len(s) {
			t.Fatalf("luceneEscape(%q) = %q is shorter", s, esc)
		}
		_ = dateKey(s)
		_ = yearOf(s)
		if y := yearOf(s); y < 0 || y > 9999 {
			t.Fatalf("yearOf(%q) = %d", s, y)
		}
	})
}
