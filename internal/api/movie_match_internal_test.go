package api

import (
	"testing"

	"github.com/ryanborg/mediarium/internal/library"
)

// Only releases of the movie itself may be grabbed: a title search also
// returns other films that share words with it.
func TestMatchesMovieOnlyThisFilm(t *testing.T) {
	cases := []struct {
		release string
		title   string
		year    int
		want    bool
	}{
		{"Heart.of.the.Beast.2026.1080p.WEB-DL.x264-GRP", "Heart of the Beast", 2026, true},
		{"Beast.2026.1080p.WEB-DL.x264-GRP", "Heart of the Beast", 2026, false},
		{"Heart.of.the.Beast.Returns.2026.1080p.WEB-DL.x264-GRP", "Heart of the Beast", 2026, false},
		{"Heart.of.the.Beast.2019.1080p.WEB-DL.x264-GRP", "Heart of the Beast", 2026, false},
		{"Lord.of.the.Rings.The.Fellowship.of.the.Ring.2001.1080p.BluRay.x264-GRP", "The Lord of the Rings: The Fellowship of the Ring", 2001, true},
		{"The.Matrix.1999.1080p.BluRay.x264-GRP", "The Matrix", 1999, true},
		{"Matrix.1999.1080p.BluRay.x264-GRP", "The Matrix", 1999, true},
		{"Fast.and.Furious.2009.1080p.BluRay.x264-GRP", "Fast & Furious", 2009, true},
		{"Spider-Man.Brand.New.Day.2026.2160p.WEB-DL.x265-GRP", "Spider-Man: Brand New Day", 2026, true},
		{"A.Quiet.Place.2018.1080p.BluRay.x264-GRP", "A Quiet Place", 2018, true},
		{"Quiet.Place.Day.One.2024.1080p.BluRay.x264-GRP", "A Quiet Place", 2024, false},
	}
	for _, tc := range cases {
		t.Run(tc.release, func(t *testing.T) {
			got := matchesMovie(tc.release, library.Movie{Title: tc.title, Year: tc.year})
			if got != tc.want {
				t.Fatalf("matchesMovie(%q, %q %d) = %v, want %v", tc.release, tc.title, tc.year, got, tc.want)
			}
		})
	}
}
