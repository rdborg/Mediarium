package api_test

import (
	"net/http"
	"testing"
	"time"
)

func TestBooksOnTheCalendar(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(fakeSeriesLibrary(t).URL)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/book-series/openlibrary/OL5L/follow", map[string]any{"name": "Saga", "ebook": true}, http.StatusOK)
	waitBackground(t, server)
	list := getJSON[[]map[string]any](t, client, base+"/api/books")
	soon := time.Now().AddDate(0, 0, 20).Format("2006-01-02")
	far := time.Now().AddDate(2, 0, 0).Format("2006-01-02")
	server.TestSetBookReleaseDate(int64(list[0]["id"].(float64)), soon)
	server.TestSetBookReleaseDate(int64(list[1]["id"].(float64)), far)

	var books []map[string]any
	for _, e := range getJSON[[]map[string]any](t, client, base+"/api/calendar") {
		if e["kind"] == "book" {
			books = append(books, e)
		}
	}
	if len(books) != 1 || books[0]["releaseDate"] != soon || books[0]["bookId"] != list[0]["id"] || books[0]["status"] != "missing" {
		t.Fatalf("only the book coming out within the window is on the calendar: %v", books)
	}
	if sub, _ := books[0]["subtitle"].(string); sub == "" {
		t.Fatalf("no subtitle: %v", books[0])
	}

	// Ebooks and audiobooks off: no books on the calendar.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": false, "movies": true}, http.StatusOK)
	for _, e := range getJSON[[]map[string]any](t, client, base+"/api/calendar") {
		if e["kind"] == "book" {
			t.Fatalf("a book on the calendar with books off: %v", e)
		}
	}
}
