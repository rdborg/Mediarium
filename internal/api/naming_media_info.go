package api

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rdborg/mediarium/internal/mediainfo"
	"github.com/rdborg/mediarium/internal/organizer"
)

// namingUsesMediaInfo reports whether any of the formats has a token that
// reads the file's own details, so a format without one never opens the file.
func namingUsesMediaInfo(formats ...string) bool {
	for _, f := range formats {
		l := strings.ToLower(f)
		if strings.Contains(l, "mediainfo") || strings.Contains(l, "{codec") {
			return true
		}
	}
	return false
}

// fillFromFile puts what the file itself says into the naming context, in
// place of what the release name guessed. The details saved for the file
// (media_info) are used when they still match the file; a file not read yet
// (one just downloaded) has its headers read now, which is quick and bounded.
// A file that can't be read leaves the release's values as they are.
func (s *Server) fillFromFile(ctx *organizer.NamingContext, path string, formats ...string) {
	if path == "" || !namingUsesMediaInfo(formats...) {
		return
	}
	info, ok := s.fileMediaInfo(path)
	if !ok {
		return
	}
	before := ctx.AudioCodec
	applyFileInfo(ctx, info)
	// A release word such as DD5.1 carries the channel layout. When the format doesn't write
	// the layout apart, the word stays, so a name keeps its 5.1 as it always had.
	if audioFamily(before) == "dd51" && !namingWritesChannels(formats...) {
		if f := audioFamily(ctx.AudioCodec); f == "ac3" || f == "eac3" {
			ctx.AudioCodec = before
		}
	}
}

// namingWritesChannels reports whether any of the formats writes the channel layout as its own token.
func namingWritesChannels(formats ...string) bool {
	for _, f := range formats {
		if strings.Contains(strings.ToLower(f), "audiochannels") {
			return true
		}
	}
	return false
}

// fileMediaInfo returns the details of the file at path, from the saved ones
// when they are current, else read from the file.
func (s *Server) fileMediaInfo(path string) (mediainfo.Info, bool) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return mediainfo.Info{}, false
	}
	if s.MovieRepo != nil {
		if row, found, err := s.MovieRepo.GetMediaInfo(path); err == nil && found && row.Size == st.Size() && row.MTime == st.ModTime().UnixNano() {
			if row.Failed || row.Info == "" {
				return mediainfo.Info{}, false
			}
			var info mediainfo.Info
			if json.Unmarshal([]byte(row.Info), &info) != nil {
				return mediainfo.Info{}, false
			}
			return info, true
		}
	}
	info, err := mediainfo.ReadFile(path)
	if err != nil {
		return mediainfo.Info{}, false
	}
	return info, true
}

// applyFileInfo overrides the release-name values of ctx with the file's own
// where the file says something. Where the release already names the same
// thing (x265 for HEVC, DD5.1 for E-AC-3) its spelling is kept, so a format's
// output doesn't change when the two agree.
func applyFileInfo(ctx *organizer.NamingContext, info mediainfo.Info) {
	if v := info.Video; v != nil {
		if v.Codec != "" && !sameVideoCodec(ctx.Codec, v.Codec) {
			ctx.Codec = videoCodecName(v.Codec)
		}
		if v.BitDepth > 0 {
			ctx.BitDepth = v.BitDepth
		}
		// A file that reads as plain SDR leaves the release's word alone: the
		// headers of some containers don't say, and a release is rarely wrong
		// about calling itself HDR.
		switch v.HDR {
		case "HDR10", "HDR10+", "Dolby Vision", "HLG":
			ctx.HDR = v.HDR
		}
	}
	if len(info.Audio) > 0 {
		main := info.Audio[0]
		for _, a := range info.Audio {
			if a.Default {
				main = a
				break
			}
		}
		// A release word such as DD5.1 or DDP5.1 already holds the channel layout, which
		// {Mediainfo AudioChannels} writes again: the file's own codec name (AC3, EAC3) goes in its place.
		if main.Codec != "" && (!sameAudioCodec(ctx.AudioCodec, main) || audioFamily(ctx.AudioCodec) == "dd51") {
			ctx.AudioCodec = audioCodecName(main)
		}
		ctx.AudioChannels = channelLayout(main.Channels)
		if langs := languageNames(func(add func(string)) {
			for _, a := range info.Audio {
				add(a.Lang)
			}
		}); len(langs) > 0 {
			ctx.Languages = langs
		}
	}
	ctx.SubtitleLanguages = languageNames(func(add func(string)) {
		for _, t := range info.Subtitles {
			add(t.Lang)
		}
	})
}

func languageNames(each func(add func(string))) []string {
	var out []string
	seen := map[string]bool{}
	each(func(code string) {
		if code != "" && !seen[code] {
			seen[code] = true
			out = append(out, mediainfo.LanguageName(code))
		}
	})
	return out
}

// channelLayout writes a track's channel count as Radarr and Sonarr do:
// "2.0", "5.1", "7.1".
func channelLayout(n int) string {
	switch {
	case n <= 0:
		return ""
	case n >= 6:
		return fmt.Sprintf("%d.1", n-1)
	default:
		return fmt.Sprintf("%d.0", n)
	}
}

func codecKey(s string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", ".", "", "-", "", "_", "").Replace(s))
}

// videoFamily folds the spellings of one video codec together.
func videoFamily(s string) string {
	switch k := codecKey(s); k {
	case "x264", "h264", "avc":
		return "avc"
	case "x265", "h265", "hevc":
		return "hevc"
	case "xvid", "divx", "mpeg4":
		return "mpeg4"
	case "mpeg2", "mpeg2video":
		return "mpeg2"
	default:
		return k
	}
}

func sameVideoCodec(release, file string) bool {
	return release != "" && videoFamily(release) == videoFamily(file)
}

// videoCodecName writes the file's codec the way the release names do (x265).
func videoCodecName(c string) string {
	switch videoFamily(c) {
	case "avc":
		return "x264"
	case "hevc":
		return "x265"
	case "mpeg2":
		return "MPEG2"
	case "mpeg4":
		return "MPEG4"
	}
	return c
}

// audioFamily folds the spellings of one audio codec together.
func audioFamily(s string) string {
	switch k := codecKey(s); k {
	case "ac3", "dolbydigital":
		return "ac3"
	case "eac3", "ddp", "ddplus", "dolbydigitalplus":
		return "eac3"
	case "dtshdma", "dtshd", "dtshdhra":
		return "dtshd"
	default:
		return k
	}
}

// sameAudioCodec reports whether the release's audio word names the track.
// DD5.1 in a release stands for Dolby Digital and Dolby Digital Plus alike,
// and "Atmos" for a track with an Atmos layer.
func sameAudioCodec(release string, a mediainfo.Audio) bool {
	if release == "" {
		return false
	}
	f := audioFamily(a.Codec)
	switch r := audioFamily(release); r {
	case "dd51":
		return f == "ac3" || f == "eac3"
	case "atmos":
		return a.Atmos || f == "truehd" || f == "eac3"
	default:
		return r == f
	}
}

// audioCodecName writes the file's codec the way the release names do.
func audioCodecName(a mediainfo.Audio) string {
	name := a.Codec
	switch audioFamily(a.Codec) {
	case "ac3":
		name = "AC3"
	case "eac3":
		name = "EAC3"
	}
	if a.Atmos {
		return name + " Atmos"
	}
	return name
}
