package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestMusicImportRegistersAnExistingCollectionInPlace(t *testing.T) {
	e := newMusicEnv(t)
	files := map[string]string{
		"Fixture Band/First Album (1999)/01 - Opening.flac":        "one",
		"Fixture Band/First Album (1999)/02 - Middle Song.flac":    "two",
		"Fixture Band/First Album (1999)/03 - Closing Time.flac":   "three",
		"Fixture Band/First Album (1999)/cover.jpg":                "jpeg",
		"Fixture Band/Second Album [2005] [MP3]/01 - Whatever.mp3": "mp3",
		"Fixture Band/Nonexistent Record (2010)/01 - Lost.flac":    "lost",
		"Fixture Band/stray.mp3":                                   "stray",
		"Nobody At All/Some Album/01 - Song.flac":                  "song",
	}
	for rel, content := range files {
		p := filepath.Join(e.musicRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	start := postJSON[map[string]any](t, e.client, e.base+"/api/music/import/scan", nil, http.StatusAccepted)
	waitBackground(t, e.server)
	job := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/import/scan/%v", e.base, start["id"]))
	if job["phase"] != "done" || job["total"] != float64(4) || job["done"] != float64(4) {
		t.Fatalf("job: %v", job)
	}
	sum := job["summary"].(map[string]any)
	if sum["imported"] != float64(2) || sum["unmatched"] != float64(3) || sum["already"] != float64(0) || sum["artists"] != float64(2) {
		t.Fatalf("summary: %v", sum)
	}
	byAlbum := map[string]map[string]any{}
	for _, r := range job["results"].([]any) {
		m := r.(map[string]any)
		key, _ := m["album"].(string)
		if key == "" {
			key = "(loose)"
		}
		byAlbum[key] = m
	}
	first := byAlbum["First Album"]
	// Placeholder bytes with a .flac name have no readable header: quality stays Unknown.
	if first["status"] != "imported" || first["quality"] != "Unknown" || first["tracks"] != float64(3) || first["files"] != float64(3) {
		t.Fatalf("first album: %v", first)
	}
	if second := byAlbum["Second Album"]; second["status"] != "imported" || second["quality"] != "Unknown" {
		t.Fatalf("placeholder bytes cannot be read, so Unknown: %v", second)
	}
	if r := byAlbum["Nonexistent Record"]; r["status"] != "unmatched" || r["message"] == "" {
		t.Fatalf("nonexistent record: %v", r)
	}
	if r := byAlbum["Some Album"]; r["status"] != "unmatched" || r["artist"] != "Nobody At All" {
		t.Fatalf("unknown artist: %v", r)
	}
	if r := byAlbum["(loose)"]; r["status"] != "unmatched" || r["files"] != float64(1) {
		t.Fatalf("loose file: %v", r)
	}

	// The album is registered where it is, with its tracks linked to the files.
	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album (1999)")
	album := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/albums/%v", e.base, first["albumId"]))
	if album["status"] != "downloaded" || album["path"] != folder || album["monitored"] != true {
		t.Fatalf("album: %v", album)
	}
	tracks := album["tracks"].([]any)
	if len(tracks) != 3 || tracks[1].(map[string]any)["filePath"] != filepath.Join(folder, "02 - Middle Song.flac") {
		t.Fatalf("tracks: %v", tracks)
	}
	// Nothing was moved or renamed.
	for rel, content := range files {
		got, err := os.ReadFile(filepath.Join(e.musicRoot, filepath.FromSlash(rel)))
		if err != nil || string(got) != content {
			t.Errorf("%s changed: %q %v", rel, got, err)
		}
	}
	// The artist joined the library without a wave of wanted albums: only
	// the two found albums are monitored, so nothing else is searched for.
	artists := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/artists")
	if len(artists) != 1 || artists[0]["monitoredCount"] != float64(2) || artists[0]["downloadedCount"] != float64(2) || artists[0]["monitored"] != true {
		t.Fatalf("artists: %v", artists)
	}
	if w := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/wanted"); len(w) != 0 {
		t.Fatalf("nothing should be wanted: %v", w)
	}

	// Scanning again finds them already there.
	again := postJSON[map[string]any](t, e.client, e.base+"/api/music/import/scan", nil, http.StatusAccepted)
	waitBackground(t, e.server)
	job = getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/import/scan/%v", e.base, again["id"]))
	if s := job["summary"].(map[string]any); s["already"] != float64(2) || s["imported"] != float64(0) {
		t.Fatalf("second scan: %v", s)
	}
	if status, _ := doStatus(t, e.client, http.MethodGet, e.base+"/api/music/import/scan/nope"); status != http.StatusNotFound {
		t.Fatalf("unknown scan: %d", status)
	}
}

func TestMusicImportIsForAdministrators(t *testing.T) {
	server, base, _, member, _ := familyServer(t)
	_ = server
	if status, _ := doStatus(t, member, http.MethodPost, base+"/api/music/import/scan"); status != http.StatusForbidden {
		t.Fatalf("member scan: %d", status)
	}
}
