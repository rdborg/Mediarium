package music_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/store"
)

func newRepo(t *testing.T) *music.Repo {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return music.NewRepo(db)
}

func TestArtistsAlbumsAndTracks(t *testing.T) {
	r := newRepo(t)
	artist, albums, err := r.AddArtist(music.Artist{MBID: "mb-radiohead", Name: "Radiohead", SortName: "Radiohead", Monitored: true, MonitorNew: true, AddedBy: 1},
		[]music.Album{
			{MBID: "rg-okc", Title: "OK Computer", Type: music.TypeAlbum, ReleaseDate: "1997-05-21", Monitored: true},
			{MBID: "rg-ph", Title: "Pablo Honey", ReleaseDate: "1993", Monitored: false},
			{MBID: "rg-okc", Title: "OK Computer (listed twice)"},
			{MBID: "rg-new", Title: "Next One"},
		})
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	if artist.ID == 0 || artist.AddedAt == "" || artist.AddedBy != 1 || len(albums) != 3 {
		t.Fatalf("artist %+v, %d albums", artist, len(albums))
	}
	if albums[1].Type != music.TypeAlbum || albums[1].Status != music.StatusMissing || albums[0].Year() != 1997 || albums[2].Year() != 0 {
		t.Fatalf("defaults: %+v", albums)
	}
	if _, _, err := r.AddArtist(music.Artist{MBID: "mb-radiohead", Name: "Again"}, nil); !errors.Is(err, music.ErrArtistExists) {
		t.Fatalf("adding twice: %v", err)
	}
	if a, ok, err := r.GetArtistByMBID("mb-radiohead"); err != nil || !ok || a.ID != artist.ID {
		t.Fatalf("by mbid: %+v %v %v", a, ok, err)
	}
	if _, ok, err := r.GetArtistByMBID("nope"); err != nil || ok {
		t.Fatalf("unknown mbid: %v %v", ok, err)
	}

	list, err := r.ListAlbums(artist.ID)
	if err != nil || len(list) != 3 || list[0].Title != "Pablo Honey" || list[2].Title != "Next One" {
		t.Fatalf("albums oldest first, undated last: %+v %v", list, err)
	}

	okc := albums[0]
	if err := r.ReplaceTracks(okc.ID, []music.Track{{Disc: 1, Position: 1, Title: "Airbag", LengthMs: 284000}, {Disc: 1, Position: 2, Title: "Paranoid Android"}}); err != nil {
		t.Fatalf("tracks: %v", err)
	}
	tracks, _ := r.ListTracks(okc.ID)
	if err := r.SetTrackFile(tracks[0].ID, "/music/Radiohead/OK Computer (1997)/01 - Airbag.flac"); err != nil {
		t.Fatal(err)
	}
	// A new tracklist keeps the file of a track still listed, drops the rest.
	if err := r.ReplaceTracks(okc.ID, []music.Track{{Disc: 1, Position: 1, Title: "Airbag (Remastered)"}, {Disc: 2, Position: 1, Title: "Lucky"}}); err != nil {
		t.Fatalf("replace tracks: %v", err)
	}
	tracks, _ = r.ListTracks(okc.ID)
	if len(tracks) != 2 || tracks[0].Title != "Airbag (Remastered)" || tracks[0].FilePath == "" || tracks[1].Disc != 2 {
		t.Fatalf("tracks after replace: %+v", tracks)
	}

	if err := r.SetAlbumStatus(okc.ID, music.StatusDownloading); err != nil {
		t.Fatal(err)
	}
	if err := r.SetAlbumImported(okc.ID, music.TierFLAC, "/music/Radiohead/OK Computer (1997)"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetAlbumRelease(okc.ID, "rel-1"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetAlbumMonitored(okc.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetAlbum(okc.ID)
	if err != nil || got.Status != music.StatusDownloaded || got.Quality != "FLAC" || got.Path == "" || got.ReleaseMBID != "rel-1" || got.Monitored {
		t.Fatalf("album after import: %+v %v", got, err)
	}
	if err := r.SetAlbumMonitored(999, true); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown album: %v", err)
	}

	sums, err := r.ListArtists()
	if err != nil || len(sums) != 1 || sums[0].Albums != 3 || sums[0].Downloaded != 1 || sums[0].Monitored != 0 {
		t.Fatalf("summaries: %+v %v", sums, err)
	}
	all, _ := r.ListAllAlbums()
	if len(all) != 3 {
		t.Fatalf("all albums: %d", len(all))
	}

	if err := r.DeleteArtist(artist.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetAlbum(okc.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("albums go with the artist: %v", err)
	}
	if tr, _ := r.ListTracks(okc.ID); len(tr) != 0 {
		t.Fatalf("tracks go too: %v", tr)
	}
	if err := r.DeleteArtist(artist.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleting twice: %v", err)
	}
}

func TestSeedPresetsOnceWithFallback(t *testing.T) {
	r := newRepo(t)
	for i := 0; i < 2; i++ {
		if err := r.SeedPresets(); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	ps, err := r.ListProfiles()
	if err != nil || len(ps) != 2 {
		t.Fatalf("profiles: %+v %v", ps, err)
	}
	def, ok := music.ResolveProfile(ps, 0)
	if !ok || def.Name != music.PresetLossy || len(def.FallbackProfiles) != 0 {
		t.Fatalf("default: %+v", def)
	}
	if p, _ := music.ResolveProfile(ps, ps[1].ID); p.Name != music.PresetLossless || len(p.FallbackProfiles) != 1 || p.FallbackProfiles[0].Name != music.PresetLossy {
		t.Fatalf("by id: %+v", p)
	}
	if p, _ := music.ResolveProfile(ps, 4242); p.Name != music.PresetLossy {
		t.Fatalf("unknown id falls back to the default: %+v", p)
	}
	if _, ok := music.ResolveProfile(nil, 0); ok {
		t.Fatal("no profiles")
	}
}
