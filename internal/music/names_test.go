package music

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeAndSameNames(t *testing.T) {
	same := [][2]string{
		{"The Beatles", "Beatles, The"},
		{"Sigur Rós", "Sigur Ros"},
		{"Simon & Garfunkel", "Simon and Garfunkel"},
		{"AC/DC", "ACDC"},
		{"OK Computer", "OK Computer (Collector's Edition)"},
		{"Rumours", "Rumours (Super Deluxe)"},
		{"Nevermind", "Nevermind Remastered"},
		{"Abbey Road", "Abbey Road [50th Anniversary]"},
	}
	for _, p := range same {
		if !SameAlbum(p[0], p[1]) {
			t.Errorf("SameAlbum(%q, %q) should be true", p[0], p[1])
		}
	}
	different := [][2]string{
		{"OK Computer", "Kid A"},
		{"Special", "Special Herbs"},
		{"", ""},
	}
	for _, p := range different {
		if SameAlbum(p[0], p[1]) {
			t.Errorf("SameAlbum(%q, %q) should be false", p[0], p[1])
		}
	}
	if !SameArtist("The Beatles", "beatles") || SameArtist("Beatles", "The Rolling Stones") {
		t.Fatal("SameArtist")
	}
}

func TestReleaseMatch(t *testing.T) {
	cases := []struct {
		title  string
		ok     bool
		reason string
	}{
		{"Radiohead - OK Computer (1997) [FLAC]", true, ""},
		{"Radiohead - OK Computer OKNOTOK (2017) [FLAC]", false, "another album"},
		{"Radiohead - OK Computer (Collector's Edition) (2009) [FLAC]", true, ""},
		{"Radiohead-OK_Computer-REMASTERED-2CD-FLAC-2009-GRP", true, ""},
		{"Radiohead - Kid A (2000) [FLAC]", false, "another album"},
		{"Coldplay - OK Computer (1997) [FLAC]", false, "another artist"},
		{"Radiohead - Discography (1993-2016) [FLAC]", false, "a discography or collection, not one album"},
		{"Radiohead - OK Computer (1995) [FLAC]", false, "older than the album"},
	}
	for _, tc := range cases {
		ok, reason := ReleaseMatch(ParseRelease(tc.title), "Radiohead", "OK Computer", 1997)
		if ok != tc.ok || reason != tc.reason {
			t.Errorf("%q: got (%v, %q), want (%v, %q)", tc.title, ok, reason, tc.ok, tc.reason)
		}
	}
}

func TestBestArtistAndAlbum(t *testing.T) {
	cands := []NameCandidate{
		{Name: "Radiohead Tribute Band", Score: 90},
		{Name: "Radiohead", SortName: "Radiohead", Score: 80},
		{Name: "Radiohead", SortName: "Radiohead", Score: 100},
	}
	if got := BestArtist("radiohead", cands); got != 2 {
		t.Fatalf("BestArtist = %d", got)
	}
	if got := BestArtist("Beatles", []NameCandidate{{Name: "The Beatles", SortName: "Beatles, The"}}); got != 0 {
		t.Fatalf("sort name: %d", got)
	}
	if got := BestArtist("Nobody", cands); got != -1 {
		t.Fatalf("no match: %d", got)
	}

	albums := []AlbumCandidate{{"Pablo Honey", 1993}, {"OK Computer", 1997}, {"OK Computer", 2017}, {"Kid A", 2000}}
	for _, tc := range []struct {
		title string
		year  int
		want  int
	}{
		{"OK Computer", 2017, 2},
		{"OK Computer", 1997, 1},
		{"OK Computer", 0, 1},
		{"ok computer (remastered)", 0, 1},
		{"Amnesiac", 2001, -1},
	} {
		if got := BestAlbum(tc.title, tc.year, albums); got != tc.want {
			t.Errorf("BestAlbum(%q, %d) = %d, want %d", tc.title, tc.year, got, tc.want)
		}
	}
}

func TestParseAlbumFolder(t *testing.T) {
	cases := []struct {
		folder, artist, title string
		year                  int
	}{
		{"OK Computer (1997)", "Radiohead", "OK Computer", 1997},
		{"OK Computer [1997] [FLAC]", "Radiohead", "OK Computer", 1997},
		{"1997 - OK Computer", "Radiohead", "OK Computer", 1997},
		{"(1997) OK Computer", "Radiohead", "OK Computer", 1997},
		{"Radiohead - OK Computer (1997)", "Radiohead", "OK Computer", 1997},
		{"OK Computer", "Radiohead", "OK Computer", 0},
		{"1989 (2014)", "Taylor Swift", "1989", 2014},
		{"1999", "Prince", "1999", 0},
		{"2001 A Space Odyssey", "Various", "2001 A Space Odyssey", 0},
	}
	for _, tc := range cases {
		title, year := ParseAlbumFolder(tc.folder, tc.artist)
		if title != tc.title || year != tc.year {
			t.Errorf("ParseAlbumFolder(%q) = (%q, %d), want (%q, %d)", tc.folder, title, year, tc.title, tc.year)
		}
	}
}

func TestScanLibrary(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{
		"Radiohead/OK Computer (1997)/01 - Airbag.flac",
		"Radiohead/OK Computer (1997)/cover.jpg",
		"Radiohead/Kid A (2000)/CD1/01 - Everything in Its Right Place.flac",
		"Radiohead/Empty Folder/readme.txt",
		"Radiohead/loose track.mp3",
		"Portishead/1994 - Dummy/01 - Mysterons.mp3",
		".stfolder/x.flac",
		"Nothing Here/notes.txt",
	} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	artists, err := ScanLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 2 || artists[0].Name != "Portishead" || artists[1].Name != "Radiohead" {
		t.Fatalf("artists: %+v", artists)
	}
	p := artists[0]
	if len(p.Albums) != 1 || p.Albums[0].Title != "Dummy" || p.Albums[0].Year != 1994 || len(p.Albums[0].Files) != 1 {
		t.Fatalf("portishead: %+v", p.Albums)
	}
	r := artists[1]
	if len(r.Albums) != 2 || r.Albums[0].Title != "Kid A" || len(r.Albums[0].Files) != 1 || r.Albums[1].Title != "OK Computer" {
		t.Fatalf("radiohead albums: %+v", r.Albums)
	}
	if len(r.Loose) != 1 || filepath.Base(r.Loose[0]) != "loose track.mp3" {
		t.Fatalf("loose files: %v", r.Loose)
	}
	if _, err := ScanLibrary(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a missing folder is an error")
	}
}

func TestNaming(t *testing.T) {
	clean := Sanitizer(func(s string) string { return strings.ReplaceAll(s, "/", "") })
	if got := AlbumFolder("AC/DC Live", 1992, clean); got != "ACDC Live (1992)" {
		t.Fatalf("sanitized: %s", got)
	}
	if got := AlbumPath("/music", "Radiohead", "OK Computer", 1997, nil); got != filepath.Join("/music", "Radiohead", "OK Computer (1997)") {
		t.Fatalf("album path: %s", got)
	}
	if got := AlbumFolder("Untitled", 0, nil); got != "Untitled" {
		t.Fatalf("no year: %s", got)
	}
	if got := TrackFileName(1, 7, "Lucky", false, ".FLAC", nil); got != "07 - Lucky.flac" {
		t.Fatalf("single disc: %s", got)
	}
	if got := TrackFileName(2, 7, "Lucky", true, ".flac", nil); got != "2-07 - Lucky.flac" {
		t.Fatalf("multi disc: %s", got)
	}
	if got := TrackFileName(1, 3, "", false, ".mp3", nil); got != "03.mp3" {
		t.Fatalf("no title: %s", got)
	}
	if got := ArtistFolder("", clean); got != "Unknown Artist" {
		t.Fatalf("empty artist: %s", got)
	}
}
