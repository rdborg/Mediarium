package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Music can be searched by album or by song, and a soundtrack found through a
// song from it can be added on its own, even though it isn't one of the
// artist's own albums (#143, SanketBaraiya).
func TestMusicSearchByAlbumAndSong(t *testing.T) {
	const artist = "33333333-3333-4333-8333-333333333333"
	const soundtrack = "bbbbbbbb-0000-4000-8000-000000000009"
	const studio = "bbbbbbbb-0000-4000-8000-000000000001"
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/2/release-group", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("artist") != "" { // the artist's own albums
			fmt.Fprintf(w, `{"release-groups":[{"id":%q,"title":"Studio One","primary-type":"Album","first-release-date":"2010-01-01"}],"release-group-count":1}`, studio)
			return
		}
		fmt.Fprintf(w, `{"release-groups":[{"id":%q,"title":"Interstellar","primary-type":"Album","secondary-types":["Soundtrack"],"first-release-date":"2014-11-17","score":100,"artist-credit":[{"name":"Hans Zimmer","artist":{"id":%q,"name":"Hans Zimmer"}}]}]}`, soundtrack, artist)
	})
	mux.HandleFunc("/ws/2/recording", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"recordings":[{"title":"Cornfield Chase","score":100,"artist-credit":[{"name":"Hans Zimmer","artist":{"id":%q,"name":"Hans Zimmer"}}],"releases":[
			{"status":"Official","date":"2014-11-17","release-group":{"id":%q,"title":"Interstellar","primary-type":"Album","secondary-types":["Soundtrack"]}},
			{"status":"Official","date":"2015-01-01","release-group":{"id":%q,"title":"Interstellar","primary-type":"Album","secondary-types":["Soundtrack"]}}]}]}`, artist, soundtrack, soundtrack)
	})
	mux.HandleFunc("/ws/2/release-group/"+soundtrack, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":%q,"title":"Interstellar","primary-type":"Album","secondary-types":["Soundtrack"],"first-release-date":"2014-11-17","artist-credit":[{"name":"Hans Zimmer","artist":{"id":%q,"name":"Hans Zimmer"}}]}`, soundtrack, artist)
	})
	mux.HandleFunc("/ws/2/artist/"+artist, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":%q,"name":"Hans Zimmer","sort-name":"Zimmer, Hans","type":"Person","country":"DE"}`, artist)
	})
	mb := httptest.NewServer(mux)
	t.Cleanup(mb.Close)

	server, base, client := loginNewServer(t)
	server.TestSetMusicBrainz(mb.URL)
	musicRoot := filepath.Join(t.TempDir(), "music")
	if err := os.MkdirAll(musicRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"music": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"musicPath": musicRoot}, http.StatusOK)

	albums := getJSON[[]map[string]any](t, client, base+"/api/music/search?type=album&q=Interstellar")
	if len(albums) != 1 || albums[0]["title"] != "Interstellar" || albums[0]["artistName"] != "Hans Zimmer" || albums[0]["inLibrary"] != false {
		t.Fatalf("album search: %v", albums)
	}
	songs := getJSON[[]map[string]any](t, client, base+"/api/music/search?type=song&q=Cornfield+Chase")
	if len(songs) != 1 || songs[0]["song"] != "Cornfield Chase" || songs[0]["mbid"] != soundtrack {
		t.Fatalf("song search should give the soundtrack once: %v", songs)
	}

	added := postJSON[map[string]any](t, client, base+"/api/music/artists", map[string]any{"mbid": artist, "monitor": "none", "albumMbid": soundtrack}, http.StatusCreated)
	full := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/music/artists/%v", base, added["id"]))
	found := false
	for _, a := range full["albums"].([]any) {
		if al := a.(map[string]any); al["mbid"] == soundtrack && al["monitored"] == true {
			found = true
		}
	}
	if !found {
		t.Fatalf("the soundtrack should be on the artist and wanted: %v", full["albums"])
	}
	again := getJSON[[]map[string]any](t, client, base+"/api/music/search?type=album&q=Interstellar")
	if again[0]["inLibrary"] != true {
		t.Errorf("the search should now say the album is in the library: %v", again)
	}
}
