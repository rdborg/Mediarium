package music_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/origin"
)

// An artist keeps how it came into the library. Its albums come the same way,
// by the same person, unless an album says otherwise; an album added later
// keeps its own origin, and one added without any (as everything from before
// this was kept) has none.
func TestArtistAndAlbumOrigins(t *testing.T) {
	repo := newRepo(t)
	follow := origin.Origin{Kind: origin.Auto, Name: origin.JobFollow}
	artist, albums, err := repo.AddArtist(music.Artist{MBID: "mb-1", Name: "Artist", AddedBy: 7, Origin: origin.Origin{Kind: origin.Request}}, []music.Album{
		{MBID: "rg-1", Title: "With the artist"},
		{MBID: "rg-2", Title: "Its own", Origin: origin.Origin{Kind: origin.Manual, UserID: 3}},
	})
	if err != nil || len(albums) != 2 {
		t.Fatalf("add artist: %v", err)
	}
	later, err := repo.AddAlbums(artist.ID, []music.Album{
		{MBID: "rg-3", Title: "Found by following", Origin: follow},
		{MBID: "rg-4", Title: "From before", Origin: origin.Origin{}},
		{MBID: "rg-5", Title: "Odd kind", Origin: origin.Origin{Kind: "teleport", UserID: 9}},
	})
	if err != nil || len(later) != 3 {
		t.Fatalf("add albums: %v", err)
	}
	got, err := repo.GetArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != (origin.Origin{Kind: origin.Request}) || got.AddedBy != 7 {
		t.Fatalf("artist: %+v", got)
	}
	list, err := repo.ListArtists()
	if err != nil || len(list) != 1 || list[0].Origin.Kind != origin.Request {
		t.Fatalf("listed artist: %+v %v", list, err)
	}
	want := map[string]origin.Origin{
		"With the artist":    {Kind: origin.Request, UserID: 7},
		"Its own":            {Kind: origin.Manual, UserID: 3},
		"Found by following": follow,
		"From before":        {},
		"Odd kind":           {},
	}
	stored, err := repo.ListAlbums(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range stored {
		if a.Origin != want[a.Title] {
			t.Errorf("%s: origin %+v, want %+v", a.Title, a.Origin, want[a.Title])
		}
		one, err := repo.GetAlbum(a.ID)
		if err != nil || one.Origin != a.Origin {
			t.Errorf("%s: GetAlbum origin %+v, %v", a.Title, one.Origin, err)
		}
	}
}
