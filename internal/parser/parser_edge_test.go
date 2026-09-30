package parser_test

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/parser"
)

// TestParseEdgeCases pins the odd release names that have gone wrong. Only
// the fields a case names are compared.
func TestParseEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want parser.Release
	}{
		// Season packs
		{"season pack with a space", "Show Name Season 1 Complete 1080p BluRay", parser.Release{Title: "Show Name", Season: 1, Resolution: "1080p", Source: "BluRay"}},
		{"season pack with dots", "Show.Name.Season.2.Complete.1080p", parser.Release{Title: "Show Name", Season: 2, Resolution: "1080p"}},
		{"season pack glued", "Show.Name.Season3.1080p", parser.Release{Title: "Show Name", Season: 3, Resolution: "1080p"}},
		{"special episodes are season zero", "Show.Name.S00E01.720p.HDTV.x264-GRP", parser.Release{Title: "Show Name", Season: 0, Episode: 1, Episodes: []int{1}, Resolution: "720p", Source: "HDTV", Codec: "x264", Group: "GRP"}},

		// Multi-episode ranges must not swallow a resolution
		{"range followed by a resolution", "Show.Name.S01E01-720p.WEB-DL", parser.Release{Title: "Show Name", Season: 1, Episode: 1, Episodes: []int{1}, Resolution: "720p", Source: "WEB-DL"}},
		{"episode range has no group", "Show.Name.S01E01-E03", parser.Release{Title: "Show Name", Season: 1, Episode: 1, Episodes: []int{1, 2, 3}}},
		{"season range end is not a group", "Show.Name.S01E100-S01E102", parser.Release{Title: "Show Name", Season: 1, Episode: 100, Episodes: []int{100}}},
		{"backwards range keeps the first", "Show.Name.S01E05-E02", parser.Release{Title: "Show Name", Season: 1, Episode: 5, Episodes: []int{5}}},

		// Codecs, HDR and editions with dots
		{"H.264 with a dot", "Movie.2019.1080p.BluRay.H.264-GRP", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"H.265 with a dot", "Movie.2019.2160p.WEB-DL.H.265-GRP", parser.Release{Title: "Movie", Year: 2019, Resolution: "2160p", Source: "WEB-DL", Codec: "x265", Group: "GRP"}},
		{"Dolby Vision with a dot", "Movie.2019.2160p.Dolby.Vision.WEB-DL", parser.Release{Title: "Movie", Year: 2019, Resolution: "2160p", Source: "WEB-DL", HDR: "Dolby Vision"}},
		{"Director's Cut with a dot", "Movie.2019.Director's.Cut.1080p.BluRay-GRP", parser.Release{Title: "Movie", Year: 2019, Edition: "Director's Cut", Resolution: "1080p", Source: "BluRay", Group: "GRP"}},
		{"Directors Cut with a dot", "Movie.2019.Directors.Cut.1080p.BluRay-GRP", parser.Release{Title: "Movie", Year: 2019, Edition: "Directors Cut", Resolution: "1080p", Source: "BluRay", Group: "GRP"}},

		// Release group
		{"WEB-DL is not a group", "Movie.2019.2160p.WEB-DL", parser.Release{Title: "Movie", Year: 2019, Resolution: "2160p", Source: "WEB-DL"}},
		{"DTS-HD is not a group", "Movie.2019.1080p.BluRay.DTS-HD", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Source: "BluRay", AudioCodec: "DTS-HD"}},
		{"dashed date is not a group", "Daily.Show.2021-05-14.720p", parser.Release{Title: "Daily Show", Year: 2021, Resolution: "720p"}},
		{"group with a video extension", "Movie.2019.1080p.BluRay.x264-GRP.m4v", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"a hyphenated title has no group", "Spider-Man", parser.Release{Title: "Spider-Man"}},
		{"a hyphenated title with a year", "X-Men.2000.1080p.BluRay-GRP", parser.Release{Title: "X-Men", Year: 2000, Resolution: "1080p", Source: "BluRay", Group: "GRP"}},
		{"a bare name with a dash", "Jay-Z", parser.Release{Title: "Jay-Z"}},

		// Years in titles
		{"title that is a year", "1917.2019.1080p.BluRay.x264-GRP", parser.Release{Title: "1917", Year: 2019, Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"title that is a year without a release year", "1917.1080p.BluRay.x264-GRP", parser.Release{Title: "1917", Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"title that is only a year", "1917", parser.Release{Title: "1917"}},
		{"year first in a title", "2001 A Space Odyssey 1968 1080p BluRay", parser.Release{Title: "2001 A Space Odyssey", Year: 1968, Resolution: "1080p", Source: "BluRay"}},
		{"year in parentheses", "2012 (2009)", parser.Release{Title: "2012", Year: 2009}},
		{"resolution written 1920x1080 is no year", "Some Show - 01 [1920x1080]", parser.Release{Title: "Some Show - 01 [1920x1080]"}},
		{"digits glued to a year are no year", "Movie.Name.12019.1080p", parser.Release{Title: "Movie Name 12019", Resolution: "1080p"}},
		{"airdate with dots", "Some.Show.2019.05.14.720p.HDTV.x264-GRP", parser.Release{Title: "Some Show", Year: 2019, Resolution: "720p", Source: "HDTV", Codec: "x264", Group: "GRP"}},
		{"airdate with dashes", "Late.Night.2021-05-14.1080p.WEB-DL", parser.Release{Title: "Late Night", Year: 2021, Resolution: "1080p", Source: "WEB-DL"}},

		// A title that looks like a tag
		{"title Cam", "Cam.2018.1080p.WEBRip.x264-GRP", parser.Release{Title: "Cam", Year: 2018, Resolution: "1080p", Source: "WEBRip", Codec: "x264", Group: "GRP"}},
		{"title Proper", "Proper.Manners.2019.1080p.WEB-DL", parser.Release{Title: "Proper Manners", Year: 2019, Resolution: "1080p", Source: "WEB-DL"}},
		{"tag alone stays a tag", "CAM", parser.Release{Source: "CAM"}},

		// Odd input
		{"empty", "", parser.Release{}},
		{"only punctuation", "....---___", parser.Release{}},
		{"only a dash", "-", parser.Release{}},
		{"only spaces", "    ", parser.Release{}},
		{"unicode title", "Amélie.2001.1080p.BluRay.x264-GRP", parser.Release{Title: "Amélie", Year: 2001, Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"cjk title", "千と千尋の神隠し.2001.1080p.BluRay-GRP", parser.Release{Title: "千と千尋の神隠し", Year: 2001, Resolution: "1080p", Source: "BluRay", Group: "GRP"}},
		{"emoji title", "Movie 🎬 Night.2020.720p", parser.Release{Title: "Movie 🎬 Night", Year: 2020, Resolution: "720p"}},
		{"invalid utf-8 is dropped", "Bad\xff\xfeName.2020.720p", parser.Release{Title: "BadName", Year: 2020, Resolution: "720p"}},
		{"NUL byte", "Movie\x00Name.2020.720p", parser.Release{Title: "Movie\x00Name", Year: 2020, Resolution: "720p"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parser.Parse(tc.in)
			assertReleaseEqual(t, tc.want, got)
			if !utf8.ValidString(got.Title) {
				t.Errorf("Title %q is not valid UTF-8", got.Title)
			}
		})
	}
}

func TestParseHugeNameIsFast(t *testing.T) {
	inputs := map[string]string{
		"long title":     strings.Repeat("Word.", 2000) + "2020.1080p.BluRay.x264-GRP",
		"long spaces":    strings.Repeat(" ", 10000) + "x",
		"long dashes":    strings.Repeat("-", 10000),
		"long dots":      strings.Repeat(".", 10000),
		"long digits":    strings.Repeat("1", 10000),
		"long episodes":  "Show.S01" + strings.Repeat("E01", 3000),
		"long tokens":    strings.Repeat("1080p.x264.", 1000),
		"long years":     strings.Repeat("2019.", 2000),
		"long unicode":   strings.Repeat("日本語", 3400),
		"long emoji":     strings.Repeat("🎬", 2500),
		"long brackets":  strings.Repeat("([{", 3000),
		"long season":    strings.Repeat("Season 1 ", 1200),
		"long range":     "Show.S01E01-" + strings.Repeat("E", 5000),
		"long open tags": strings.Repeat("S1E", 4000),
	}
	for name, in := range inputs {
		start := time.Now()
		r := parser.Parse(in)
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%s: Parse took %s", name, d)
		}
		if !utf8.ValidString(r.Title) {
			t.Errorf("%s: Title is not valid UTF-8", name)
		}
		if len(r.Episodes) > 1000 {
			t.Errorf("%s: %d episodes", name, len(r.Episodes))
		}
	}
}

func TestParseIsDeterministic(t *testing.T) {
	for _, in := range []string{"The.Matrix.1999.1080p.WEB-DL.x264-GROUP", "Show.S01E01E02.720p", "", "\xff\x00"} {
		a, b := parser.Parse(in), parser.Parse(in)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%q parsed two ways: %+v and %+v", in, a, b)
		}
	}
}
