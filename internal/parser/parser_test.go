package parser_test

import (
	"slices"
	"testing"

	"github.com/ryanborg/mediarium/internal/parser"
)

func TestParseMovies(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want parser.Release
	}{
		{
			name: "web-dl x264 with group",
			in:   "The.Matrix.1999.1080p.WEB-DL.x264-GROUP",
			want: parser.Release{Title: "The Matrix", Year: 1999, Resolution: "1080p", Source: "WEB-DL", Codec: "x264", Group: "GROUP"},
		},
		{
			name: "bluray remux with audio and hdr",
			in:   "Dune.Part.Two.2024.2160p.UHD.BluRay.REMUX.HDR10.DTS-HD.MA-GROUP",
			want: parser.Release{Title: "Dune Part Two", Year: 2024, Resolution: "2160p", Source: "Remux", HDR: "HDR10", AudioCodec: "DTS-HD", Group: "GROUP"},
		},
		{
			name: "proper repack",
			in:   "Oppenheimer.2023.PROPER.REPACK.1080p.WEBRip.x265-GROUP",
			want: parser.Release{Title: "Oppenheimer", Year: 2023, Resolution: "1080p", Source: "WEBRip", Codec: "x265", Proper: true, Repack: true, Group: "GROUP"},
		},
		{
			name: "extended edition",
			in:   "Blade.Runner.2049.EXTENDED.2017.720p.BluRay.x264-GROUP",
			want: parser.Release{Title: "Blade Runner 2049", Year: 2017, Edition: "EXTENDED", Resolution: "720p", Source: "BluRay", Codec: "x264", Group: "GROUP"},
		},
		{
			name: "cam quality",
			in:   "Some.Movie.2022.CAM.XVID-GROUP",
			want: parser.Release{Title: "Some Movie", Year: 2022, Source: "CAM", Codec: "XviD", Group: "GROUP"},
		},
		{
			name: "with dots underscores and spaces mixed",
			in:   "The_Grand_Budapest_Hotel.2014.1080p.BluRay.x264.DTS-GROUP",
			want: parser.Release{Title: "The Grand Budapest Hotel", Year: 2014, Resolution: "1080p", Source: "BluRay", Codec: "x264", AudioCodec: "DTS", Group: "GROUP"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parser.Parse(tc.in)
			assertReleaseEqual(t, tc.want, got)
		})
	}
}

func TestParseTV(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want parser.Release
	}{
		{
			name: "standard season episode",
			in:   "Breaking.Bad.S05E14.1080p.WEB-DL.x264-GROUP",
			want: parser.Release{Title: "Breaking Bad", Season: 5, Episode: 14, Episodes: []int{14}, Resolution: "1080p", Source: "WEB-DL", Codec: "x264", Group: "GROUP"},
		},
		{
			name: "season pack",
			in:   "The.Bear.S02.1080p.WEB-DL.x265-GROUP",
			want: parser.Release{Title: "The Bear", Season: 2, Resolution: "1080p", Source: "WEB-DL", Codec: "x265", Group: "GROUP"},
		},
		{
			name: "lowercase sxxexx",
			in:   "some.show.s01e02.720p.hdtv.x264-grp",
			want: parser.Release{Title: "some show", Season: 1, Episode: 2, Episodes: []int{2}, Resolution: "720p", Source: "HDTV", Codec: "x264", Group: "grp"},
		},
		{
			name: "concatenated multi-episode",
			in:   "Show.Name.S01E01E02E03.1080p.WEB-DL.x264-GROUP",
			want: parser.Release{Title: "Show Name", Season: 1, Episode: 1, Episodes: []int{1, 2, 3}, Resolution: "1080p", Source: "WEB-DL", Codec: "x264", Group: "GROUP"},
		},
		{
			name: "dashed multi-episode range with E prefix",
			in:   "Show.Name.S01E01-E03.1080p.WEB-DL.x264-GROUP",
			want: parser.Release{Title: "Show Name", Season: 1, Episode: 1, Episodes: []int{1, 2, 3}, Resolution: "1080p", Source: "WEB-DL", Codec: "x264", Group: "GROUP"},
		},
		{
			name: "dashed multi-episode range without second E",
			in:   "Show.Name.S01E01-03.1080p.WEB-DL.x264-GROUP",
			want: parser.Release{Title: "Show Name", Season: 1, Episode: 1, Episodes: []int{1, 2, 3}, Resolution: "1080p", Source: "WEB-DL", Codec: "x264", Group: "GROUP"},
		},
		{
			name: "NxNN season episode",
			in:   "Show.Name.1x05.720p.HDTV.x264-GRP",
			want: parser.Release{Title: "Show Name", Season: 1, Episode: 5, Episodes: []int{5}, Resolution: "720p", Source: "HDTV", Codec: "x264", Group: "GRP"},
		},
		{
			name: "series folder with parenthesised year",
			in:   "Breaking Bad (2008)",
			want: parser.Release{Title: "Breaking Bad", Year: 2008},
		},
		{
			name: "series folder keeps a legitimate parenthesised qualifier",
			in:   "Show Name (US) (2010)",
			want: parser.Release{Title: "Show Name (US)", Year: 2010},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parser.Parse(tc.in)
			assertReleaseEqual(t, tc.want, got)
		})
	}
}

func assertReleaseEqual(t *testing.T, want, got parser.Release) {
	t.Helper()
	if want.Title != got.Title {
		t.Errorf("Title: want %q, got %q", want.Title, got.Title)
	}
	if want.Year != got.Year {
		t.Errorf("Year: want %d, got %d", want.Year, got.Year)
	}
	if want.Season != got.Season {
		t.Errorf("Season: want %d, got %d", want.Season, got.Season)
	}
	if want.Episode != got.Episode {
		t.Errorf("Episode: want %d, got %d", want.Episode, got.Episode)
	}
	if !slices.Equal(want.Episodes, got.Episodes) {
		t.Errorf("Episodes: want %v, got %v", want.Episodes, got.Episodes)
	}
	if want.Resolution != got.Resolution {
		t.Errorf("Resolution: want %q, got %q", want.Resolution, got.Resolution)
	}
	if want.Source != got.Source {
		t.Errorf("Source: want %q, got %q", want.Source, got.Source)
	}
	if want.Codec != got.Codec {
		t.Errorf("Codec: want %q, got %q", want.Codec, got.Codec)
	}
	if want.AudioCodec != got.AudioCodec {
		t.Errorf("AudioCodec: want %q, got %q", want.AudioCodec, got.AudioCodec)
	}
	if want.HDR != got.HDR {
		t.Errorf("HDR: want %q, got %q", want.HDR, got.HDR)
	}
	if want.Edition != got.Edition {
		t.Errorf("Edition: want %q, got %q", want.Edition, got.Edition)
	}
	if want.Group != got.Group {
		t.Errorf("Group: want %q, got %q", want.Group, got.Group)
	}
	if want.Proper != got.Proper {
		t.Errorf("Proper: want %v, got %v", want.Proper, got.Proper)
	}
	if want.Repack != got.Repack {
		t.Errorf("Repack: want %v, got %v", want.Repack, got.Repack)
	}
}
