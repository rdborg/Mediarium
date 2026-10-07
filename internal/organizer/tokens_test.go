package organizer_test

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

func TestRadarrStyleTokens(t *testing.T) {
	movie := organizer.NamingContext{
		MovieTitle: "The Movie: Part 2", Year: 2024, Quality: "1080p", Source: "BluRay", Codec: "x265",
		AudioCodec: "DTS", HDR: "DV", Edition: "Extended", ReleaseGroup: "GROUP", Proper: true, TMDBID: 603,
	}
	plain := organizer.NamingContext{MovieTitle: "Plain Film", Year: 2001, Quality: "2160p", Source: "WEB-DL"}
	cases := []struct {
		name, format string
		ctx          organizer.NamingContext
		want         string
	}{
		{"a Radarr user's own format", "{Movie CleanTitle} ({Release Year}) - {Custom Formats}{ - Edition Tags}", movie, "The Movie Part 2 (2024) - Extended"},
		{"edition prefix only when there is one", "{Movie CleanTitle} ({Release Year}){ - Edition Tags}", plain, "Plain Film (2001)"},
		{"brackets inside the braces", "{Movie Title} {[Quality Full]}{-Release Group}", movie, "The Movie: Part 2 [Bluray-1080p Proper]-GROUP"},
		{"brackets vanish with an empty value", "{Movie Title} {[Quality Full]}{-Release Group}", plain, "Plain Film [WEBDL-2160p]"},
		{"title the", "{Movie TitleThe}", movie, "Movie: Part 2, The"},
		{"first character", "{Movie TitleFirstCharacter}", movie, "M"},
		{"dotted names", "{Movie.CleanTitle}.{Release.Year}", movie, "The.Movie.Part.2.2024"},
		{"Plex edition tag keeps its braces", "{Movie CleanTitle} ({Release Year}) {edition-{Edition Tags}}", movie, "The Movie Part 2 (2024) {edition-Extended}"},
		{"Plex edition tag disappears without an edition", "{Movie CleanTitle} ({Release Year}) {edition-{Edition Tags}}", plain, "Plain Film (2001)"},
		{"media info", "{MediaInfo VideoCodec} {MediaInfo AudioCodec} {[MediaInfo VideoDynamicRangeType]}", movie, "x265 DTS [DV]"},
		{"ids", "{Movie Title} [tmdbid-{TmdbId}]{ imdb-{ImdbId}}", movie, "The Movie: Part 2 [tmdbid-603]"},
		{"old tokens unchanged", "{Movie Title} ({Year}) [{Quality} {Source}]", movie, "The Movie: Part 2 (2024) [1080p BluRay]"},
		{"case doesn't matter", "{movie cleantitle} ({RELEASE YEAR})", movie, "The Movie Part 2 (2024)"},
		{"ampersand", "{Movie CleanTitle}", organizer.NamingContext{MovieTitle: "Salt & Static"}, "Salt and Static"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := organizer.Render(tc.format, tc.ctx); got != tc.want {
				t.Errorf("Render(%q) = %q, want %q", tc.format, got, tc.want)
			}
		})
	}
}

func TestSonarrStyleTokens(t *testing.T) {
	ep := organizer.NamingContext{SeriesTitle: "Show: Origins", Year: 2019, Season: 1, Episode: 2, EpisodeTitle: "Pilot, Part 2!", Quality: "720p", Source: "HDTV", ReleaseGroup: "GRP"}
	cases := []struct{ format, want string }{
		{"{Series TitleYear} - S{season:00}E{episode:00} - {Episode CleanTitle} [{Quality Full}]{-Release Group}", "Show: Origins (2019) - S01E02 - Pilot Part 2 [HDTV-720p]-GRP"},
		{"{Series.CleanTitleYear}.S{Season:00}E{Episode:00}", "Show.Origins.(2019).S01E02"},
	}
	for _, tc := range cases {
		if got := organizer.Render(tc.format, ep); got != tc.want {
			t.Errorf("Render(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
}

func TestSchemesAreValid(t *testing.T) {
	for _, s := range organizer.Schemes {
		if msg := organizer.CheckFormat(s.Movie, organizer.TokenMovie); msg != "" {
			t.Errorf("%s movie format: %s", s.ID, msg)
		}
		if msg := organizer.CheckFormat(s.Episode, organizer.TokenTV); msg != "" {
			t.Errorf("%s episode format: %s", s.ID, msg)
		}
	}
	for _, tk := range organizer.Tokens() {
		kind := organizer.TokenMovie
		if tk.Kind == organizer.TokenTV {
			kind = organizer.TokenTV
		}
		f := tk.Token
		if kind == organizer.TokenMovie {
			f = "{Movie Title} " + f
		} else {
			f = "S{Season:00}E{Episode:00} " + f
		}
		if msg := organizer.CheckFormat(f, kind); msg != "" {
			t.Errorf("listed token %s is refused: %s", tk.Token, msg)
		}
	}
}

func TestCheckFormat(t *testing.T) {
	cases := []struct {
		format string
		kind   organizer.TokenKind
		want   string // part of the message, "" for fine
	}{
		{"{Movie CleanTitle} ({Release Year}) - {Custom Formats}{ - Edition Tags}", organizer.TokenMovie, ""},
		{"{Movie Title} {Made Up}", organizer.TokenMovie, "isn't a token"},
		{"{Release Year} {Quality}", organizer.TokenMovie, "Include the movie's title"},
		{"{Movie Title} {Episode Title}", organizer.TokenMovie, "is for episodes"},
		{"{Series Title} - {Episode Title}", organizer.TokenTV, "Include {Season} and {Episode}"},
		{"{Series Title} {Air-Date}", organizer.TokenTV, ""},
		{"{Movie Title} {Year:xx}", organizer.TokenMovie, "use zeros"},
		{"{Movie Title} / {Year}", organizer.TokenMovie, "can't contain"},
		{"{Movie Title} {Year", organizer.TokenMovie, "is missing"},
		{"{Movie Title} {edition-{Nope}}", organizer.TokenMovie, "isn't a token"},
	}
	for _, tc := range cases {
		got := organizer.CheckFormat(tc.format, tc.kind)
		if (tc.want == "") != (got == "") || (tc.want != "" && !strings.Contains(got, tc.want)) {
			t.Errorf("CheckFormat(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
}
