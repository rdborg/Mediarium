package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newFakeOpenLibrary(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"docs":[{"key":"/works/OL1W","title":"The Hobbit","author_name":["J.R.R. Tolkien"],"author_key":["OL26320A"],"first_publish_year":1937,"cover_i":123,"format":["Paperback","eBook","Audio CD"]}]}`)
	})
	mux.HandleFunc("/trending/weekly.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"works":[{"key":"/works/OL1W","title":"The Hobbit","author_name":["J.R.R. Tolkien"],"cover_i":123},{"key":"/works/OL9W","title":"Atomic Habits","author_name":["James Clear"],"cover_i":5}]}`)
	})
	mux.HandleFunc("/works/OL1W.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"title":"The Hobbit","description":"A hobbit goes on a trip.","covers":[123]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestBookRoutesAnswer404WhileBothFormatsAreOff(t *testing.T) {
	_, base, client := loginNewServer(t)
	for _, path := range []string{"/api/books", "/api/books/search?q=x", "/api/books/1"} {
		if status, _ := doStatus(t, client, http.MethodGet, base+path); status != http.StatusNotFound {
			t.Errorf("GET %s with books off: %d", path, status)
		}
	}
}

func TestEbookSearchAddGrabImport(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	root := t.TempDir()
	ebooks, audiobooks := filepath.Join(root, "ebooks"), filepath.Join(root, "audiobooks")
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": ebooks, "audiobooksPath": audiobooks}, http.StatusOK)

	found := getJSON[[]map[string]any](t, client, base+"/api/books/search?q=hobbit")
	if len(found) != 1 || found[0]["key"] != "OL1W" || found[0]["coverUrl"] == "" {
		t.Fatalf("search: %v", found)
	}
	add := map[string]any{"olKey": "OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "year": 1937, "ebook": true}
	// Audiobooks are off, so asking for one is refused.
	add["audiobook"] = true
	postJSON[map[string]any](t, client, base+"/api/books", add, http.StatusConflict)
	add["audiobook"] = false
	book := postJSON[map[string]any](t, client, base+"/api/books", add, http.StatusCreated)
	if book["description"] != "A hobbit goes on a trip." || book["ebook"].(map[string]any)["status"] != "missing" {
		t.Fatalf("added: %v", book)
	}
	postJSON[map[string]any](t, client, base+"/api/books", add, http.StatusConflict)
	trending := getJSON[[]map[string]any](t, client, base+"/api/books/discover?list=trending")
	if len(trending) != 2 || trending[0]["libraryId"] != book["id"] || trending[1]["libraryId"] != nil {
		t.Fatalf("trending marks what is in the library: %v", trending)
	}
	if status, _ := doStatus(t, client, http.MethodGet, base+"/api/books/discover?list=nope"); status != http.StatusBadRequest {
		t.Errorf("unknown list: %d", status)
	}
	if again := getJSON[[]map[string]any](t, client, base+"/api/books/search?q=hobbit"); again[0]["libraryId"] != book["id"] {
		t.Fatalf("search marks the book as in the library: %v", again)
	}

	articles := map[string]nntpArticle{
		"e1@x": {fileName: "The Hobbit.epub", content: []byte("epub " + strings.Repeat("e", 300))},
		"n1@x": {fileName: "release.nfo", content: []byte("nfo")},
	}
	host, port := newArticleNNTPServer(t, articles)
	idx := newMusicIndexer(t, []string{
		"J.R.R. Tolkien - The Silmarillion (1977) [EPUB]",
		"J.R.R. Tolkien - The Hobbit (1937) [PDF]",
		"J.R.R. Tolkien - The Hobbit (1937) [EPUB]",
		"J.R.R. Tolkien - The Hobbit (1937) [M4B]",
	}, nzbFor(articles))
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "Books", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2}, http.StatusCreated)

	bookURL := fmt.Sprintf("%s/api/books/%v", base, book["id"])
	releases := getJSON[[]map[string]any](t, client, bookURL+"/releases?format=ebook")
	if len(releases) != 2 || releases[0]["title"] != "J.R.R. Tolkien - The Hobbit (1937) [EPUB]" {
		t.Fatalf("releases (the EPUB first, no other book, no audiobook): %v", releases)
	}
	if cats := idx.categories(); len(cats) == 0 || cats[0] != "7000,7020" {
		t.Fatalf("searched categories: %v", cats)
	}

	now := postJSON[map[string]any](t, client, bookURL+"/search?format=ebook", nil, http.StatusOK)
	if now["grabbed"] != float64(1) {
		t.Fatalf("search now: %v", now)
	}
	waitBackground(t, server)

	want := filepath.Join(ebooks, "J.R.R. Tolkien", "The Hobbit (1937)", "The Hobbit.epub")
	if got, err := os.ReadFile(want); err != nil || string(got) != string(articles["e1@x"].content) {
		t.Fatalf("ebook at %s: %q (%v)", want, got, err)
	}
	after := getJSON[map[string]any](t, client, bookURL)
	if e := after["ebook"].(map[string]any); e["status"] != "downloaded" || e["format"] != "epub" || e["path"] != want {
		t.Fatalf("book after import: %v", after)
	}
	queueList := getJSON[[]map[string]any](t, client, base+"/api/queue")
	if len(queueList) != 1 || queueList[0]["status"] != "completed" {
		t.Fatalf("queue: %v", queueList)
	}

	if status, _ := doStatus(t, client, http.MethodDelete, bookURL+"?deleteFiles=true"); status != http.StatusOK {
		t.Fatalf("delete: %d", status)
	}
	if _, err := os.Stat(want); err == nil {
		t.Fatal("the ebook should be gone from its folder")
	}
}

func TestImportBooksAlreadyOnDisk(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	ebooks := filepath.Join(t.TempDir(), "ebooks")
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": ebooks}, http.StatusOK)
	hobbit := filepath.Join(ebooks, "J.R.R. Tolkien", "The Hobbit (1937)", "The Hobbit - J.R.R. Tolkien.epub")
	for _, p := range []string{hobbit, filepath.Join(ebooks, "Someone Else", "Unknown Thing.pdf")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("book"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{}, http.StatusAccepted)
	waitBackground(t, server)
	st := getJSON[map[string]any](t, client, base+"/api/books/import")
	sum := st["summary"].(map[string]any)
	if st["phase"] != "done" || st["total"] != float64(2) || sum["imported"] != float64(1) || sum["unmatched"] != float64(1) {
		t.Fatalf("import: %v", st)
	}
	list := getJSON[[]map[string]any](t, client, base+"/api/books")
	if len(list) != 1 {
		t.Fatalf("books: %v", list)
	}
	e := list[0]["ebook"].(map[string]any)
	if e["status"] != "downloaded" || e["path"] != hobbit || e["format"] != "epub" || e["wanted"] != true {
		t.Fatalf("imported book: %v", list[0])
	}
	if _, err := os.Stat(hobbit); err != nil {
		t.Fatal("the file stays where it was")
	}

	// A second run finds it already there.
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{}, http.StatusAccepted)
	waitBackground(t, server)
	st = getJSON[map[string]any](t, client, base+"/api/books/import")
	if st["summary"].(map[string]any)["already"] != float64(1) {
		t.Fatalf("second import: %v", st)
	}

	// Removing it with its files takes the book's own folder, not the author's.
	if status, _ := doStatus(t, client, http.MethodDelete, fmt.Sprintf("%s/api/books/%v?deleteFiles=true", base, list[0]["id"])); status != http.StatusOK {
		t.Fatalf("delete: %d", status)
	}
	if _, err := os.Stat(filepath.Dir(hobbit)); err == nil {
		t.Fatal("the book's folder should be gone")
	}
	if _, err := os.Stat(filepath.Join(ebooks, "J.R.R. Tolkien")); err != nil {
		t.Fatal("the author's folder stays")
	}
	if _, err := os.Stat(filepath.Join(ebooks, "Someone Else", "Unknown Thing.pdf")); err != nil {
		t.Fatal("other books stay")
	}
}

func TestBookInfoPage(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)

	info := getJSON[map[string]any](t, client, base+"/api/book-works/OL1W")
	if info["title"] != "The Hobbit" || info["author"] != "J.R.R. Tolkien" || info["year"] != float64(1937) || info["hasEbook"] != true || info["hasAudio"] != true ||
		info["description"] != "A hobbit goes on a trip." || info["libraryId"] != nil {
		t.Fatalf("info: %v", info)
	}
	found := getJSON[[]map[string]any](t, client, base+"/api/books/search?q=hobbit")
	if found[0]["hasEbook"] != true || found[0]["hasAudio"] != true {
		t.Fatalf("search results say which editions exist: %v", found[0])
	}
	book := postJSON[map[string]any](t, client, base+"/api/books", map[string]any{"olKey": "OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "ebook": true}, http.StatusCreated)
	if again := getJSON[map[string]any](t, client, base+"/api/book-works/OL1W"); again["libraryId"] != book["id"] {
		t.Fatalf("in the library: %v", again)
	}
	if status, _ := doStatus(t, client, http.MethodGet, base+"/api/book-works/OL9W"); status != http.StatusNotFound {
		t.Fatalf("unknown book: %d", status)
	}
}

