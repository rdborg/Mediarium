package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

// albumOnDisk gives the First Album of the fixture artist three FLAC tracks
// and a cover in its folder, registered as imported.
func albumOnDisk(t *testing.T, e *musicEnv) (albumID int64, folder string, flacBytes []byte) {
	t.Helper()
	artist := e.addArtist(t, "none")
	albumID = int64(albumByTitle(t, artist, "First Album")["id"].(float64))
	folder = filepath.Join(e.musicRoot, "Fixture Band", "First Album (1999)")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	flacBytes = flacFile(44100, 16, 200, "TITLE=x")
	tracks := []music.Track{{Disc: 1, Position: 1, Title: "Opening"}, {Disc: 1, Position: 2, Title: "Middle Song"}, {Disc: 1, Position: 3, Title: "Closing Time"}}
	if err := e.server.MusicRepo.ReplaceTracks(albumID, tracks); err != nil {
		t.Fatal(err)
	}
	stored, _ := e.server.MusicRepo.ListTracks(albumID)
	for i, name := range []string{"01 - Opening.flac", "02 - Middle Song.flac", "03 - Closing Time.flac"} {
		p := filepath.Join(folder, name)
		if err := os.WriteFile(p, flacBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := e.server.MusicRepo.SetTrackFile(stored[i].ID, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(folder, "cover.jpg"), fakeJPEG, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "rip.log"), []byte("log"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.server.MusicRepo.SetAlbumImported(albumID, music.TierFLAC, folder); err != nil {
		t.Fatal(err)
	}
	return albumID, folder, flacBytes
}

func TestAlbumFilesListsTheFolderAndMarksTheTracks(t *testing.T) {
	e := newMusicEnv(t)
	id, _, flacBytes := albumOnDisk(t, e)

	got := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/albums/%d/files", e.base, id))
	if got["folder"] != "Fixture Band/First Album (1999)" {
		t.Fatalf("folder is relative to the music folder: %v", got["folder"])
	}
	byPath := map[string]map[string]any{}
	for _, f := range got["files"].([]any) {
		m := f.(map[string]any)
		byPath[m["path"].(string)] = m
	}
	if len(byPath) != 5 {
		t.Fatalf("files: %v", byPath)
	}
	track := byPath["02 - Middle Song.flac"]
	if track["kind"] != "audio" || track["main"] != true || track["trackId"] == nil || track["size"] != float64(len(flacBytes)) || track["modified"] == "" {
		t.Fatalf("track entry: %v", track)
	}
	if c := byPath["cover.jpg"]; c["kind"] != "image" || c["main"] != nil || c["trackId"] != nil {
		t.Fatalf("cover entry: %v", c)
	}
	if l := byPath["rip.log"]; l["kind"] != "other" {
		t.Fatalf("log entry: %v", l)
	}

	// An album that is not on disk has an empty listing; unknown is 404.
	artist := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/artists")[0]
	other := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/artists/%v", e.base, artist["id"]))["albums"].([]any)
	for _, a := range other {
		if a.(map[string]any)["id"] != float64(id) {
			empty := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/albums/%v/files", e.base, a.(map[string]any)["id"]))
			if empty["folder"] != "" || len(empty["files"].([]any)) != 0 {
				t.Fatalf("not on disk: %v", empty)
			}
			break
		}
	}
	if status, _ := doStatus(t, e.client, http.MethodGet, e.base+"/api/music/albums/9999/files"); status != http.StatusNotFound {
		t.Fatalf("unknown album: %d", status)
	}

	// A folder outside the music folder is never listed.
	outside := t.TempDir()
	if err := e.server.MusicRepo.SetAlbumImported(id, music.TierFLAC, outside); err != nil {
		t.Fatal(err)
	}
	if status, _ := doStatus(t, e.client, http.MethodGet, fmt.Sprintf("%s/api/music/albums/%d/files", e.base, id)); status != http.StatusForbidden {
		t.Fatalf("outside the music folder: %d", status)
	}
}

func TestStreamPlaysAnAlbumTrackWithRanges(t *testing.T) {
	e := newMusicEnv(t)
	id, _, flacBytes := albumOnDisk(t, e)
	url := fmt.Sprintf("%s/api/files/stream?album=%d&path=", e.base, id)

	status, hdr, body := getRaw(t, e.client, url+"01%20-%20Opening.flac")
	if status != 200 || hdr.Get("Content-Type") != "audio/flac" || string(body) != string(flacBytes) {
		t.Fatalf("track: %d %q %d bytes", status, hdr.Get("Content-Type"), len(body))
	}
	if hdr.Get("Accept-Ranges") != "bytes" || hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("Content-Security-Policy") != "sandbox" ||
		hdr.Get("Content-Disposition") == "" {
		t.Fatalf("headers must keep the sandbox and range support: %v", hdr)
	}
	status, hdr, body = getRaw(t, e.client, url+"02%20-%20Middle%20Song.flac", "Range", "bytes=4-9")
	if status != http.StatusPartialContent || hdr.Get("Content-Range") != fmt.Sprintf("bytes 4-9/%d", len(flacBytes)) || string(body) != string(flacBytes[4:10]) {
		t.Fatalf("range: %d %q %q", status, hdr.Get("Content-Range"), body)
	}
	if status, hdr, _ := getRaw(t, e.client, url+"cover.jpg"); status != 200 || hdr.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("the cover previews as an image: %d %q", status, hdr.Get("Content-Type"))
	}

	// Content types by extension.
	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album (1999)")
	for name, want := range map[string]string{"a.mp3": "audio/mpeg", "b.m4a": "audio/mp4", "c.ogg": "audio/ogg", "d.opus": "audio/opus", "e.aac": "audio/aac"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, hdr, _ := getRaw(t, e.client, url+name); hdr.Get("Content-Type") != want {
			t.Errorf("%s: %q, want %q", name, hdr.Get("Content-Type"), want)
		}
	}

	// Refused: other file types, paths leaving the folder, missing files,
	// unknown albums, ambiguous requests.
	for name, tc := range map[string]struct {
		url  string
		want int
	}{
		"a log file":       {url + "rip.log", http.StatusUnsupportedMediaType},
		"path traversal":   {url + "..%2F..%2Fsecret.flac", http.StatusBadRequest},
		"an absolute path": {url + "%2Fetc%2Fpasswd.flac", http.StatusBadRequest},
		"a missing file":   {url + "nope.flac", http.StatusNotFound},
		"unknown album":    {e.base + "/api/files/stream?album=9999&path=a.flac", http.StatusNotFound},
		"bad id":           {e.base + "/api/files/stream?album=x&path=a.flac", http.StatusBadRequest},
		"no path":          {fmt.Sprintf("%s/api/files/stream?album=%d", e.base, id), http.StatusBadRequest},
		"album and movie":  {fmt.Sprintf("%s/api/files/stream?album=%d&movie=1&path=a.flac", e.base, id), http.StatusBadRequest},
	} {
		if status, _, _ := getRaw(t, e.client, tc.url); status != tc.want {
			t.Errorf("%s: %d, want %d", name, status, tc.want)
		}
	}

	// A symlink in the album folder that leads out is not followed.
	secret := filepath.Join(t.TempDir(), "secret.flac")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(folder, "link.flac")); err == nil {
		if status, _, body := getRaw(t, e.client, url+"link.flac"); status == 200 || string(body) == "secret" {
			t.Fatalf("a symlink out of the album folder must not be served: %d", status)
		}
	}

	// With the music module off, tracks are not served.
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/modules", map[string]any{"music": false}, http.StatusOK)
	if status, _, _ := getRaw(t, e.client, url+"01%20-%20Opening.flac"); status != http.StatusNotFound {
		t.Fatalf("music off: %d", status)
	}
}
