package ebookconv

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The test books were made by Calibre from one three-chapter EPUB with two
// pictures and links between chapters: as an older MOBI, as AZW3 (KF8) and
// as a MOBI with both halves.
func TestConvert(t *testing.T) {
	cases := []struct {
		file     string
		chapters int // XHTML files, the generated table of contents included
		images   int
		styles   bool
	}{
		{"testdata/test.mobi", 4, 2, false},
		{"testdata/test.azw3", 4, 2, true},
		{"testdata/test-both.mobi", 4, 2, true},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		meta, err := Convert(data, &out)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		if meta.Title != "Test Book" || meta.Author != "Ann Writer" || meta.Language != "en" {
			t.Errorf("%s: metadata %+v", tc.file, meta)
		}
		checkEPUB(t, tc.file, out.Bytes(), tc.chapters, tc.images, tc.styles)
	}
}

var hrefPattern = regexp.MustCompile(`href="([^"#]*)#([^"]+)"`)

func checkEPUB(t *testing.T, name string, epub []byte, chapters, images int, styles bool) {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(epub), int64(len(epub)))
	if err != nil {
		t.Fatalf("%s: not a zip: %v", name, err)
	}
	if z.File[0].Name != "mimetype" || z.File[0].Method != zip.Store {
		t.Fatalf("%s: the mimetype entry must come first, uncompressed", name)
	}
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		files[f.Name] = string(b)
	}
	var text, img, css int
	ids := map[string]bool{}
	idPattern := regexp.MustCompile(`\sid="([^"]+)"`)
	for n, body := range files {
		switch {
		case strings.HasSuffix(n, ".xhtml"):
			text++
			for _, m := range idPattern.FindAllStringSubmatch(body, -1) {
				ids[strings.TrimPrefix(n, "OEBPS/text/")+"#"+m[1]] = true
			}
		case strings.HasPrefix(n, "OEBPS/images/"):
			img++
		case strings.HasPrefix(n, "OEBPS/styles/"):
			css++
		}
		if strings.HasSuffix(n, ".xhtml") || strings.HasSuffix(n, ".opf") || strings.HasSuffix(n, ".ncx") {
			d := xml.NewDecoder(strings.NewReader(body))
			for {
				if _, err := d.Token(); err != nil {
					if err != io.EOF {
						t.Errorf("%s: %s isn't well-formed XML: %v", name, n, err)
					}
					break
				}
			}
			if strings.Contains(body, "kindle:") || strings.Contains(body, `src=""`) {
				t.Errorf("%s: %s has an unresolved Kindle reference", name, n)
			}
		}
	}
	if text != chapters || img != images || (css > 0) != styles {
		t.Errorf("%s: %d chapters, %d images, %d styles; want %d, %d, styles %v", name, text, img, css, chapters, images, styles)
	}
	// Every link to another chapter lands on an id that exists.
	for n, body := range files {
		if !strings.HasSuffix(n, ".xhtml") {
			continue
		}
		self := strings.TrimPrefix(n, "OEBPS/text/")
		for _, m := range hrefPattern.FindAllStringSubmatch(body, -1) {
			file := m[1]
			if file == "" {
				file = self
			}
			if !ids[file+"#"+m[2]] {
				t.Errorf("%s: %s links to %s#%s, which doesn't exist", name, n, file, m[2])
			}
		}
	}
	if !strings.Contains(files["OEBPS/text/part0001.xhtml"]+files["OEBPS/text/part0000.xhtml"], "Café crème") {
		t.Errorf("%s: accented text didn't survive", name)
	}
}

func TestConvertRefuses(t *testing.T) {
	good, err := os.ReadFile("testdata/test.mobi")
	if err != nil {
		t.Fatal(err)
	}
	r0 := int(binary.BigEndian.Uint32(good[78:]))
	withDRM := append([]byte(nil), good...)
	binary.BigEndian.PutUint16(withDRM[r0+12:], 2)
	huffman := append([]byte(nil), good...)
	binary.BigEndian.PutUint16(huffman[r0:], 17480)

	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"not a book", []byte("this is not a book at all, just some text that is long enough to have a header......................"), ErrNotMobi},
		{"empty", nil, ErrNotMobi},
		{"DRM", withDRM, ErrDRM},
		{"Huffman compression", huffman, ErrUnsupported},
		{"cut short", good[:100], ErrNotMobi},
	}
	for _, tc := range cases {
		_, err := Convert(tc.data, io.Discard)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestPalmDOC(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"\x03\x80\x81\x82", "\x80\x81\x82"}, // literal run of 3
		{"a\xe2", "a b"},                     // space + 'b'
		{"abcd\x80\x21", "abcdabcd"},         // copy 4 bytes from 4 back
		{"ab\x80\x10", "ababa"},              // overlapping copy
		{"x\x80", "x"},                       // truncated pair
		{"\x08abc", "abc"},                   // literal run past the end
	}
	for _, tc := range cases {
		if got := string(palmDOC([]byte(tc.in))); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStripTrailing(t *testing.T) {
	// One trailing entry of size 3 ("\x83" = 3, last byte) and a multibyte tail of 1+1.
	rec := []byte("textXY\x00\x00\x83")
	if got := string(stripTrailing(rec, 0x2)); got != "textXY" {
		t.Errorf("trailing entry: %q", got)
	}
	if got := string(stripTrailing([]byte("text\x01\x01"), 0x1)); got != "text" {
		t.Errorf("multibyte: %q", got)
	}
	if got := stripTrailing([]byte{0xFF}, 0x2); got != nil && len(got) > 1 {
		t.Errorf("bad sizes must not panic: %q", got)
	}
}
