package api_test

import (
	"net/http"
	"path/filepath"
	"testing"
)

// Settings > Library and files > Folders says where a title added without a folder
// choice goes: the main folder, one of the extra folders, or the folder with
// the most room. The Add dialog, a request, an import list or the Radarr
// compatible API all follow it; an explicit choice still wins.
func TestNewMoviesGoWhereTheFoldersSettingSays(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newTitleTMDB(t).URL)
	main, usb := t.TempDir(), t.TempDir()
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesPath": main, "moviesExtraPaths": []string{usb}}, http.StatusOK)

	// Only the folders in the list can be the rule.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesNewFolder": "/nowhere"}, http.StatusBadRequest)
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesNewFolder": main}, http.StatusBadRequest)
	got := putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesNewFolder": usb}, http.StatusOK)
	if filepath.Clean(got["moviesNewFolder"].(string)) != filepath.Clean(usb) {
		t.Fatalf("the rule is saved and returned: %v", got["moviesNewFolder"])
	}

	// The folders answer says where a title goes right now.
	folders := getJSON[map[string]any](t, client, base+"/api/library/folders")
	newTitles, _ := folders["newTitles"].(map[string]any)
	movies, _ := newTitles["movies"].(map[string]any)
	if filepath.Clean(movies["folder"].(string)) != filepath.Clean(usb) || movies["mostRoom"] != false {
		t.Fatalf("newTitles.movies = %v", movies)
	}

	// No choice: the rule. An explicit main folder: the main folder.
	added := postJSON[map[string]any](t, client, base+"/api/movies", map[string]any{"tmdbId": 603}, http.StatusCreated)
	if filepath.Clean(added["rootPath"].(string)) != filepath.Clean(usb) {
		t.Fatalf("a movie added without a choice follows the rule: %v", added["rootPath"])
	}
	other := postJSON[map[string]any](t, client, base+"/api/movies", map[string]any{"tmdbId": 700, "rootPath": main}, http.StatusCreated)
	if other["rootPath"] != nil && other["rootPath"] != "" {
		t.Fatalf("an explicit main folder stays the main folder: %v", other)
	}

	// The dashboard shows a card for each folder, the main one first, with
	// the one new movies go to marked.
	dash := getJSON[map[string]any](t, client, base+"/api/dashboard")
	cards, _ := dash["folders"].([]any)
	if len(cards) != 7 {
		t.Fatalf("a card for each movie folder, the other kinds and downloads: %d cards", len(cards))
	}
	first, second := cards[0].(map[string]any), cards[1].(map[string]any)
	if first["key"] != "movies" || first["newTitles"] != nil || second["key"] != "movies-extra-1" || second["newTitles"] != true || filepath.Clean(second["path"].(string)) != filepath.Clean(usb) {
		t.Fatalf("movie cards = %v, %v", first, second)
	}

	// "Most room" is accepted, shown as such, and with both folders on one disk
	// in the tests the tie goes to the main folder.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesNewFolder": "most-room"}, http.StatusOK)
	settings := getJSON[map[string]any](t, client, base+"/api/settings")
	if settings["moviesNewFolder"] != "most-room" {
		t.Fatalf("the rule reads back: %v", settings["moviesNewFolder"])
	}
	folders = getJSON[map[string]any](t, client, base+"/api/library/folders")
	newTitles, _ = folders["newTitles"].(map[string]any)
	movies, _ = newTitles["movies"].(map[string]any)
	if movies["mostRoom"] != true || movies["folder"] == "" {
		t.Fatalf("newTitles.movies = %v", movies)
	}

	// Taking the folder out of the list makes the rule fall back to the main folder.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesNewFolder": usb}, http.StatusOK)
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesExtraPaths": []string{}}, http.StatusOK)
	settings = getJSON[map[string]any](t, client, base+"/api/settings")
	if settings["moviesNewFolder"] != "" {
		t.Fatalf("a rule naming a folder that is gone reads as the main folder: %v", settings["moviesNewFolder"])
	}
	// And the list and the rule can be saved together, the rule checked against the new list.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesExtraPaths": []string{usb}, "moviesNewFolder": usb}, http.StatusOK)
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"moviesExtraPaths": []string{}, "moviesNewFolder": usb}, http.StatusBadRequest)
}

func TestNewShowsGoWhereTheFoldersSettingSays(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newTVTMDBServer(t, nil).URL)
	main, usb := t.TempDir(), t.TempDir()
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"tvPath": main, "tvExtraPaths": []string{usb}, "tvNewFolder": usb}, http.StatusOK)

	added := postJSON[map[string]any](t, client, base+"/api/series", map[string]any{"tmdbId": 1399}, http.StatusCreated)
	if filepath.Clean(added["rootPath"].(string)) != filepath.Clean(usb) {
		t.Fatalf("a show added without a choice follows the rule: %v", added["rootPath"])
	}
	// The show's page reads the same.
	shows := getJSON[[]map[string]any](t, client, base+"/api/series")
	if len(shows) != 1 || filepath.Clean(shows[0]["rootPath"].(string)) != filepath.Clean(usb) {
		t.Fatalf("the list says where the show lives: %v", shows)
	}
}
