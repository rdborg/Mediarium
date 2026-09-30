package api_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	albumWithCover    = "aaaaaaaa-0000-4000-8000-000000000001" // First Album
	albumWithoutCover = "aaaaaaaa-0000-4000-8000-000000000002" // Second Album
)

func getRaw(t *testing.T, client *http.Client, url string, header ...string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body
}

func TestMusicCoversAreFetchedOnceCachedAndServedLocally(t *testing.T) {
	e := newMusicEnv(t)
	artist := e.addArtist(t, "none")
	first := albumByTitle(t, artist, "First Album")
	second := albumByTitle(t, artist, "Second Album")
	firstURL := e.base + first["coverUrl"].(string)

	if artist["coverUrl"] != fmt.Sprintf("/api/music/artists/%v/cover", artist["id"]) {
		t.Fatalf("artist cover url: %v", artist["coverUrl"])
	}

	status, hdr, body := getRaw(t, e.client, firstURL)
	if status != 200 || hdr.Get("Content-Type") != "image/jpeg" || !bytes.Equal(body, fakeJPEG) {
		t.Fatalf("album cover: %d %q %d bytes", status, hdr.Get("Content-Type"), len(body))
	}
	if cc := hdr.Get("Cache-Control"); cc != "private, max-age=86400" {
		t.Fatalf("a cover must be cacheable by the browser, got Cache-Control %q", cc)
	}
	if hdr.Get("Last-Modified") == "" || hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", hdr)
	}
	// It is kept under the config folder, and asking again does not go back
	// to the archive.
	if _, err := os.Stat(filepath.Join(e.server.TestConfigDir(), "music-covers", albumWithCover+".img")); err != nil {
		t.Fatalf("cover not cached: %v", err)
	}
	getRaw(t, e.client, firstURL)
	if n := e.covers.count(albumWithCover); n != 1 {
		t.Fatalf("the archive should be asked once, was asked %d times", n)
	}
	// A browser that has it already gets 304.
	if status, _, _ := getRaw(t, e.client, firstURL, "If-Modified-Since", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)); status != http.StatusNotModified {
		t.Fatalf("conditional request: %d", status)
	}

	// An artist's picture is the cover of one of its albums.
	if status, _, body := getRaw(t, e.client, e.base+artist["coverUrl"].(string)); status != 200 || !bytes.Equal(body, fakeJPEG) {
		t.Fatalf("artist cover: %d", status)
	}

	// No cover art: a plain 404, remembered so the archive is not asked again.
	secondURL := e.base + second["coverUrl"].(string)
	for i := 0; i < 3; i++ {
		if status, _, _ := getRaw(t, e.client, secondURL); status != http.StatusNotFound {
			t.Fatalf("album without cover: %d", status)
		}
	}
	if n := e.covers.count(albumWithoutCover); n != 1 {
		t.Fatalf("a missing cover is remembered: archive asked %d times", n)
	}
	if status, _, _ := getRaw(t, e.client, e.base+"/api/music/albums/99999/cover"); status != http.StatusNotFound {
		t.Fatalf("unknown album: %d", status)
	}
	if status, _, _ := getRaw(t, e.client, e.base+"/api/music/artists/99999/cover"); status != http.StatusNotFound {
		t.Fatalf("unknown artist: %d", status)
	}
}

func TestMusicCoverInTheAlbumFolderWinsAndIsNeverReplaced(t *testing.T) {
	e := newMusicEnv(t)
	artist := e.addArtist(t, "none")
	first := albumByTitle(t, artist, "First Album")
	id := int64(first["id"].(float64))

	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album (1999)")
	own := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("my own scan of the sleeve")...)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "folder.jpg"), own, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.server.MusicRepo.SetAlbumImported(id, "FLAC", folder); err != nil {
		t.Fatal(err)
	}
	status, _, body := getRaw(t, e.client, e.base+first["coverUrl"].(string))
	if status != 200 || !bytes.Equal(body, own) || e.covers.count(albumWithCover) != 0 {
		t.Fatalf("the cover in the folder should be served without asking the archive: %d %q hits=%d", status, body, e.covers.count(albumWithCover))
	}

	// A folder outside the music folder is never read.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "cover.jpg"), []byte("\xFF\xD8\xFFsecret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.server.MusicRepo.SetAlbumImported(id, "FLAC", outside); err != nil {
		t.Fatal(err)
	}
	status, _, body = getRaw(t, e.client, e.base+first["coverUrl"].(string))
	if status != 200 || !bytes.Equal(body, fakeJPEG) {
		t.Fatalf("a path outside the music folder must be ignored: %d %q", status, body)
	}
}
