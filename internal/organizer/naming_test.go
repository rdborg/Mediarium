package organizer_test

import (
	"testing"

	"github.com/ryanborg/mediarium/internal/organizer"
)

func TestRenderPresets(t *testing.T) {
	ctx := organizer.NamingContext{
		MovieTitle:   "The Matrix",
		Year:         1999,
		Quality:      "1080p",
		Source:       "BluRay",
		Codec:        "x264",
		ReleaseGroup: "GROUP",
	}

	cases := []struct {
		preset string
		want   string
	}{
		{"plex", "The Matrix (1999)"},
		{"jellyfin", "The Matrix (1999) [1080p]"},
		{"kodi", "The Matrix (1999) [1080p BluRay]"},
		{"minimal", "The Matrix"},
	}
	for _, tc := range cases {
		t.Run(tc.preset, func(t *testing.T) {
			format, ok := organizer.Presets[tc.preset]
			if !ok {
				t.Fatalf("unknown preset %s", tc.preset)
			}
			got := organizer.Render(format, ctx)
			if got != tc.want {
				t.Errorf("preset %s: want %q, got %q", tc.preset, tc.want, got)
			}
		})
	}
}

func TestRenderCustomFormatWithGroup(t *testing.T) {
	ctx := organizer.NamingContext{
		MovieTitle:   "Dune Part Two",
		Year:         2024,
		Quality:      "2160p",
		Edition:      "IMAX",
		ReleaseGroup: "GROUP",
	}
	got := organizer.Render("{Movie Title} ({Year}) [{Quality}] {Custom Formats}-{Release Group}", ctx)
	want := "Dune Part Two (2024) [2160p] IMAX-GROUP"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestRenderMissingYearCollapsesEmptyParens(t *testing.T) {
	ctx := organizer.NamingContext{MovieTitle: "Untitled"}
	got := organizer.Render("{Movie Title} ({Year})", ctx)
	if got != "Untitled" {
		t.Errorf("expected empty-year parens to collapse away, got %q", got)
	}
}

func TestRenderZeroPad(t *testing.T) {
	ctx := organizer.NamingContext{SeriesTitle: "The Bear", Season: 2, Episode: 5}
	got := organizer.Render("{Series Title} S{Season:00}E{Episode:00}", ctx)
	want := "The Bear S02E05"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestSanitize(t *testing.T) {
	name := `Movie: The <Return> / "Sequel"?`
	stripped := organizer.Sanitize(name, organizer.SanitizeStrip, "")
	if stripped != "Movie The Return  Sequel" {
		t.Errorf("unexpected stripped result: %q", stripped)
	}
	replaced := organizer.Sanitize(name, organizer.SanitizeReplace, "-")
	if replaced != "Movie- The -Return- - -Sequel--" {
		t.Errorf("unexpected replaced result: %q", replaced)
	}
}

func TestRenderTVPresets(t *testing.T) {
	ctx := organizer.NamingContext{
		SeriesTitle: "Fixture Show", EpisodeTitle: "The Pilot", Year: 2011,
		Season: 1, Episode: 2, Quality: "1080p", Source: "WEB-DL",
	}
	cases := []struct{ preset, want string }{
		{"plex", "Fixture Show - S01E02 - The Pilot"},
		{"jellyfin", "Fixture Show - S01E02 - The Pilot [1080p]"},
		{"kodi", "Fixture Show - S01E02 - The Pilot [1080p WEB-DL]"},
		{"minimal", "S01E02"},
	}
	for _, tc := range cases {
		t.Run(tc.preset, func(t *testing.T) {
			if got := organizer.Render(organizer.TVPresets[tc.preset], ctx); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
	if got := organizer.Render(organizer.TVSeriesFolder, ctx); got != "Fixture Show (2011)" {
		t.Errorf("series folder: got %q", got)
	}
	if got := organizer.Render(organizer.TVSeasonFolder, ctx); got != "Season 01" {
		t.Errorf("season folder: got %q", got)
	}
}

// An episode with no title yet (TMDB hasn't named it) must not leave a
// dangling " - " on the filename.
func TestRenderTVEmptyEpisodeTitle(t *testing.T) {
	ctx := organizer.NamingContext{SeriesTitle: "Show", Season: 3, Episode: 10}
	if got := organizer.Render(organizer.TVPresets["plex"], ctx); got != "Show - S03E10" {
		t.Fatalf("want %q, got %q", "Show - S03E10", got)
	}
}

// The PRD writes tokens lowercase ("{season:00}"); presets use title case.
func TestRenderTokensCaseInsensitive(t *testing.T) {
	ctx := organizer.NamingContext{SeriesTitle: "Show", Season: 4, Episode: 5}
	if got := organizer.Render("{series title} s{SEASON:00}e{episode:00}", ctx); got != "Show s04e05" {
		t.Fatalf("got %q", got)
	}
}
