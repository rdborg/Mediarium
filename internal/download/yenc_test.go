package download

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"testing"
)

func TestYencRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"simple ascii", []byte("hello world, this is a test of yEnc encoding")},
		{"bytes needing escape", []byte{0x00, 0x0A, 0x0D, 0x3D, 0x01, 0xFF, 0x2A}},
		{"binary-ish", bytes.Repeat([]byte{0x00, 0x10, 0x20, 0xAA, 0xFF}, 50)},
		{"empty", []byte{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := EncodeYenc("test.bin", tc.data)
			part, err := DecodeYenc(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !bytes.Equal(part.Data, tc.data) {
				t.Fatalf("round trip mismatch:\nwant %v\ngot  %v", tc.data, part.Data)
			}
			if part.Name != "test.bin" {
				t.Errorf("unexpected name: %s", part.Name)
			}
			if !part.CRC32Valid {
				t.Errorf("expected valid crc32")
			}
		})
	}
}

func TestDecodeYencMultipart(t *testing.T) {
	raw := "=ybegin part=1 line=128 size=10 name=file.bin\r\n" +
		"=ypart begin=1 end=5\r\n" +
		encodeBodyOnly([]byte("ABCDE")) +
		"=yend size=5 pcrc32=deadbeef\r\n"

	part, err := DecodeYenc(bytes.NewReader([]byte(raw)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if part.PartBegin != 1 || part.PartEnd != 5 {
		t.Fatalf("unexpected part range: begin=%d end=%d", part.PartBegin, part.PartEnd)
	}
	if string(part.Data) != "ABCDE" {
		t.Fatalf("unexpected data: %q", part.Data)
	}
	if part.CRC32Valid {
		t.Fatalf("expected crc mismatch to be detected (pcrc32 is a deliberately wrong fixture value)")
	}
}

// Which checksum describes the article's own data: pcrc32 always, crc32 only
// in a post of one part (in a multi-part post it is the whole file's).
func TestDecodeYencChecksumKinds(t *testing.T) {
	body := encodeBodyOnly([]byte("ABCDE"))
	good := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte("ABCDE")))
	tests := []struct {
		name    string
		raw     string
		damaged bool
	}{
		{"single part, right crc32", "=ybegin line=128 size=5 name=a\r\n" + body + "=yend size=5 crc32=" + good + "\r\n", false},
		{"single part, wrong crc32", "=ybegin line=128 size=5 name=a\r\n" + body + "=yend size=5 crc32=deadbeef\r\n", true},
		{"single part, no checksum", "=ybegin line=128 size=5 name=a\r\n" + body + "=yend size=5\r\n", false},
		{"part, right pcrc32", "=ybegin part=1 line=128 size=10 name=a\r\n=ypart begin=1 end=5\r\n" + body + "=yend size=5 part=1 pcrc32=" + good + " crc32=deadbeef\r\n", false},
		{"part, wrong pcrc32", "=ybegin part=1 line=128 size=10 name=a\r\n=ypart begin=1 end=5\r\n" + body + "=yend size=5 part=1 pcrc32=deadbeef\r\n", true},
		{"cut short, no checksum, size from =yend", "=ybegin line=128 size=9 name=a\r\n" + body + "=yend size=9\r\n", true},
		{"cut short, no checksum, size from =ypart", "=ybegin part=1 line=128 size=20 name=a\r\n=ypart begin=1 end=9\r\n" + body + "=yend part=1\r\n", true},
		{"empty with a checksum of nothing", "=ybegin part=1 line=128 size=20 name=a\r\n=ypart begin=1 end=5\r\n=yend size=5 pcrc32=00000000\r\n", true},
		{"empty, no checksum", "=ybegin line=128 size=5 name=a\r\n=yend size=5\r\n", true},
		{"right checksum settles an odd header", "=ybegin part=1 line=128 size=20 name=a\r\n=ypart begin=1 end=9\r\n" + body + "=yend size=9 pcrc32=" + good + "\r\n", false},
		{"part, only the file's crc32", "=ybegin part=1 line=128 size=10 name=a\r\n=ypart begin=1 end=5\r\n" + body + "=yend size=5 part=1 crc32=deadbeef\r\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			part, err := DecodeYenc(bytes.NewReader([]byte(tt.raw)))
			if err != nil {
				t.Fatal(err)
			}
			if part.Damaged() != tt.damaged {
				t.Fatalf("Damaged() = %v, want %v (part %+v)", part.Damaged(), tt.damaged, part)
			}
		})
	}
}

func TestDecodeYencMalformed(t *testing.T) {
	if _, err := DecodeYenc(bytes.NewReader([]byte("not yenc at all\r\n"))); err == nil {
		t.Fatal("expected error for missing =ybegin/=yend")
	}
}

// encodeBodyOnly reuses EncodeYenc's line-encoding logic to build just the
// body lines for a hand-written multipart fixture header/footer above.
func encodeBodyOnly(data []byte) string {
	full := EncodeYenc("file.bin", data)
	lines := bytes.Split(full, []byte("\r\n"))
	// Drop the =ybegin (first) and =yend (last, plus trailing empty split).
	body := lines[1 : len(lines)-2]
	var out bytes.Buffer
	for _, l := range body {
		out.Write(l)
		out.WriteString("\r\n")
	}
	return out.String()
}
