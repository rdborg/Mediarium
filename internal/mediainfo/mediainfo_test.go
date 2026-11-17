package mediainfo

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func read(t *testing.T, data []byte, ext string) Info {
	t.Helper()
	info, err := Read(bytes.NewReader(data), int64(len(data)), ext)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return info
}

func TestMatroska(t *testing.T) {
	h264 := mkvT{kind: mkTypeVideo, codec: "V_MPEG4/ISO/AVC", w: 1920, h: 800, defaultDur: 41708333, private: avcCRecord(100, 8)}
	hevc10 := mkvT{kind: mkTypeVideo, codec: "V_MPEGH/ISO/HEVC", w: 3840, h: 2160, depth: 10, transfer: 16, defaultDur: 41708333}
	tests := []struct {
		name string
		data []byte
		want Info
	}{
		{
			name: "H.264 with AAC and an SRT",
			data: buildMKV("matroska", 5400.5, []byte("frames"), h264,
				mkvT{kind: mkTypeAudio, codec: "A_AAC", lang: "eng", channels: 2, name: "Stereo"},
				mkvT{kind: mkTypeSubtitle, codec: "S_TEXT/UTF8", lang: "eng"}),
			want: Info{
				Container: "matroska", Duration: 5400.5,
				Video:     &Video{Codec: "H.264", Width: 1920, Height: 800, BitDepth: 8, HDR: "SDR", FPS: 23.976},
				Audio:     []Audio{{Codec: "AAC", Channels: 2, Lang: "en", Title: "Stereo", Default: true}},
				Subtitles: []Subtitle{{Codec: "SRT", Lang: "en", Default: true}},
			},
		},
		{
			name: "HEVC 10-bit HDR10 with AC-3, Atmos TrueHD and forced PGS",
			data: buildMKV("matroska", 7200, []byte("frames"), hevc10,
				mkvT{kind: mkTypeAudio, codec: "A_TRUEHD", lang: "eng", channels: 8, name: "TrueHD Atmos 7.1"},
				mkvT{kind: mkTypeAudio, codec: "A_AC3", lang: "ger", channels: 6, noDefault: true},
				mkvT{kind: mkTypeSubtitle, codec: "S_HDMV/PGS", lang: "eng", forced: true, noDefault: true},
				mkvT{kind: mkTypeSubtitle, codec: "S_HDMV/PGS", lang: "dut", noDefault: true}),
			want: Info{
				Container: "matroska", Duration: 7200,
				Video: &Video{Codec: "HEVC", Width: 3840, Height: 2160, BitDepth: 10, HDR: "HDR10", FPS: 23.976},
				Audio: []Audio{
					{Codec: "TrueHD", Channels: 8, Lang: "en", Title: "TrueHD Atmos 7.1", Default: true, Atmos: true},
					{Codec: "AC-3", Channels: 6, Lang: "de"},
				},
				Subtitles: []Subtitle{{Codec: "PGS", Lang: "en", Forced: true}, {Codec: "PGS", Lang: "nl"}},
			},
		},
		{
			name: "Dolby Vision profile 8 from the block addition record",
			data: buildMKV("matroska", 60, nil,
				mkvT{kind: mkTypeVideo, codec: "V_MPEGH/ISO/HEVC", w: 3840, h: 1608, depth: 10, transfer: 16, dv: dvRecord(8)}),
			want: Info{Container: "matroska", Duration: 60, Video: &Video{Codec: "HEVC", Width: 3840, Height: 1608, BitDepth: 10, HDR: "Dolby Vision", DVProfile: 8}},
		},
		{
			name: "HDR10+ found in the first frames",
			data: buildMKV("matroska", 60, append([]byte("abc"), hdr10PlusMark...), hevc10),
			want: Info{Container: "matroska", Duration: 60, Video: &Video{Codec: "HEVC", Width: 3840, Height: 2160, BitDepth: 10, HDR: "HDR10+", FPS: 23.976}},
		},
		{
			name: "HLG",
			data: buildMKV("matroska", 60, nil, mkvT{kind: mkTypeVideo, codec: "V_MPEGH/ISO/HEVC", w: 1920, h: 1080, depth: 10, transfer: 18}),
			want: Info{Container: "matroska", Duration: 60, Video: &Video{Codec: "HEVC", Width: 1920, Height: 1080, BitDepth: 10, HDR: "HLG"}},
		},
		{
			name: "WebM with VP9 and Opus, language from BCP 47",
			data: buildMKV("webm", 30, nil,
				mkvT{kind: mkTypeVideo, codec: "V_VP9", w: 1280, h: 720},
				mkvT{kind: mkTypeAudio, codec: "A_OPUS", lang: "eng", bcp47: "pt-BR", channels: 2}),
			want: Info{
				Container: "webm", Duration: 30,
				Video: &Video{Codec: "VP9", Width: 1280, Height: 720, HDR: "SDR"},
				Audio: []Audio{{Codec: "Opus", Channels: 2, Lang: "pt", Default: true}},
			},
		},
		{
			name: "10-bit H.264 read from the setup record, DTS-HD named by its title, no language",
			data: buildMKV("matroska", 100, nil,
				mkvT{kind: mkTypeVideo, codec: "V_MPEG4/ISO/AVC", w: 1920, h: 1080, private: avcCRecord(110, 10)},
				mkvT{kind: mkTypeAudio, codec: "A_DTS", channels: 6, lang: "und", name: "DTS-HD MA 5.1"},
				mkvT{kind: mkTypeAudio, codec: "A_DTS", channels: 8, lang: "fre", name: "DTS:X"},
				mkvT{kind: mkTypeAudio, codec: "A_FLAC", channels: 2, lang: "jpn"}),
			want: Info{
				Container: "matroska", Duration: 100,
				Video: &Video{Codec: "H.264", Width: 1920, Height: 1080, BitDepth: 10, HDR: "SDR"},
				Audio: []Audio{
					{Codec: "DTS-HD MA", Channels: 6, Title: "DTS-HD MA 5.1", Default: true},
					{Codec: "DTS:X", Channels: 8, Lang: "fr", Title: "DTS:X", Default: true},
					{Codec: "FLAC", Channels: 2, Lang: "ja", Default: true},
				},
			},
		},
		{
			name: "no language set means English, as Matroska says",
			data: buildMKV("matroska", 10, nil, mkvT{kind: mkTypeAudio, codec: "A_EAC3", channels: 6, name: "Dolby Atmos"}),
			want: Info{Container: "matroska", Duration: 10, Audio: []Audio{{Codec: "E-AC-3", Channels: 6, Lang: "en", Title: "Dolby Atmos", Default: true, Atmos: true}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := read(t, tt.data, "mkv")
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestMP4(t *testing.T) {
	avc := mp4T{handler: "vide", lang: "und", scale: 24000, delta: 1001, w: 1920, h: 1080,
		entry: visualEntry("avc1", 1920, 1080, box("avcC", avcCRecord(100, 8)))}
	aac := mp4T{handler: "soun", lang: "eng", entry: audioEntryBox("mp4a", 2, esdsAAC(2))}
	tests := []struct {
		name string
		data []byte
		want Info
	}{
		{
			name: "H.264 and AAC, moov after the movie data",
			data: buildMP4("isom", 600, []byte("frames"), avc, aac),
			want: Info{
				Container: "mp4", Duration: 600,
				Video: &Video{Codec: "H.264", Width: 1920, Height: 1080, BitDepth: 8, HDR: "SDR", FPS: 23.976},
				Audio: []Audio{{Codec: "AAC", Channels: 2, Lang: "en"}},
			},
		},
		{
			name: "HEVC with Dolby Vision, AC-3 and E-AC-3 Atmos, tx3g subtitle in German",
			data: buildMP4("qt  ", 120, nil,
				mp4T{handler: "vide", w: 3840, h: 2160, entry: visualEntry("dvh1", 3840, 2160, box("hvcC", hvcCRecord(10)), box("dvcC", dvRecord(5)))},
				mp4T{handler: "soun", lang: "fra", entry: audioEntryBox("ac-3", 2, box("dac3", dac3Record(7, 1)))},
				mp4T{handler: "soun", lang: "eng", entry: audioEntryBox("ec-3", 2, box("dec3", dec3Record(7, 1, 16)))},
				mp4T{handler: "sbtl", lang: "ger", entry: box("tx3g", make([]byte, 8))}),
			want: Info{
				Container: "mov", Duration: 120,
				Video: &Video{Codec: "HEVC", Width: 3840, Height: 2160, BitDepth: 10, HDR: "Dolby Vision", DVProfile: 5},
				Audio: []Audio{
					{Codec: "AC-3", Channels: 6, Lang: "fr"},
					{Codec: "E-AC-3", Channels: 6, Lang: "en", Atmos: true},
				},
				Subtitles: []Subtitle{{Codec: "mov_text", Lang: "de"}},
			},
		},
		{
			name: "HDR10 from colr, found with the chunk offset to HDR10+",
			data: buildMP4("isom", 60, append(make([]byte, 100), hdr10PlusMark...),
				mp4T{handler: "vide", w: 3840, h: 2160, entry: visualEntry("hvc1", 3840, 2160, box("hvcC", hvcCRecord(10)), box("colr", []byte("nclx"), u16(9), u16(16), u16(9), []byte{0}))}),
			want: Info{Container: "mp4", Duration: 60, Video: &Video{Codec: "HEVC", Width: 3840, Height: 2160, BitDepth: 10, HDR: "HDR10+"}},
		},
		{
			name: "plain HDR10 when the frames carry no HDR10+ message",
			data: buildMP4("isom", 60, make([]byte, 200),
				mp4T{handler: "vide", w: 3840, h: 2160, entry: visualEntry("hev1", 3840, 2160, box("hvcC", hvcCRecord(10)), box("colr", []byte("nclx"), u16(9), u16(16), u16(9), []byte{0}))}),
			want: Info{Container: "mp4", Duration: 60, Video: &Video{Codec: "HEVC", Width: 3840, Height: 2160, BitDepth: 10, HDR: "HDR10"}},
		},
		{
			name: "AV1 10-bit",
			data: buildMP4("isom", 60, nil,
				mp4T{handler: "vide", w: 1920, h: 1080, entry: visualEntry("av01", 1920, 1080, box("av1C", []byte{0x81, 0x00, 0x4C, 0x00}))}),
			want: Info{Container: "mp4", Duration: 60, Video: &Video{Codec: "AV1", Width: 1920, Height: 1080, BitDepth: 10, HDR: "SDR"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := read(t, tt.data, "mp4")
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestOtherFilesReturnTheContainerOnly(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		ext  string
	}{
		{"text", bytes.Repeat([]byte("hello, this is not a video. "), 10), "avi"},
		{"short", []byte("abc"), "mkv"},
		{"empty", nil, "wmv"},
		{"ts packets", append([]byte{0x47, 0x40, 0x11, 0x10}, make([]byte, 600)...), "ts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := read(t, tt.data, tt.ext)
			if !reflect.DeepEqual(got, Info{Container: tt.ext}) {
				t.Errorf("got %+v", got)
			}
		})
	}
}

// A release that was never unpacked, saved under a video's name, is named by
// what it really is.
func TestArchivesUnderAVideoName(t *testing.T) {
	pad := make([]byte, 600)
	tests := []struct {
		name, ext, want string
		head            []byte
	}{
		{"rar5 as mkv", "mkv", "rar", []byte("Rar!\x1a\x07\x01\x00")},
		{"rar4 as mp4", "mp4", "rar", []byte("Rar!\x1a\x07\x00")},
		{"zip as mkv", "mkv", "zip", []byte("PK\x03\x04")},
		{"7z as avi", "avi", "7z", []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := read(t, append(append([]byte{}, tt.head...), pad...), tt.ext)
			if !reflect.DeepEqual(got, Info{Container: tt.want}) {
				t.Errorf("got %+v, want container %q", got, tt.want)
			}
			if !IsArchive(got.Container) {
				t.Errorf("IsArchive(%q) = false", got.Container)
			}
		})
	}
	for _, c := range []string{"matroska", "mp4", "mkv", "avi", "ts", ""} {
		if IsArchive(c) {
			t.Errorf("IsArchive(%q) = true", c)
		}
	}
	// "PK" alone, or "Rar" without its marker, is not enough.
	for _, head := range [][]byte{[]byte("PKZZ"), []byte("Rar?\x1a\x07")} {
		if got := read(t, append(append([]byte{}, head...), pad...), "mkv"); got.Container != "mkv" {
			t.Errorf("%q read as %+v", head, got)
		}
	}
}

// Hostile and damaged files must end quickly, without a panic, and without
// reading much.
func TestHostileFiles(t *testing.T) {
	valid := buildMKV("matroska", 60, []byte("frames"),
		mkvT{kind: mkTypeVideo, codec: "V_MPEGH/ISO/HEVC", w: 3840, h: 2160, depth: 10, transfer: 16},
		mkvT{kind: mkTypeAudio, codec: "A_AC3", lang: "eng", channels: 6})
	mp4 := buildMP4("isom", 600, []byte("frames"),
		mp4T{handler: "vide", w: 1920, h: 1080, entry: visualEntry("avc1", 1920, 1080, box("avcC", avcCRecord(100, 8)))},
		mp4T{handler: "soun", lang: "eng", entry: audioEntryBox("mp4a", 2, esdsAAC(2))})

	huge := func(id int64) []byte { // an element that claims 2^50 bytes
		return append(append(eid(id), 0x01, 0x04, 0, 0, 0, 0, 0, 0), []byte("x")...)
	}
	header := el(mkEBMLHeader, estr(mkDocType, "matroska"))
	selfBox := append([]byte{0, 0, 0, 8}, "free"...) // a box of just a header, repeated
	loop := append(append(box("ftyp", []byte("isom")), box("moov", bytes.Repeat(selfBox, 5000))...), box("moov", box("trak", box("trak")))...)
	var nested []byte
	for i := 0; i < 200; i++ {
		nested = box("trak", nested)
	}

	tests := []struct {
		name string
		data []byte
	}{
		{"huge Tracks size", append(append(header, el(mkSegment, huge(mkTracks))...), 0)},
		{"huge Segment size", append(append(header, eid(mkSegment)...), 0x01, 0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"unknown-size Segment and Cluster", append(append(header, eid(mkSegment)...), append([]byte{0xFF}, append(eid(mkCluster), 0xFF)...)...)},
		{"huge header", append(eid(mkEBMLHeader), 0x01, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"SeekHead pointing at itself", buildSelfSeek()},
		{"MP4 with a 64-bit size larger than the file", append(box("ftyp", []byte("isom")), []byte{0, 0, 0, 1, 'm', 'o', 'o', 'v', 0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}...)},
		{"MP4 with thousands of empty boxes", loop},
		{"MP4 with deeply nested boxes", append(box("ftyp", []byte("isom")), box("moov", nested)...)},
		{"MP4 box of size 0 at the top", append(box("ftyp", []byte("isom")), []byte{0, 0, 0, 0, 'm', 'o', 'o', 'v'}...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &countReader{r: bytes.NewReader(tt.data)}
			_, _ = Read(cr, int64(len(tt.data)), "mkv") // an error or a partial answer are both fine
			if cr.n > readBudget+4096 {
				t.Errorf("read %d bytes", cr.n)
			}
		})
	}

	t.Run("every cut-off of a real file", func(t *testing.T) {
		for _, data := range [][]byte{valid, mp4} {
			for n := 0; n < len(data); n++ {
				_, _ = Read(bytes.NewReader(data[:n]), int64(n), "mkv")
			}
		}
	})

	t.Run("random bytes changed in a real file", func(t *testing.T) {
		rng := rand.New(rand.NewSource(1))
		for _, base := range [][]byte{valid, mp4} {
			for i := 0; i < 3000; i++ {
				d := append([]byte(nil), base...)
				for j := 0; j < 1+rng.Intn(4); j++ {
					d[rng.Intn(len(d))] = byte(rng.Intn(256))
				}
				_, _ = Read(bytes.NewReader(d), int64(len(d)), "mp4")
			}
		}
	})

	t.Run("a big file with a track list that is too large", func(t *testing.T) {
		d := append(header, el(mkSegment, eid(mkTracks), esize(1<<30))...)
		if _, err := Read(bytes.NewReader(d), int64(len(d)), "mkv"); err == nil {
			t.Log("no error, but no reading either")
		}
	})
}

func buildSelfSeek() []byte {
	header := el(mkEBMLHeader, estr(mkDocType, "matroska"))
	seek := el(mkSeekHead, el(mkSeek, el(mkSeekID, eid(mkTracks)), euint(mkSeekPosition, 0)))
	return append(header, el(mkSegment, seek)...)
}

type countReader struct {
	r interface {
		ReadAt([]byte, int64) (int, error)
	}
	n int
}

func (c *countReader) ReadAt(p []byte, off int64) (int, error) {
	c.n += len(p)
	return c.r.ReadAt(p, off)
}

func FuzzRead(f *testing.F) {
	f.Add(buildMKV("matroska", 60, nil, mkvT{kind: mkTypeVideo, codec: "V_VP9", w: 1280, h: 720}))
	f.Add(buildMP4("isom", 60, nil, mp4T{handler: "soun", lang: "eng", entry: audioEntryBox("mp4a", 2, esdsAAC(2))}))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Read(bytes.NewReader(data), int64(len(data)), "mkv")
	})
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Movie (2020).mkv")
	data := buildMKV("matroska", 90, nil, mkvT{kind: mkTypeVideo, codec: "V_AV1", w: 1920, h: 1080})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := ReadFile(path)
	if err != nil || info.Video == nil || info.Video.Codec != "AV1" {
		t.Fatalf("got %+v, %v", info, err)
	}
	if _, err := ReadFile(filepath.Join(dir, "missing.mkv")); err == nil {
		t.Error("a missing file should be an error")
	}
	txt := filepath.Join(dir, "notes.avi")
	_ = os.WriteFile(txt, []byte("this is not a video file at all"), 0o644)
	if info, err := ReadFile(txt); err != nil || info.Container != "avi" {
		t.Errorf("got %+v, %v", info, err)
	}
}

func TestLanguage(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"eng"}, "en"},
		{[]string{"ger"}, "de"},
		{[]string{"deu"}, "de"},
		{[]string{"fre"}, "fr"},
		{[]string{"pt-BR"}, "pt"},
		{[]string{"en-US", "fra"}, "en"},
		{[]string{"", "dut"}, "nl"},
		{[]string{"und"}, ""},
		{[]string{"und", "jpn"}, "ja"},
		{[]string{"mul"}, ""},
		{[]string{"  ENG  "}, "en"},
		{[]string{"xyz"}, "xyz"},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := language(tt.in...); got != tt.want {
			t.Errorf("language(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolutionAndChannels(t *testing.T) {
	res := []struct {
		w, h int
		want string
	}{
		{3840, 2160, "2160p"}, {3840, 1608, "2160p"}, {4096, 1716, "2160p"}, {1920, 1080, "1080p"}, {1920, 800, "1080p"},
		{1440, 1080, "1080p"}, {1280, 720, "720p"}, {1280, 536, "720p"}, {720, 576, "576p"}, {720, 480, "480p"}, {640, 360, "480p"}, {0, 0, ""},
	}
	for _, tt := range res {
		if got := (Video{Width: tt.w, Height: tt.h}).Resolution(); got != tt.want {
			t.Errorf("%dx%d = %q, want %q", tt.w, tt.h, got, tt.want)
		}
	}
	ch := map[int]string{0: "", 1: "1.0", 2: "2.0", 6: "5.1", 8: "7.1"}
	for n, want := range ch {
		if got := ChannelLayout(n); got != want {
			t.Errorf("ChannelLayout(%d) = %q, want %q", n, got, want)
		}
	}
}
