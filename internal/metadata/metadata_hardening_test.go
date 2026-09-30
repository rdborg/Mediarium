package metadata

import (
	"strings"
	"testing"
)

func TestYearOfDate(t *testing.T) {
	for date, want := range map[string]int{
		"2024-03-01": 2024,
		"1999":       1999,
		"":           0,
		"202":        0,
		"-123-01-01": 0,
		"+123":       0,
		"12ab-01":    0,
		" 2024":      0,
		"20x4-01-01": 0,
		"٢٠٢٤":       0,
		"0000":       0,
	} {
		if got := yearOfDate(date); got != want {
			t.Errorf("yearOfDate(%q) = %d, want %d", date, got, want)
		}
		if got := (Movie{ReleaseDate: date}).Year(); got != want {
			t.Errorf("Movie.Year(%q) = %d, want %d", date, got, want)
		}
		if got := (Show{FirstAirDate: date}).Year(); got != want {
			t.Errorf("Show.Year(%q) = %d, want %d", date, got, want)
		}
	}
}

func FuzzYearOfDate(f *testing.F) {
	for _, s := range []string{"2024-03-01", "", "-1", "12ab", "\xff\xfe\xfd\xfc", "9999", "٠٠٠٠"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, date string) {
		if y := yearOfDate(date); y < 0 || y > 9999 {
			t.Fatalf("yearOfDate(%q) = %d", date, y)
		}
	})
}

func TestTrailerKeyCannotAddToTheAddress(t *testing.T) {
	got := youtubeTrailers([]Video{{Site: "YouTube", Key: "abc&list=evil#frag", Type: "Trailer"}})
	if len(got) != 1 || strings.ContainsAny(strings.TrimPrefix(got[0].URL, "https://www.youtube.com/watch?v="), "&#") {
		t.Fatalf("trailer address = %+v", got)
	}
}
