package music_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

func TestOnlySongMarksJustThatTrackAsWanted(t *testing.T) {
	r := newRepo(t)
	_, albums, err := r.AddArtist(music.Artist{MBID: "mb-hz", Name: "Hans Zimmer", Monitored: true},
		[]music.Album{{MBID: "rg-int", Title: "Interstellar", Monitored: true}, {MBID: "rg-inc", Title: "Inception", Monitored: true}})
	if err != nil {
		t.Fatal(err)
	}
	id := albums[0].ID
	list := []music.Track{
		{Disc: 1, Position: 1, Title: "Dreaming of the Crash"},
		{Disc: 1, Position: 2, Title: "Cornfield Chase"},
		{Disc: 1, Position: 3, Title: "Dust"},
		{Disc: 1, Position: 4, Title: "Stay (Reprise)"},
	}
	if err := r.ReplaceTracks(id, list); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		song   string
		wanted []string
	}{
		{"no song: every track stays wanted", "", []string{"Dreaming of the Crash", "Cornfield Chase", "Dust", "Stay (Reprise)"}},
		{"one song, other case and accents", "cornfíeld CHASE", []string{"Cornfield Chase"}},
		{"a bracketed part does not matter", "Stay", []string{"Stay (Reprise)"}},
		{"unknown song: nothing is switched off", "Not On This Album", []string{"Dreaming of the Crash", "Cornfield Chase", "Dust", "Stay (Reprise)"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// start from all wanted, as after a fresh tracklist
			all, _ := r.ListTracks(id)
			for _, tr := range all {
				if err := r.SetTrackMonitored(tr.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.SetOnlySong(id, tc.song); err != nil {
				t.Fatal(err)
			}
			n, err := r.ApplyOnlySong(id)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(tc.wanted) {
				t.Fatalf("wanted count = %d, want %d", n, len(tc.wanted))
			}
			got, _ := r.ListTracks(id)
			var names []string
			for _, tr := range got {
				if tr.Monitored {
					names = append(names, tr.Title)
				}
			}
			if len(names) != len(tc.wanted) {
				t.Fatalf("wanted tracks = %v, want %v", names, tc.wanted)
			}
			for i := range names {
				if names[i] != tc.wanted[i] {
					t.Fatalf("wanted tracks = %v, want %v", names, tc.wanted)
				}
			}
		})
	}

	// Another album is not touched, and a refreshed tracklist keeps the flags.
	if song, _ := r.OnlySong(albums[1].ID); song != "" {
		t.Fatalf("other album has a wanted song: %q", song)
	}
	_ = r.SetOnlySong(id, "Dust")
	if _, err := r.ApplyOnlySong(id); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceTracks(id, list); err != nil {
		t.Fatal(err)
	}
	tr, _ := r.ListTracks(id)
	if tr[0].Monitored || !tr[2].Monitored {
		t.Fatalf("flags lost on refresh: %+v", tr)
	}
}
