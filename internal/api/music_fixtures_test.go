package api_test

import (
	"bytes"
	"encoding/binary"
)

// Generators for small, valid audio files: headers, tags and filler only.

// flacFile builds a FLAC with a STREAMINFO block and Vorbis comments.
func flacFile(sampleRate, bps, seconds int, comments ...string) []byte {
	var b bytes.Buffer
	b.WriteString("fLaC")
	total := uint64(seconds) * uint64(sampleRate)
	d := make([]byte, 34)
	d[10] = byte(sampleRate >> 12)
	d[11] = byte(sampleRate >> 4)
	d[12] = byte(sampleRate&0xF)<<4 | 1<<1 | byte((bps-1)>>4)
	d[13] = byte((bps-1)&0xF)<<4 | byte(total>>32&0xF)
	binary.BigEndian.PutUint32(d[14:18], uint32(total))
	block := func(typ byte, last bool, payload []byte) {
		h := []byte{typ, byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload))}
		if last {
			h[0] |= 0x80
		}
		b.Write(h)
		b.Write(payload)
	}
	block(0, len(comments) == 0, d)
	if len(comments) > 0 {
		var vc bytes.Buffer
		binary.Write(&vc, binary.LittleEndian, uint32(4))
		vc.WriteString("test")
		binary.Write(&vc, binary.LittleEndian, uint32(len(comments)))
		for _, c := range comments {
			binary.Write(&vc, binary.LittleEndian, uint32(len(c)))
			vc.WriteString(c)
		}
		block(4, true, vc.Bytes())
	}
	b.Write(make([]byte, 2000))
	return b.Bytes()
}

// mp3File builds a constant-bitrate MPEG-1 Layer III file (bitrate index 14
// is 320 kbit/s, 13 is 256, 9 is 128) with an ID3v2.3 tag from frame/text pairs.
func mp3File(bitrateIndex int, frames [][2]string) []byte {
	var body bytes.Buffer
	for _, f := range frames {
		text := append([]byte{0}, []byte(f[1])...)
		body.WriteString(f[0])
		binary.Write(&body, binary.BigEndian, uint32(len(text)))
		body.Write([]byte{0, 0})
		body.Write(text)
	}
	n := body.Len()
	var b bytes.Buffer
	b.Write([]byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)})
	b.Write(body.Bytes())
	kbps := map[int]int{14: 320, 13: 256, 9: 128}[bitrateIndex]
	size := 144 * kbps * 1000 / 44100
	for i := 0; i < 10; i++ {
		f := make([]byte, size)
		f[0], f[1], f[2] = 0xFF, 0xFB, byte(bitrateIndex<<4)
		b.Write(f)
	}
	return b.Bytes()
}
