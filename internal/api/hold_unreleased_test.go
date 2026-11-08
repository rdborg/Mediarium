package api

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

func TestHoldUnreleasedSwitch(t *testing.T) {
	t.Cleanup(func() { setHoldUnreleased(true) })
	profile := quality.Presets()[quality.PresetAny]
	tests := []struct {
		name string
		hold bool
		m    library.Movie
		want bool // does a scheduled run search for it
	}{
		{"out, wait on", true, library.Movie{Monitored: true, Status: library.StatusMissing, ReleaseDate: "2020-01-01"}, true},
		{"not out, wait on", true, library.Movie{Monitored: true, Status: library.StatusMissing, ReleaseDate: "2999-01-01"}, false},
		{"not out, wait off", false, library.Movie{Monitored: true, Status: library.StatusMissing, ReleaseDate: "2999-01-01"}, true},
		{"no date, wait on", true, library.Movie{Monitored: true, Status: library.StatusMissing}, true},
	}
	for _, tc := range tests {
		setHoldUnreleased(tc.hold)
		if got := huntWantsMovie(tc.m, profile); got != tc.want {
			t.Errorf("%s: huntWantsMovie = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHoldNotes(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"episode with a date", unairedHoldNote(library.Episode{Season: 2, Episode: 5, AirDate: "2026-11-14"}), "Waiting for S02E05 to air on 14 Nov 2026."},
		{"episode without a usable date", unairedHoldNote(library.Episode{Season: 1, Episode: 1}), "Waiting for S01E01 to air."},
		{"movie with a date", unreleasedMovieNote("2026-12-20"), "Waiting for the release on 20 Dec 2026."},
		{"movie without a date", unreleasedMovieNote(""), "Waiting for the release."},
	}
	for _, tc := range tests {
		if !strings.HasPrefix(tc.got, tc.want) || !strings.HasSuffix(tc.got, "Search now looks anyway.") {
			t.Errorf("%s: %q does not start with %q", tc.name, tc.got, tc.want)
		}
	}
}
