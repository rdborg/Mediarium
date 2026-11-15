package music

import (
	"errors"
	"reflect"
	"testing"
)

func TestNewManualID(t *testing.T) {
	if got, err := NewManualID("CAT-001"); err != nil || got != "manual:CAT-001" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := NewManualID("bad id"); !errors.Is(err, ErrBadExternalID) {
		t.Errorf("a space should be refused: %v", err)
	}
	a, _ := NewManualID("")
	b, _ := NewManualID("")
	if !IsManualID(a) || a == b {
		t.Errorf("random ids: %q %q", a, b)
	}
	if IsManualID("aaaaaaaa-0000-4000-8000-000000000001") || !(Album{MBID: a}).Manual() || !(Artist{MBID: a}).Manual() {
		t.Error("IsManualID / Manual")
	}
}

func TestTracksFromFiles(t *testing.T) {
	type pos struct {
		Disc, Position int
		Title          string
	}
	cases := []struct {
		name  string
		files []FileInfo
		want  []pos
	}{
		{"none", nil, nil},
		{"numbered", []FileInfo{
			{Path: "/d/02 - B.flac", Track: 2, Title: "B"},
			{Path: "/d/01 - A.flac", Track: 1, Title: "A"},
		}, []pos{{1, 1, "A"}, {1, 2, "B"}}},
		{"two discs", []FileInfo{
			{Path: "/d/CD2/01 - C.flac", Disc: 2, Track: 1, Title: "C"},
			{Path: "/d/CD1/01 - A.flac", Disc: 1, Track: 1, Title: "A"},
		}, []pos{{1, 1, "A"}, {2, 1, "C"}}},
		{"no numbers: name order", []FileInfo{
			{Path: "/d/b song.mp3", Title: "b song"},
			{Path: "/d/a song.mp3", Title: "a song"},
		}, []pos{{1, 1, "a song"}, {1, 2, "b song"}}},
		{"clashing numbers fall back to name order", []FileInfo{
			{Path: "/d/x.mp3", Track: 1, Title: "x"},
			{Path: "/d/y.mp3", Track: 1, Title: "y"},
		}, []pos{{1, 1, "x"}, {1, 2, "y"}}},
		{"missing title uses the file name", []FileInfo{{Path: "/d/Lonely Song.ogg", Track: 4}}, []pos{{1, 4, "Lonely Song"}}},
	}
	for _, tc := range cases {
		got := TracksFromFiles(tc.files)
		var have []pos
		for _, g := range got {
			have = append(have, pos{g.Disc, g.Position, g.Title})
			if !g.Monitored {
				t.Errorf("%s: tracks start wanted", tc.name)
			}
		}
		if !reflect.DeepEqual(have, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, have, tc.want)
		}
	}
}
