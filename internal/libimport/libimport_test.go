package libimport_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ryanborg/mediarium/internal/libimport"
)

func touch(t *testing.T, root, rel string, size int) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

type found struct {
	title string
	year  int
	files int
}

func summarize(groups []libimport.Group) []found {
	var out []found
	for _, g := range groups {
		out = append(out, found{g.Title, g.Year, len(g.Files)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].title < out[j].title })
	return out
}

func TestScanMovies(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Inception (2010)/Inception (2010) 1080p BluRay.mkv", 10)
	touch(t, root, "Heat (1995)/movie.mkv", 10)
	touch(t, root, "The.Matrix.1999.720p.BluRay.x264-GRP.mkv", 10)
	touch(t, root, "Blade Runner 2049 (2017)/Blade.Runner.2049.2017.2160p.WEB-DL.mkv", 10)
	touch(t, root, "Inception (2010)/Inception (2010) 720p.mkv", 5) // second version of the same movie
	touch(t, root, "Inception (2010)/Extras/Making Of.mkv", 5)
	touch(t, root, "Inception (2010)/Inception (2010)-trailer.mkv", 5)
	touch(t, root, "Heat (1995)/sample.mkv", 1)
	touch(t, root, "Heat (1995)/notes.txt", 1)
	touch(t, root, "@eaDir/Junk (2001)/Junk.mkv", 1)

	res, err := libimport.ScanMovies(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := summarize(res.Groups)
	want := []found{
		{"Blade Runner 2049", 2017, 1},
		{"Heat", 1995, 1},
		{"Inception", 2010, 2},
		{"The Matrix", 1999, 1},
	}
	if len(got) != len(want) {
		t.Fatalf("groups = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("groups = %+v, want %+v", got, want)
		}
	}
	for _, g := range res.Groups {
		if g.Title == "The Matrix" && g.Files[0].Quality != "Bluray-720p" {
			t.Errorf("The Matrix quality = %q, want Bluray-720p", g.Files[0].Quality)
		}
		if g.Title == "Heat" && g.Files[0].Quality != "Unknown" {
			t.Errorf("Heat quality = %q, want Unknown (nothing in the names says)", g.Files[0].Quality)
		}
	}
}

func TestScanMoviesFutureLookingYearBelongsToTheTitle(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Blade Runner 2049.mkv", 10)
	res, err := libimport.ScanMovies(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Title != "Blade Runner 2049" || res.Groups[0].Year != 0 {
		t.Fatalf("got %+v", res.Groups)
	}
}

func TestScanTV(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Breaking Bad (2008)/Season 01/Breaking.Bad.S01E01.720p.BluRay.x264-GRP.mkv", 10)
	touch(t, root, "Breaking Bad (2008)/Season 01/Breaking.Bad.S01E02.720p.BluRay.x264-GRP.mkv", 10)
	touch(t, root, "Breaking Bad (2008)/Season 02/S02E01.mkv", 10)
	touch(t, root, "The Office (US)/Season 1/The Office 1x03 - Health Care.avi", 10)
	touch(t, root, "The Office (US)/Season 1/The Office S01E04E05.mkv", 10)
	touch(t, root, "Loose.Show.S03E07.1080p.WEB-DL.mkv", 10)                               // no series folder
	touch(t, root, "Breaking Bad (2008)/Season 01/Breaking.Bad.S01E01.1080p.mkv", 20)      // bigger duplicate of E01
	touch(t, root, "Breaking Bad (2008)/Season 01/random home video.mkv", 10)              // no season/episode
	touch(t, root, "Breaking Bad (2008)/Season 01/Breaking.Bad.S01E03-sample.mkv", 1)      // sample
	touch(t, root, "Breaking Bad (2008)/Season 01/Featurettes/Breaking.Bad.S01E09.mkv", 1) // extras folder

	res, err := libimport.ScanTV(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := summarize(res.Groups)
	want := []found{
		{"Breaking Bad", 2008, 3}, // S01E01 (1080p copy), S01E02, S02E01
		{"Loose Show", 0, 1},
		{"The Office (US)", 0, 2},
	}
	if len(got) != len(want) {
		t.Fatalf("groups = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("groups = %+v, want %+v", got, want)
		}
	}

	for _, g := range res.Groups {
		if g.Title != "Breaking Bad" {
			continue
		}
		for _, f := range g.Files {
			if f.Season == 1 && len(f.Episodes) == 1 && f.Episodes[0] == 1 && f.SizeBytes != 20 {
				t.Errorf("kept the smaller duplicate of S01E01: %+v", f)
			}
		}
	}
	for _, g := range res.Groups {
		if g.Title != "The Office (US)" {
			continue
		}
		var multi bool
		for _, f := range g.Files {
			if len(f.Episodes) == 2 && f.Episodes[0] == 4 && f.Episodes[1] == 5 {
				multi = true
			}
		}
		if !multi {
			t.Errorf("multi-episode file S01E04E05 not recognised: %+v", g.Files)
		}
	}

	// The unreadable file and the smaller duplicate are reported, samples
	// and extras are silently ignored.
	if len(res.Skipped) != 2 {
		t.Fatalf("skipped = %v, want 2 entries", res.Skipped)
	}
}

func TestScanRejectsMissingFolder(t *testing.T) {
	if _, err := libimport.ScanMovies(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a folder that doesn't exist")
	}
}

func TestDecide(t *testing.T) {
	cands := func(cs ...libimport.Candidate) []libimport.Candidate { return cs }
	c := func(id int, title string, year int) libimport.Candidate {
		return libimport.Candidate{TMDBID: id, Title: title, Year: year}
	}
	cases := []struct {
		name      string
		title     string
		year      int
		in        []libimport.Candidate
		want      libimport.MatchStatus
		wantFirst int
	}{
		{"exact title and year", "Heat", 1995, cands(c(1, "Heat", 1986), c(2, "Heat", 1995)), libimport.MatchMatched, 2},
		{"punctuation is ignored", "Mr Robot", 2015, cands(c(1, "Mr. Robot", 2015)), libimport.MatchMatched, 1},
		{"one year off still matches when it's the only near one", "Some Film", 2009, cands(c(1, "Some Film", 2010), c(2, "Some Film", 1990)), libimport.MatchMatched, 1},
		{"no year, single exact title", "Inception", 0, cands(c(1, "Inception", 2010), c(2, "Inception: The Cobol Job", 2010)), libimport.MatchMatched, 1},
		{"no year, several exact titles", "Heat", 0, cands(c(1, "Heat", 1986), c(2, "Heat", 1995)), libimport.MatchAmbiguous, 1},
		{"year given but titles differ", "Heat", 1995, cands(c(1, "Heated Rivalry", 2025)), libimport.MatchAmbiguous, 1},
		{"exact title but wrong year", "Heat", 2001, cands(c(1, "Heat", 1995)), libimport.MatchAmbiguous, 1},
		{"nothing found", "Zzzz", 0, nil, libimport.MatchUnmatched, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, ordered := libimport.Decide(tc.title, tc.year, tc.in)
			if status != tc.want {
				t.Fatalf("status = %s, want %s", status, tc.want)
			}
			if tc.wantFirst != 0 && ordered[0].TMDBID != tc.wantFirst {
				t.Fatalf("first candidate = %d, want %d", ordered[0].TMDBID, tc.wantFirst)
			}
		})
	}
}
