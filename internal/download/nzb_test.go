package download

import "testing"

const fixtureNZB = `<?xml version="1.0" encoding="iso-8859-1"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
<file subject="[1/1] &quot;movie.mkv&quot; yEnc (1/2)" poster="poster@example.com" date="1700000000">
<groups>
<group>alt.binaries.movies</group>
</groups>
<segments>
<segment bytes="500000" number="2">seg2@example</segment>
<segment bytes="500000" number="1">seg1@example</segment>
</segments>
</file>
</nzb>`

func TestParseNZB(t *testing.T) {
	nzb, err := ParseNZB([]byte(fixtureNZB))
	if err != nil {
		t.Fatalf("parse nzb: %v", err)
	}
	if len(nzb.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(nzb.Files))
	}
	f := nzb.Files[0]
	if f.Subject == "" {
		t.Fatal("expected non-empty subject")
	}
	if len(f.Groups) != 1 || f.Groups[0] != "alt.binaries.movies" {
		t.Fatalf("unexpected groups: %v", f.Groups)
	}
	if len(f.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(f.Segments))
	}
	// Segments must come back sorted by number, regardless of document order.
	if f.Segments[0].Number != 1 || f.Segments[0].MessageID != "seg1@example" {
		t.Fatalf("expected segment 1 first, got %+v", f.Segments[0])
	}
	if f.Segments[1].Number != 2 || f.Segments[1].MessageID != "seg2@example" {
		t.Fatalf("expected segment 2 second, got %+v", f.Segments[1])
	}
	if nzb.TotalBytes() != 1_000_000 {
		t.Fatalf("expected total bytes 1000000, got %d", nzb.TotalBytes())
	}
}

func TestParseNZBEmpty(t *testing.T) {
	if _, err := ParseNZB([]byte(`<nzb></nzb>`)); err == nil {
		t.Fatal("expected error for nzb with no files")
	}
}

func TestArticleFilename(t *testing.T) {
	got := articleFilename(`[1/1] "movie.mkv" yEnc (1/2)`, 0)
	if got != "movie.mkv" {
		t.Fatalf("expected movie.mkv, got %q", got)
	}
	got = articleFilename("no quotes here", 3)
	if got != "file_3.bin" {
		t.Fatalf("expected fallback filename, got %q", got)
	}
}
