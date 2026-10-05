package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFollowAnAuthor(t *testing.T) {
	server, base, client := loginNewServer(t)
	year := time.Now().Year()
	ol := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" || !strings.HasPrefix(r.URL.Query().Get("q"), "author_key:OL7234434A") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"docs":[
			{"key":"/works/OL1W","title":"A New Novel","author_name":["Andy Weir"],"author_key":["OL7234434A"],"first_publish_year":%d,"cover_i":1},
			{"key":"/works/OL2W","title":"The Martian / Artemis / Project Hail Mary","author_name":["Andy Weir"],"first_publish_year":%d,"cover_i":2},
			{"key":"/works/OL3W","title":"No Cover Yet","author_name":["Andy Weir"],"first_publish_year":%d},
			{"key":"/works/OL4W","title":"The Martian","author_name":["Andy Weir"],"first_publish_year":2011,"cover_i":4}]}`, year, year, year)
	}))
	defer ol.Close()
	server.TestSetOpenLibrary(ol.URL)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)

	works := getJSON[[]map[string]any](t, client, base+"/api/book-authors/OL7234434A/works")
	if len(works) != 3 {
		t.Fatalf("box sets are left out of an author's books: %v", works)
	}
	if status, _ := doStatus(t, client, http.MethodGet, base+"/api/book-authors/..%2Fx/works"); status != http.StatusBadRequest && status != http.StatusNotFound {
		t.Fatalf("odd author key: %d", status)
	}

	postJSONMethod[any](t, client, http.MethodPut, base+"/api/book-authors/OL7234434A/follow", map[string]any{"name": "Andy Weir", "audiobook": true}, http.StatusConflict)
	postJSONMethod[any](t, client, http.MethodPut, base+"/api/book-authors/OL7234434A/follow", map[string]any{"name": "Andy Weir", "ebook": true}, http.StatusOK)
	if list := getJSON[[]map[string]any](t, client, base+"/api/book-authors"); len(list) != 1 || list[0]["name"] != "Andy Weir" {
		t.Fatalf("followed: %v", list)
	}

	if n := server.TestCheckFollowedAuthors(); n != 1 {
		t.Fatalf("only the new book with a cover is added, got %d", n)
	}
	waitBackground(t, server)
	books := getJSON[[]map[string]any](t, client, base+"/api/books")
	if len(books) != 1 || books[0]["title"] != "A New Novel" || books[0]["ebook"].(map[string]any)["wanted"] != true {
		t.Fatalf("books: %v", books)
	}
	if n := server.TestCheckFollowedAuthors(); n != 0 {
		t.Fatalf("a second check adds nothing new, got %d", n)
	}
	postJSONMethod[any](t, client, http.MethodDelete, base+"/api/book-authors/OL7234434A/follow", nil, http.StatusOK)
	if list := getJSON[[]map[string]any](t, client, base+"/api/book-authors"); len(list) != 0 {
		t.Fatalf("unfollowed: %v", list)
	}
}
