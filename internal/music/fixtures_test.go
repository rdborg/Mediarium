package music

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// Generators for small, valid audio files. They hold only headers, tags and
// filler, which is all the readers look at.

func writeFixture(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ---- FLAC ----

type flacSpec struct {
	sampleRate  int
	channels    int
	bps         int
	seconds     int
	comments    []string // "KEY=value"
	picture     bool
	audioBytes  int
	id3Prefix   bool
	noStreamInf bool
}

func flacBlock(typ byte, last bool, payload []byte) []byte {
	h := []byte{typ, byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload))}
	if last {
		h[0] |= 0x80
	}
	return append(h, payload...)
}

func buildFLAC(s flacSpec) []byte {
	var b bytes.Buffer
	if s.id3Prefix {
		b.Write(id3v2(nil))
	}
	b.WriteString("fLaC")
	total := uint64(s.seconds) * uint64(s.sampleRate)
	d := make([]byte, 34)
	d[10] = byte(s.sampleRate >> 12)
	d[11] = byte(s.sampleRate >> 4)
	d[12] = byte(s.sampleRate&0xF)<<4 | byte(s.channels-1)<<1 | byte((s.bps-1)>>4)
	d[13] = byte((s.bps-1)&0xF)<<4 | byte(total>>32&0xF)
	binary.BigEndian.PutUint32(d[14:18], uint32(total))
	type blk struct {
		typ     byte
		payload []byte
	}
	var blocks []blk
	if !s.noStreamInf {
		blocks = append(blocks, blk{0, d})
	}
	if len(s.comments) > 0 {
		var vc bytes.Buffer
		vendor := "fixture"
		binary.Write(&vc, binary.LittleEndian, uint32(len(vendor)))
		vc.WriteString(vendor)
		binary.Write(&vc, binary.LittleEndian, uint32(len(s.comments)))
		for _, c := range s.comments {
			binary.Write(&vc, binary.LittleEndian, uint32(len(c)))
			vc.WriteString(c)
		}
		blocks = append(blocks, blk{4, vc.Bytes()})
	}
	if s.picture {
		var pic bytes.Buffer
		mime, data := "image/jpeg", []byte("jpegbytes")
		for _, v := range []any{uint32(3), uint32(len(mime))} {
			binary.Write(&pic, binary.BigEndian, v)
		}
		pic.WriteString(mime)
		binary.Write(&pic, binary.BigEndian, uint32(0)) // description length
		for i := 0; i < 4; i++ {
			binary.Write(&pic, binary.BigEndian, uint32(0)) // width, height, depth, colours
		}
		binary.Write(&pic, binary.BigEndian, uint32(len(data)))
		pic.Write(data)
		blocks = append(blocks, blk{6, pic.Bytes()})
	}
	for i, bl := range blocks {
		b.Write(flacBlock(bl.typ, i == len(blocks)-1, bl.payload))
	}
	b.Write(make([]byte, s.audioBytes))
	return b.Bytes()
}

// ---- MP3 ----

// id3v2 builds an ID3v2.3 tag from frame id -> text pairs (in order).
func id3v2(frames [][2]string) []byte {
	var body bytes.Buffer
	for _, f := range frames {
		text := append([]byte{0}, []byte(f[1])...)
		body.WriteString(f[0])
		binary.Write(&body, binary.BigEndian, uint32(len(text)))
		body.Write([]byte{0, 0})
		body.Write(text)
	}
	n := body.Len()
	h := []byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
	return append(h, body.Bytes()...)
}

// mp3Frame44 builds an MPEG-1 Layer III stereo 44.1 kHz frame with the given
// bitrate index (14 = 320, 13 = 256, 9 = 128).
func mp3Frame44(bitrateIndex int, xing []byte) []byte {
	rates := map[int]int{14: 320, 13: 256, 11: 192, 9: 128}
	size := 144 * rates[bitrateIndex] * 1000 / 44100
	f := make([]byte, size)
	f[0], f[1], f[2], f[3] = 0xFF, 0xFB, byte(bitrateIndex<<4), 0x00
	copy(f[36:], xing) // 4-byte header + 32 bytes of side information
	return f
}

func xingHeader(kind string, frames, bytesInAudio uint32) []byte {
	b := []byte(kind)
	b = binary.BigEndian.AppendUint32(b, 3) // frames and bytes present
	b = binary.BigEndian.AppendUint32(b, frames)
	return binary.BigEndian.AppendUint32(b, bytesInAudio)
}

func buildMP3CBR(bitrateIndex, frames int, prefix []byte, id3v1 bool) []byte {
	var b bytes.Buffer
	b.Write(prefix)
	for i := 0; i < frames; i++ {
		b.Write(mp3Frame44(bitrateIndex, nil))
	}
	if id3v1 {
		tag := make([]byte, 128)
		copy(tag, "TAG")
		b.Write(tag)
	}
	return b.Bytes()
}

// buildMP3VBR builds a file whose Xing header claims an average bitrate of
// avgKbps over 30 seconds.
func buildMP3VBR(avgKbps int) []byte {
	const seconds = 30
	frames := uint32(seconds * 44100 / 1152)
	audio := uint32(avgKbps * 1000 / 8 * seconds)
	var b bytes.Buffer
	b.Write(mp3Frame44(9, xingHeader("Xing", frames, audio)))
	b.Write(mp3Frame44(9, nil))
	b.Write(mp3Frame44(9, nil))
	return b.Bytes()
}

// ---- MP4 ----

func makeBox(typ string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	return append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), append([]byte(typ), body...)...)
}

// buildM4A builds an MP4 with one audio track of the given codec ("mp4a" or
// "alac"), a duration in seconds and a media data box of mdatBytes.
func buildM4A(codec string, sampleRate, sampleSize, alacDepth, seconds, mdatBytes int) []byte {
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:16], 1000)
	binary.BigEndian.PutUint32(mvhd[16:20], uint32(seconds*1000))

	entry := make([]byte, 0, 64)
	entry = append(entry, make([]byte, 6)...) // reserved
	entry = append(entry, 0, 1)               // data reference index
	entry = append(entry, make([]byte, 8)...) // version, revision, vendor
	entry = append(entry, 0, 2)               // channels
	entry = binary.BigEndian.AppendUint16(entry, uint16(sampleSize))
	entry = append(entry, 0, 0, 0, 0) // compression id, packet size
	entry = binary.BigEndian.AppendUint32(entry, uint32(sampleRate)<<16)
	if codec == "alac" {
		cookie := append(make([]byte, 4), 0, 0, 0x10, 0)                   // version+flags, frame length
		cookie = append(cookie, 0, byte(alacDepth))                        // compatible version, bit depth
		cookie = append(cookie, make([]byte, 14)...)                       // pb, mb, kb, channels, max run, max frame, average bitrate
		cookie = binary.BigEndian.AppendUint32(cookie, uint32(sampleRate)) // the real sample rate
		entry = append(entry, makeBox("alac", cookie)...)
	}
	sampleEntry := makeBox(codec, entry)
	stsd := makeBox("stsd", []byte{0, 0, 0, 0, 0, 0, 0, 1}, sampleEntry)
	trak := makeBox("trak", makeBox("mdia", makeBox("minf", makeBox("stbl", stsd))))
	return bytes.Join([][]byte{
		makeBox("ftyp", []byte("M4A "), make([]byte, 4), []byte("M4A ")),
		makeBox("moov", makeBox("mvhd", mvhd), trak),
		makeBox("mdat", make([]byte, mdatBytes)),
	}, nil)
}

// ---- ADTS AAC ----

func buildADTS(frames, frameLen int) []byte {
	var b bytes.Buffer
	for i := 0; i < frames; i++ {
		h := []byte{0xFF, 0xF1, 0x50, byte(2&3)<<6 | byte(frameLen>>11&3), byte(frameLen >> 3), byte(frameLen&7)<<5 | 0x1F, 0xFC}
		b.Write(h)
		b.Write(make([]byte, frameLen-7))
	}
	return b.Bytes()
}

// ---- Ogg ----

func oggPage(headerType byte, granule int64, packet []byte) []byte {
	h := []byte("OggS")
	h = append(h, 0, headerType)
	h = binary.LittleEndian.AppendUint64(h, uint64(granule))
	h = append(h, make([]byte, 12)...) // serial, sequence, checksum
	h = append(h, 1, byte(len(packet)))
	return append(h, packet...)
}

// buildOgg builds an Ogg stream of the given codec ("vorbis" or "opus") that
// lasts seconds and is totalBytes long.
func buildOgg(codec string, seconds, totalBytes int) []byte {
	var first []byte
	var granule int64
	switch codec {
	case "vorbis":
		p := append([]byte{1}, "vorbis"...)
		p = append(p, 0, 0, 0, 0, 2)
		p = binary.LittleEndian.AppendUint32(p, 44100)
		p = append(p, make([]byte, 12)...)
		first, granule = p, int64(seconds)*44100
	default:
		p := []byte("OpusHead")
		p = append(p, 1, 2)
		p = binary.LittleEndian.AppendUint16(p, 312)
		p = binary.LittleEndian.AppendUint32(p, 44100)
		p = append(p, 0, 0, 0)
		first, granule = p, int64(seconds)*48000+312
	}
	a := oggPage(2, 0, first)
	z := oggPage(4, granule, []byte("last"))
	filler := totalBytes - len(a) - len(z)
	if filler < 0 {
		filler = 0
	}
	return bytes.Join([][]byte{a, make([]byte, filler), z}, nil)
}
