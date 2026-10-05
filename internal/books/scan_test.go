package books

import (
	"path/filepath"
	"testing"
)

func TestGuess(t *testing.T) {
	cases := []struct {
		parts  []string
		file   bool
		author string
		title  string
		year   int
	}{
		{[]string{"J.R.R. Tolkien", "The Hobbit (1937)", "The Hobbit"}, true, "J.R.R. Tolkien", "The Hobbit", 1937},
		{[]string{"Andy Weir", "Project Hail Mary (2021)"}, false, "Andy Weir", "Project Hail Mary", 2021},
		{[]string{"Frank Herbert", "Dune"}, true, "Frank Herbert", "Dune", 0},
		{[]string{"Frank Herbert", "Dune (12)", "Dune - Frank Herbert"}, true, "Frank Herbert", "Dune", 0},
		{[]string{"Frank Herbert", "Frank Herbert - Dune Messiah"}, true, "Frank Herbert", "Dune Messiah", 0},
		{[]string{"Brandon Sanderson", "Stormlight Archive", "The Way of Kings"}, false, "Brandon Sanderson", "The Way of Kings", 0},
		{[]string{"Brandon Sanderson", "Stormlight Archive", "1 - The Way of Kings", "The Way of Kings"}, true, "Brandon Sanderson", "The Way of Kings", 0},
		{[]string{"Brandon Sanderson", "Stormlight Archive", "Book 2 - Words of Radiance"}, false, "Brandon Sanderson", "Words of Radiance", 0},
		{[]string{"Unknown", "1984"}, true, "Unknown", "1984", 0},
		{[]string{"Terry Pratchett - Mort [Unabridged]"}, true, "Terry Pratchett", "Mort", 0},
		{[]string{"2008 - The Hunger Games"}, false, "", "The Hunger Games", 2008},
		{[]string{"Neuromancer"}, true, "", "Neuromancer", 0},
		{[]string{"Iain_M_Banks", "Excession"}, false, "Iain M Banks", "Excession", 0},
		{nil, false, "", "", 0},
	}
	for _, tc := range cases {
		a, ti, y := Guess(tc.parts, tc.file)
		if a != tc.author || ti != tc.title || y != tc.year {
			t.Errorf("Guess(%q, %v) = %q, %q, %d; want %q, %q, %d", tc.parts, tc.file, a, ti, y, tc.author, tc.title, tc.year)
		}
	}
}

func TestSameTitle(t *testing.T) {
	cases := []struct {
		local, catalogue string
		exact, want      bool
	}{
		{"The Hobbit", "The Hobbit", true, true},
		{"Hobbit", "The Hobbit", true, true},
		{"The Hobbit Unabridged", "The Hobbit", true, false},
		{"The Hobbit Unabridged", "The Hobbit", false, true},
		{"Dune", "Dune Messiah", false, false},
		{"Dune Messiah", "Dune", true, false},
		{"", "Dune", false, false},
	}
	for _, tc := range cases {
		if got := SameTitle(tc.local, tc.catalogue, tc.exact); got != tc.want {
			t.Errorf("SameTitle(%q, %q, %v) = %v", tc.local, tc.catalogue, tc.exact, got)
		}
	}
}

func TestSameAuthor(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Tolkien", "J.R.R. Tolkien", true},
		{"Weir, Andy", "Andy Weir", true},
		{"Andy Weir", "Brandon Sanderson", false},
		{"", "Andy Weir", false},
	}
	for _, tc := range cases {
		if got := SameAuthor(tc.a, tc.b); got != tc.want {
			t.Errorf("SameAuthor(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

func TestScanFolders(t *testing.T) {
	eb := t.TempDir()
	write(t, filepath.Join(eb, "Frank Herbert", "Dune (12)", "Dune - Frank Herbert.epub"), 10)
	write(t, filepath.Join(eb, "Frank Herbert", "Dune (12)", "Dune - Frank Herbert.pdf"), 10)
	write(t, filepath.Join(eb, "Frank Herbert", "Dune (12)", "cover.jpg"), 10)
	write(t, filepath.Join(eb, "Terry Pratchett - Mort.mobi"), 10)
	write(t, filepath.Join(eb, ".trash", "Old - Book.epub"), 10)
	list, err := ScanEbooks(eb)
	if err != nil || len(list) != 2 {
		t.Fatalf("ebooks: %+v %v", list, err)
	}
	if list[0].Title != "Dune" || list[0].Format != "epub" || list[0].Files != 2 || list[0].Author != "Frank Herbert" {
		t.Errorf("dune: %+v", list[0])
	}
	if list[1].Title != "Mort" || list[1].Author != "Terry Pratchett" || list[1].Format != "mobi" {
		t.Errorf("mort: %+v", list[1])
	}

	ab := t.TempDir()
	write(t, filepath.Join(ab, "Andy Weir", "Project Hail Mary (2021)", "01.m4b"), 10)
	write(t, filepath.Join(ab, "Brandon Sanderson", "The Way of Kings", "CD 1", "01.mp3"), 10)
	write(t, filepath.Join(ab, "Brandon Sanderson", "The Way of Kings", "CD 2", "01.mp3"), 10)
	write(t, filepath.Join(ab, "Loose Author - Loose Book.mp3"), 10)
	list, err = ScanAudiobooks(ab)
	if err != nil || len(list) != 3 {
		t.Fatalf("audiobooks: %+v %v", list, err)
	}
	byTitle := map[string]LocalBook{}
	for _, b := range list {
		byTitle[b.Title] = b
	}
	if b := byTitle["Project Hail Mary"]; b.Author != "Andy Weir" || b.Year != 2021 || b.Format != "m4b" {
		t.Errorf("hail mary: %+v", b)
	}
	if b := byTitle["The Way of Kings"]; b.Files != 2 || filepath.Base(b.Path) != "The Way of Kings" {
		t.Errorf("discs belong to the book folder: %+v", b)
	}
	if b := byTitle["Loose Book"]; b.Author != "Loose Author" || filepath.Base(b.Path) != "Loose Author - Loose Book.mp3" {
		t.Errorf("loose file: %+v", b)
	}
}

func TestLooksLikeSet(t *testing.T) {
	cases := []struct {
		title string
		set   bool
	}{
		{"The Martian / Artemis / Project Hail Mary", true},
		{"The Hunger Games Box Set", true},
		{"Summary of Project Hail Mary", true},
		{"Harry Potter Books 1-7", true},
		{"The Complete Collection", true},
		{"Project Hail Mary", false},
		{"The Collector", false},
		{"1984", false},
	}
	for _, tc := range cases {
		if got := LooksLikeSet(tc.title); got != tc.set {
			t.Errorf("LooksLikeSet(%q) = %v", tc.title, got)
		}
	}
	if !ValidAuthorKey("OL26320A") || ValidAuthorKey("OL1W") || ValidAuthorKey("../x") {
		t.Error("author keys")
	}
}
