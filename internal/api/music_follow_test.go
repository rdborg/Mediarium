package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

func TestUpdateArtistFollowingAndProfile(t *testing.T) {
	e := newMusicEnv(t)
	artist := e.addArtist(t, "all")
	id := artist["id"]
	url := fmt.Sprintf("%s/api/music/artists/%v", e.base, id)
	profiles := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/profiles")
	lossless := profiles[1]

	// Following off: albums keep their own flags and stay wanted.
	got := postJSONMethod[map[string]any](t, e.client, http.MethodPut, url, map[string]any{"monitored": false}, http.StatusOK)
	if got["monitored"] != false || got["monitorNew"] != false || got["monitoredCount"] != float64(4) || len(got["albums"].([]any)) != 4 {
		t.Fatalf("after unfollowing: %v", got)
	}
	for _, a := range got["albums"].([]any) {
		if a.(map[string]any)["monitored"] != true {
			t.Fatalf("unfollowing must not touch the album flags: %v", a)
		}
	}
	wanted := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/wanted")
	if len(wanted) != 3 { // the three that are out; the announced one is not wanted yet
		t.Fatalf("monitored albums of an unfollowed artist are still wanted: %d", len(wanted))
	}

	// A profile, then back to the default with 0; omitted fields change nothing.
	got = postJSONMethod[map[string]any](t, e.client, http.MethodPut, url, map[string]any{"profileId": lossless["id"]}, http.StatusOK)
	if got["profileId"] != lossless["id"] || got["profileName"] != "Lossless (FLAC)" || got["monitored"] != false {
		t.Fatalf("profile: %v", got)
	}
	got = postJSONMethod[map[string]any](t, e.client, http.MethodPut, url, map[string]any{"profileId": 0, "monitored": true}, http.StatusOK)
	if got["profileId"] != float64(0) || got["profileName"] != "Lossy (MP3 320)" || got["monitored"] != true || got["monitorNew"] != true {
		t.Fatalf("back to the default, followed again: %v", got)
	}
	if got := postJSONMethod[map[string]any](t, e.client, http.MethodPut, url, map[string]any{}, http.StatusOK); got["monitored"] != true {
		t.Fatalf("an empty change keeps everything: %v", got)
	}

	if status, _ := doJSONStatus(t, e.client, http.MethodPut, url, map[string]any{"profileId": 99999}); status != http.StatusBadRequest {
		t.Fatalf("unknown profile: %d", status)
	}
	if status, _ := doJSONStatus(t, e.client, http.MethodPut, e.base+"/api/music/artists/99999", map[string]any{"monitored": true}); status != http.StatusNotFound {
		t.Fatalf("unknown artist: %d", status)
	}
}

func TestOnlyAdministratorsUpdateArtists(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	server.TestSetMusicBrainz("http://127.0.0.1:1")
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/modules", map[string]any{"music": true}, http.StatusOK)
	if status, _ := doJSONStatus(t, member, http.MethodPut, base+"/api/music/artists/1", map[string]any{"monitored": false}); status != http.StatusForbidden {
		t.Fatalf("member update: %d", status)
	}
}

func TestRefreshPicksUpNewReleasesOfFollowedArtists(t *testing.T) {
	e := newMusicEnv(t)
	const fixtureArtist = "11111111-1111-4111-8111-111111111111"
	// Two artists with the same MusicBrainz id cannot exist, so use the
	// fixture artist once, followed, with only two of its albums known and
	// one with the wrong date.
	followed, _, err := e.server.MusicRepo.AddArtist(music.Artist{MBID: fixtureArtist, Name: "Fixture Band", Monitored: true, MonitorNew: true}, []music.Album{
		{MBID: "aaaaaaaa-0000-4000-8000-000000000001", Title: "First Album", ReleaseDate: "1990", Monitored: false},
		{MBID: "aaaaaaaa-0000-4000-8000-000000000005", Title: "Upcoming Album", ReleaseDate: "", Monitored: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	// An unfollowed artist is not checked (the fake would give it the fixture's albums if it were).
	unfollowed, _, err := e.server.MusicRepo.AddArtist(music.Artist{MBID: "22222222-2222-4222-8222-222222222222", Name: "Bandleader", Monitored: false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	e.server.TestRefreshMusicArtists(context.Background())

	albums, err := e.server.MusicRepo.ListAlbums(followed.ID)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]music.Album{}
	for _, a := range albums {
		byTitle[a.Title] = a
	}
	if len(albums) != 4 {
		t.Fatalf("want the four release groups of the fixture artist, got %d: %+v", len(albums), albums)
	}
	for _, title := range []string{"Second Album", "A Single"} {
		if a, ok := byTitle[title]; !ok || !a.Monitored || a.Status != music.StatusMissing {
			t.Errorf("%s should be new and monitored: %+v", title, a)
		}
	}
	if byTitle["A Single"].Type != music.TypeSingle {
		t.Errorf("type: %+v", byTitle["A Single"])
	}
	if byTitle["First Album"].ReleaseDate != "1999-03-01" || byTitle["First Album"].Monitored {
		t.Errorf("an existing album keeps its flags and takes MusicBrainz's date: %+v", byTitle["First Album"])
	}
	if byTitle["Upcoming Album"].ReleaseDate != "2099-01-01" {
		t.Errorf("an announced album gets its date: %+v", byTitle["Upcoming Album"])
	}

	// Running it again adds nothing.
	e.server.TestRefreshMusicArtists(context.Background())
	if again, _ := e.server.MusicRepo.ListAlbums(followed.ID); len(again) != 4 {
		t.Fatalf("second run: %d albums", len(again))
	}

	if none, _ := e.server.MusicRepo.ListAlbums(unfollowed.ID); len(none) != 0 {
		t.Fatalf("an unfollowed artist is not checked: %+v", none)
	}
}
