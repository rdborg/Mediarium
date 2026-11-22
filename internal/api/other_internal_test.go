package api

import "testing"

func TestOtherNameFromRelease(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Some.Lecture.Series", "Some Lecture Series"},
		{"Some_Lecture_Series", "Some Lecture Series"},
		{"Already Has Spaces v1.2", "Already Has Spaces v1.2"},
		{"  - Trailing dash -  ", "Trailing dash"},
		{"Home.Videos.1998-GROUP", "Home Videos 1998-GROUP"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := otherNameFromRelease(tt.in); got != tt.want {
			t.Errorf("otherNameFromRelease(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOtherJunk(t *testing.T) {
	for name, want := range map[string]bool{
		"movie.mkv": false, "notes.txt": false, "cover.jpg": false,
		"a.par2": true, "a.sfv": true, "a.nzb": true, ".DS_Store": true, "x.torrent": true, "x.part": true,
	} {
		if got := otherJunk(name); got != want {
			t.Errorf("otherJunk(%q) = %v, want %v", name, got, want)
		}
	}
}
