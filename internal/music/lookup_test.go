package music_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

func TestLookupsByMBIDAndCounts(t *testing.T) {
	r := newRepo(t)
	if c, err := r.Counts(); err != nil || c != (music.Counts{}) {
		t.Fatalf("empty library counts: %+v %v", c, err)
	}
	a1, albums, err := r.AddArtist(music.Artist{MBID: "mb-a", Name: "A"}, []music.Album{
		{MBID: "rg-1", Title: "One"}, {MBID: "rg-2", Title: "Two"}, {MBID: "rg-3", Title: "Three"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.AddArtist(music.Artist{MBID: "mb-b", Name: "B"}, []music.Album{{MBID: "rg-1", Title: "One (split)"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetAlbumStatus(albums[0].ID, music.StatusDownloaded); err != nil {
		t.Fatal(err)
	}
	if err := r.SetAlbumStatus(albums[1].ID, music.StatusDownloading); err != nil {
		t.Fatal(err)
	}

	got, err := r.AlbumsByMBID([]string{"rg-1", "rg-3", "rg-none"})
	if err != nil || len(got) != 2 || got["rg-1"].ArtistID != a1.ID || got["rg-3"].Title != "Three" {
		t.Fatalf("albums by mbid (split release goes to the first artist): %+v %v", got, err)
	}
	if got, err := r.AlbumsByMBID(nil); err != nil || len(got) != 0 {
		t.Fatalf("no ids: %+v %v", got, err)
	}
	artists, err := r.ArtistsByMBID([]string{"mb-a", "mb-b", "mb-none"})
	if err != nil || len(artists) != 2 || artists["mb-b"].Name != "B" {
		t.Fatalf("artists by mbid: %+v %v", artists, err)
	}

	c, err := r.Counts()
	// 4 albums: one downloaded, one downloading (neither missing), two missing.
	if err != nil || c != (music.Counts{Artists: 2, Albums: 4, Downloaded: 1, Missing: 2}) {
		t.Fatalf("counts: %+v %v", c, err)
	}
}
