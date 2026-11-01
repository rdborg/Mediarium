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
		{"season range end is not a group", "Show.Name.S01E100-S01E102", parser.Release{Title: "Show Name", Season: 1, Episode: 100, Episodes: []int{100, 101, 102}}},
		{"backwards range keeps the first", "Show.Name.S01E05-E02", parser.Release{Title: "Show Name", Season: 1, Episode: 5, Episodes: []int{5}}},

		// A separator between season and episode is still one episode, not a pack
		{"space between season and episode", "Show S01 E01 1080p", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1}, Resolution: "1080p"}},
		{"dots and one digit", "Show.S1.E1.1080p", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1}, Resolution: "1080p"}},
		{"dash between season and episode", "Show.S02-E05.720p.HDTV-GRP", parser.Release{Title: "Show", Season: 2, Episode: 5, Episodes: []int{5}, Resolution: "720p", Source: "HDTV", Group: "GRP"}},
		{"underscore between season and episode", "Show_S03_E10_1080p", parser.Release{Title: "Show", Season: 3, Episode: 10, Episodes: []int{10}, Resolution: "1080p"}},
		{"spaced multi-episode", "Show S01 E01E02 720p", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1, 2}, Resolution: "720p"}},
		{"range with the season repeated", "Show.Name.S01E01-S01E03", parser.Release{Title: "Show Name", Season: 1, Episode: 1, Episodes: []int{1, 2, 3}}},
		{"web in a film title", "Charlotte's.Web.2006.1080p.BluRay.x264-GRP", parser.Release{Title: "Charlotte's Web", Year: 2006, Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"web as the source of a film", "The.Dark.Web.2021.1080p.WEB-DL.x264-GRP", parser.Release{Title: "The Dark Web", Year: 2021, Resolution: "1080p", Source: "WEB-DL", Codec: "x264", Group: "GRP"}},
		{"HDR10+ followed by a dot", "Movie.2021.2160p.WEB-DL.HDR10+.x265-GRP", parser.Release{Title: "Movie", Year: 2021, Resolution: "2160p", Source: "WEB-DL", HDR: "HDR10+", Codec: "x265", Group: "GRP"}},
		{"1080i is a 1080p picture", "Show.Name.S01E01.1080i.HDTV.x264-GRP", parser.Release{Title: "Show Name", Season: 1, Episode: 1, Episodes: []int{1}, Resolution: "1080p", Source: "HDTV", Codec: "x264", Group: "GRP"}},
		{"576p is standard definition", "Movie.2005.576p.BluRay.x264-GRP", parser.Release{Title: "Movie", Year: 2005, Resolution: "480p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"spaced range", "Show S01 E01-E03 720p", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1, 2, 3}, Resolution: "720p"}},
		{"pack followed by an audio tag", "Show.S01.EAC3.1080p-GRP", parser.Release{Title: "Show", Season: 1, Resolution: "1080p", AudioCodec: "EAC3", Group: "GRP"}},

		// More episode shapes
		{"episodes joined by a dot", "Show.S01E01.E02.1080p.WEB-DL-GRP", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1, 2}, Resolution: "1080p", Source: "WEB-DL", Group: "GRP"}},
		{"episodes joined by a dot at the end", "Show.S01E01.E02", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1, 2}}},
		{"NxNN range", "Show.1x01-1x03.720p.HDTV-GRP", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1, 2, 3}, Resolution: "720p", Source: "HDTV", Group: "GRP"}},
		{"NxNN range without the second season", "Show.1x01-03.720p", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1, 2, 3}, Resolution: "720p"}},
		{"NxNN range over two seasons keeps the first", "Show.1x05-2x01.720p", parser.Release{Title: "Show", Season: 1, Episode: 5, Episodes: []int{5}, Resolution: "720p"}},
		{"NxNN followed by a resolution", "Show.1x05-720p", parser.Release{Title: "Show", Season: 1, Episode: 5, Episodes: []int{5}, Resolution: "720p"}},
		{"year-numbered season", "Show.S2024E01.1080p.WEB-GRP", parser.Release{Title: "Show", Season: 2024, Episode: 1, Episodes: []int{1}, Resolution: "1080p", Source: "WEB", Group: "GRP"}},
		{"year-numbered season with a range", "Show.S2019E01-E03", parser.Release{Title: "Show", Season: 2019, Episode: 1, Episodes: []int{1, 2, 3}}},
		{"three digits is not a season", "Show.S201E01", parser.Release{Title: "Show S201E01"}},

		// Packs of several seasons
		{"multi-season pack", "Show.S01-S03.1080p", parser.Release{Title: "Show", Season: 1, SeasonTo: 3, Resolution: "1080p"}},
		{"multi-season pack without the second S", "Show.S01-03.1080p", parser.Release{Title: "Show", Season: 1, SeasonTo: 3, Resolution: "1080p"}},
		{"multi-season pack in words", "Show.Season.1-4.1080p.BluRay-GRP", parser.Release{Title: "Show", Season: 1, SeasonTo: 4, Resolution: "1080p", Source: "BluRay", Group: "GRP"}},
		{"backwards season range is one season", "Show.S03-S01.1080p", parser.Release{Title: "Show", Season: 3, Resolution: "1080p"}},
		{"season pack followed by a resolution", "Show.S01-720p", parser.Release{Title: "Show", Season: 1, Resolution: "720p"}},
		{"season pack with a group", "Show.S02-GRP", parser.Release{Title: "Show", Season: 2, Group: "GRP"}},
		{"episode wins over a pack marker", "Show.S01-S02E05.720p", parser.Release{Title: "Show", Season: 2, Episode: 5, Episodes: []int{5}, Resolution: "720p"}},

		// Season marker first
		{"title after Season", "Season.2.Show.1080p", parser.Release{Title: "Show", Season: 2, Resolution: "1080p"}},
		{"title after S01E01", "S01E05.Show.Name.720p.HDTV-GRP", parser.Release{Title: "Show Name", Season: 1, Episode: 5, Episodes: []int{5}, Resolution: "720p", Source: "HDTV", Group: "GRP"}},
		{"title after Season up to the year", "Season 1 Show Name 2019 1080p", parser.Release{Title: "Show Name", Year: 2019, Season: 1, Resolution: "1080p"}},
		{"title after Season up to the group", "Season.1.Show-GRP", parser.Release{Title: "Show", Season: 1, Group: "GRP"}},
		{"only a season marker", "S01E01", parser.Release{Season: 1, Episode: 1, Episodes: []int{1}}},

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
		{"postbot is not a group", "Movie.2019.1080p.BluRay.x264-GRP-postbot", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"Obfuscated is not a group", "Movie.2019.1080p.WEB-DL.x264-GRP-Obfuscated", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Source: "WEB-DL", Codec: "x264", Group: "GRP"}},
		{"xpost is not a group", "Show.S01E01.720p.HDTV.x264-GRP-xpost", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1}, Resolution: "720p", Source: "HDTV", Codec: "x264", Group: "GRP"}},
		{"Scrambled is not a group", "Movie.2019.1080p-GRP-Scrambled", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},
		{"BUYMORE is not a group", "Movie.2019.1080p-GRP-BUYMORE", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},
		{"WhiteRev is not a group", "Movie.2019.1080p-GRP-WhiteRev", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},
		{"AsRequested is not a group", "Movie.2019.1080p-GRP-AsRequested", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},
		{"Chaos is not a group", "Movie.2019.1080p-GRP-Chaos", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},
		{"RP is not a group", "Movie.2019.1080p-GRP-RP", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},
		{"NZBGeek is not a group", "Movie.2019.1080p-GRP-NZBGeek", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},
		{"sample is not a group", "Movie.2019.1080p-GRP-sample.mkv", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},
		{"suffixes in any case and stacked", "Movie.2019.1080p-GRP-OBFUSCATED-postBOT", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},
		{"suffix with nothing before it", "Movie.2019.1080p-postbot", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p"}},
		{"suffix after a bare title", "Movie-postbot", parser.Release{Title: "Movie"}},
		{"a group that only starts like a suffix", "Movie.2019.1080p-RPG", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "RPG"}},
		{"a site tag after the group", "Movie.2019.1080p.BluRay.x264-GRP[rarbg]", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"two site tags after the group", "Show.S01E01.1080p.WEB-GRP[eztv][rarbg]", parser.Release{Title: "Show", Season: 1, Episode: 1, Episodes: []int{1}, Resolution: "1080p", Source: "WEB", Group: "GRP"}},
		{"a site tag after a suffix", "Movie.2019.1080p-GRP-postbot[rarbg]", parser.Release{Title: "Movie", Year: 2019, Resolution: "1080p", Group: "GRP"}},

		// Years in titles
		{"title that is a year", "1917.2019.1080p.BluRay.x264-GRP", parser.Release{Title: "1917", Year: 2019, Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"title that is a year without a release year", "1917.1080p.BluRay.x264-GRP", parser.Release{Title: "1917", Resolution: "1080p", Source: "BluRay", Codec: "x264", Group: "GRP"}},
		{"title that is only a year", "1917", parser.Release{Title: "1917"}},
		{"year first in a title", "2001 A Space Odyssey 1968 1080p BluRay", parser.Release{Title: "2001 A Space Odyssey", Year: 1968, Resolution: "1080p", Source: "BluRay"}},
		{"year in parentheses", "2012 (2009)", parser.Release{Title: "2012", Year: 2009}},
		{"resolution written 1920x1080 is no year", "Some Show - 01 [1920x1080]", parser.Release{Title: "Some Show"}}, // episode 1, anime style
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
		"long spaced":    strings.Repeat("S01 E01 ", 40000),
		"long dotted":    "Show.S01E01" + strings.Repeat(".E01", 100000),
		"long seasons":   strings.Repeat("S01-S03.", 40000),
		"long suffixes":  "Movie.2019" + strings.Repeat("-postbot", 40000),
		"long site tags": "Movie.2019-GRP" + strings.Repeat("[rarbg]", 40000),
		"long crosses":   strings.Repeat("1x01-1x03.", 30000),
		"long Season":    strings.Repeat("Season.", 50000) + "1",
	}
	for name, in := range inputs {
		start := time.Now()
		r := parser.Parse(in)
		if d := time.Since(start); d > 2*time.Second*slowdown {
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

// A span of years names the first one as the year, and the title stops before it.
func TestYearSpanStartsWithTheFirstYear(t *testing.T) {
	cases := []struct {
		in    string
		title string
		year  int
	}{
		{"James Bond Collection (1962-2021)", "James Bond Collection", 1962},
		{"Friends (1994-2004)", "Friends", 1994},
		{"Friends.1994-2004.Complete.1080p.BluRay-GRP", "Friends", 1994},
		{"Heat (1995)", "Heat", 1995},
	}
	for _, c := range cases {
		r := parser.Parse(c.in)
		if r.Title != c.title || r.Year != c.year {
			t.Errorf("Parse(%q) = %q %d, want %q %d", c.in, r.Title, r.Year, c.title, c.year)
		}
	}
}
