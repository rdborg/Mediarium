package api_test

import (
	"net/http"
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

// musicCounts reads library.music out of a GET /api/dashboard answer.
func musicCounts(t *testing.T, dash map[string]any) map[string]any {
	t.Helper()
	lib, _ := dash["library"].(map[string]any)
	m, ok := lib["music"].(map[string]any)
	if !ok {
		t.Fatalf("library.music is missing: %v", lib)
	}
	return m
}

func TestDashboardCountsMusicEvenWhenTheModuleIsOff(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)

	// Nothing yet: zeros, not a missing field.
	for name, client := range map[string]*http.Client{"admin": admin, "member": member} {
		m := musicCounts(t, getJSON[map[string]any](t, client, base+"/api/dashboard"))
		if m["artists"] != float64(0) || m["albums"] != float64(0) || m["downloaded"] != float64(0) || m["missing"] != float64(0) {
			t.Fatalf("%s, empty library: %v", name, m)
		}
	}

	mb, _ := newFakeMusicBrainzHits(t)
	server.TestSetMusicBrainz(mb.URL)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/settings", map[string]any{"musicEnabled": true, "musicPath": t.TempDir()}, http.StatusOK)
	artist := postJSON[map[string]any](t, admin, base+"/api/music/artists", map[string]any{
		"mbid": "11111111-1111-4111-8111-111111111111", "monitor": "none",
	}, http.StatusCreated)
	first := albumByTitle(t, artist, "First Album")
	// One album downloaded, one downloading; the other two are missing.
	if err := server.MusicRepo.SetAlbumStatus(int64(first["id"].(float64)), music.StatusDownloaded); err != nil {
		t.Fatal(err)
	}
	if err := server.MusicRepo.SetAlbumStatus(int64(albumByTitle(t, artist, "Second Album")["id"].(float64)), music.StatusDownloading); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{"artists": float64(1), "albums": float64(4), "downloaded": float64(1), "missing": float64(2)}
	check := func(when string, client *http.Client) {
		t.Helper()
		m := musicCounts(t, getJSON[map[string]any](t, client, base+"/api/dashboard"))
		for k, v := range want {
			if m[k] != v {
				t.Errorf("%s: library.music = %v, want %v", when, m, want)
				return
			}
		}
	}
	check("module on, admin", admin)
	check("module on, member", member)

	// Switching the module off hides the pages, not the numbers.
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/settings", map[string]any{"musicEnabled": false}, http.StatusOK)
	check("module off, admin", admin)
	check("module off, member", member)
}
