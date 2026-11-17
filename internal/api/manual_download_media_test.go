package api_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/books"
)

// postManual sends the "add a download by hand" form as the given client.
func postManual(t *testing.T, client *http.Client, base string, f manualForm) (int, map[string]any) {
	t.Helper()
	body, ctype := f.body()
	req, err := http.NewRequest(http.MethodPost, base+"/api/downloads/manual", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", ctype)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// holdDownloads keeps new downloads waiting in line (they may only start in two
// hours' time), so a test can look at what was queued.
func holdDownloads(t *testing.T, client *http.Client, base string) {
	t.Helper()
	h := time.Now().Hour()
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"downloadHours": fmt.Sprintf("%d-%d", (h+2)%24, (h+3)%24)}, http.StatusOK)
}

func TestManualDownloadForAnAlbumIsImportedLikeAnyDownload(t *testing.T) {
	e := newMusicEnv(t)
	e.useLosslessByDefault(t)
	artist := e.addArtist(t, "none")
	first := albumByTitle(t, artist, "First Album")
	albumURL := fmt.Sprintf("%s/api/music/albums/%v", e.base, first["id"])
	albumID := strconv.FormatInt(int64(first["id"].(float64)), 10)

	articles := map[string]nntpArticle{
		"t1@x":  {fileName: "01 - Opening.flac", content: []byte("flac one " + strings.Repeat("a", 300))},
		"t2@x":  {fileName: "02 - Middle Song.flac", content: []byte("flac two " + strings.Repeat("b", 300))},
		"t3@x":  {fileName: "03 - Closing Time.flac", content: []byte("flac three " + strings.Repeat("c", 300))},
		"art@x": {fileName: "cover.jpg", content: []byte("jpeg bytes")},
	}
	host, port := newArticleNNTPServer(t, articles)
	postJSON[map[string]any](t, e.client, e.base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2}, http.StatusCreated)

	status, body := postManual(t, e.client, e.base, manualForm{
		fields:   map[string]string{"albumId": albumID},
		fileName: "Fixture.Band-First.Album-1999-FLAC.nzb",
		file:     []byte(nzbFor(articles)),
	})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body %v", status, body)
	}
	if body["releaseTitle"] != "Fixture.Band-First.Album-1999-FLAC" || body["protocol"] != "usenet" {
		t.Errorf("response = %v", body)
	}
	waitBackground(t, e.server)

	q := getJSON[[]map[string]any](t, e.client, e.base+"/api/queue")
	if len(q) != 1 || q[0]["status"] != "completed" || q[0]["albumId"] != first["id"] {
		t.Fatalf("queue: %v", q)
	}
	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album (1999)")
	if got, err := os.ReadFile(filepath.Join(folder, "02 - Middle Song.flac")); err != nil || string(got) != string(articles["t2@x"].content) {
		t.Fatalf("track 2 in the library: %q (%v)", got, err)
	}
	if album := getJSON[map[string]any](t, e.client, albumURL); album["status"] != "downloaded" || album["path"] != folder {
		t.Fatalf("album after import: %v", album)
	}
}

func TestManualDownloadForAnAlbumRefusesWhatDoesNotFit(t *testing.T) {
	e := newMusicEnv(t)
	artist := e.addArtist(t, "none")
	first := albumByTitle(t, artist, "First Album")
	albumID := strconv.FormatInt(int64(first["id"].(float64)), 10)
	articles := map[string]nntpArticle{"x@x": {fileName: "a.flac", content: []byte("flac")}}
	host, port := newArticleNNTPServer(t, articles)
	postJSON[map[string]any](t, e.client, e.base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 1}, http.StatusCreated)
	nzb := []byte(nzbFor(articles))

	if status, body := postManual(t, e.client, e.base, manualForm{fields: map[string]string{"albumId": "99999"}, fileName: "a.nzb", file: nzb}); status != http.StatusNotFound || !strings.Contains(fmt.Sprint(body["error"]), "album isn't in your library") {
		t.Errorf("an album that isn't there: %d %v", status, body)
	}
	if status, body := postManual(t, e.client, e.base, manualForm{fields: map[string]string{"albumId": albumID, "movieId": "1"}, fileName: "a.nzb", file: nzb}); status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(body["error"]), "not both") {
		t.Errorf("an album and a movie: %d %v", status, body)
	}
	if status, body := postManual(t, e.client, e.base, manualForm{fields: map[string]string{"albumId": albumID}}); status != http.StatusBadRequest {
		t.Errorf("no file: %d %v", status, body)
	}
	if items, _ := e.server.QueueRepo.List(); len(items) != 0 {
		t.Fatalf("%d downloads queued by refused requests", len(items))
	}

	// One download per album at a time, as for any grab by hand.
	holdDownloads(t, e.client, e.base)
	again := manualForm{fields: map[string]string{"albumId": albumID}, fileName: "Fixture.Band-First.Album.nzb", file: nzb}
	if status, body := postManual(t, e.client, e.base, again); status != http.StatusAccepted {
		t.Fatalf("first download: %d %v", status, body)
	}
	if status, body := postManual(t, e.client, e.base, again); status != http.StatusConflict {
		t.Errorf("second download for the same album: %d %v, want 409", status, body)
	}

	// With music switched off nothing is accepted.
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/modules", map[string]any{"music": false}, http.StatusOK)
	if status, body := postManual(t, e.client, e.base, again); status != http.StatusConflict {
		t.Errorf("music switched off: %d %v, want 409", status, body)
	}
}

// bookEnv boots a server with ebooks and audiobooks on, a fixture Usenet server
// holding the given articles, and one book wanted in both formats.
func bookEnv(t *testing.T, articles map[string]nntpArticle) (server *api.Server, base string, client *http.Client, book books.Book, ebooks, audiobooks string) {
	t.Helper()
	server, base, client = loginNewServer(t)
	root := t.TempDir()
	ebooks, audiobooks = filepath.Join(root, "ebooks"), filepath.Join(root, "audiobooks")
	for _, d := range []string{ebooks, audiobooks} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true, "audiobooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": ebooks, "audiobooksPath": audiobooks}, http.StatusOK)
	host, port := newArticleNNTPServer(t, articles)
	postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2}, http.StatusCreated)
	b, err := server.BookRepo.Add(books.Book{OLKey: "OL1W", Title: "The Hobbit", Author: "J.R.R. Tolkien", Year: 1937, WantEbook: true, WantAudiobook: true})
	if err != nil {
		t.Fatal(err)
	}
	return server, base, client, b, ebooks, audiobooks
}

func TestManualDownloadForABookFormatIsImportedLikeAnyDownload(t *testing.T) {
	articles := map[string]nntpArticle{"e1@x": {fileName: "The Hobbit.epub", content: []byte("epub " + strings.Repeat("e", 300))}}
	server, base, client, b, ebooks, _ := bookEnv(t, articles)
	bookURL := fmt.Sprintf("%s/api/books/%d", base, b.ID)

	status, body := postManual(t, client, base, manualForm{
		fields:   map[string]string{"bookId": strconv.FormatInt(b.ID, 10), "format": "ebook"},
		fileName: "Tolkien.The.Hobbit.EPUB.nzb",
		file:     []byte(nzbFor(articles)),
	})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body %v", status, body)
	}
	waitBackground(t, server)

	want := filepath.Join(ebooks, "J.R.R. Tolkien", "The Hobbit (1937)", "The Hobbit.epub")
	if got, err := os.ReadFile(want); err != nil || string(got) != string(articles["e1@x"].content) {
		t.Fatalf("ebook at %s: %q (%v)", want, got, err)
	}
	after := getJSON[map[string]any](t, client, bookURL)
	if e := after["ebook"].(map[string]any); e["status"] != "downloaded" || e["format"] != "epub" {
		t.Fatalf("ebook after import: %v", after)
	}
	if a := after["audiobook"].(map[string]any); a["status"] != "missing" {
		t.Errorf("the audiobook was not asked for and stays missing: %v", a)
	}
	q := getJSON[[]map[string]any](t, client, base+"/api/queue")
	if len(q) != 1 || q[0]["status"] != "completed" || q[0]["bookId"] != float64(b.ID) {
		t.Fatalf("queue: %v", q)
	}
}

func TestManualDownloadForAnAudiobookIsImported(t *testing.T) {
	articles := map[string]nntpArticle{"a1@x": {fileName: "The Hobbit.m4b", content: []byte("m4b " + strings.Repeat("m", 400))}}
	server, base, client, b, _, audiobooks := bookEnv(t, articles)
	status, body := postManual(t, client, base, manualForm{
		fields:   map[string]string{"bookId": strconv.FormatInt(b.ID, 10), "format": "audiobook"},
		fileName: "Tolkien.The.Hobbit.M4B.nzb",
		file:     []byte(nzbFor(articles)),
	})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body %v", status, body)
	}
	waitBackground(t, server)
	after := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/books/%d", base, b.ID))
	a := after["audiobook"].(map[string]any)
	if a["status"] != "downloaded" || !strings.HasPrefix(fmt.Sprint(a["path"]), audiobooks) {
		t.Fatalf("audiobook after import: %v", after)
	}
}

func TestManualDownloadForABookRefusesWhatDoesNotFit(t *testing.T) {
	articles := map[string]nntpArticle{"e1@x": {fileName: "The Hobbit.epub", content: []byte("epub")}}
	server, base, client, b, _, _ := bookEnv(t, articles)
	id := strconv.FormatInt(b.ID, 10)
	nzb := []byte(nzbFor(articles))
	form := func(fields map[string]string) manualForm {
		return manualForm{fields: fields, fileName: "a.nzb", file: nzb}
	}

	tests := []struct {
		name   string
		fields map[string]string
		status int
		text   string
	}{
		{"no format", map[string]string{"bookId": id}, 400, "Choose the format"},
		{"a made-up format", map[string]string{"bookId": id, "format": "comic"}, 400, "Choose the format"},
		{"a book that isn't there", map[string]string{"bookId": "99999", "format": "ebook"}, 404, "book isn't in your library"},
		{"a book and an album", map[string]string{"bookId": id, "format": "ebook", "albumId": "1"}, 400, "not both"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := postManual(t, client, base, form(tt.fields))
			if status != tt.status || !strings.Contains(fmt.Sprint(body["error"]), tt.text) {
				t.Errorf("status %d, body %v; want %d with %q", status, body, tt.status, tt.text)
			}
		})
	}
	if items, _ := server.QueueRepo.List(); len(items) != 0 {
		t.Fatalf("%d downloads queued by refused requests", len(items))
	}

	// A format the book isn't kept in.
	only, err := server.BookRepo.Add(books.Book{OLKey: "OL2W", Title: "Only Paper", Author: "A. Writer", WantEbook: true})
	if err != nil {
		t.Fatal(err)
	}
	if status, body := postManual(t, client, base, form(map[string]string{"bookId": strconv.FormatInt(only.ID, 10), "format": "audiobook"})); status != http.StatusConflict || !strings.Contains(fmt.Sprint(body["error"]), "isn't kept in that format") {
		t.Errorf("a format the book isn't kept in: %d %v", status, body)
	}

	// One download per book format at a time.
	holdDownloads(t, client, base)
	if status, body := postManual(t, client, base, form(map[string]string{"bookId": id, "format": "ebook"})); status != http.StatusAccepted {
		t.Fatalf("first download: %d %v", status, body)
	}
	if status, body := postManual(t, client, base, form(map[string]string{"bookId": id, "format": "ebook"})); status != http.StatusConflict {
		t.Errorf("second download for the same format: %d %v, want 409", status, body)
	}

	// Audiobooks switched off.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"audiobooks": false}, http.StatusOK)
	if status, body := postManual(t, client, base, form(map[string]string{"bookId": id, "format": "audiobook"})); status != http.StatusConflict || !strings.Contains(fmt.Sprint(body["error"]), "Audiobooks are switched off") {
		t.Errorf("audiobooks off: %d %v", status, body)
	}
}

func TestManualDownloadForAlbumsAndBooksIsForAdministrators(t *testing.T) {
	server, base, _, member, _ := familyServer(t)
	for _, f := range []manualForm{
		{fields: map[string]string{"albumId": "1"}, fileName: "a.nzb", file: []byte("<nzb/>")},
		{fields: map[string]string{"bookId": "1", "format": "ebook"}, fileName: "a.nzb", file: []byte("<nzb/>")},
	} {
		if status, body := postManual(t, member, base, f); status != http.StatusForbidden {
			t.Errorf("a member posting %v: %d %v, want 403", f.fields, status, body)
		}
	}
	if items, _ := server.QueueRepo.List(); len(items) != 0 {
		t.Errorf("%d downloads queued by a member", len(items))
	}
}
