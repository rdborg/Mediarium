package mediaservers

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestPlexRefreshesTheMusicLibraryFolder(t *testing.T) {
	plex := newFakePlex(t, "tok")
	plex.sections = []Library{
		{ID: "1", Title: "Movies", Type: "movie", Locations: []string{"/data/movies"}},
		{ID: "2", Title: "Music", Type: "artist", Locations: []string{"/data/music"}},
		{ID: "3", Title: "More Music", Type: "artist", Locations: []string{"/data/other-music"}},
	}
	c := NewClient("test")
	tests := []struct {
		name    string
		pathMap []PathMapping
		folders []string
		want    []string
	}{
		{"an album folder is scanned in its music library, with the path mapping", []PathMapping{{From: "/music", To: "/data/music"}},
			[]string{"/music/Radiohead/OK Computer (1997)"}, []string{"2 /data/music/Radiohead/OK Computer (1997)"}},
		{"two albums are two scans", nil,
			[]string{"/data/music/A/One (2001)", "/data/music/A/Two (2002)"}, []string{"2 /data/music/A/One (2001)", "2 /data/music/A/Two (2002)"}},
		{"a folder no library holds scans every music library, never the movies", nil,
			[]string{"/music/Radiohead/OK Computer (1997)"}, []string{"2", "3"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plex.mu.Lock()
			plex.refreshes = nil
			plex.mu.Unlock()
			s := plex.server()
			s.PathMap = tc.pathMap
			if _, err := c.RefreshFolders(context.Background(), s, MediaMusic, tc.folders); err != nil {
				t.Fatal(err)
			}
			plex.mu.Lock()
			got := append([]string(nil), plex.refreshes...)
			plex.mu.Unlock()
			sort.Strings(got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("refreshes = %q, want %q", got, tc.want)
			}
		})
	}

	// Without a music library on the server the answer says so in plain words.
	movies := newFakePlex(t, "tok")
	movies.sections = []Library{{ID: "1", Title: "Movies", Type: "movie", Locations: []string{"/data/movies"}}}
	_, err := c.RefreshFolders(context.Background(), movies.server(), MediaMusic, []string{"/music/A/B"})
	if err == nil || err.Error() != "Plex has no music library to refresh." {
		t.Fatalf("want a plain message, got %v", err)
	}
}

func TestEmbyAndJellyfinGetTheAlbumFolder(t *testing.T) {
	for _, kind := range []Kind{KindJellyfin, KindEmby} {
		t.Run(string(kind), func(t *testing.T) {
			f := newFakeEmby(t, kind, "k")
			f.locations = []string{"/media/music"}
			s := f.server()
			s.PathMap = []PathMapping{{From: "/music", To: "/media/music"}}
			if _, err := NewClient("test").RefreshFolders(context.Background(), s, MediaMusic, []string{"/music/A/One (2001)", "/music/A/One (2001)"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.updated, []string{"/media/music/A/One (2001)"}) {
				t.Fatalf("updated = %q", f.updated)
			}
		})
	}
}

func TestRefresherDebouncesMusicImportsPerAlbumFolder(t *testing.T) {
	plex := newFakePlex(t, "tok")
	plex.sections = []Library{
		{ID: "1", Title: "Movies", Type: "movie", Locations: []string{"/data/movies"}},
		{ID: "2", Title: "Music", Type: "artist", Locations: []string{"/data/music"}},
	}
	p := plex.server()
	p.PathMap = []PathMapping{{From: "/music", To: "/data/music"}}
	store := &memStore{servers: []Server{p}}
	r := NewRefresher(store, NewClient("test"))
	r.Delay = time.Hour // only Flush runs it

	album := filepath.Join("/music", "Radiohead", "OK Computer (1997)")
	for _, track := range []string{"01 - Airbag.flac", "02 - Paranoid Android.flac", "03 - Subterranean Homesick Alien.flac"} {
		r.Imported(MediaMusic, filepath.Join(album, track))
	}
	r.Imported(MediaMusic, filepath.Join("/music", "Radiohead", "Kid A (2000)", "01 - Everything In Its Right Place.flac"))
	r.Flush()

	sort.Strings(plex.refreshes)
	want := []string{"2 /data/music/Radiohead/Kid A (2000)", "2 /data/music/Radiohead/OK Computer (1997)"}
	if !reflect.DeepEqual(plex.refreshes, want) {
		t.Fatalf("plex refreshes = %q, want %q", plex.refreshes, want)
	}
}
