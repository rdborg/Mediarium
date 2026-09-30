package download

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The download package reads bytes written by strangers (NZB files from
// indexers, subjects and article bodies from Usenet posters, status lines from
// a news server). These fuzz targets check that odd input never panics, never
// gets more memory than it is worth, and can never name a file outside the
// download folder.

func checkOutputName(t *testing.T, name string) {
	t.Helper()
	if name == "" || name == "." || name == ".." {
		t.Fatalf("output name %q is not a file name", name)
	}
	if strings.ContainsAny(name, "/\\") {
		t.Fatalf("output name %q holds a path separator", name)
	}
	if strings.Contains(name, "..") && strings.Trim(name, ". ") == "" {
		t.Fatalf("output name %q is only dots", name)
	}
	if len(name) > 255 {
		t.Fatalf("output name is %d bytes long", len(name))
	}
	if !utf8.ValidString(name) {
		t.Fatalf("output name %q is not valid UTF-8", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("output name %q holds a control character", name)
		}
	}
	if strings.HasPrefix(strings.ToLower(name), ".mediarium-") {
		t.Fatalf("output name %q collides with the download's own files", name)
	}
}

// FuzzOutputFileNames: whatever a post's subject says, the name it is saved
// under is a plain, short, unique file name.
func FuzzOutputFileNames(f *testing.F) {
	for _, s := range []string{
		`[1/50] "Movie.2024.mkv" yEnc (1/500)`, `"../../etc/passwd"`, `".."`, `"."`, `"..."`, `""`, ``, `"a/b\c.mkv"`,
		`"CON"`, `".mediarium-progress"`, `".MEDIARIUM-progress.tmp"`, `"` + strings.Repeat("a", 400) + `.mkv"`,
		`"` + strings.Repeat("é", 300) + `.mkv"`, `"` + strings.Repeat("a", 250) + "." + strings.Repeat("b", 60) + `"`,
		"\"a\x00b\"", "\"a\nb\"", `"C:\Windows\x.exe"`, `"  "`, `"a"` + `"b"`, `"` + strings.Repeat(".", 300) + `x` + strings.Repeat(".", 10) + `"`,
		"\"\xff\xfe.mkv\"", `"a<b>c:d|e?f*g.mkv"`,
	} {
		f.Add(s, uint8(3))
	}
	f.Fuzz(func(t *testing.T, subject string, n uint8) {
		files := make([]NZBFile, int(n%8)+1)
		for i := range files {
			files[i] = NZBFile{Subject: subject}
		}
		names := outputFileNames(files)
		if len(names) != len(files) {
			t.Fatalf("got %d names for %d files", len(names), len(files))
		}
		seen := map[string]bool{}
		for _, name := range names {
			checkOutputName(t, name)
			if seen[strings.ToLower(name)] {
				t.Fatalf("name %q is used twice", name)
			}
			seen[strings.ToLower(name)] = true
		}
	})
}

// FuzzParseNZB: any input is either an error or an NZB whose ids, groups and
// sizes are safe to use, and whose file names are safe to write.
func FuzzParseNZB(f *testing.F) {
	const head = `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd"><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`
	seed := head + `<file poster="p" date="1" subject="[1/2] &quot;a.mkv&quot; yEnc (1/2)"><groups><group>alt.binaries.test</group></groups><segments><segment bytes="100" number="2">b@x</segment><segment bytes="100" number="1">a@x</segment></segments></file></nzb>`
	f.Add([]byte(seed))
	f.Add([]byte(`<?xml version="1.0" encoding="iso-8859-1"?><nzb><file subject="&#233;"><groups><group>g</group></groups><segments><segment bytes="-5" number="1">&lt;a@b&gt;</segment></segments></file></nzb>`))
	f.Add([]byte(`<nzb><file subject="x"><groups><group>a b</group></groups><segments><segment bytes="9223372036854775807" number="1">a@b</segment></segments></file></nzb>`))
	f.Add([]byte(`<nzb><file subject="x"><segments><segment bytes="1" number="1">a
b</segment></segments></file></nzb>`))
	f.Add([]byte(`<!DOCTYPE lolz [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;&lol;">]><nzb><file subject="&lol2;"><segments><segment bytes="1" number="1">a@b</segment></segments></file></nzb>`))
	f.Add([]byte(strings.Repeat("<a>", 5000)))
	f.Add([]byte(head + strings.Repeat(`<file subject="x"><segments><segment bytes="1" number="1">a@b</segment></segments></file>`, 200) + `</nzb>`))
	f.Add([]byte(`<nzb ` + strings.Repeat(`a="1" `, 3000) + `></nzb>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		nzb, err := ParseNZB(data)
		if err != nil {
			return
		}
		if len(nzb.Files) == 0 {
			t.Fatal("no error, no files")
		}
		if total := nzb.TotalBytes(); total < 0 {
			t.Fatalf("total size %d is negative", total)
		}
		for _, file := range nzb.Files {
			for _, g := range file.Groups {
				if !validNNTPToken(g) {
					t.Fatalf("group %q got through", g)
				}
			}
			last := -1 << 62
			for _, s := range file.Segments {
				if !validMessageID(s.MessageID) || strings.ContainsAny(s.MessageID, "\r\n\x00 ") {
					t.Fatalf("message id %q got through", s.MessageID)
				}
				if s.Bytes < 0 || s.Bytes > maxSegmentBytes {
					t.Fatalf("segment size %d got through", s.Bytes)
				}
				if s.Number < last {
					t.Fatal("segments are not in order")
				}
				last = s.Number
			}
		}
		names := outputFileNames(nzb.Files)
		seen := map[string]bool{}
		for _, name := range names {
			checkOutputName(t, name)
			if seen[strings.ToLower(name)] {
				t.Fatalf("name %q is used twice", name)
			}
			seen[strings.ToLower(name)] = true
		}
	})
}

// FuzzDecodeYenc: no article body can make the decoder panic or produce more
// bytes than it was given, and a body the encoder made always decodes back.
func FuzzDecodeYenc(f *testing.F) {
	f.Add([]byte("=ybegin part=1 line=128 size=10 name=my file.bin\r\n=ypart begin=1 end=5\r\nabcde\r\n=yend size=5 pcrc32=deadbeef\r\n"))
	f.Add([]byte("=ybegin line=128 size=1 name=a\r\n=\r\n=yend size=1\r\n"))
	f.Add([]byte("=ybegin size=99999999999999999999 name=a\n=ypart begin=-1 end=99999999999999999999\n=yend crc32=zz\n"))
	f.Add([]byte("=ybegin\n=ybegin\n=yend\n=yend\n"))
	f.Add(EncodeYenc("x.bin", []byte{0, 10, 13, 61, 255, 214, 19, 227}))
	f.Add([]byte(strings.Repeat("=", 1000)))
	f.Fuzz(func(t *testing.T, data []byte) {
		part, err := DecodeYenc(bytes.NewReader(data))
		if err == nil && len(part.Data) > len(data) {
			t.Fatalf("decoded %d bytes out of %d", len(part.Data), len(data))
		}
		// Round trip: encoding then decoding gives the bytes back.
		enc := EncodeYenc("f.bin", data)
		back, err := DecodeYenc(bytes.NewReader(enc))
		if err != nil {
			t.Fatalf("own output does not decode: %v", err)
		}
		if !bytes.Equal(back.Data, data) {
			t.Fatalf("round trip changed the data")
		}
		if !back.CRC32Valid {
			t.Fatal("own output has a wrong checksum")
		}
	})
}

// FuzzSegmentOffset: an article can never be placed where the file could not
// plausibly reach.
func FuzzSegmentOffset(f *testing.F) {
	f.Add(int64(1), int64(10), int64(100), 5)
	f.Add(int64(-3), int64(1<<62), int64(1<<40), 0)
	f.Add(int64(1<<62), int64(0), int64(1), 1000)
	f.Add(int64(9223372036854775807), int64(9223372036854775807), int64(9223372036854775807), 3)
	f.Fuzz(func(t *testing.T, begin, end, limit int64, n int) {
		if n < 0 || n > 1<<16 {
			return
		}
		part := &YencPart{PartBegin: begin, PartEnd: end, Data: make([]byte, n)}
		off, err := segmentOffset(part, limit)
		if err != nil {
			return
		}
		if off < 0 || off+int64(n) < off || off+int64(n) > limit && limit >= 0 {
			t.Fatalf("offset %d with %d bytes passes limit %d", off, n, limit)
		}
	})
}

// FuzzParseStatusLine: a status line gives three plain digits or an error.
func FuzzParseStatusLine(f *testing.F) {
	for _, s := range []string{"200 ok\r\n", "+20 x", "-12", "2", "", "211 5 1 5 alt.test", "abc", "999", "0000", "２００ x", "200\x00", "381 password required"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		code, msg, err := parseStatusLine(line)
		if err != nil {
			return
		}
		if code < 0 || code > 999 {
			t.Fatalf("code %d out of range for %q", code, line)
		}
		if got := fmt.Sprintf("%03d", code); !strings.HasPrefix(strings.TrimRight(line, "\r\n"), got) {
			t.Fatalf("code %d does not start %q", code, line)
		}
		if msg != strings.TrimSpace(msg) {
			t.Fatalf("message %q is not trimmed", msg)
		}
	})
}

// FuzzReadDotBlock: whatever the server sends, reading a multi-line block
// stops, and a block written with dot-stuffing reads back unchanged.
func FuzzReadDotBlock(f *testing.F) {
	f.Add([]byte("a\r\n..b\r\n...\r\n.\r\n"))
	f.Add([]byte(".\r\n"))
	f.Add([]byte("no terminator"))
	f.Add([]byte(strings.Repeat("x", 100000)))
	f.Add([]byte("\r\n\r\n.\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := readDotBlock(bufio.NewReader(bytes.NewReader(data)), nil)
		if err == nil && len(out) > len(data) {
			t.Fatalf("block grew from %d to %d bytes", len(data), len(out))
		}

		// Write the data as a block the way a server does, and read it back.
		var lines []string
		for _, l := range strings.Split(string(data), "\n") {
			lines = append(lines, strings.TrimRight(l, "\r"))
		}
		var wire bytes.Buffer
		var want strings.Builder
		for _, l := range lines {
			if strings.HasPrefix(l, ".") {
				wire.WriteByte('.')
			}
			wire.WriteString(l + "\r\n")
			want.WriteString(l + "\n")
		}
		wire.WriteString(".\r\n")
		got, err := readDotBlock(bufio.NewReader(&wire), nil)
		if err != nil {
			t.Fatalf("well-formed block failed: %v", err)
		}
		if string(got) != want.String() {
			t.Fatalf("block came back as %q, want %q", got, want.String())
		}
	})
}

// A command that cannot be sent must not put its arguments (a user name, a
// password) in the error.
func TestFailedCommandDoesNotShowThePassword(t *testing.T) {
	server, client := net.Pipe()
	server.Close()
	c := &NNTPConn{conn: client, reader: bufio.NewReader(client)}
	err := c.Authenticate("alice", "hunter2-secret")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "hunter2-secret") || strings.Contains(err.Error(), "alice") {
		t.Fatalf("error shows the login: %v", err)
	}
}

func TestParseStatusLineNeedsThreeDigits(t *testing.T) {
	cases := []struct {
		line    string
		code    int
		message string
		ok      bool
	}{
		{"200 Posting allowed\r\n", 200, "Posting allowed", true},
		{"211 5 1 5 alt.test", 211, "5 1 5 alt.test", true},
		{"381", 381, "", true},
		{"+20 x", 0, "", false},
		{"-12 x", 0, "", false},
		{" 20 x", 0, "", false},
		{"2", 0, "", false},
		{"", 0, "", false},
		{"abc", 0, "", false},
		{"２００ x", 0, "", false},
	}
	for _, tc := range cases {
		code, msg, err := parseStatusLine(tc.line)
		if (err == nil) != tc.ok || code != tc.code || msg != tc.message {
			t.Errorf("parseStatusLine(%q) = %d, %q, %v", tc.line, code, msg, err)
		}
	}
}

// A server that never ends a line, or never sends the closing dot, cannot make
// a download use all the memory.
func TestReadDotBlockIsBounded(t *testing.T) {
	endless := io.MultiReader(strings.NewReader("a\r\n"), io.LimitReader(zeroReader{}, 4*maxArticleLine))
	if _, err := readDotBlock(bufio.NewReader(endless), nil); err == nil || !errors.Is(err, errLineTooLong) {
		t.Errorf("a line without an end: %v", err)
	}
	lines := io.LimitReader(repeatReader("0123456789abcdef\r\n"), 2*maxArticleBytes)
	if _, err := readDotBlock(bufio.NewReader(lines), nil); err == nil || !errors.Is(err, errLineTooLong) {
		t.Errorf("a body without an end: %v", err)
	}
	if _, err := readDBlock("a\r\n..b\r\n.\r\n"); err != nil {
		t.Error(err)
	}
}

func readDBlock(s string) ([]byte, error) {
	return readDotBlock(bufio.NewReader(strings.NewReader(s)), nil)
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

type repeatReader string

func (r repeatReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		n += copy(p[n:], r)
	}
	return len(p), nil
}

// An article may not claim a place far beyond its file.
func TestSegmentOffsetLimits(t *testing.T) {
	f := NZBFile{Segments: []NZBSegment{{Bytes: 700000}, {Bytes: 700000}}}
	limit := fileSizeLimit(f)
	if limit < 1400000 || limit > 100<<20 {
		t.Fatalf("limit %d", limit)
	}
	ok := &YencPart{PartBegin: 700001, Data: make([]byte, 690000)}
	if off, err := segmentOffset(ok, limit); err != nil || off != 700000 {
		t.Errorf("a normal article: %d, %v", off, err)
	}
	far := &YencPart{PartBegin: 1 << 50, Data: []byte("x")}
	if _, err := segmentOffset(far, limit); err == nil {
		t.Error("an article claiming a place a petabyte in was accepted")
	}
	if _, err := segmentOffset(&YencPart{PartBegin: 9223372036854775807, Data: make([]byte, 10)}, limit); err == nil {
		t.Error("an overflowing offset was accepted")
	}
	if fileSizeLimit(NZBFile{}) != maxUndeclaredFileBytes {
		t.Error("a file with no declared sizes should fall back to the fixed limit")
	}
}

func TestNZBSegmentSizesAreKeptSane(t *testing.T) {
	nzb, err := ParseNZB([]byte(`<nzb><file subject="x"><groups><group>a.b</group></groups><segments>` +
		`<segment bytes="-5" number="1">a@b</segment><segment bytes="9223372036854775807" number="2">c@d</segment></segments></file></nzb>`))
	if err != nil {
		t.Fatal(err)
	}
	if total := nzb.TotalBytes(); total < 0 || total > 2*maxSegmentBytes {
		t.Fatalf("total %d", total)
	}
}

func TestFileNamesAreCleanedAndNeverOnlyDots(t *testing.T) {
	long := `"` + strings.Repeat(".", 250) + "x" + strings.Repeat(".", 10) + `"`
	names := outputFileNames([]NZBFile{{Subject: long}, {Subject: "\"evil\u202egpj.exe\""}, {Subject: "\"a\xffb.mkv\""}, {Subject: "\"a\x7fb.mkv\""}})
	if strings.Trim(names[0], ". ") == "" {
		t.Errorf("name %q is only dots", names[0])
	}
	for _, n := range names[1:] {
		if strings.ContainsAny(n, "\u202e\x7f\xff") || !utf8.ValidString(n) {
			t.Errorf("name %q keeps a reordering or invalid character", n)
		}
	}
}

func TestYencNameKeepsSpaces(t *testing.T) {
	part, err := DecodeYenc(strings.NewReader("=ybegin line=128 size=1 name=My Movie 2024.mkv\r\n" + "k\r\n=yend size=1\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if part.Name != "My Movie 2024.mkv" {
		t.Errorf("name = %q", part.Name)
	}
}

func TestNZBWithTooManyFilesIsRefused(t *testing.T) {
	file := `<file subject="x"><groups><group>a.b</group></groups><segments><segment bytes="1" number="1">a@b</segment></segments></file>`
	body := func(n int) []byte { return []byte("<nzb>" + strings.Repeat(file, n) + "</nzb>") }
	if _, err := ParseNZB(body(maxNZBFiles + 1)); err == nil {
		t.Fatal("an NZB with more files than allowed was accepted")
	}
	if nzb, err := ParseNZB(body(500)); err != nil || len(nzb.Files) != 500 {
		t.Fatalf("an ordinary NZB was refused: %v", err)
	}
}

func TestFileSizeLimitIgnoresSizesThatWereNotMeasured(t *testing.T) {
	small := NZBFile{Segments: []NZBSegment{{Bytes: 1}, {Bytes: 1}, {Bytes: 1}}}
	if got := fileSizeLimit(small); got != maxUndeclaredFileBytes {
		t.Errorf("a made-up size of 1 byte per article limited the file to %d", got)
	}
	real := NZBFile{Segments: []NZBSegment{{Bytes: 750000}, {Bytes: 750000}, {Bytes: 20000}}}
	if got := fileSizeLimit(real); got >= maxUndeclaredFileBytes {
		t.Errorf("real sizes were ignored: %d", got)
	}
}

// FuzzLoadResume: a progress file with any content is either used or thrown
// away, and never says more was saved than the release holds.
func FuzzLoadResume(f *testing.F) {
	f.Add([]byte(`{"v":1,"release":"x","files":[{"done":"/w==","bytes":10}]}`))
	f.Add([]byte(`{"v":1,"release":"x","files":[{"done":"!!!","bytes":-5},{"done":"","bytes":9223372036854775807}]}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(``))
	nzb := &NZB{Files: []NZBFile{
		{Subject: `"a.bin"`, Segments: []NZBSegment{{Number: 1, Bytes: 10, MessageID: "1@x"}, {Number: 2, Bytes: 10, MessageID: "2@x"}}},
		{Subject: `"b.bin"`, Segments: []NZBSegment{{Number: 1, Bytes: 10, MessageID: "3@x"}}},
	}}
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		paths := []string{filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")}
		for _, p := range paths {
			if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// where the fuzzed bytes are a record, make it one that belongs to this NZB
		var rec resumeRecord
		if json.Unmarshal(data, &rec) == nil {
			rec.Version, rec.Release = 1, nzbFingerprint(nzb)
			data, _ = json.Marshal(rec)
		}
		if err := os.WriteFile(filepath.Join(dir, ResumeFile), data, 0o644); err != nil {
			t.Fatal(err)
		}
		st := loadResume(dir, nzb, paths)
		var limit int64
		for _, f := range nzb.Files {
			limit += fileSizeLimit(f)
		}
		if got := st.savedBytes(); got < 0 || got > limit {
			t.Fatalf("saved bytes = %d", got)
		}
		st.mark(0, 0, 10)
		st.save()
	})
}

func TestNZBInOtherCharsets(t *testing.T) {
	// windows-1251 "Привет" is these bytes; iso-8859-15 has the euro sign at 0xA4
	cyr := []byte{0xcf, 0xf0, 0xe8, 0xe2, 0xe5, 0xf2}
	build := func(label string, subject []byte) []byte {
		out := []byte(`<?xml version="1.0" encoding="` + label + `"?><nzb><file subject="&quot;`)
		out = append(out, subject...)
		out = append(out, []byte(`.mkv&quot;"><groups><group>a.b</group></groups><segments><segment bytes="1" number="1">a@b</segment></segments></file></nzb>`)...)
		return out
	}
	for _, tc := range []struct {
		label   string
		subject []byte
		want    string
	}{
		{"windows-1251", cyr, "Привет.mkv"},
		{"iso-8859-15", []byte{'a', 0xa4}, "a€.mkv"},
		{"ISO-8859-1", []byte{'c', 'a', 'f', 0xe9}, "café.mkv"},
		{"utf-8", []byte("café"), "café.mkv"},
		{"no-such-charset", []byte("plain"), "plain.mkv"},
	} {
		nzb, err := ParseNZB(build(tc.label, tc.subject))
		if err != nil {
			t.Errorf("%s: %v", tc.label, err)
			continue
		}
		if got := articleFilename(nzb.Files[0].Subject, 0); got != tc.want {
			t.Errorf("%s: name %q, want %q", tc.label, got, tc.want)
		}
	}
}

// A post that says its article belongs a petabyte into the file must not make
// a petabyte-long file: the article counts as missing.
func TestArticleFarPastTheEndOfItsFileIsMissingNotWritten(t *testing.T) {
	data := []byte(strings.Repeat("payload ", 100))
	honest := buildMultipartYenc("movie.mkv", data, 1, 400)
	liar := []byte(strings.Replace(string(buildMultipartYenc("movie.mkv", data, 401, 800)), "begin=401 end=800", "begin=1125899906842624 end=1125899906843023", 1))
	srv := newFakeNNTPServer(t, map[string][]byte{"a@x": honest, "b@x": liar})
	nzb := &NZB{Files: []NZBFile{{
		Subject: `"movie.mkv"`,
		Groups:  []string{"alt.binaries.test"},
		Segments: []NZBSegment{
			{Number: 1, Bytes: 50000, MessageID: "a@x"},
			{Number: 2, Bytes: 50000, MessageID: "b@x"},
		},
	}}}
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Username: "u", Password: "p", Connections: 1}
	dir := t.TempDir()
	res, err := DownloadFromServers(context.Background(), []ClientConfig{cfg}, nzb, dir, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if res.MissingSegments != 1 {
		t.Fatalf("missing = %d, want the lying article counted as missing", res.MissingSegments)
	}
	info, err := os.Stat(res.Paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1<<20 {
		t.Fatalf("the file is %d bytes long", info.Size())
	}
}
