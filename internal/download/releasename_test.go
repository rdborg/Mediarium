package download

import (
	"fmt"
	"strings"
	"testing"
)

// nzbOf builds an NZB document from "subject: size" pairs (size in bytes of
// its one article) with an optional head.
func nzbOf(head string, files ...[2]string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`)
	if head != "" {
		b.WriteString("<head>" + head + "</head>")
	}
	for i, f := range files {
		fmt.Fprintf(&b, `<file poster="p@example.com" date="1700000000" subject="%s"><groups><group>alt.binaries.test</group></groups><segments><segment bytes="%s" number="1">s%d@example</segment></segments></file>`, f[0], f[1], i)
	}
	b.WriteString("</nzb>")
	return []byte(b.String())
}

func TestReleaseStem(t *testing.T) {
	tests := []struct{ file, want string }{
		{"Heat.1995.1080p.BluRay.x264-GRP.mkv", "Heat.1995.1080p.BluRay.x264-GRP"},
		{"Heat.1995.1080p.BluRay.x264-GRP.part01.rar", "Heat.1995.1080p.BluRay.x264-GRP"},
		{"Heat.1995.1080p.BluRay.x264-GRP.r00", "Heat.1995.1080p.BluRay.x264-GRP"},
		{"Heat.1995.1080p.BluRay.x264-GRP.7z.001", "Heat.1995.1080p.BluRay.x264-GRP"},
		{"Heat.1995.1080p.BluRay.x264-GRP.par2", "Heat.1995.1080p.BluRay.x264-GRP"},
		{"Heat.1995.1080p.BluRay.x264-GRP.vol000+01.par2", "Heat.1995.1080p.BluRay.x264-GRP"},
		{"Show.S01E02.720p.HDTV.x264-GRP.mkv", "Show.S01E02.720p.HDTV.x264-GRP"},
		{"Fixture Band - First Album (1999) FLAC.rar", "Fixture Band - First Album (1999) FLAC"},
		{"Some Book (2020).epub", "Some Book (2020)"},
		{`folder/Name.With.Dots.2021.2160p.mkv`, "Name.With.Dots.2021.2160p"},
		{"Heat.1995.1080p.BluRay.x264-GRP.nfo", ""},
		{"Heat.1995.1080p.BluRay.x264-GRP.sfv", ""},
		{"poster.jpg", ""},
		{"Name.1080p.x264", ""}, // ".x264" is a tag, not an ending
		{"file_3.bin", ""},
		{"a1b2c3d4e5f6a7b8c9d0e1f2a3b4.mkv", ""}, // a random name tells nothing
		{"m2.mkv", ""},                           // too short to be a name
	}
	for _, tt := range tests {
		if got := releaseStem(tt.file); got != tt.want {
			t.Errorf("releaseStem(%q) = %q, want %q", tt.file, got, tt.want)
		}
	}
}

func TestNZBReleaseName(t *testing.T) {
	tests := []struct {
		name string
		nzb  []byte
		want string
	}{
		{"the biggest file is the release", nzbOf("",
			[2]string{`[1/4] &quot;Heat.1995.1080p.BluRay.x264-GRP.nfo&quot; yEnc (1/1)`, "2000"},
			[2]string{`[2/4] &quot;Heat.1995.1080p.BluRay.x264-GRP.par2&quot; yEnc (1/1)`, "40000"},
			[2]string{`[3/4] &quot;Heat.1995.1080p.BluRay.x264-GRP.mkv&quot; yEnc (1/900)`, "9000000"},
			[2]string{`[4/4] &quot;sample-heat.mkv&quot; yEnc (1/9)`, "900000"},
		), "Heat.1995.1080p.BluRay.x264-GRP"},
		{"an archive set", nzbOf("",
			[2]string{`&quot;Show.S02E05.720p.WEB-DL.x264-GRP.part02.rar&quot; yEnc (1/50)`, "5000000"},
			[2]string{`&quot;Show.S02E05.720p.WEB-DL.x264-GRP.part01.rar&quot; yEnc (1/50)`, "5100000"},
			[2]string{`&quot;Show.S02E05.720p.WEB-DL.x264-GRP.vol000+01.par2&quot; yEnc (1/3)`, "30000"},
		), "Show.S02E05.720p.WEB-DL.x264-GRP"},
		{"a name in the head wins", nzbOf(`<meta type="title">The.Movie.2020.2160p.WEB-DL.DDP5.1.x265-GRP</meta>`,
			[2]string{`&quot;abcdefghijklmnopqrstuvwxyz0123.mkv&quot; yEnc (1/10)`, "100"},
		), "The.Movie.2020.2160p.WEB-DL.DDP5.1.x265-GRP"},
		{"a name type in the head", nzbOf(`<meta type="name">Another.Movie.2019.720p.BluRay.x264-GRP</meta>`,
			[2]string{`&quot;x.bin&quot; yEnc (1/10)`, "100"},
		), "Another.Movie.2019.720p.BluRay.x264-GRP"},
		{"the repair file when the rest is hidden", nzbOf("",
			[2]string{`&quot;a8f7e3c1b2d4a8f7e3c1b2d4a8f7e3c1.bin&quot; yEnc (1/90)`, "9000000"},
			[2]string{`&quot;Hidden.Movie.2018.1080p.WEB-DL.x264-GRP.par2&quot; yEnc (1/1)`, "40000"},
		), "Hidden.Movie.2018.1080p.WEB-DL.x264-GRP"},
		{"everything hidden gives nothing", nzbOf("",
			[2]string{`&quot;a8f7e3c1b2d4a8f7e3c1b2d4a8f7e3c1.bin&quot; yEnc (1/90)`, "9000000"},
			[2]string{`no quotes at all (1/3)`, "100"},
		), ""},
		{"a subject without quotes", nzbOf("",
			[2]string{`[01/12] - Plain.Name.2021.1080p.WEB-DL.x264-GRP.mkv yEnc (1/90)`, "9000000"},
		), "Plain.Name.2021.1080p.WEB-DL.x264-GRP"},
	}
	for _, tt := range tests {
		n, err := ParseNZB(tt.nzb)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got := n.ReleaseName(); got != tt.want {
			t.Errorf("%s: ReleaseName() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestMetaTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"  The.Movie.2020.1080p  ", "The.Movie.2020.1080p"},
		{"", ""},
		{"BadName", ""},
		{strings.Repeat("x", 301), ""},
	}
	for _, tt := range tests {
		if got := metaTitle(tt.in); got != tt.want {
			t.Errorf("metaTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
