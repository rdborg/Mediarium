package music

import (
	"bytes"
	"testing"
)

// FuzzParseAudioInfo reads damaged audio headers (FLAC, Ogg, MP4, MP3, ADTS).
func FuzzParseAudioInfo(f *testing.F) {
	f.Add([]byte("fLaC\x00\x00\x00\x22" + string(make([]byte, 34))))
	f.Add([]byte("OggS\x00\x02" + string(make([]byte, 40))))
	f.Add([]byte("\x00\x00\x00\x14ftypM4A \x00\x00\x00\x00M4A "))
	f.Add([]byte("\xff\xfb\x90\x00" + string(make([]byte, 200))))
	f.Add([]byte("ID3\x04\x00\x00\x00\x00\x00\x10"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseAudioInfo(bytes.NewReader(data), int64(len(data)))
	})
}
