package ebookconv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// KF8 books (AZW3): the text is a "raw markup" stream. Each XHTML file is a
// skeleton with fragments that go into it at given places, listed in two
// indexes (SKEL and FRAG). Styles and SVG sit in further "flows" (FDST), and
// links point at kindle:pos, kindle:embed and kindle:flow addresses.

type indexEntry struct {
	text string
	tags map[int][]int
}

// vwi reads a forward variable-width number: seven bits a byte, the high bit
// marking the last byte.
func vwi(b []byte, off int) (consumed, value int) {
	for off+consumed < len(b) {
		v := b[off+consumed]
		consumed++
		value = value<<7 | int(v&0x7F)
		if v&0x80 != 0 || consumed >= 5 {
			break
		}
	}
	return consumed, value
}

type tagDef struct{ tag, perEntry, mask, end int }

func bitsSet(v int) int {
	n := 0
	for ; v != 0; v &= v - 1 {
		n++
	}
	return n
}

// tagMap reads one index entry's values.
func tagMap(cbCount int, defs []tagDef, data []byte, start, end int) map[int][]int {
	type found struct{ tag, count, bytes, perEntry int }
	var list []found
	cbIndex := 0
	dataStart := start + cbCount
	for _, d := range defs {
		if d.end == 1 {
			cbIndex++
			continue
		}
		if start+cbIndex >= len(data) {
			break
		}
		cb := int(data[start+cbIndex])
		v := cb & d.mask
		if v == 0 {
			continue
		}
		if v == d.mask && bitsSet(d.mask) > 1 {
			n, val := vwi(data, dataStart)
			dataStart += n
			list = append(list, found{d.tag, -1, val, d.perEntry})
			continue
		}
		mask := d.mask
		for mask != 0 && mask&1 == 0 {
			mask >>= 1
			v >>= 1
		}
		list = append(list, found{d.tag, v, 0, d.perEntry})
	}
	out := map[int][]int{}
	for _, f := range list {
		var vals []int
		if f.count >= 0 {
			for range f.count * f.perEntry {
				if dataStart >= end || dataStart >= len(data) {
					break
				}
				n, val := vwi(data, dataStart)
				dataStart += n
				vals = append(vals, val)
			}
		} else {
			for used := 0; used < f.bytes && dataStart < len(data); {
				n, val := vwi(data, dataStart)
				dataStart += n
				used += n
				vals = append(vals, val)
			}
		}
		out[f.tag] = vals
	}
	return out
}

// readIndex reads a KF8 index (SKEL, FRAG...) starting at record first, and
// its strings table (CNCX).
func readIndex(p *pdb, first int) ([]indexEntry, map[int]string, error) {
	hdr := p.record(first)
	if len(hdr) < 56 || string(hdr[:4]) != "INDX" {
		return nil, nil, fmt.Errorf("no index at record %d", first)
	}
	word := func(b []byte, i int) int { return int(u32(b, 4+4*i)) }
	hlen, count, nctoc := word(hdr, 0), word(hdr, 5), word(hdr, 12)
	if hlen+12 > len(hdr) || string(hdr[hlen:hlen+4]) != "TAGX" || count < 0 || count > 10000 {
		return nil, nil, fmt.Errorf("bad index header")
	}
	firstEntry := int(u32(hdr, hlen+4))
	cbCount := int(u32(hdr, hlen+8))
	var defs []tagDef
	for i := 12; i+4 <= firstEntry && hlen+i+4 <= len(hdr); i += 4 {
		b := hdr[hlen+i:]
		defs = append(defs, tagDef{int(b[0]), int(b[1]), int(b[2]), int(b[3])})
	}
	cncx := map[int]string{}
	for j := 0; j < nctoc && j < 100; j++ {
		rec := p.record(first + count + 1 + j)
		for o := 0; o < len(rec); {
			if rec[o] == 0 {
				break
			}
			n, l := vwi(rec, o)
			if o+n+l > len(rec) {
				break
			}
			cncx[j*0x10000+o] = string(rec[o+n : o+n+l])
			o += n + l
		}
	}
	var out []indexEntry
	for i := first + 1; i <= first+count; i++ {
		d := p.record(i)
		if len(d) < 56 || string(d[:4]) != "INDX" {
			continue
		}
		idxt, n := word(d, 4), word(d, 5)
		if idxt+4+2*n > len(d) {
			continue
		}
		pos := make([]int, 0, n+1)
		for j := range n {
			pos = append(pos, int(binary.BigEndian.Uint16(d[idxt+4+2*j:])))
		}
		pos = append(pos, idxt)
		for j := range n {
			s, e := pos[j], pos[j+1]
			if s >= len(d) || s >= e {
				continue
			}
			tl := int(d[s])
			if s+1+tl > len(d) {
				continue
			}
			out = append(out, indexEntry{text: string(d[s+1 : s+1+tl]), tags: tagMap(cbCount, defs, d, s+1+tl, e)})
		}
	}
	return out, cncx, nil
}

// kf8Part is one rebuilt XHTML file, with where it came from in the raw text.
type kf8Part struct {
	name       string
	start, end int // its range in the raw markup
	text       []byte
}

type kf8Frag struct {
	insert, length int
}

var (
	kindlePos   = regexp.MustCompile(`kindle:pos:fid:([0-9A-Va-v]{4}):off:([0-9A-Va-v]{10})`)
	kindleEmbed = regexp.MustCompile(`kindle:embed:([0-9A-Va-v]{4})(?:\?mime=[A-Za-z0-9/+.-]*)?`)
	kindleFlow  = regexp.MustCompile(`kindle:flow:([0-9A-Va-v]{4})\?mime=([A-Za-z0-9/+.-]*)`)
	idAttr      = regexp.MustCompile(`<[^>]*\s(?:id|name)\s*=\s*["']([^"']+)["'][^>]*>`)
)

// base32 reads a KF8 base-32 number (a fid or an offset); -1 when it isn't
// one, or is too large to be a real position.
func base32(s string) int {
	n, err := strconv.ParseInt(strings.ToLower(s), 32, 64)
	if err != nil || n < 0 || n > math.MaxInt32 {
		return -1
	}
	return int(n)
}

func convertKF8(p *pdb, h *header, meta Metadata) (*epubBook, error) {
	raw, err := h.text(p)
	if err != nil {
		return nil, err
	}
	flows := [][]byte{raw}
	if h.fdst >= 0 {
		if rec := p.record(h.start + h.fdst); len(rec) >= 12 && string(rec[:4]) == "FDST" {
			n := int(u32(rec, 8))
			flows = flows[:0]
			for i := 0; i < n && 12+8*i+8 <= len(rec); i++ {
				s, e := int(u32(rec, 12+8*i)), int(u32(rec, 16+8*i))
				if s < 0 || e > len(raw) || s > e {
					break
				}
				flows = append(flows, raw[s:e])
			}
			if len(flows) == 0 {
				flows = [][]byte{raw}
			}
		}
	}
	text := flows[0]

	var parts []kf8Part
	var frags []kf8Frag
	if h.skelIndex >= 0 && h.fragIndex >= 0 {
		skel, _, errS := readIndex(p, h.start+h.skelIndex)
		frag, _, errF := readIndex(p, h.start+h.fragIndex)
		if errS == nil && errF == nil {
			for _, f := range frag {
				ins, _ := strconv.Atoi(f.text)
				l := 0
				if v := f.tags[6]; len(v) >= 2 {
					l = v[1]
				}
				frags = append(frags, kf8Frag{insert: ins, length: l})
			}
			fp := 0
			for si, s := range skel {
				pos := s.tags[6]
				cnt := s.tags[1]
				if len(pos) < 2 || len(cnt) < 1 || pos[0] < 0 || pos[0]+pos[1] > len(text) {
					continue
				}
				start := pos[0]
				base := start + pos[1]
				sk := append([]byte(nil), text[start:base]...)
				for i := 0; i < cnt[0] && fp < len(frags); i++ {
					f := frags[fp]
					fp++
					if base+f.length > len(text) {
						break
					}
					slice := text[base : base+f.length]
					ins := f.insert - start
					if ins < 0 || ins > len(sk) {
						ins = bytes.LastIndex(sk, []byte("</body>"))
						if ins < 0 {
							ins = len(sk)
						}
					}
					if insideTag(sk, ins) {
						if gt := bytes.IndexByte(sk[ins:], '>'); gt >= 0 {
							ins += gt + 1
						}
					}
					sk = append(sk[:ins], append(append([]byte(nil), slice...), sk[ins:]...)...)
					base += f.length
				}
				parts = append(parts, kf8Part{name: fmt.Sprintf("part%04d.xhtml", si), start: start, end: base, text: sk})
			}
		}
	}
	if len(parts) == 0 {
		parts = []kf8Part{{name: "part0000.xhtml", start: 0, end: len(text), text: text}}
	}

	// Where a kindle:pos link points: the part holding that place, and the
	// nearest id at or before it.
	target := func(fid, off int) string {
		if fid < 0 || fid >= len(frags) {
			return parts[0].name
		}
		pos := frags[fid].insert + off
		for _, pt := range parts {
			if pos < pt.start || pos >= pt.end {
				continue
			}
			n := pos - pt.start
			if n > len(pt.text) {
				n = len(pt.text)
			}
			if lt := bytes.IndexByte(pt.text[n:], '<'); lt == 0 || (lt > 0 && bytes.IndexByte(pt.text[n:], '>') < lt) {
				if gt := bytes.IndexByte(pt.text[n:], '>'); gt >= 0 {
					n += gt + 1
				}
			}
			if all := idAttr.FindAllSubmatch(pt.text[:n], -1); len(all) > 0 {
				return pt.name + "#" + string(all[len(all)-1][1])
			}
			return pt.name
		}
		return parts[0].name
	}

	book := &epubBook{meta: meta}
	first := firstResource(p, h)
	res := map[int]*resource{}
	getRes := func(n int) *resource {
		if r, ok := res[n]; ok {
			return r
		}
		rec := p.record(first + n - 1)
		var r *resource
		if ext, mime := sniffImage(rec); ext != "" {
			r = &resource{name: fmt.Sprintf("image%04d.%s", n, ext), mime: mime, data: rec}
			book.images = append(book.images, *r)
		} else if data, ext := fontRecord(rec); data != nil {
			mime := map[string]string{"otf": "font/otf", "woff": "font/woff", "ttf": "font/ttf"}[ext]
			r = &resource{name: fmt.Sprintf("font%04d.%s", n, ext), mime: mime, data: data}
			book.fonts = append(book.fonts, *r)
		}
		res[n] = r
		return r
	}
	embed := func(b []byte) []byte {
		return kindleEmbed.ReplaceAllFunc(b, func(m []byte) []byte {
			r := getRes(base32(string(kindleEmbed.FindSubmatch(m)[1])))
			if r == nil {
				return nil
			}
			if strings.HasPrefix(r.mime, "font/") {
				return []byte("../fonts/" + r.name)
			}
			return []byte("../images/" + r.name)
		})
	}
	flowFile := map[int]string{}
	flow := func(b []byte) []byte {
		return kindleFlow.ReplaceAllFunc(b, func(m []byte) []byte {
			sm := kindleFlow.FindSubmatch(m)
			n := base32(string(sm[1]))
			if n <= 0 || n >= len(flows) {
				return nil
			}
			if name, ok := flowFile[n]; ok {
				return []byte(name)
			}
			mime := string(sm[2])
			var name string
			switch {
			case strings.Contains(mime, "css"):
				css := embed(flows[n])
				book.styles = append(book.styles, stylesheet{name: fmt.Sprintf("flow%04d.css", n), data: css})
				name = "../styles/" + fmt.Sprintf("flow%04d.css", n)
			case strings.Contains(mime, "svg"):
				im := resource{name: fmt.Sprintf("flow%04d.svg", n), mime: "image/svg+xml", data: embed(flows[n])}
				book.images = append(book.images, im)
				name = "../images/" + im.name
			default:
				return nil
			}
			flowFile[n] = name
			return []byte(name)
		})
	}

	if c := h.exthInt(201); c >= 0 {
		if r := getRes(c + 1); r != nil && !strings.HasPrefix(r.mime, "font/") {
			book.cover = r.name
		}
	}
	for _, pt := range parts {
		t := kindlePos.ReplaceAllFunc(pt.text, func(m []byte) []byte {
			sm := kindlePos.FindSubmatch(m)
			link := target(base32(string(sm[1])), base32(string(sm[2])))
			if strings.HasPrefix(link, pt.name+"#") {
				link = link[len(pt.name):]
			}
			return []byte(link)
		})
		t = flow(embed(t))
		doc, err := html.Parse(bytes.NewReader(toUTF8(t, h.encoding)))
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		writeXHTML(&buf, doc, meta.Title, nil)
		book.chapters = append(book.chapters, chapter{name: pt.name, title: headingText(doc), data: buf.Bytes()})
	}
	if len(book.chapters) == 0 {
		return nil, fmt.Errorf("the book has no text")
	}
	return book, nil
}
