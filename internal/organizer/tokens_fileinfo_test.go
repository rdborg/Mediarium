package organizer_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

func TestFileInfoTokens(t *testing.T) {
	full := organizer.NamingContext{
		MovieTitle: "Film", Year: 2026, Codec: "x265", AudioCodec: "EAC3", HDR: "HDR10",
		AudioChannels: "5.1", BitDepth: 10, SubtitleLanguages: []string{"English", "German"}, Languages: []string{"English"},
	}
	cases := []struct {
		name, format string
		ctx          organizer.NamingContext
		want         string
	}{
		{"channels", "{Movie Title} {MediaInfo AudioChannels}", full, "Film 5.1"},
		{"bit depth", "{Movie Title} {MediaInfo VideoBitDepth}bit", full, "Film 10bit"},
		{"subtitle languages", "{Movie Title} [{MediaInfo SubtitleLanguages}]", full, "Film [English+German]"},
		{"a Radarr style format", "{Movie Title} {[MediaInfo VideoCodec]}{[MediaInfo AudioCodec]}{[MediaInfo AudioChannels]}", full, "Film [x265][EAC3][5.1]"},
		{"nothing known writes nothing", "{Movie Title}{ [MediaInfo AudioChannels]}{ [MediaInfo VideoBitDepth]}{ [MediaInfo SubtitleLanguages]}", organizer.NamingContext{MovieTitle: "Film"}, "Film"},
	}
	for _, tc := range cases {
		if got := organizer.Render(tc.format, tc.ctx); got != tc.want {
			t.Errorf("%s: Render(%q) = %q, want %q", tc.name, tc.format, got, tc.want)
		}
	}
}
