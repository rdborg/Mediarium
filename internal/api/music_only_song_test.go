package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// "Add only this song" (#143): the album is added and downloaded as a whole,
// but only the chosen song is imported and wanted; the other tracks are listed
// as not wanted, and the album still counts as complete.
func TestMusicOnlyOneSongIsImported(t *testing.T) {
	d := newMusicDownload(t)

	postJSONMethod[map[string]any](t, d.client, http.MethodPut, d.albumURL+"/monitored", map[string]any{"monitored": true, "song": "middle song"}, http.StatusOK)
	before := getJSON[map[string]any](t, d.client, d.albumURL)
	if before["onlySong"] != "middle song" {
		t.Fatalf("the album should remember the song: %v", before)
	}

	d.grab(t)

	album := getJSON[map[string]any](t, d.client, d.albumURL)
	if album["status"] != "downloaded" || album["onlySong"] != "middle song" {
		t.Fatalf("album after the import: %v", album)
	}
	tracks := album["tracks"].([]any)
	if len(tracks) != 3 {
		t.Fatalf("tracks: %v", tracks)
	}
	for i, want := range []struct {
		monitored, hasFile bool
	}{{false, false}, {true, true}, {false, false}} {
		tr := tracks[i].(map[string]any)
		if tr["monitored"] != want.monitored || tr["hasFile"] != want.hasFile {
			t.Errorf("track %d: want monitored=%v hasFile=%v, got %v", i+1, want.monitored, want.hasFile, tr)
		}
	}
	folder := filepath.Join(d.musicRoot, "Fixture Band", "First Album (1999)")
	if _, err := os.Stat(filepath.Join(folder, "02 - Middle Song.flac")); err != nil {
		t.Errorf("the wanted song should be in the library: %v", err)
	}
	for _, name := range []string{"01 - Opening.flac", "03 - Closing Time.flac"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err == nil {
			t.Errorf("%s is not wanted and must not be imported", name)
		}
	}

	// Not wanted is not missing: the finished album is not in the wanted list.
	for _, w := range getJSON[[]map[string]any](t, d.client, d.base+"/api/music/wanted") {
		if w["id"] == album["id"] {
			t.Fatalf("a finished album is not wanted: %v", w)
		}
	}

	// Changing your mind: a track can be wanted again from the Tracks tab.
	first := tracks[0].(map[string]any)
	postJSONMethod[map[string]any](t, d.client, http.MethodPut, fmt.Sprintf("%s/api/music/tracks/%v/monitored", d.base, first["id"]), map[string]any{"monitored": true}, http.StatusOK)
	again := getJSON[map[string]any](t, d.client, d.albumURL)["tracks"].([]any)
	if again[0].(map[string]any)["monitored"] != true {
		t.Fatalf("track 1 should be wanted now: %v", again[0])
	}
	// And the whole album can be wanted again.
	postJSONMethod[map[string]any](t, d.client, http.MethodPut, d.albumURL+"/monitored", map[string]any{"monitored": true, "song": ""}, http.StatusOK)
	all := getJSON[map[string]any](t, d.client, d.albumURL)
	if all["onlySong"] != nil {
		t.Fatalf("the song should be cleared: %v", all["onlySong"])
	}
	for _, tr := range all["tracks"].([]any) {
		if tr.(map[string]any)["monitored"] != true {
			t.Fatalf("every track should be wanted again: %v", all["tracks"])
		}
	}
}

// An album without a wanted song is imported whole, as before.
func TestMusicWholeAlbumStaysWantedByDefault(t *testing.T) {
	d := newMusicDownload(t)
	d.grab(t)
	for _, tr := range getJSON[map[string]any](t, d.client, d.albumURL)["tracks"].([]any) {
		if m := tr.(map[string]any); m["monitored"] != true || m["hasFile"] != true {
			t.Fatalf("every track is wanted and imported: %v", m)
		}
	}
}
