package books

import (
	"bytes"
	"testing"
)

// FuzzReadChapters feeds the chapter reader damaged and made-up MP4 files. A
// downloaded audiobook is untrusted, so whatever is inside must give chapters
// or an error, never a panic or a huge allocation.
func FuzzReadChapters(f *testing.F) {
	f.Add([]byte{})
	f.Add(box("ftyp", []byte("M4B ")))
	f.Add(append(box("ftyp", []byte("M4B ")), box("moov", box("udta", box("chpl", be32(0), []byte{2}, be64(0), []byte{1, 'a'}, be64(10000000), []byte{1, 'b'})))...))
	f.Add(box("moov", box("trak", box("tkhd", be32(0, 0, 0, 1, 0)), box("tref", box("chap", be32(2))), box("mdia", box("mdhd", be32(0, 0, 0, 1000, 0))))))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadChapters(bytes.NewReader(data), int64(len(data)))
	})
}
