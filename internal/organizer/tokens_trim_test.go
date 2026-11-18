package organizer_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

// The TRaSH Guides' Sonarr names use {Episode CleanTitle:90} (at most 90
// characters) and {Series CleanTitleWithoutYear} {(Series Year)}.
func TestTrashSonarrNameTokens(t *testing.T) {
	ep := organizer.NamingContext{
		SeriesTitle: "Show: Origins", Year: 2019, Season: 1, Episode: 2,
		EpisodeTitle: "A very long episode title that goes on and on", Quality: "1080p", Source: "WEB-DL", ReleaseGroup: "GRP",
	}
	cases := []struct {
		name, format, want string
	}{
		{"a short title is not touched", "{Episode CleanTitle:90}", "A very long episode title that goes on and on"},
		{"a long title is cut with an ellipsis", "{Episode Title:20}", "A very long episode…"},
		{"zeros still pad", "S{season:00}E{episode:00}", "S01E02"},
		{"title and year apart", "{Series CleanTitleWithoutYear} {(Series Year)}", "Show Origins (2019)"},
		{"the title with its punctuation", "{Series TitleWithoutYear}", "Show: Origins"},
		{"the year alone", "{Series Year}", "2019"},
		{"the default TRaSH episode name", "{Series CleanTitleWithoutYear} {(Series Year)} - S{season:00}E{episode:00} - {Episode CleanTitle:90} {[Custom Formats]}{[Quality Full]}{-Release Group}", "Show Origins (2019) - S01E02 - A very long episode title that goes on and on [WEBDL-1080p]-GRP"},
		{"one character", "{Episode Title:1}", "A"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := organizer.Render(tc.format, ep); got != tc.want {
				t.Errorf("Render(%q) = %q, want %q", tc.format, got, tc.want)
			}
		})
	}
}

func TestTrashSonarrNamesPassTheChecks(t *testing.T) {
	for _, f := range []string{
		"{Series CleanTitleWithoutYear} {(Series Year)} - S{season:00}E{episode:00} - {Episode CleanTitle:90} {[Custom Formats]}{[Quality Full]}{-Release Group}",
		"{Series CleanTitleWithoutYear} {(Series Year)} - {Air-Date} - S{season:00}E{episode:00}",
	} {
		if msg := organizer.CheckFormat(f, organizer.TokenTV); msg != "" {
			t.Errorf("%q: %s", f, msg)
		}
	}
	for _, f := range []string{"{Series CleanTitleWithoutYear} {(Series Year)}", "{Series CleanTitleWithoutYear} {(Series Year)} {tvdb-{TvdbId}}"} {
		if msg := organizer.CheckFolderFormat(f, organizer.TokenTV); msg != "" {
			t.Errorf("%q: %s", f, msg)
		}
	}
	// A letter after the colon still does not work.
	if msg := organizer.CheckFormat("{Movie Title:abc}", organizer.TokenMovie); msg == "" {
		t.Errorf("a letter after the colon should be refused")
	}
}
