// Package ebookconv turns Kindle books (MOBI, AZW, AZW3) into EPUB, so
// Mediarium's own reader, which shows EPUB, can open them. It reads DRM-free
// files only. Older MOBI files keep their text as one HTML document that is
// split at its page breaks; KF8 files (AZW3, and the newer half of "both"
// MOBI files) are rebuilt from their skeleton and fragment index, the way the
// Kindle itself does. Nothing is decoded but text, images and fonts, so a
// conversion takes a fraction of a second.
package ebookconv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Errors a caller can tell apart.
var (
	ErrNotMobi     = errors.New("not a MOBI or AZW3 book")
	ErrDRM         = errors.New("the book is locked with DRM")
	ErrUnsupported = errors.New("this MOBI file uses a compression Mediarium can't read")
)

// pdb is a Palm database: the container of every MOBI-family book.
type pdb struct {
	data    []byte
	offsets []uint32
}

func readPDB(data []byte) (*pdb, error) {
	if len(data) < 78 {
		return nil, ErrNotMobi
	}
	if t := string(data[60:68]); t != "BOOKMOBI" && t != "TEXtREAd" {
		return nil, ErrNotMobi
	}
	n := int(binary.BigEndian.Uint16(data[76:]))
	if n == 0 || 78+8*n > len(data) {
		return nil, ErrNotMobi
	}
	p := &pdb{data: data, offsets: make([]uint32, n)}
	for i := range n {
		p.offsets[i] = binary.BigEndian.Uint32(data[78+8*i:])
	}
	return p, nil
}

// record returns record i, or nil when it doesn't exist or is out of range.
func (p *pdb) record(i int) []byte {
	if i < 0 || i >= len(p.offsets) {
		return nil
	}
	start := int(p.offsets[i])
	end := len(p.data)
	if i+1 < len(p.offsets) {
		end = int(p.offsets[i+1])
	}
	if start > end || end > len(p.data) {
		return nil
	}
	return p.data[start:end]
}

// header is the part of a book's first record (PalmDOC, MOBI and EXTH
// headers) the converter uses. Record numbers in it count from start, the
// record the header is in (KF8 halves of "both" files start part way).
type header struct {
	start        int
	compression  uint16
	textLength   uint32
	textRecords  int
	encoding     uint32 // 1252 or 65001
	version      uint32 // 8 for KF8
	firstImage   int    // the first resource record, from start
	extraFlags   uint16
	fdst         int // KF8: the FDST record, from start (-1 = none)
	fragIndex    int // KF8: the fragment index record (-1 = none)
	skelIndex    int // KF8: the skeleton index record (-1 = none)
	fullName     string
	resBase      int // set for the KF8 half of a "both" file: its resources are the MOBI 6 half's
	exth         map[uint32][][]byte
	huffRecord   int
	huffRecCount int
}

const none = 0xFFFFFFFF

func u32(b []byte, off int) uint32 {
	if off < 0 || off+4 > len(b) {
		return none
	}
	return binary.BigEndian.Uint32(b[off:])
}

func idx(v uint32) int {
	if v == none {
		return -1
	}
	return int(v)
}

func parseHeader(p *pdb, start int) (*header, error) {
	r0 := p.record(start)
	if len(r0) < 16 {
		return nil, ErrNotMobi
	}
	h := &header{start: start, fdst: -1, fragIndex: -1, skelIndex: -1, firstImage: -1, encoding: 1252, huffRecord: -1, resBase: -1}
	h.compression = binary.BigEndian.Uint16(r0)
	h.textLength = binary.BigEndian.Uint32(r0[4:])
	h.textRecords = int(binary.BigEndian.Uint16(r0[8:]))
	if enc := binary.BigEndian.Uint16(r0[12:]); enc != 0 {
		return nil, ErrDRM
	}
	if len(r0) < 24 || string(r0[16:20]) != "MOBI" {
		// A plain PalmDOC book: text records only.
		return h, nil
	}
	hlen := int(binary.BigEndian.Uint32(r0[20:]))
	h.encoding = u32(r0, 28)
	h.version = u32(r0, 36)
	h.firstImage = idx(u32(r0, 108))
	h.huffRecord = idx(u32(r0, 112))
	h.huffRecCount = int(u32(r0, 116))
	if drm := u32(r0, 168); drm != none && drm != 0 && u32(r0, 164) != none {
		return nil, ErrDRM
	}
	if off, n := int(u32(r0, 84)), int(u32(r0, 88)); off > 0 && n > 0 && off+n <= len(r0) {
		h.fullName = string(r0[off : off+n])
	}
	if hlen >= 0xE4 && len(r0) >= 244 && h.version >= 5 {
		h.extraFlags = binary.BigEndian.Uint16(r0[242:])
	}
	if h.version >= 8 {
		h.fdst = idx(u32(r0, 0xC0))
		h.fragIndex = idx(u32(r0, 0xF8))
		h.skelIndex = idx(u32(r0, 0xFC))
	}
	h.exth = map[uint32][][]byte{}
	if u32(r0, 128)&0x40 != 0 {
		e := 16 + hlen
		if e+12 <= len(r0) && string(r0[e:e+4]) == "EXTH" {
			count := int(u32(r0, e+8))
			pos := e + 12
			for range count {
				if pos+8 > len(r0) {
					break
				}
				typ, l := u32(r0, pos), int(u32(r0, pos+4))
				if l < 8 || pos+l > len(r0) {
					break
				}
				h.exth[typ] = append(h.exth[typ], r0[pos+8:pos+l])
				pos += l
			}
		}
	}
	return h, nil
}

func (h *header) exthString(typ uint32) string {
	if v := h.exth[typ]; len(v) > 0 {
		return string(v[0])
	}
	return ""
}

func (h *header) exthInt(typ uint32) int {
	if v := h.exth[typ]; len(v) > 0 && len(v[0]) == 4 {
		return int(binary.BigEndian.Uint32(v[0]))
	}
	return -1
}

// trailingSize reads a backward variable-width number ending at size.
func trailingSize(b []byte, size int) int {
	result, shift := 0, 0
	for size > 0 {
		v := b[size-1]
		result |= int(v&0x7F) << shift
		shift += 7
		size--
		if v&0x80 != 0 || shift >= 28 {
			break
		}
	}
	return result
}

// stripTrailing removes the extra data some MOBI files append to each text
// record (multibyte overlap and indexing hints).
func stripTrailing(rec []byte, flags uint16) []byte {
	size := len(rec)
	num := 0
	for f := flags >> 1; f != 0; f >>= 1 {
		if f&1 != 0 {
			num += trailingSize(rec, size-num)
		}
	}
	if flags&1 != 0 && size-num-1 >= 0 {
		num += int(rec[size-num-1]&0x3) + 1
	}
	if num > size {
		return nil
	}
	return rec[:size-num]
}

// palmDOC undoes PalmDOC's LZ77 compression.
func palmDOC(in []byte) []byte {
	out := make([]byte, 0, len(in)*2)
	for i := 0; i < len(in) && len(out) < maxPalmDOCRecord; {
		c := in[i]
		i++
		switch {
		case c >= 1 && c <= 8:
			end := i + int(c)
			if end > len(in) {
				end = len(in)
			}
			out = append(out, in[i:end]...)
			i = end
		case c < 0x80:
			out = append(out, c)
		case c >= 0xC0:
			out = append(out, ' ', c^0x80)
		default:
			if i >= len(in) {
				return out
			}
			pair := int(c)<<8 | int(in[i])
			i++
			dist := (pair >> 3) & 0x7FF
			n := (pair & 7) + 3
			if dist == 0 || dist > len(out) {
				continue
			}
			for range n {
				out = append(out, out[len(out)-dist])
			}
		}
	}
	return out
}

// Limits that keep a damaged or hostile file from using up memory: a
// PalmDOC record unpacks to 4 KB (16 KB allows for odd writers), and no
// real book has more text than maxBookText.
const (
	maxPalmDOCRecord = 16 << 10
	maxBookText      = 256 << 20
)

// text decompresses the book's text records into its raw markup.
func (h *header) text(p *pdb) ([]byte, error) {
	var buf bytes.Buffer
	for i := 1; i <= h.textRecords; i++ {
		if buf.Len() > maxBookText {
			return nil, fmt.Errorf("the book's text is larger than %d MB", maxBookText>>20)
		}
		rec := p.record(h.start + i)
		if rec == nil {
			break
		}
		rec = stripTrailing(rec, h.extraFlags)
		switch h.compression {
		case 1:
			buf.Write(rec)
		case 2:
			buf.Write(palmDOC(rec))
		default:
			return nil, ErrUnsupported
		}
	}
	out := buf.Bytes()
	if h.textLength > 0 && int(h.textLength) < len(out) {
		out = out[:h.textLength]
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the book has no text")
	}
	return out, nil
}
