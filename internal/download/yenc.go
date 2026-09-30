package download

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
	"strings"
)

// YencPart is one decoded yEnc-encoded article body, with enough header
// info to reassemble multi-part files in order.
type YencPart struct {
	Name       string
	PartBegin  int64 // 1-based byte offset within the whole file (0 if single-part)
	PartEnd    int64
	Size       int64 // declared total file size (from =ybegin, single-part; or =ysize on multi-part)
	Data       []byte
	CRC32      uint32
	CRC32Valid bool
}

// DecodeYenc decodes a single yEnc-encoded article body (the content of one
// NNTP ARTICLE/BODY response), per the yEnc 1.3 draft spec.
func DecodeYenc(r io.Reader) (*YencPart, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var part YencPart
	var buf bytes.Buffer
	sawBegin := false
	sawEnd := false
	sawCRC := false

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "=ybegin"):
			sawBegin = true
			part.Name = yencName(line)
			if size, err := strconv.ParseInt(yencField(line, "size"), 10, 64); err == nil {
				part.Size = size
			}
		case strings.HasPrefix(line, "=ypart"):
			if begin, err := strconv.ParseInt(yencField(line, "begin"), 10, 64); err == nil {
				part.PartBegin = begin
			}
			if end, err := strconv.ParseInt(yencField(line, "end"), 10, 64); err == nil {
				part.PartEnd = end
			}
		case strings.HasPrefix(line, "=yend"):
			sawEnd = true
			crcHex := yencField(line, "pcrc32")
			if crcHex == "" {
				crcHex = yencField(line, "crc32")
			}
			if crcHex != "" {
				if v, err := strconv.ParseUint(crcHex, 16, 32); err == nil {
					part.CRC32 = uint32(v)
					sawCRC = true
				}
			}
		default:
			if !sawBegin {
				continue // preamble before =ybegin (NNTP status lines etc.)
			}
			decodeYencLine(&buf, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan yenc body: %w", err)
	}
	if !sawBegin || !sawEnd {
		return nil, fmt.Errorf("malformed yenc article: missing =ybegin/=yend")
	}

	part.Data = buf.Bytes()
	if sawCRC {
		part.CRC32Valid = crc32.ChecksumIEEE(part.Data) == part.CRC32
	}
	return &part, nil
}

// decodeYencLine appends one decoded line's bytes to buf. yEnc escapes any
// byte whose encoded value collides with NUL/TAB/LF/CR/'=' by prefixing it
// with '=' and adding 64 before the usual +42 encoding offset.
func decodeYencLine(buf *bytes.Buffer, line string) {
	raw := []byte(line)
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b == '=' && i+1 < len(raw) {
			i++
			buf.WriteByte(raw[i] - 64 - 42)
			continue
		}
		buf.WriteByte(b - 42)
	}
}

// yencName reads the name= field of a =ybegin line. It is always the last
// field and runs to the end of the line, so a name with spaces in it stays
// whole. The name is only what the poster wrote: nothing here uses it as a path.
func yencName(line string) string {
	const key = " name="
	i := strings.Index(line, key)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(line[i+len(key):])
}

func yencField(line, key string) string {
	parts := strings.Fields(line)
	prefix := key + "="
	for _, p := range parts {
		if strings.HasPrefix(p, prefix) {
			return strings.TrimPrefix(p, prefix)
		}
	}
	return ""
}

// EncodeYenc produces a single-part yEnc article body for data, named name.
// Used to build local test fixtures (no live Usenet server needed) and can
// double as a reference implementation if the app ever needs to verify its
// own decoder against known-good output.
func EncodeYenc(name string, data []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "=ybegin line=128 size=%d name=%s\r\n", len(data), name)

	const lineWidth = 128
	col := 0
	for _, b := range data {
		enc := b + 42
		switch enc {
		case 0x00, 0x0A, 0x0D, 0x3D:
			buf.WriteByte('=')
			buf.WriteByte(enc + 64)
			col += 2
		default:
			buf.WriteByte(enc)
			col++
		}
		if col >= lineWidth {
			buf.WriteString("\r\n")
			col = 0
		}
	}
	if col > 0 {
		buf.WriteString("\r\n")
	}
	fmt.Fprintf(&buf, "=yend size=%d crc32=%08x\r\n", len(data), crc32.ChecksumIEEE(data))
	return buf.Bytes()
}

// maxUndeclaredFileBytes is how far into a file an article may say it belongs
// when the NZB gave no sizes to judge by.
const maxUndeclaredFileBytes = 1 << 40

// fileSizeLimit is the most a file of the NZB can plausibly hold: what its
// articles declare (they are encoded, so slightly bigger than what they
// decode to), with room to spare. An article that claims a place beyond it is
// lying, and writing it there would make a huge sparse file or fill the disk.
func fileSizeLimit(f NZBFile) int64 {
	var declared int64
	for _, s := range f.Segments {
		declared += s.Bytes
	}
	// Articles are tens to hundreds of KB. Sizes far below that were not
	// measured (a hand-made NZB), so they say nothing about the file.
	if declared <= 0 || declared/int64(len(f.Segments)) < 4096 {
		return maxUndeclaredFileBytes
	}
	return declared*2 + 16<<20
}

// segmentOffset is where in its file a decoded article is written: the
// article's own =ypart begin (1-based), or the start of the file for a
// single-part post. It refuses a place past limit.
func segmentOffset(part *YencPart, limit int64) (int64, error) {
	offset := part.PartBegin - 1
	if offset < 0 {
		offset = 0
	}
	if offset > limit || int64(len(part.Data)) > limit-offset {
		return 0, fmt.Errorf("the article says it belongs at byte %d, which is past the end of the file", offset)
	}
	return offset, nil
}
