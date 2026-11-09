package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// The one music search box (#143): artists, albums and songs in one ranked
// list, each with its kind; a source that fails is left out with a note and
// does not hide the others.
func TestMusicSearchAllMixesAndRanks(t *testing.T) {
	const artist = "33333333-3333-4333-8333-333333333333"
	const soundtrack = "bbbbbbbb-0000-4000-8000-000000000009"
	const single = "bbbbbbbb-0000-4000-8000-000000000008"
	var failArtists atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/2/artist", func(w http.ResponseWriter, r *http.Request) {
		if failArtists.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"artists":[{"id":%q,"name":"Interstellar","type":"Group","score":100,"disambiguation":"UK band"}]}`, artist)
	})
	mux.HandleFunc("/ws/2/release-group", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"release-groups":[
			{"id":%q,"title":"Interstellar","primary-type":"Single","first-release-date":"2019-01-01","score":100,"artist-credit":[{"name":"Fleeting Echo","artist":{"id":"44444444-4444-4444-8444-444444444444","name":"Fleeting Echo"}}]},
			{"id":%q,"title":"Interstellar (Original Motion Picture Soundtrack)","primary-type":"Album","secondary-types":["Soundtrack"],"first-release-date":"2014-11-17","score":95,"artist-credit":[{"name":"Hans Zimmer","artist":{"id":"55555555-5555-4555-8555-555555555555","name":"Hans Zimmer"}}]}]}`, single, soundtrack)
	})
	mux.HandleFunc("/ws/2/recording", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"recordings":[{"title":"Interstellar Theme","score":90,"artist-credit":[{"name":"Hans Zimmer","artist":{"id":"55555555-5555-4555-8555-555555555555","name":"Hans Zimmer"}}],"releases":[
			{"status":"Official","date":"2015-01-01","release-group":{"id":%q,"title":"Space Themes","primary-type":"Album","secondary-types":["Compilation"]}},
			{"status":"Official","date":"2014-11-17","release-group":{"id":%q,"title":"Interstellar (Original Motion Picture Soundtrack)","primary-type":"Album","secondary-types":["Soundtrack"]}}]}]}`, single, soundtrack)
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

	ans := getJSON[map[string]any](t, client, base+"/api/music/search?type=all&q=Interstellar")
	items := ans["items"].([]any)
	if len(items) != 4 || ans["note"] != nil {
		t.Fatalf("want 4 rows (1 artist, 2 albums, 1 song) and no note: %v", ans)
	}
	first := items[0].(map[string]any)
	if first["kind"] != "album" || first["mbid"] != soundtrack || first["soundtrack"] != true {
		t.Fatalf("the film's soundtrack should be first: %v", first)
	}
	kinds := map[string]int{}
	for _, it := range items {
		m := it.(map[string]any)
		kinds[m["kind"].(string)]++
		if m["kind"] == "song" && (m["song"] != "Interstellar Theme" || m["mbid"] != soundtrack) {
			t.Errorf("a song sits on its soundtrack album, not the compilation: %v", m)
		}
	}
	if kinds["artist"] != 1 || kinds["album"] != 2 || kinds["song"] != 1 {
		t.Fatalf("kinds: %v", kinds)
	}

	// One source failing does not hide the rest.
	failArtists.Store(true)
	ans = getJSON[map[string]any](t, client, base+"/api/music/search?type=all&q=Interstellar+Theme")
	if ans["note"] == nil || len(ans["items"].([]any)) == 0 {
		t.Fatalf("expected the albums and songs with a note about the artists: %v", ans)
	}
	// The older answer is unchanged: artists only, as a plain list.
	failArtists.Store(false)
	artists := getJSON[[]map[string]any](t, client, base+"/api/music/search?q=Interstellar+Band")
	if len(artists) != 1 || artists[0]["name"] != "Interstellar" {
		t.Fatalf("the artists-only search still answers a plain list: %v", artists)
	}
}
