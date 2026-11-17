package api_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingMusicBrainz answers every request with an error and counts them, so
// a test can tell that nothing asked MusicBrainz about an album added by hand.
func countingMusicBrainz(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func uploadFile(t *testing.T, client *http.Client, url string, body []byte, wantStatus int) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("file", "cover.png")
	part.Write(body)
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: %d, want %d: %s", url, resp.StatusCode, wantStatus, b)
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestManualAlbumValidationAndDuplicates(t *testing.T) {
	e := newMusicEnv(t)
	url := e.base + "/api/music/manual"
	for name, body := range map[string]map[string]any{
		"no artist":    {"album": "Handmade"},
		"no album":     {"artist": "Hand Band"},
		"far past":     {"artist": "Hand Band", "album": "Handmade", "year": 12},
		"far future":   {"artist": "Hand Band", "album": "Handmade", "year": 2999},
		"bad type":     {"artist": "Hand Band", "album": "Handmade", "type": "boxset"},
		"bad outer id": {"artist": "Hand Band", "album": "Handmade", "externalId": "../etc"},
	} {
		if got := postJSON[map[string]any](t, e.client, url, body, http.StatusBadRequest); got["error"] == nil {
			t.Errorf("%s: no message: %v", name, got)
		}
	}
	first := postJSON[map[string]any](t, e.client, url, map[string]any{"artist": "Hand Band", "album": "Handmade", "year": 2003, "externalId": "CAT-001"}, http.StatusCreated)
	if first["newArtist"] != true {
		t.Fatalf("a new artist is made: %v", first)
	}
	// The same title again, with other capitals, goes nowhere new.
	postJSON[map[string]any](t, e.client, url, map[string]any{"artist": "hand band", "album": "HANDMADE"}, http.StatusConflict)
	// A second album goes to the artist already there.
	second := postJSON[map[string]any](t, e.client, url, map[string]any{"artist": "Hand BAND", "album": "More Handmade", "type": "ep"}, http.StatusCreated)
	if second["newArtist"] != false || second["artistId"] != first["artistId"] {
		t.Fatalf("the second album should join the first artist: %v / %v", first, second)
	}
	artist := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/artists/%v", e.base, first["artistId"]))
	if artist["manual"] != true || len(artist["albums"].([]any)) != 2 {
		t.Fatalf("artist: %v", artist)
	}
	a := albumByTitle(t, artist, "Handmade")
	if a["manual"] != true || a["releaseDate"] != "2003" || a["monitored"] != true || a["status"] != "missing" || a["mbid"] != "manual:CAT-001" || a["coverArchiveUrl"] != "" {
		t.Fatalf("album: %v", a)
	}
	if albumByTitle(t, artist, "More Handmade")["type"] != "ep" {
		t.Fatalf("type: %v", artist["albums"])
	}
}

// An album added by hand is found, downloaded and imported like any other; its
// tracklist comes from the files, and MusicBrainz is never asked.
func TestManualAlbumSearchGrabImportWithoutMusicBrainz(t *testing.T) {
	e := newMusicEnv(t)
	e.useLosslessByDefault(t)
	mb, asked := countingMusicBrainz(t)
	e.server.TestSetMusicBrainz(mb.URL)

	// No year: the album still counts as wanted.
	added := postJSON[map[string]any](t, e.client, e.base+"/api/music/manual", map[string]any{"artist": "Hand Band", "album": "Handmade"}, http.StatusCreated)
	albumURL := fmt.Sprintf("%s/api/music/albums/%v", e.base, added["albumId"])

	// A cover given by hand is what the album shows.
	if status, _ := doStatus(t, e.client, http.MethodGet, albumURL+"/cover"); status == http.StatusOK {
		t.Fatal("no cover yet")
	}
	uploadFile(t, e.client, albumURL+"/cover", []byte("not a picture"), http.StatusBadRequest)
	uploadFile(t, e.client, albumURL+"/cover", tinyPNG(t), http.StatusOK)
	if status, _ := doStatus(t, e.client, http.MethodGet, albumURL+"/cover"); status != http.StatusOK {
		t.Fatalf("cover after upload: %d", status)
	}

	articles := map[string]nntpArticle{
		"t1@x": {fileName: "01 - First Thing.flac", content: []byte("flac one " + strings.Repeat("a", 300))},
		"t2@x": {fileName: "02 - Second Thing.flac", content: []byte("flac two " + strings.Repeat("b", 300))},
	}
	host, port := newArticleNNTPServer(t, articles)
	idx := newMusicIndexer(t, []string{"Hand Band - Handmade (2003) [FLAC]"}, nzbFor(articles))
	postJSON[map[string]any](t, e.client, e.base+"/api/indexers", map[string]any{"name": "Music Indexer", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	postJSON[map[string]any](t, e.client, e.base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2}, http.StatusCreated)

	wanted := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/wanted")
	if len(wanted) != 1 || wanted[0]["id"] != added["albumId"] {
		t.Fatalf("wanted: %v", wanted)
	}
	now := postJSON[map[string]any](t, e.client, albumURL+"/search-now", nil, http.StatusOK)
	if now["grabbed"] != float64(1) {
		t.Fatalf("search now: %v", now)
	}
	waitBackground(t, e.server)

	var folder string
	album := getJSON[map[string]any](t, e.client, albumURL)
	if p, _ := album["path"].(string); p != "" {
		folder = p
	}
	if folder == "" || !strings.HasPrefix(folder, filepath.Join(e.musicRoot, "Hand Band")) {
		t.Fatalf("album folder: %v", album)
	}
	for _, name := range []string{"01 - First Thing.flac", "02 - Second Thing.flac", "cover.jpg"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Errorf("expected %s in the album folder: %v", name, err)
		}
	}
	tracks, _ := album["tracks"].([]any)
	if album["status"] != "downloaded" || len(tracks) != 2 || tracks[1].(map[string]any)["title"] != "Second Thing" || tracks[1].(map[string]any)["hasFile"] != true {
		t.Fatalf("album after import: %v", album)
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("MusicBrainz was asked %d times about an album added by hand", n)
	}
}

// A refresh of the followed artists leaves an artist added by hand as it is.
func TestManualArtistIsNotTouchedByARefresh(t *testing.T) {
	e := newMusicEnv(t)
	mb, asked := countingMusicBrainz(t)
	e.server.TestSetMusicBrainz(mb.URL)
	added := postJSON[map[string]any](t, e.client, e.base+"/api/music/manual", map[string]any{"artist": "Hand Band", "album": "Handmade", "year": 2003}, http.StatusCreated)

	e.server.TestRefreshMusicArtists(context.Background())
	artistURL := fmt.Sprintf("%s/api/music/artists/%v", e.base, added["artistId"])
	artist := getJSON[map[string]any](t, e.client, artistURL)
	albums := artist["albums"].([]any)
	if artist["name"] != "Hand Band" || len(albums) != 1 || albums[0].(map[string]any)["title"] != "Handmade" || albums[0].(map[string]any)["releaseDate"] != "2003" {
		t.Fatalf("a refresh changed the entry: %v", artist)
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("a refresh asked MusicBrainz %d times about an artist added by hand", n)
	}
	// Its discography has nothing hidden, and there is nothing on MusicBrainz to add from.
	disc := getJSON[map[string]any](t, e.client, artistURL+"/discography")
	if len(disc["hidden"].([]any)) != 0 {
		t.Fatalf("discography: %v", disc)
	}
	postJSON[map[string]any](t, e.client, artistURL+"/albums", map[string]any{"mbid": "aaaaaaaa-0000-4000-8000-000000000001"}, http.StatusBadRequest)
}

func TestAlbumCoverUploadIsOnlyForAlbumsAddedByHand(t *testing.T) {
	e := newMusicEnv(t)
	artist := e.addArtist(t, "all")
	first := albumByTitle(t, artist, "First Album")
	uploadFile(t, e.client, fmt.Sprintf("%s/api/music/albums/%v/cover", e.base, first["id"]), tinyPNG(t), http.StatusBadRequest)
}

func TestManualBookAdd(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	url := base + "/api/books/manual"
	// Both formats are off on a fresh install.
	postJSON[map[string]any](t, client, url, map[string]any{"title": "Home Brew", "ebook": true}, http.StatusNotFound)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true, "audiobooks": true}, http.StatusOK)

	for name, body := range map[string]map[string]any{
		"no title":    {"author": "A. Writer", "ebook": true},
		"no format":   {"title": "Home Brew"},
		"bad year":    {"title": "Home Brew", "year": 5, "ebook": true},
		"bad id":      {"title": "Home Brew", "ebook": true, "externalId": "a b"},
		"huge author": {"title": "Home Brew", "author": strings.Repeat("x", 400), "ebook": true},
	} {
		if got := postJSON[map[string]any](t, client, url, body, http.StatusBadRequest); got["error"] == nil {
			t.Errorf("%s: no message: %v", name, got)
		}
	}
	b := postJSON[map[string]any](t, client, url, map[string]any{
		"title": " Home Brew ", "author": "A. Writer", "year": 2001, "ebook": true, "audiobook": true, "externalId": "9781234567897", "description": "Typed in.",
	}, http.StatusCreated)
	if b["title"] != "Home Brew" || b["author"] != "A. Writer" || b["olKey"] != "manual:9781234567897" || b["year"] != float64(2001) {
		t.Fatalf("book: %v", b)
	}
	if b["ebook"].(map[string]any)["wanted"] != true || b["audiobook"].(map[string]any)["wanted"] != true {
		t.Fatalf("formats: %v", b)
	}
	// The same book again, however it is capitalised, and the same id again.
	postJSON[map[string]any](t, client, url, map[string]any{"title": "home brew", "author": "a. writer", "ebook": true}, http.StatusConflict)
	postJSON[map[string]any](t, client, url, map[string]any{"title": "Another", "author": "B. Writer", "ebook": true, "externalId": "9781234567897"}, http.StatusConflict)

	// The cover is added with the book's own cover route, like for any book.
	id := int(b["id"].(float64))
	uploadFile(t, client, fmt.Sprintf("%s/api/books/%d/cover", base, id), tinyPNG(t), http.StatusOK)
	got := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/books/%d", base, id))
	if got["coverUrl"] == "" || got["description"] != "Typed in." {
		t.Fatalf("book after cover: %v", got)
	}
}

// The cover given to an album added by hand goes when its artist is removed;
// another artist's cover stays.
func TestManualCoverIsRemovedWithItsArtist(t *testing.T) {
	e := newMusicEnv(t)
	add := func(artist, album string) int64 {
		got := postJSON[map[string]any](t, e.client, e.base+"/api/music/manual", map[string]any{"artist": artist, "album": album}, http.StatusCreated)
		id := int64(got["albumId"].(float64))
		uploadFile(t, e.client, fmt.Sprintf("%s/api/music/albums/%d/cover", e.base, id), tinyPNG(t), http.StatusOK)
		return id
	}
	first, second, other := add("Hand Band", "Handmade"), add("Hand Band", "Second Hand"), add("Other Band", "Kept")
	artist := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/albums/%d", e.base, first))["artistId"]
	if status, body := doStatus(t, e.client, http.MethodDelete, fmt.Sprintf("%s/api/music/artists/%v", e.base, artist)); status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("remove artist: %d %s", status, body)
	}
	for _, c := range []struct {
		name string
		id   int64
		kept bool
	}{
		{"first album of the removed artist", first, false},
		{"second album of the removed artist", second, false},
		{"album of another artist", other, true},
	} {
		_, err := os.Stat(e.server.TestManualCoverPath(c.id))
		if kept := err == nil; kept != c.kept {
			t.Errorf("%s: cover kept = %v, want %v (%v)", c.name, kept, c.kept, err)
		}
	}
}

// Adding an album by hand to an artist already in the library needs the
// manage permission, as adding an album from the artist's page does. A new
// artist only needs the right to add music.
func TestManualAlbumForAnExistingArtistNeedsManage(t *testing.T) {
	e := newMusicEnv(t)
	url := e.base + "/api/music/manual"
	postJSON[map[string]any](t, e.client, url, map[string]any{"artist": "Hand Band", "album": "Handmade"}, http.StatusCreated)
	postJSON[map[string]any](t, e.client, e.base+"/api/users", map[string]any{
		"username": "pat", "password": "pat-password-1", "role": "member",
		"permissions": map[string]any{"manage": false, "music": true, "addDirect": true},
	}, http.StatusCreated)
	jar, _ := cookiejar.New(nil)
	pat := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	postJSON[map[string]any](t, pat, e.base+"/api/auth/login", map[string]string{"username": "pat", "password": "pat-password-1"}, http.StatusOK)

	got := postJSON[map[string]any](t, pat, url, map[string]any{"artist": "hand band", "album": "More Handmade"}, http.StatusForbidden)
	if msg, _ := got["error"].(string); !strings.Contains(msg, "already in your library") {
		t.Fatalf("the refusal should say why: %v", got)
	}
	if added := postJSON[map[string]any](t, pat, url, map[string]any{"artist": "Other Band", "album": "Fresh"}, http.StatusCreated); added["newArtist"] != true {
		t.Fatalf("a new artist is still fine: %v", added)
	}
	postJSON[map[string]any](t, e.client, url, map[string]any{"artist": "Hand Band", "album": "More Handmade"}, http.StatusCreated)
}
