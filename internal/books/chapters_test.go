package books

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func box(typ string, parts ...[]byte) []byte {
	payload := bytes.Join(parts, nil)
	b := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(b, uint32(8+len(payload)))
	copy(b[4:], typ)
	return append(b, payload...)
}

func be32(vs ...uint32) []byte {
	b := make([]byte, 4*len(vs))
	for i, v := range vs {
		binary.BigEndian.PutUint32(b[4*i:], v)
	}
	return b
}

func be64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// textSample is a QuickTime text sample: a 2-byte length and the text.
func textSample(s string) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(len(s)))
	return append(b, s...)
}

func tkhd(id uint32) []byte {
	return box("tkhd", be32(0, 0, 0, id, 0))
}

func mdhd(timescale uint32) []byte {
	return box("mdhd", be32(0, 0, 0, timescale, 0))
}

// fakeM4B builds a small file: an "ftyp" box, the samples (in "mdat") and a
// "moov" with an audio track pointing at a text chapter track, and/or a Nero
// chapter list.
func fakeM4B(t *testing.T, titles []string, starts []uint32, timescale uint32, withQT, withNero bool, neroTitles []string) []byte {
	t.Helper()
	ftyp := box("ftyp", []byte("M4B "), be32(0))
	var samples []byte
	var sizes []uint32
	for _, s := range titles {
		ts := textSample(s)
		sizes = append(sizes, uint32(len(ts)))
		samples = append(samples, ts...)
	}
	mdatOffset := uint32(len(ftyp) + 8)
	mdat := box("mdat", samples)

	var moovParts [][]byte
	moovParts = append(moovParts, box("trak", tkhd(1), box("tref", box("chap", be32(2))), box("mdia", mdhd(44100))))
	if withQT {
		// stts: one (count 1, delta) entry per chapter, from the starts.
		var stts []uint32
		for i := range starts {
			end := starts[i] + 100
			if i+1 < len(starts) {
				end = starts[i+1]
			}
			stts = append(stts, 1, end-starts[i])
		}
		stbl := box("stbl",
			box("stts", be32(0, uint32(len(starts))), be32(stts...)),
			box("stsz", be32(0, 0, uint32(len(sizes))), be32(sizes...)),
			box("stsc", be32(0, 1, 1, uint32(len(sizes)), 1)),
			box("stco", be32(0, 1, mdatOffset)),
		)
		moovParts = append(moovParts, box("trak", tkhd(2), box("mdia", mdhd(timescale), box("minf", stbl))))
	}
	if withNero {
		chpl := []byte{0, 0, 0, 0, byte(len(neroTitles))}
		for i, s := range neroTitles {
			chpl = append(chpl, be64(uint64(i)*600*10_000_000)...)
			chpl = append(chpl, byte(len(s)))
			chpl = append(chpl, s...)
		}
		moovParts = append(moovParts, box("udta", box("chpl", chpl)))
	}
	return bytes.Join([][]byte{ftyp, mdat, box("moov", moovParts...)}, nil)
}

func TestReadChapters(t *testing.T) {
	cases := []struct {
		name   string
		file   []byte
		titles string
		starts []float64
	}{
		{
			name:   "QuickTime chapter track",
			file:   fakeM4B(t, []string{"Opening Credits", "Chapter 1", "Chapter 2"}, []uint32{0, 1000, 61000}, 1000, true, false, nil),
			titles: "Opening Credits|Chapter 1|Chapter 2",
			starts: []float64{0, 1, 61},
		},
		{
			name:   "Nero chapter list",
			file:   fakeM4B(t, nil, nil, 1000, false, true, []string{"Prologue", "Part One", "Part Two"}),
			titles: "Prologue|Part One|Part Two",
			starts: []float64{0, 600, 1200},
		},
		{
			name:   "QuickTime wins over Nero",
			file:   fakeM4B(t, []string{"A", "B"}, []uint32{0, 500}, 100, true, true, []string{"X", "Y"}),
			titles: "A|B",
			starts: []float64{0, 5},
		},
		{
			name:   "an empty title gets a number",
			file:   fakeM4B(t, []string{"First", ""}, []uint32{0, 30}, 1, true, false, nil),
			titles: "First|Chapter 2",
			starts: []float64{0, 30},
		},
	}
	for _, tc := range cases {
		got, err := ReadChapters(bytes.NewReader(tc.file), int64(len(tc.file)))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var titles []string
		for i, c := range got {
			titles = append(titles, c.Title)
			if i < len(tc.starts) && c.Start != tc.starts[i] {
				t.Errorf("%s: chapter %d starts at %v, want %v", tc.name, i, c.Start, tc.starts[i])
			}
		}
		if s := joinTitles(titles); s != tc.titles {
			t.Errorf("%s: titles %q, want %q", tc.name, s, tc.titles)
		}
	}
}

func joinTitles(t []string) string {
	var b bytes.Buffer
	for i, s := range t {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(s)
	}
	return b.String()
}

func TestReadChaptersWithoutChapters(t *testing.T) {
	cases := map[string][]byte{
		"no chapters":    fakeM4B(t, nil, nil, 1000, false, false, nil),
		"one chapter":    fakeM4B(t, []string{"Only"}, []uint32{0}, 1000, true, false, nil),
		"not an mp4":     []byte("ID3\x04\x00\x00\x00\x00\x00\x00 not an mp4 at all"),
		"empty":          {},
		"truncated moov": append(box("ftyp", []byte("M4B ")), 0, 0, 0x10, 0, 'm', 'o', 'o', 'v', 1, 2, 3),
	}
	for name, file := range cases {
		got, err := ReadChapters(bytes.NewReader(file), int64(len(file)))
		if err != nil || got != nil {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
}

func TestCleanText(t *testing.T) {
	cases := map[string]string{
		"Chapter 1":                       "Chapter 1",
		"  padded \x00\x00":               "padded",
		"\xfe\xff\x00C\x00h\x00a\x00p":    "Chap",
		"\xff\xfeC\x00a\x00f\x00\xe9\x00": "Café",
		"bad \xff utf8":                   "bad  utf8",
	}
	for in, want := range cases {
		if got := cleanText([]byte(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
