package books

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Chapters inside an M4B (or M4A/MP4) audiobook file. A whole book in one
// file usually carries its chapter marks in one of two places: a QuickTime
// chapter track (a text track that another track points at with a "chap"
// reference; what iTunes and most converters write) or Nero's "chpl" list in
// the file's user data. Both are read here, from the file's index (the "moov"
// box) only, so no audio is decoded.

// Chapter is one chapter: its title and where it starts, in seconds.
type Chapter struct {
	Title string  `json:"title"`
	Start float64 `json:"start"`
}

const (
	maxMoovSize = 64 << 20 // the index of even a 60-hour book is far smaller
	maxChapters = 2000
)

var errNoChapters = errors.New("no chapters")

type mp4Box struct {
	typ  string
	data []byte // the payload, after the header
}

// boxes splits b into the boxes it holds.
func boxes(b []byte) []mp4Box {
	var out []mp4Box
	for len(b) >= 8 {
		size := uint64(binary.BigEndian.Uint32(b))
		typ := string(b[4:8])
		hdr := uint64(8)
		switch size {
		case 0:
			size = uint64(len(b))
		case 1:
			if len(b) < 16 {
				return out
			}
			size, hdr = binary.BigEndian.Uint64(b[8:16]), 16
		}
		if size < hdr || size > uint64(len(b)) {
			return out
		}
		out = append(out, mp4Box{typ: typ, data: b[hdr:size]})
		b = b[size:]
	}
	return out
}

func child(b []byte, typ string) []byte {
	for _, x := range boxes(b) {
		if x.typ == typ {
			return x.data
		}
	}
	return nil
}

func boxPath(b []byte, types ...string) []byte {
	for _, t := range types {
		if b = child(b, t); b == nil {
			return nil
		}
	}
	return b
}

// readMoov finds the "moov" box among the file's top-level boxes and reads it.
func readMoov(r io.ReaderAt, size int64) ([]byte, error) {
	var off int64
	hdr := make([]byte, 16)
	for off+8 <= size {
		if _, err := r.ReadAt(hdr[:8], off); err != nil {
			return nil, err
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr))
		typ := string(hdr[4:8])
		hdrLen := int64(8)
		switch boxSize {
		case 0:
			boxSize = size - off
		case 1:
			if _, err := r.ReadAt(hdr[8:16], off+8); err != nil {
				return nil, err
			}
			boxSize, hdrLen = int64(binary.BigEndian.Uint64(hdr[8:16])), 16
		}
		if boxSize < hdrLen || off+boxSize > size {
			return nil, errNoChapters
		}
		if typ == "moov" {
			n := boxSize - hdrLen
			if n > maxMoovSize {
				return nil, errNoChapters
			}
			buf := make([]byte, n)
			if _, err := r.ReadAt(buf, off+hdrLen); err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			return buf, nil
		}
		off += boxSize
	}
	return nil, errNoChapters
}

// ReadChapters reads the chapter marks of an MP4-family audio file (M4B,
// M4A). It answers nil when the file has none or fewer than two.
func ReadChapters(r io.ReaderAt, size int64) ([]Chapter, error) {
	moov, err := readMoov(r, size)
	if errors.Is(err, errNoChapters) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	chapters := quickTimeChapters(moov, r, size)
	if len(chapters) < 2 {
		chapters = neroChapters(moov)
	}
	if len(chapters) < 2 {
		return nil, nil
	}
	for i := range chapters {
		if strings.TrimSpace(chapters[i].Title) == "" {
			chapters[i].Title = "Chapter " + itoa(i+1)
		}
	}
	return chapters, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// neroChapters reads moov/udta/chpl: a count, then for each chapter its start
// (in 100-nanosecond units) and a title of up to 255 bytes.
func neroChapters(moov []byte) []Chapter {
	b := boxPath(moov, "udta", "chpl")
	if len(b) < 5 {
		return nil
	}
	version := b[0]
	b = b[4:]
	if version != 0 {
		if len(b) < 4 {
			return nil
		}
		b = b[4:]
	}
	if len(b) < 1 {
		return nil
	}
	n := int(b[0])
	b = b[1:]
	var out []Chapter
	for i := 0; i < n && len(b) >= 9; i++ {
		start := binary.BigEndian.Uint64(b)
		l := int(b[8])
		b = b[9:]
		if l > len(b) {
			break
		}
		out = append(out, Chapter{Title: cleanText(b[:l]), Start: float64(start) / 1e7})
		b = b[l:]
	}
	return out
}

type mp4Track struct {
	id        uint32
	chapRefs  []uint32
	timescale uint32
	stbl      []byte
}

func parseTrack(trak []byte) mp4Track {
	var t mp4Track
	if tkhd := child(trak, "tkhd"); len(tkhd) >= 4 {
		if tkhd[0] == 1 && len(tkhd) >= 24 {
			t.id = binary.BigEndian.Uint32(tkhd[20:])
		} else if len(tkhd) >= 16 {
			t.id = binary.BigEndian.Uint32(tkhd[12:])
		}
	}
	if chap := boxPath(trak, "tref", "chap"); len(chap) >= 4 {
		for i := 0; i+4 <= len(chap); i += 4 {
			t.chapRefs = append(t.chapRefs, binary.BigEndian.Uint32(chap[i:]))
		}
	}
	if mdhd := boxPath(trak, "mdia", "mdhd"); len(mdhd) >= 4 {
		if mdhd[0] == 1 && len(mdhd) >= 24 {
			t.timescale = binary.BigEndian.Uint32(mdhd[20:])
		} else if len(mdhd) >= 16 {
			t.timescale = binary.BigEndian.Uint32(mdhd[12:])
		}
	}
	t.stbl = boxPath(trak, "mdia", "minf", "stbl")
	return t
}

// quickTimeChapters reads the text track that another track names as its
// chapter track: each text sample is one chapter title, and the sample times
// are the chapter starts.
func quickTimeChapters(moov []byte, r io.ReaderAt, size int64) []Chapter {
	var tracks []mp4Track
	for _, x := range boxes(moov) {
		if x.typ == "trak" {
			tracks = append(tracks, parseTrack(x.data))
		}
	}
	var chapID uint32
	for _, t := range tracks {
		if len(t.chapRefs) > 0 {
			chapID = t.chapRefs[0]
			break
		}
	}
	if chapID == 0 {
		return nil
	}
	for _, t := range tracks {
		if t.id == chapID && t.timescale > 0 && t.stbl != nil {
			return textSamples(t, r, size)
		}
	}
	return nil
}

func u32s(b []byte, skip int) []uint32 {
	if len(b) < skip {
		return nil
	}
	b = b[skip:]
	out := make([]uint32, 0, len(b)/4)
	for i := 0; i+4 <= len(b); i += 4 {
		out = append(out, binary.BigEndian.Uint32(b[i:]))
	}
	return out
}

func textSamples(t mp4Track, r io.ReaderAt, size int64) []Chapter {
	stts := u32s(child(t.stbl, "stts"), 4) // count, then (sample count, delta) pairs
	stsz := u32s(child(t.stbl, "stsz"), 4) // fixed size, count, then sizes
	stsc := u32s(child(t.stbl, "stsc"), 4) // count, then (first chunk, samples per chunk, description) triples
	var offsets []uint64
	if co := u32s(child(t.stbl, "stco"), 4); len(co) > 0 {
		for _, o := range co[1:] {
			offsets = append(offsets, uint64(o))
		}
	} else if co64 := child(t.stbl, "co64"); len(co64) >= 8 {
		b := co64[8:]
		for i := 0; i+8 <= len(b); i += 8 {
			offsets = append(offsets, binary.BigEndian.Uint64(b[i:]))
		}
	}
	if len(stts) < 1 || len(stsz) < 2 || len(stsc) < 1 || len(offsets) == 0 {
		return nil
	}
	n := int(stsz[1])
	if n <= 0 || n > maxChapters {
		return nil
	}
	sizes := make([]uint32, n)
	for i := range sizes {
		if stsz[0] != 0 {
			sizes[i] = stsz[0]
		} else if 2+i < len(stsz) {
			sizes[i] = stsz[2+i]
		}
	}
	// Each sample's start time.
	starts := make([]uint64, 0, n)
	var at uint64
	for i := 1; i+1 < len(stts) && len(starts) < n; i += 2 {
		for c := uint32(0); c < stts[i] && len(starts) < n; c++ {
			starts = append(starts, at)
			at += uint64(stts[i+1])
		}
	}
	// Each sample's place in the file: chunks hold runs of samples.
	sampleOff := make([]uint64, 0, n)
	entries := int(stsc[0])
	for ci := range offsets {
		chunk := uint32(ci + 1)
		perChunk := uint32(0)
		for e := 0; e < entries && 1+e*3+2 < len(stsc); e++ {
			if stsc[1+e*3] <= chunk {
				perChunk = stsc[1+e*3+1]
			}
		}
		off := offsets[ci]
		for s := uint32(0); s < perChunk && len(sampleOff) < n; s++ {
			sampleOff = append(sampleOff, off)
			off += uint64(sizes[len(sampleOff)-1])
		}
	}
	var out []Chapter
	for i := 0; i < n && i < len(starts) && i < len(sampleOff); i++ {
		if sizes[i] < 2 || sizes[i] > 4096 || int64(sampleOff[i])+int64(sizes[i]) > size {
			continue
		}
		buf := make([]byte, sizes[i])
		if _, err := r.ReadAt(buf, int64(sampleOff[i])); err != nil && !errors.Is(err, io.EOF) {
			return nil
		}
		l := int(binary.BigEndian.Uint16(buf))
		if l > len(buf)-2 {
			l = len(buf) - 2
		}
		out = append(out, Chapter{Title: cleanText(buf[2 : 2+l]), Start: float64(starts[i]) / float64(t.timescale)})
	}
	return out
}

// cleanText reads a chapter title: UTF-8, or UTF-16 with a byte-order mark.
func cleanText(b []byte) string {
	if len(b) >= 2 && ((b[0] == 0xFE && b[1] == 0xFF) || (b[0] == 0xFF && b[1] == 0xFE)) {
		big := b[0] == 0xFE
		b = b[2:]
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			if big {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			} else {
				u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
			}
		}
		return strings.TrimSpace(string(utf16.Decode(u)))
	}
	s := string(b)
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	return strings.TrimSpace(strings.TrimRight(s, "\x00"))
}
