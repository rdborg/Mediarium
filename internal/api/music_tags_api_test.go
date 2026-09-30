package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMusicScanUsesTagsAndReadsRealQuality(t *testing.T) {
	e := newMusicEnv(t)
	write := func(rel string, data []byte) {
		p := filepath.Join(e.musicRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tagged := func(track int, title string) []byte {
		return flacFile(96000, 24, 200, "ARTIST=Fixture Band", "ALBUMARTIST=Fixture Band", "ALBUM=First Album", "DATE=1999",
			fmt.Sprintf("TRACKNUMBER=%d", track), "TITLE="+title)
	}
	// Folder and file names say nothing useful; the tags identify everything.
	write("Some Random Name/Whatever/x1.flac", tagged(1, "Opening"))
	write("Some Random Name/Whatever/x2.flac", tagged(2, "Middle Song"))
	write("Some Random Name/Whatever/x3.flac", tagged(3, "Closing Time"))
	// Tags that lead nowhere fall back to the folder names; MP3 quality is
	// read from the frame headers (320 kbit/s here).
	write("Fixture Band/A Single (2006)/01 - A Single.mp3", mp3File(14, [][2]string{{"TPE1", "Nobody Real"}, {"TALB", "Bogus Record"}}))

	start := postJSON[map[string]any](t, e.client, e.base+"/api/music/import/scan", nil, http.StatusAccepted)
	waitBackground(t, e.server)
	job := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/import/scan/%v", e.base, start["id"]))
	byAlbum := map[string]map[string]any{}
	for _, r := range job["results"].([]any) {
		m := r.(map[string]any)
		byAlbum[m["album"].(string)] = m
	}
	first := byAlbum["First Album"]
	if first == nil || first["status"] != "imported" || first["artist"] != "Fixture Band" || first["tracks"] != float64(3) || first["quality"] != "FLAC 24bit" {
		t.Fatalf("tags should identify the album and its 24-bit quality: %v\n%v", first, job["results"])
	}
	single := byAlbum["A Single"]
	if single == nil || single["status"] != "imported" || single["quality"] != "MP3-320/V0" {
		t.Fatalf("folder-name fallback and MP3 quality: %v", job["results"])
	}

	album := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/albums/%v", e.base, first["albumId"]))
	tracks := album["tracks"].([]any)
	if len(tracks) != 3 || !strings.HasSuffix(tracks[1].(map[string]any)["filePath"].(string), "x2.flac") || tracks[0].(map[string]any)["hasFile"] != true {
		t.Fatalf("tracks matched by their tags: %v", tracks)
	}
	// A real 24-bit FLAC is above the Lossless cutoff: never wanted again.
	if cut := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/wanted?kind=cutoff"); len(cut) != 0 {
		// The MP3 is at the Lossy cutoff but on a fallback under the default profile, so it is wanted.
		if len(cut) != 1 || cut[0]["title"] != "A Single" {
			t.Fatalf("cutoff list: %v", cut)
		}
	}
}

func TestMusicImportNamesFilesByTagsAndTrustsTheFilesOverTheReleaseName(t *testing.T) {
	e := newMusicEnv(t)
	artist := e.addArtist(t, "none")
	first := albumByTitle(t, artist, "First Album")
	albumURL := fmt.Sprintf("%s/api/music/albums/%v", e.base, first["id"])

	file := func(track int, title string) nntpArticle {
		return nntpArticle{fileName: fmt.Sprintf("zz%d.flac", track), content: flacFile(96000, 24, 200, fmt.Sprintf("TRACKNUMBER=%d", track), "TITLE="+title)}
	}
	articles := map[string]nntpArticle{"a@x": file(3, "Closing Time"), "b@x": file(1, "Opening"), "c@x": file(2, "Middle Song")}
	host, port := newArticleNNTPServer(t, articles)
	// The release is named as MP3; the files are 24-bit FLAC.
	idx := newMusicIndexer(t, []string{"Fixture Band - First Album (1999) [MP3 320]"}, nzbFor(articles))
	postJSON[map[string]any](t, e.client, e.base+"/api/indexers", map[string]any{"name": "Music Indexer", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	postJSON[map[string]any](t, e.client, e.base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2}, http.StatusCreated)

	if now := postJSON[map[string]any](t, e.client, albumURL+"/search-now", nil, http.StatusOK); now["grabbed"] != float64(1) {
		t.Fatalf("search now: %v", now)
	}
	waitBackground(t, e.server)

	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album (1999)")
	for _, name := range []string{"01 - Opening.flac", "02 - Middle Song.flac", "03 - Closing Time.flac"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
	// The release had no cover: the front cover was fetched and saved.
	if got, _ := os.ReadFile(filepath.Join(folder, "cover.jpg")); string(got) != string(fakeJPEG) {
		t.Errorf("cover.jpg should have been saved from the Cover Art Archive, got %q", got)
	}
	album := getJSON[map[string]any](t, e.client, albumURL)
	if album["status"] != "downloaded" || album["quality"] != "FLAC 24bit" {
		t.Fatalf("the quality is what the files are, not what the release was called: %v", album)
	}
}
