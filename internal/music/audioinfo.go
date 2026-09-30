package music

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

// AudioInfo is what the header of an audio file says about its encoding: the
// codec, sample rate, bit depth of a lossless file and the average bitrate.
// It is read by small readers of our own because the tag library reads
// artist, album and title but nothing about the audio itself. Nothing here
// writes to a file.
type AudioInfo struct {
	Codec       string // flac, alac, mp3, aac, vorbis or opus
	SampleRate  int    // Hz, 0 if unknown
	BitDepth    int    // bits per sample of a lossless file, 0 if unknown or lossy
	Channels    int
	DurationSec float64 // 0 if unknown
	BitrateKbps int     // average, 0 if unknown
	VBR         bool    // variable bitrate (an MP3 with a Xing or VBRI header)
	Lossless    bool
}

// ErrUnknownAudio is returned for a file whose header is not one of the
// formats read here.
var ErrUnknownAudio = errors.New("not a recognised audio file")

// ReadAudioInfo reads the encoding details of an audio file from its header.
// The format is taken from the file's first bytes, not its extension.
func ReadAudioInfo(path string) (AudioInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return AudioInfo{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return AudioInfo{}, fmt.Errorf("stat %s: %w", path, err)
	}
	info, err := ParseAudioInfo(f, st.Size())
	if err != nil {
		return AudioInfo{}, fmt.Errorf("read audio header of %s: %w", path, err)
	}
	return info, nil
}

// ParseAudioInfo is ReadAudioInfo for any reader of size bytes.
func ParseAudioInfo(r io.ReaderAt, size int64) (AudioInfo, error) {
	start := skipID3v2(r, size)
	head := make([]byte, 12)
	n, _ := r.ReadAt(head, start)
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, []byte("fLaC")):
		return parseFLAC(r, size, start)
	case bytes.HasPrefix(head, []byte("OggS")):
		return parseOgg(r, size, start)
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return parseMP4(r, size)
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		if head[1]&0x06 == 0 { // layer bits 00: an AAC stream in ADTS frames
			return parseADTS(r, size, start)
		}
		return parseMP3(r, size, start)
	}
	return AudioInfo{}, ErrUnknownAudio
}

// skipID3v2 returns the offset of the first byte after an ID3v2 tag at the
// start of the file (0 when there is none).
func skipID3v2(r io.ReaderAt, size int64) int64 {
	h := make([]byte, 10)
	if n, _ := r.ReadAt(h, 0); n < 10 || string(h[:3]) != "ID3" {
		return 0
	}
	length := int64(h[6]&0x7F)<<21 | int64(h[7]&0x7F)<<14 | int64(h[8]&0x7F)<<7 | int64(h[9]&0x7F)
	end := 10 + length
	if h[5]&0x10 != 0 { // footer present
		end += 10
	}
	if end > size {
		return 0
	}
	return end
}

func kbps(bytes int64, seconds float64) int {
	if seconds <= 0 || bytes <= 0 {
		return 0
	}
	return int(float64(bytes) * 8 / seconds / 1000)
}

// ---- FLAC ----

func parseFLAC(r io.ReaderAt, size, start int64) (AudioInfo, error) {
	pos := start + 4
	var info AudioInfo
	found := false
	for {
		hdr := make([]byte, 4)
		if n, _ := r.ReadAt(hdr, pos); n < 4 {
			return AudioInfo{}, errors.New("truncated FLAC metadata")
		}
		last := hdr[0]&0x80 != 0
		typ := hdr[0] & 0x7F
		length := int64(hdr[1])<<16 | int64(hdr[2])<<8 | int64(hdr[3])
		if typ == 0 {
			if length < 34 {
				return AudioInfo{}, errors.New("FLAC STREAMINFO too short")
			}
			d := make([]byte, 34)
			if n, _ := r.ReadAt(d, pos+4); n < 34 {
				return AudioInfo{}, errors.New("truncated FLAC STREAMINFO")
			}
			info.Codec, info.Lossless = "flac", true
			info.SampleRate = int(d[10])<<12 | int(d[11])<<4 | int(d[12])>>4
			info.Channels = int(d[12]>>1&7) + 1
			info.BitDepth = int((d[12]&1)<<4|d[13]>>4) + 1
			total := int64(d[13]&0x0F)<<32 | int64(binary.BigEndian.Uint32(d[14:18]))
			if info.SampleRate > 0 && total > 0 {
				info.DurationSec = float64(total) / float64(info.SampleRate)
			}
			found = true
		}
		pos += 4 + length
		if last || pos >= size {
			break
		}
	}
	if !found {
		return AudioInfo{}, errors.New("FLAC file without STREAMINFO")
	}
	info.BitrateKbps = kbps(size-pos, info.DurationSec) // audio frames only, not tags or pictures
	return info, nil
}

// ---- MP3 ----

var (
	mp3BitratesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mp3BitratesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	mp3RatesV1    = [4]int{44100, 48000, 32000, 0}
	mp3RatesV2    = [4]int{22050, 24000, 16000, 0}
	mp3RatesV25   = [4]int{11025, 12000, 8000, 0}
)

type mp3Frame struct {
	bitrate    int // kbit/s from the header
	sampleRate int
	samples    int // samples per frame
	size       int // bytes, with padding
	side       int // side information bytes (where a Xing header starts after the 4-byte header)
	channels   int
	v1         bool
}

// parseMP3Header decodes a 4-byte Layer III frame header; ok is false for
// anything else.
func parseMP3Header(h []byte) (f mp3Frame, ok bool) {
	if len(h) < 4 || h[0] != 0xFF || h[1]&0xE0 != 0xE0 {
		return f, false
	}
	version := h[1] >> 3 & 3 // 0 = MPEG 2.5, 2 = MPEG 2, 3 = MPEG 1
	layer := h[1] >> 1 & 3   // 1 = Layer III
	if version == 1 || layer != 1 {
		return f, false
	}
	bi, si := h[2]>>4, h[2]>>2&3
	if bi == 0 || bi == 15 || si == 3 {
		return f, false
	}
	padding := int(h[2] >> 1 & 1)
	f.v1 = version == 3
	f.channels = 2
	if h[3]>>6 == 3 {
		f.channels = 1
	}
	switch version {
	case 3:
		f.bitrate, f.sampleRate, f.samples = mp3BitratesV1[bi], mp3RatesV1[si], 1152
		f.size = 144*f.bitrate*1000/f.sampleRate + padding
		f.side = map[bool]int{true: 17, false: 32}[f.channels == 1]
	default:
		f.bitrate, f.samples = mp3BitratesV2[bi], 576
		f.sampleRate = mp3RatesV2[si]
		if version == 0 {
			f.sampleRate = mp3RatesV25[si]
		}
		f.size = 72*f.bitrate*1000/f.sampleRate + padding
		f.side = map[bool]int{true: 9, false: 17}[f.channels == 1]
	}
	return f, f.size > 4
}

func parseMP3(r io.ReaderAt, size, start int64) (AudioInfo, error) {
	// Find the first frame: a header that decodes, followed by another one
	// where its size says (a run of 0xFF in the padding can look like a header).
	buf := make([]byte, 128*1024)
	n, _ := r.ReadAt(buf, start)
	buf = buf[:n]
	var first mp3Frame
	firstAt := int64(-1)
	for i := 0; i+4 <= len(buf); i++ {
		f, ok := parseMP3Header(buf[i : i+4])
		if !ok {
			continue
		}
		next := make([]byte, 4)
		if m, _ := r.ReadAt(next, start+int64(i)+int64(f.size)); m == 4 {
			if _, ok := parseMP3Header(next); !ok {
				continue
			}
		}
		first, firstAt = f, start+int64(i)
		break
	}
	if firstAt < 0 {
		return AudioInfo{}, errors.New("no MP3 frame found")
	}

	audioEnd := size
	if size >= 128 {
		tail := make([]byte, 3)
		if m, _ := r.ReadAt(tail, size-128); m == 3 && string(tail) == "TAG" {
			audioEnd -= 128 // ID3v1
		}
	}
	info := AudioInfo{Codec: "mp3", SampleRate: first.sampleRate, Channels: first.channels}
	audioBytes := audioEnd - firstAt

	// A Xing/Info tag (or VBRI) in the first frame carries the frame count.
	var frames, tagBytes int64
	body := make([]byte, 4+first.side+4+8+8)
	m, _ := r.ReadAt(body, firstAt)
	body = body[:m]
	if x := 4 + first.side; len(body) >= x+8 && (string(body[x:x+4]) == "Xing" || string(body[x:x+4]) == "Info") {
		flags := binary.BigEndian.Uint32(body[x+4 : x+8])
		info.VBR = string(body[x:x+4]) == "Xing"
		o := x + 8
		if flags&1 != 0 && len(body) >= o+4 {
			frames = int64(binary.BigEndian.Uint32(body[o : o+4]))
			o += 4
		}
		if flags&2 != 0 && len(body) >= o+4 {
			tagBytes = int64(binary.BigEndian.Uint32(body[o : o+4]))
		}
	} else {
		v := make([]byte, 18)
		if k, _ := r.ReadAt(v, firstAt+4+32); k == 18 && string(v[:4]) == "VBRI" {
			info.VBR = true
			tagBytes = int64(binary.BigEndian.Uint32(v[10:14]))
			frames = int64(binary.BigEndian.Uint32(v[14:18]))
		}
	}
	switch {
	case frames > 0:
		info.DurationSec = float64(frames) * float64(first.samples) / float64(first.sampleRate)
		if tagBytes > 0 {
			audioBytes = tagBytes
		}
		info.BitrateKbps = kbps(audioBytes, info.DurationSec)
	default:
		// Constant bitrate: the header's rate is the rate.
		info.BitrateKbps = first.bitrate
		info.DurationSec = float64(audioBytes) * 8 / float64(first.bitrate*1000)
	}
	return info, nil
}

// ---- AAC in ADTS frames (.aac) ----

func parseADTS(r io.ReaderAt, size, start int64) (AudioInfo, error) {
	rates := [...]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
	info := AudioInfo{Codec: "aac"}
	pos, frames := start, int64(0)
	h := make([]byte, 7)
	for pos+7 <= size {
		if n, _ := r.ReadAt(h, pos); n < 7 || h[0] != 0xFF || h[1]&0xF0 != 0xF0 {
			break
		}
		si := int(h[2] >> 2 & 15)
		length := int64(h[3]&3)<<11 | int64(h[4])<<3 | int64(h[5]>>5)
		if si >= len(rates) || length < 7 {
			break
		}
		if frames == 0 {
			info.SampleRate = rates[si]
			info.Channels = int(h[2]&1<<2 | h[3]>>6)
		}
		frames++
		pos += length
	}
	if frames == 0 || info.SampleRate == 0 {
		return AudioInfo{}, errors.New("no AAC frames found")
	}
	info.DurationSec = float64(frames) * 1024 / float64(info.SampleRate)
	info.BitrateKbps = kbps(pos-start, info.DurationSec)
	return info, nil
}

// ---- MP4 / M4A (AAC and Apple Lossless) ----

type mp4Box struct {
	typ        string
	start, end int64 // the whole box
	dataStart  int64 // after the header
}

// mp4Boxes lists the boxes found between from and to.
func mp4Boxes(r io.ReaderAt, from, to int64) []mp4Box {
	var out []mp4Box
	for pos := from; pos+8 <= to; {
		h := make([]byte, 16)
		n, _ := r.ReadAt(h, pos)
		if n < 8 {
			break
		}
		size := int64(binary.BigEndian.Uint32(h[:4]))
		hdr := int64(8)
		switch {
		case size == 1 && n >= 16:
			size, hdr = int64(binary.BigEndian.Uint64(h[8:16])), 16
		case size == 0:
			size = to - pos
		}
		if size < hdr || pos+size > to {
			if size >= hdr && pos+size > to { // a truncated last box: keep what is there
				size = to - pos
			} else {
				break
			}
		}
		out = append(out, mp4Box{typ: string(h[4:8]), start: pos, end: pos + size, dataStart: pos + hdr})
		pos += size
	}
	return out
}

func mp4Find(boxes []mp4Box, typ string) (mp4Box, bool) {
	for _, b := range boxes {
		if b.typ == typ {
			return b, true
		}
	}
	return mp4Box{}, false
}

func mp4Path(r io.ReaderAt, boxes []mp4Box, path ...string) (mp4Box, bool) {
	var b mp4Box
	for _, name := range path {
		var ok bool
		if b, ok = mp4Find(boxes, name); !ok {
			return mp4Box{}, false
		}
		boxes = mp4Boxes(r, b.dataStart, b.end)
	}
	return b, true
}

func parseMP4(r io.ReaderAt, size int64) (AudioInfo, error) {
	top := mp4Boxes(r, 0, size)
	moov, ok := mp4Find(top, "moov")
	if !ok {
		return AudioInfo{}, errors.New("MP4 file without a moov box")
	}
	moovBoxes := mp4Boxes(r, moov.dataStart, moov.end)

	var info AudioInfo
	if mvhd, ok := mp4Find(moovBoxes, "mvhd"); ok {
		d := make([]byte, 32)
		r.ReadAt(d, mvhd.dataStart)
		var timescale, duration uint64
		if d[0] == 1 {
			timescale, duration = uint64(binary.BigEndian.Uint32(d[20:24])), binary.BigEndian.Uint64(d[24:32])
		} else {
			timescale, duration = uint64(binary.BigEndian.Uint32(d[12:16])), uint64(binary.BigEndian.Uint32(d[16:20]))
		}
		if timescale > 0 {
			info.DurationSec = float64(duration) / float64(timescale)
		}
	}

	// The sample entry of the first audio track says which codec it is.
	found := false
	for _, trak := range moovBoxes {
		if trak.typ != "trak" {
			continue
		}
		stsd, ok := mp4Path(r, mp4Boxes(r, trak.dataStart, trak.end), "mdia", "minf", "stbl", "stsd")
		if !ok {
			continue
		}
		entryAt := stsd.dataStart + 8 // version and flags, entry count
		e := make([]byte, 40)
		if n, _ := r.ReadAt(e, entryAt); n < 36 {
			continue
		}
		entryEnd := entryAt + int64(binary.BigEndian.Uint32(e[:4]))
		switch string(e[4:8]) {
		case "mp4a":
			info.Codec = "aac"
		case "alac":
			info.Codec, info.Lossless = "alac", true
		default:
			continue
		}
		info.Channels = int(binary.BigEndian.Uint16(e[24:26]))
		info.SampleRate = int(binary.BigEndian.Uint16(e[32:34]))
		if info.Lossless {
			info.BitDepth = int(binary.BigEndian.Uint16(e[26:28]))
			// The cookie inside says the true depth and rate: an 'alac' box with
			// version and flags, then frame length (4), compatible version (1),
			// bit depth (1), ... and the sample rate in its last four bytes.
			for _, c := range mp4Boxes(r, entryAt+36, entryEnd) {
				if c.typ == "alac" {
					cfg := make([]byte, 24)
					if n, _ := r.ReadAt(cfg, c.dataStart+4); n == 24 {
						if cfg[5] > 0 {
							info.BitDepth = int(cfg[5])
						}
						// The 16.16 field of the sample entry cannot hold 96 kHz or
						// more; the cookie has the real rate.
						if sr := int(binary.BigEndian.Uint32(cfg[20:24])); sr > 0 {
							info.SampleRate = sr
						}
					}
				}
			}
		}
		found = true
		break
	}
	if !found {
		return AudioInfo{}, errors.New("MP4 file without AAC or ALAC audio")
	}
	// The average bitrate is the media data over the duration.
	var mdat int64
	for _, b := range top {
		if b.typ == "mdat" {
			mdat += b.end - b.dataStart
		}
	}
	info.BitrateKbps = kbps(mdat, info.DurationSec)
	return info, nil
}

// ---- Ogg (Vorbis and Opus), best effort ----

func parseOgg(r io.ReaderAt, size, start int64) (AudioInfo, error) {
	head := make([]byte, 27+255+40)
	n, _ := r.ReadAt(head, start)
	head = head[:n]
	if n < 28 || string(head[:4]) != "OggS" {
		return AudioInfo{}, errors.New("truncated Ogg header")
	}
	segs := int(head[26])
	off := 27 + segs
	if len(head) < off+19 {
		return AudioInfo{}, errors.New("truncated Ogg header")
	}
	pkt := head[off:]
	var info AudioInfo
	preSkip := int64(0)
	switch {
	case bytes.HasPrefix(pkt, []byte("\x01vorbis")) && len(pkt) >= 16:
		info.Codec = "vorbis"
		info.Channels = int(pkt[11])
		info.SampleRate = int(binary.LittleEndian.Uint32(pkt[12:16]))
	case bytes.HasPrefix(pkt, []byte("OpusHead")) && len(pkt) >= 12:
		info.Codec = "opus"
		info.Channels = int(pkt[9])
		preSkip = int64(binary.LittleEndian.Uint16(pkt[10:12]))
		info.SampleRate = 48000 // Opus always plays back at 48 kHz
	default:
		return AudioInfo{}, errors.New("Ogg stream is neither Vorbis nor Opus")
	}
	// The last page's granule position is the length in samples.
	tailLen := int64(65536)
	if tailLen > size {
		tailLen = size
	}
	tail := make([]byte, tailLen)
	m, _ := r.ReadAt(tail, size-tailLen)
	tail = tail[:m]
	if i := bytes.LastIndex(tail, []byte("OggS")); i >= 0 && len(tail) >= i+14 {
		granule := int64(binary.LittleEndian.Uint64(tail[i+6 : i+14]))
		if granule > preSkip && info.SampleRate > 0 {
			info.DurationSec = float64(granule-preSkip) / float64(info.SampleRate)
		}
	}
	info.BitrateKbps = kbps(size, info.DurationSec)
	return info, nil
}

// ---- Quality ----

// Tier classifies the encoding onto the quality ladder. It is Unknown when
// the header does not say enough (no bitrate, no known codec), which keeps
// such a file out of automatic upgrades.
//
// Lossless files are FLAC, or FLAC 24bit from 24 bits per sample. MP3: a
// constant bitrate from its rate (320, 256, else 192); a variable-bitrate
// file by its average, where LAME V0 averages about 220-260 kbps and is
// counted with 320. AAC is AAC-256 from 224 kbps and below that ranks with
// the lowest MP3. Ogg Vorbis and Opus are placed by average bitrate on the
// MP3 steps: a rough guide only.
func (a AudioInfo) Tier() Tier {
	switch a.Codec {
	case "flac", "alac":
		if a.BitDepth >= 24 {
			return TierFLAC24
		}
		return TierFLAC
	case "mp3":
		if a.BitrateKbps <= 0 {
			return TierUnknown
		}
		if a.VBR {
			switch {
			case a.BitrateKbps >= 220:
				return TierMP3320
			case a.BitrateKbps >= 190:
				return TierMP3256
			}
			return TierMP3192
		}
		switch {
		case a.BitrateKbps >= 300:
			return TierMP3320
		case a.BitrateKbps >= 240:
			return TierMP3256
		}
		return TierMP3192
	case "aac":
		switch {
		case a.BitrateKbps <= 0:
			return TierUnknown
		case a.BitrateKbps >= 224:
			return TierAAC256
		}
		return TierMP3192
	case "vorbis", "opus":
		switch {
		case a.BitrateKbps <= 0:
			return TierUnknown
		case a.BitrateKbps >= 256:
			return TierMP3320
		case a.BitrateKbps >= 200:
			return TierMP3256
		}
		return TierMP3192
	}
	return TierUnknown
}

// AlbumTier is the quality of a set of files: the lowest tier among those
// whose header could be read, so a folder with one MP3 among its FLACs is
// never rated as FLAC. It is Unknown when no file could be read (or there
// are none); a file that cannot be read is left out of the count rather
// than poisoning the album.
func AlbumTier(files []string) Tier {
	tiers := make([]Tier, 0, len(files))
	for _, f := range files {
		info, err := ReadAudioInfo(f)
		if err != nil {
			continue
		}
		if t := info.Tier(); t != TierUnknown {
			tiers = append(tiers, t)
		}
	}
	if len(tiers) == 0 {
		return TierUnknown
	}
	sort.Slice(tiers, func(i, j int) bool { return Rank(tiers[i]) < Rank(tiers[j]) })
	return tiers[0]
}
