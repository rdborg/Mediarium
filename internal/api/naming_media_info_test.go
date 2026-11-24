package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/mediainfo"
	"github.com/rdborg/mediarium/internal/organizer"
)

func TestApplyFileInfo(t *testing.T) {
	hevc10 := mediainfo.Info{
		Video:     &mediainfo.Video{Codec: "HEVC", BitDepth: 10, HDR: "HDR10"},
		Audio:     []mediainfo.Audio{{Codec: "E-AC-3", Channels: 6, Lang: "en", Default: true}, {Codec: "AAC", Channels: 2, Lang: "de"}},
		Subtitles: []mediainfo.Subtitle{{Codec: "SRT", Lang: "en"}, {Codec: "SRT", Lang: "de"}, {Codec: "SRT", Lang: "en", Forced: true}},
	}
	cases := []struct {
		name string
		in   organizer.NamingContext
		info mediainfo.Info
		want organizer.NamingContext
	}{
		{
			name: "a bare file name takes everything from the file",
			info: hevc10,
			want: organizer.NamingContext{Codec: "x265", AudioCodec: "EAC3", HDR: "HDR10", AudioChannels: "5.1", BitDepth: 10,
				Languages: []string{"English", "German"}, SubtitleLanguages: []string{"English", "German"}},
		},
		{
			name: "the file wins over a wrong release name",
			in:   organizer.NamingContext{Codec: "x264", AudioCodec: "AAC", HDR: "HDR10+"},
			info: hevc10,
			want: organizer.NamingContext{Codec: "x265", AudioCodec: "EAC3", HDR: "HDR10", AudioChannels: "5.1", BitDepth: 10,
				Languages: []string{"English", "German"}, SubtitleLanguages: []string{"English", "German"}},
		},
		{
			name: "the release's spelling stays when it agrees",
			in:   organizer.NamingContext{Codec: "x265", AudioCodec: "DTS", HDR: "HDR10"},
			info: mediainfo.Info{Audio: []mediainfo.Audio{{Codec: "DTS", Channels: 6}}},
			want: organizer.NamingContext{Codec: "x265", AudioCodec: "DTS", HDR: "HDR10", AudioChannels: "5.1"},
		},
		{
			// DDP5.1 reads DD5.1; written next to {Mediainfo AudioChannels} it gave "DD5.1 5.1".
			name: "DD5.1 gives way to the codec name, since the layout is written apart",
			in:   organizer.NamingContext{Codec: "x265", AudioCodec: "DD5.1", HDR: "HDR10"},
			info: hevc10,
			want: organizer.NamingContext{Codec: "x265", AudioCodec: "EAC3", HDR: "HDR10", AudioChannels: "5.1", BitDepth: 10,
				Languages: []string{"English", "German"}, SubtitleLanguages: []string{"English", "German"}},
		},
		{
			name: "DD5.1 on a Dolby Digital file is AC3",
			in:   organizer.NamingContext{AudioCodec: "DD5.1"},
			info: mediainfo.Info{Audio: []mediainfo.Audio{{Codec: "AC-3", Channels: 6}}},
			want: organizer.NamingContext{AudioCodec: "AC3", AudioChannels: "5.1"},
		},
		{
			name: "an SDR file leaves the release's HDR word",
			in:   organizer.NamingContext{HDR: "Dolby Vision"},
			info: mediainfo.Info{Video: &mediainfo.Video{Codec: "H.264", HDR: "SDR"}},
			want: organizer.NamingContext{Codec: "x264", HDR: "Dolby Vision"},
		},
		{
			name: "TrueHD with Atmos, 7.1",
			in:   organizer.NamingContext{AudioCodec: "ATMOS"},
			info: mediainfo.Info{Audio: []mediainfo.Audio{{Codec: "TrueHD", Channels: 8, Atmos: true}}},
			want: organizer.NamingContext{AudioCodec: "ATMOS", AudioChannels: "7.1"},
		},
		{
			name: "Atmos on a release that said DTS",
			in:   organizer.NamingContext{AudioCodec: "DTS"},
			info: mediainfo.Info{Audio: []mediainfo.Audio{{Codec: "E-AC-3", Channels: 6, Atmos: true}}},
			want: organizer.NamingContext{AudioCodec: "EAC3 Atmos", AudioChannels: "5.1"},
		},
		{
			name: "stereo, and a language the file does not say",
			info: mediainfo.Info{Audio: []mediainfo.Audio{{Codec: "AAC", Channels: 2}}},
			want: organizer.NamingContext{AudioCodec: "AAC", AudioChannels: "2.0"},
		},
		{
			name: "DivX stays DivX for an MPEG-4 file",
			in:   organizer.NamingContext{Codec: "DivX"},
			info: mediainfo.Info{Video: &mediainfo.Video{Codec: "MPEG-4"}},
			want: organizer.NamingContext{Codec: "DivX"},
		},
		{
			name: "a file with no details changes nothing",
			in:   organizer.NamingContext{Codec: "x265", AudioCodec: "DTS", Languages: []string{"French"}},
			info: mediainfo.Info{Container: "mkv"},
			want: organizer.NamingContext{Codec: "x265", AudioCodec: "DTS", Languages: []string{"French"}},
		},
		{
			name: "a language the list doesn't know is written in capitals",
			info: mediainfo.Info{Subtitles: []mediainfo.Subtitle{{Codec: "SRT", Lang: "xyz"}}},
			want: organizer.NamingContext{SubtitleLanguages: []string{"XYZ"}},
		},
	}
	for _, tc := range cases {
		got := tc.in
		applyFileInfo(&got, tc.info)
		if !sameContext(got, tc.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", tc.name, got, tc.want)
		}
	}
}

func sameContext(a, b organizer.NamingContext) bool {
	eq := func(x, y []string) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	return a.Codec == b.Codec && a.AudioCodec == b.AudioCodec && a.HDR == b.HDR && a.AudioChannels == b.AudioChannels &&
		a.BitDepth == b.BitDepth && eq(a.Languages, b.Languages) && eq(a.SubtitleLanguages, b.SubtitleLanguages)
}

func TestChannelLayout(t *testing.T) {
	for n, want := range map[int]string{0: "", 1: "1.0", 2: "2.0", 6: "5.1", 8: "7.1"} {
		if got := channelLayout(n); got != want {
			t.Errorf("channelLayout(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestNamingUsesMediaInfo(t *testing.T) {
	for f, want := range map[string]bool{
		"{Movie Title} ({Year})":                       false,
		"{Movie Title} [{Quality}] {MediaInfo Simple}": true,
		"{Movie Title} {codec}":                        true,
		"{Movie Title} {[mediainfo VideoBitDepth]}bit": true,
	} {
		if got := namingUsesMediaInfo(f); got != want {
			t.Errorf("namingUsesMediaInfo(%q) = %v, want %v", f, got, want)
		}
	}
}

func TestNamingWritesChannels(t *testing.T) {
	for format, want := range map[string]bool{
		"{Movie CleanTitle} {[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}": true,
		"{Movie Title} {MediaInfo AudioCodec}":                                  false,
		"{Movie Title} ({Year}) {Quality}":                                      false,
		"{movie title} {mediainfo audiochannels}":                               true,
	} {
		if got := namingWritesChannels(format); got != want {
			t.Errorf("namingWritesChannels(%q) = %v, want %v", format, got, want)
		}
	}
}
