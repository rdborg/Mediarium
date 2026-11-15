package api

import (
	"errors"
	"slices"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/music"
)

func TestAlbumQueries(t *testing.T) {
	cases := []struct {
		artist, album string
		want          []string
	}{
		{"Radiohead", "OK Computer", []string{"Radiohead OK Computer"}},
		{"Lichterkinder", "Weihnacht auch für Dich", []string{"Lichterkinder Weihnacht auch für Dich", "Lichterkinder Weihnacht auch fuer Dich"}},
		{"Björk", "Vespertine", []string{"Björk Vespertine", "Bjoerk Vespertine"}},
		{"Mötley Crüe", "Dr. Feelgood", []string{"Mötley Crüe Dr. Feelgood", "Moetley Cruee Dr. Feelgood"}},
		{"Die Ärzte", "Jazz ist anders", []string{"Die Ärzte Jazz ist anders", "Die Aerzte Jazz ist anders"}},
		{"Die Aerzte", "Jazz ist anders", []string{"Die Aerzte Jazz ist anders"}},
	}
	for _, tc := range cases {
		got := albumQueries(music.Artist{Name: tc.artist}, music.Album{Title: tc.album})
		if !slices.Equal(got, tc.want) {
			t.Errorf("albumQueries(%q, %q) = %q, want %q", tc.artist, tc.album, got, tc.want)
		}
	}
}

func TestQueriesFromOutcomes(t *testing.T) {
	outs := []indexers.Outcome{
		{IndexerName: "A", Query: "x y", Categories: []int{3040}, Results: make([]indexers.Result, 3)},
		{IndexerName: "A", Query: "x y", Categories: []int{3040}, Results: make([]indexers.Result, 2)},
		{IndexerName: "A", Query: "x z", Err: errors.New("down")},
		{IndexerName: "A", Query: "paused", Skipped: true},
	}
	got := queriesFrom(outs)
	want := []searchedQuery{{Query: "x y", Categories: []int{3040}, Count: 5}, {Query: "x z", Failed: true}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i].Query != want[i].Query || got[i].Count != want[i].Count || got[i].Failed != want[i].Failed {
			t.Errorf("query %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAlbumFilteredCounts(t *testing.T) {
	list := []musicReleasePayload{
		{Rejections: []string{"another artist"}},
		{Rejections: []string{"another album"}},
		{Rejections: []string{"another artist", "blocklisted"}},
		{},
	}
	got := albumFilteredCounts(list)
	if len(got) != 3 || got[0].Reason != "another artist" || got[0].Count != 2 {
		t.Fatalf("got %+v", got)
	}
}
