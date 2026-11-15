package library

import (
	"strings"
	"testing"
)

// Each note is one whole sentence (or two), with the count the only part
// filled in, so the screen can find its translation.
func TestLeftOutNote(t *testing.T) {
	cases := []struct {
		unknown                   int
		byAbsolute, byNumber, ani bool
		want                      string
	}{
		{0, true, true, true, ""},
		{1, false, false, false, "Left out 1 episode that the movie database doesn't list for this show."},
		{26, false, true, false, "Left out 26 episodes that the movie database doesn't list for this show."},
		{1, true, false, false, "Left out 1 episode that the movie database doesn't list for this show. Its name carries"},
		{55, true, false, false, "Left out 55 episodes that the movie database doesn't list for this show. Their names carry"},
		{1, true, false, true, "Left out 1 episode that the movie database doesn't list for this show. Its number doesn't match"},
		{3, false, true, true, "Left out 3 episodes that the movie database doesn't list for this show. Their numbers don't match"},
	}
	for _, tc := range cases {
		got := leftOutNote(tc.unknown, tc.byAbsolute, tc.byNumber, tc.ani)
		if (tc.want == "") != (got == "") || !strings.HasPrefix(got, tc.want) {
			t.Errorf("leftOutNote(%d, %v, %v, %v) = %q, want it to start %q", tc.unknown, tc.byAbsolute, tc.byNumber, tc.ani, got, tc.want)
		}
	}
}

// The hint to switch a show to TheTVDB's episode order shows only when files
// were left out, a TheTVDB key is saved and the show still follows the movie
// database, and never for files named by their number from the first episode.
func TestTVDBOrderNote(t *testing.T) {
	cases := []struct {
		name              string
		unknown           int
		byAbsolute, offer bool
		want              bool
	}{
		{"files left out, key saved", 5, false, true, true},
		{"one file left out", 1, false, true, true},
		{"nothing left out", 0, false, true, false},
		{"no key saved or already TheTVDB's order", 5, false, false, false},
		{"named from the start of the show", 5, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tvdbOrderNote(tc.unknown, tc.byAbsolute, tc.offer)
			if (got != "") != tc.want {
				t.Fatalf("tvdbOrderNote = %q, want a note: %v", got, tc.want)
			}
			if got != "" && !strings.Contains(got, "TheTVDB order") {
				t.Errorf("the note should name the choice: %q", got)
			}
		})
	}
}

func TestImportNoteKeepsTheOrderHintOnItsOwnLine(t *testing.T) {
	left := leftOutNote(3, false, true, false)
	hint := tvdbOrderNote(3, false, true)
	got := importNote(0, 0, left, []string{"/tv/Show/S04E03-E04.mkv"}, hint)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 || lines[0] != left || lines[1] != "Files left out: S04E03-E04.mkv." || lines[2] != hint {
		t.Errorf("note lines = %q", lines)
	}
	if got := importNote(0, 0, "", nil, hint); got != "" {
		t.Errorf("a hint without files left out must not show: %q", got)
	}
}
