package parser_test

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/parser"
)

// FuzzParse checks that no name, however broken, panics, hangs, gives a
// different answer the second time or produces an impossible result.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"The.Matrix.1999.1080p.WEB-DL.x264-GROUP",
		"Dune.Part.Two.2024.2160p.UHD.BluRay.REMUX.HDR10.DTS-HD.MA-GROUP",
		"Breaking.Bad.S05E14.1080p.WEB-DL.x264-GROUP",
		"Show.Name.S01E01E02E03.1080p.WEB-DL.x264-GROUP",
		"Show.Name.S01E01-E03.1080p",
		"Show.Name.1x05.720p.HDTV.x264-GRP",
		"Show Name Season 1 Complete 1080p BluRay",
		"Daily.Show.2021-05-14.720p",
		"Some.Show.2019.05.14.720p.HDTV.x264-GRP",
		"One.Piece.-.1015.-.720p",
		"[Group] Show - 01 [1920x1080]",
		"2001 A Space Odyssey 1968 1080p BluRay",
		"Blade Runner 2049 2017 1080p",
		"1917.2019.1080p.BluRay.x264-GRP",
		"1917.1080p",
		"Cam.2018.1080p.WEBRip",
		"Movie.2019.MULTi.GERMAN.DL.1080p",
		"Movie.2019.VOSTFR.1080p",
		"Movie.2019.Director's.Cut.2160p.Dolby.Vision.H.265-GRP",
		"Amélie.2001.1080p.BluRay.x264-GRP.mkv",
		"千と千尋の神隠し.2001.1080p",
		"Movie 🎬 Night.2020",
		"Movie\x00Name.2020",
		"Bad\xff\xfeName.2020",
		"",
		"-",
		"....",
		"S01",
		"S01E",
		"E01",
		"Show.S01E1-E9999",
		"Show.S99E999-E999",
		strings.Repeat("a", 10000),
		strings.Repeat("S01E01", 2000),
		strings.Repeat(".-_ ", 2500),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		start := time.Now()
		r := parser.Parse(in)
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("Parse(%q) took %s", in, d)
		}
		if again := parser.Parse(in); !reflect.DeepEqual(r, again) {
			t.Fatalf("Parse(%q) is not deterministic: %+v then %+v", in, r, again)
		}
		for name, v := range map[string]string{
			"Title": r.Title, "Resolution": r.Resolution, "Source": r.Source, "Codec": r.Codec,
			"AudioCodec": r.AudioCodec, "HDR": r.HDR, "Edition": r.Edition, "Group": r.Group,
		} {
			if !utf8.ValidString(v) {
				t.Fatalf("Parse(%q).%s = %q is not valid UTF-8", in, name, v)
			}
		}
		if r.Title != strings.TrimSpace(r.Title) || strings.Contains(r.Title, "  ") {
			t.Fatalf("Parse(%q).Title = %q has stray spaces", in, r.Title)
		}
		if strings.ContainsAny(r.Group, ". -_/\\") {
			t.Fatalf("Parse(%q).Group = %q holds a separator", in, r.Group)
		}
		if r.Year != 0 && (r.Year < 1900 || r.Year > 2099) {
			t.Fatalf("Parse(%q).Year = %d", in, r.Year)
		}
		if r.Season < 0 || r.Season > 99 {
			t.Fatalf("Parse(%q).Season = %d", in, r.Season)
		}
		if len(r.Episodes) > 1000 {
			t.Fatalf("Parse(%q) returned %d episodes", in, len(r.Episodes))
		}
		for _, e := range r.Episodes {
			if e < 0 || e > 999 {
				t.Fatalf("Parse(%q).Episodes = %v", in, r.Episodes)
			}
		}
		if r.Episode != 0 && (len(r.Episodes) == 0 || r.Episodes[0] != r.Episode) {
			t.Fatalf("Parse(%q): Episode %d but Episodes %v", in, r.Episode, r.Episodes)
		}
		for _, l := range r.Languages {
			if !utf8.ValidString(l) || l == "" {
				t.Fatalf("Parse(%q).Languages = %q", in, r.Languages)
			}
		}
	})
}
