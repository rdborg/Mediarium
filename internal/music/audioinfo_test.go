package music

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAudioInfoAndTier(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		data     []byte
		codec    string
		rate     int
		depth    int
		kbps     int // expected average, allowing 3% either way (0 = not checked)
		vbr      bool
		wantTier Tier
	}{
		{"flac 16 bit", buildFLAC(flacSpec{sampleRate: 44100, channels: 2, bps: 16, seconds: 10, audioBytes: 500000}), "flac", 44100, 16, 400, false, TierFLAC},
		{"flac 24 bit 96k", buildFLAC(flacSpec{sampleRate: 96000, channels: 2, bps: 24, seconds: 10, audioBytes: 1500000}), "flac", 96000, 24, 1200, false, TierFLAC24},
		{"flac behind an ID3 tag", buildFLAC(flacSpec{sampleRate: 48000, channels: 2, bps: 24, seconds: 5, id3Prefix: true, audioBytes: 1000}), "flac", 48000, 24, 0, false, TierFLAC24},
		{"flac with a picture is not counted as audio", buildFLAC(flacSpec{sampleRate: 44100, channels: 2, bps: 16, seconds: 10, picture: true, comments: []string{"TITLE=x"}, audioBytes: 500000}), "flac", 44100, 16, 400, false, TierFLAC},

		{"mp3 cbr 320", buildMP3CBR(14, 40, nil, false), "mp3", 44100, 0, 320, false, TierMP3320},
		{"mp3 cbr 256 with tags around it", buildMP3CBR(13, 40, id3v2([][2]string{{"TIT2", "x"}}), true), "mp3", 44100, 0, 256, false, TierMP3256},
		{"mp3 cbr 192", buildMP3CBR(11, 40, nil, false), "mp3", 44100, 0, 192, false, TierMP3192},
		{"mp3 cbr 128 ranks with the lowest", buildMP3CBR(9, 40, nil, false), "mp3", 44100, 0, 128, false, TierMP3192},
		{"mp3 vbr V0 (about 245)", buildMP3VBR(245), "mp3", 44100, 0, 245, true, TierMP3320},
		{"mp3 vbr V1 (about 205)", buildMP3VBR(205), "mp3", 44100, 0, 205, true, TierMP3256},
		{"mp3 vbr V2 (about 175)", buildMP3VBR(175), "mp3", 44100, 0, 175, true, TierMP3192},

		{"aac 256", buildM4A("mp4a", 44100, 16, 0, 10, 320000), "aac", 44100, 0, 256, false, TierAAC256},
		{"aac 128", buildM4A("mp4a", 44100, 16, 0, 10, 160000), "aac", 44100, 0, 128, false, TierMP3192},
		{"alac 16 bit", buildM4A("alac", 44100, 16, 16, 10, 400000), "alac", 44100, 16, 0, false, TierFLAC},
		{"alac 24 bit (the cookie knows)", buildM4A("alac", 96000, 16, 24, 10, 900000), "alac", 96000, 24, 0, false, TierFLAC24},
		{"aac in adts frames", buildADTS(200, 700), "aac", 44100, 0, 241, false, TierAAC256},

		{"vorbis 300 kbps", buildOgg("vorbis", 10, 375000), "vorbis", 44100, 0, 300, false, TierMP3320},
		{"vorbis 220 kbps", buildOgg("vorbis", 10, 275000), "vorbis", 44100, 0, 220, false, TierMP3256},
		{"opus 128 kbps", buildOgg("opus", 10, 160000), "opus", 48000, 0, 128, false, TierMP3192},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := ReadAudioInfo(writeFixture(t, dir, "f", tc.data))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if info.Codec != tc.codec || info.SampleRate != tc.rate || info.BitDepth != tc.depth || info.VBR != tc.vbr {
				t.Fatalf("got %+v", info)
			}
			if tc.kbps > 0 {
				if lo, hi := tc.kbps*97/100, tc.kbps*103/100+1; info.BitrateKbps < lo || info.BitrateKbps > hi {
					t.Fatalf("bitrate %d kbps, want about %d (%+v)", info.BitrateKbps, tc.kbps, info)
				}
			}
			if got := info.Tier(); got != tc.wantTier {
				t.Fatalf("tier %s, want %s (%+v)", got, tc.wantTier, info)
			}
		})
	}
}

func TestFLACDuration(t *testing.T) {
	p := writeFixture(t, t.TempDir(), "a.flac", buildFLAC(flacSpec{sampleRate: 44100, channels: 2, bps: 16, seconds: 200, audioBytes: 10}))
	info, err := ReadAudioInfo(p)
	if err != nil || info.DurationSec < 199.9 || info.DurationSec > 200.1 || info.Channels != 2 {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestReadAudioInfoRefusesWhatItCannotRead(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"text":           []byte("this is not audio at all"),
		"empty":          nil,
		"flac no info":   buildFLAC(flacSpec{noStreamInf: true, comments: []string{"TITLE=x"}}),
		"truncated flac": []byte("fLaC\x00\x00"),
		"mp4 no moov":    makeBox("ftyp", []byte("M4A "), make([]byte, 4)),
		"ogg other":      oggPage(2, 0, []byte("Speex   and more bytes here")),
		"mp3 sync only":  {0xFF, 0xFB, 0x00, 0x00, 0, 0, 0, 0},
	} {
		if _, err := ReadAudioInfo(writeFixture(t, dir, name, data)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := ReadAudioInfo(filepath.Join(dir, "missing.flac")); err == nil {
		t.Error("a missing file is an error")
	}
	_, err := ReadAudioInfo(writeFixture(t, dir, "junk", []byte("junk junk junk junk")))
	if !errors.Is(err, ErrUnknownAudio) {
		t.Errorf("junk should be ErrUnknownAudio, got %v", err)
	}
	if got := (AudioInfo{}).Tier(); got != TierUnknown {
		t.Errorf("empty info: %s", got)
	}
	if got := (AudioInfo{Codec: "mp3"}).Tier(); got != TierUnknown {
		t.Errorf("mp3 without a bitrate: %s", got)
	}
}

func TestAlbumTierIsTheLowestReadableFile(t *testing.T) {
	dir := t.TempDir()
	flac := writeFixture(t, dir, "1.flac", buildFLAC(flacSpec{sampleRate: 44100, channels: 2, bps: 16, seconds: 10, audioBytes: 10}))
	flac24 := writeFixture(t, dir, "2.flac", buildFLAC(flacSpec{sampleRate: 96000, channels: 2, bps: 24, seconds: 10, audioBytes: 10}))
	mp3 := writeFixture(t, dir, "3.mp3", buildMP3CBR(14, 20, nil, false))
	bad := writeFixture(t, dir, "4.flac", []byte("corrupt"))

	for name, tc := range map[string]struct {
		files []string
		want  Tier
	}{
		"all flac":                      {[]string{flac, flac24}, TierFLAC},
		"only 24 bit":                   {[]string{flac24}, TierFLAC24},
		"an mp3 among flac":             {[]string{flac, mp3}, TierMP3320},
		"an unreadable file is ignored": {[]string{flac24, bad}, TierFLAC24},
		"nothing readable":              {[]string{bad}, TierUnknown},
		"no files":                      {nil, TierUnknown},
	} {
		if got := AlbumTier(tc.files); got != tc.want {
			t.Errorf("%s: %s, want %s", name, got, tc.want)
		}
	}
	_ = os.Remove(bad)
}
