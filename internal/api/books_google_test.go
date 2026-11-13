package api_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleBooksSearchShowsCoversAndCanBeAdded(t *testing.T) {
	server, base, client := loginNewServer(t)
	ol := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if strings.Contains(q, "isbn:") {
			w.Write([]byte(`{"docs":[{"key":"/works/OL9W","title":"Edizione italiana","author_name":["Ilaria Tuti"],"author_key":["OL1A"],"first_publish_year":2018,"cover_i":5}]}`))
			return
		}
		w.Write([]byte(`{"docs":[{"key":"/works/OL1W","title":"Senza copertina","author_name":["Qualcuno"],"first_publish_year":2001}]}`))
	}))
	t.Cleanup(ol.Close)
	server.TestSetOpenLibrary(ol.URL)

	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 8, 12)), nil); err != nil {
		t.Fatal(err)
	}
	picture := jpegBytes.Bytes()
	var lang, gotKey string
	gb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(picture)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/volumes/") {
			id := strings.TrimPrefix(r.URL.Path, "/volumes/")
			if id == "elmovolume1" {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":"elmovolume1","volumeInfo":{"title":"Elmo","authors":["Angelo Soro"],"publishedDate":"2025","description":"La trama di Scipio.","language":"it","imageLinks":{"thumbnail":"` + gbURL(r) + `/cover.jpg?zoom=1"}}}`))
				return
			}
			if id != "oldervolume1" {
				t.Errorf("volume looked up that search did not return: %s", id)
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"oldervolume1","volumeInfo":{"title":"Older","authors":["Ilaria Tuti"],"publishedDate":"1990","description":"<p>Una nota.</p>","language":"it","canonicalVolumeLink":"https://books.google.com/books?id=oldervolume1","imageLinks":{"thumbnail":"` + gbURL(r) + `/cover.jpg"}}}`))
			return
		}
		if r.URL.Query().Get("q") == "isbn:9780140328721" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"totalItems":1}`))
			return
		}
		lang = r.URL.Query().Get("langRestrict")
		gotKey = r.URL.Query().Get("key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[
			{"id":"oldervolume1","volumeInfo":{"title":"Older","authors":["Ilaria Tuti"],"publishedDate":"1990","description":"<p>Una nota.</p>","language":"it","canonicalVolumeLink":"https://books.google.com/books?id=oldervolume1","imageLinks":{"thumbnail":"` + gbURL(r) + `/cover.jpg"},"industryIdentifiers":[{"type":"ISBN_13","identifier":"9780000000001"}]}},
			{"id":"newervolume1","volumeInfo":{"title":"Newer","authors":["Ilaria Tuti"],"publishedDate":"2024","language":"it","infoLink":"https://books.google.com/books?id=newervolume1"}},
			{"id":"elmovolume1","volumeInfo":{"title":"Elmo","authors":["Angelo Soro"],"publishedDate":"2025","language":"it","imageLinks":{"thumbnail":"` + gbURL(r) + `/cover.jpg?zoom=1"}}}
		]}`))
	}))
	t.Cleanup(gb.Close)
	server.TestSetGoogleBooks(gb.URL)
	server.TestSetCoverLinkClient(http.DefaultClient)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"infoLanguage": "it-IT"}, http.StatusOK)

	before := getJSON[[]map[string]any](t, client, base+"/api/books/search?q=ilaria")
	if gotKey != "" {
		t.Fatalf("key sent when none is saved: %q", gotKey)
	}
	if len(before) < 2 {
		t.Fatalf("without a key Google is still asked: %v", before)
	}

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings/google-books", map[string]string{"key": "test-key-value"}, http.StatusOK)
	found := getJSON[[]map[string]any](t, client, base+"/api/books/search?q=tuti")
	if lang != "it" || gotKey != "test-key-value" {
		t.Fatalf("lang=%q key=%q", lang, gotKey)
	}
	var older, newer, plain map[string]any
	for _, row := range found {
		switch row["title"] {
		case "Older":
			older = row
		case "Newer":
			newer = row
		case "Senza copertina":
			plain = row
		}
	}
	var olderCover string
	if older != nil {
		olderCover, _ = older["coverUrl"].(string)
	}
	if older == nil || older["key"] != "gb-oldervolume1" || older["googleId"] != "oldervolume1" || !strings.HasPrefix(olderCover, "/api/book-covers/gb-oldervolume1.jpg") {
		t.Fatalf("google book with a cover: %v", older)
	}
	if newer == nil || newer["key"] != "gb-newervolume1" || newer["coverUrl"] != nil {
		t.Fatalf("google book without a cover: %v", newer)
	}
	if plain == nil || plain["key"] != "OL1W" {
		t.Fatalf("open library book: %v", plain)
	}

	resp, err := client.Get(base + "/api/book-covers/gb-oldervolume1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	coverBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(coverBody, []byte{0xff, 0xd8}) {
		t.Fatalf("cover: %d %d bytes", resp.StatusCode, len(coverBody))
	}

	work := getJSON[map[string]any](t, client, base+"/api/book-works/gb-oldervolume1")
	workCover, _ := work["coverUrl"].(string)
	if work["title"] != "Older" || work["description"] != "Una nota." || !strings.HasPrefix(workCover, "/api/book-covers/gb-oldervolume1.jpg") || work["subjects"] == nil {
		t.Fatalf("work page: %v", work)
	}
	elmo := getJSON[map[string]any](t, client, base+"/api/book-works/gb-elmovolume1")
	if elmo["title"] != "Elmo" || elmo["description"] != "La trama di Scipio." {
		t.Fatalf("work page asked the volume for a missing description: %v", elmo)
	}

	added := postJSON[map[string]any](t, client, base+"/api/books", map[string]any{
		"olKey": "gb-oldervolume1", "title": "Older", "author": "Ilaria Tuti", "year": 1990, "googleId": "oldervolume1", "ebook": true,
	}, http.StatusCreated)
	gotCover, _ := added["coverUrl"].(string)
	if added["title"] != "Older" || added["olKey"] != "gb-oldervolume1" || added["description"] != "Una nota." || !strings.Contains(gotCover, "/own-cover") {
		t.Fatalf("added with google details and cover: %v", added)
	}

	fromISBN := postJSON[map[string]any](t, client, base+"/api/books/search/google", map[string]any{
		"isbn": "9780000000001", "title": "Older", "author": "Ilaria Tuti", "ebook": true, "searchNow": false,
	}, http.StatusCreated)
	if fromISBN["title"] != "Edizione italiana" || fromISBN["olKey"] != "OL9W" {
		t.Fatalf("isbn still on open library: %v", fromISBN)
	}

	missing := postJSON[map[string]any](t, client, base+"/api/books/search/google", map[string]any{
		"title": "Missing Entirely", "author": "Nobody", "ebook": true,
	}, http.StatusNotFound)
	if missing["error"] == nil {
		t.Fatalf("missing: %v", missing)
	}

	unseen, err := client.Get(base + "/api/book-covers/gb-notseen01.jpg")
	if err != nil {
		t.Fatal(err)
	}
	unseen.Body.Close()
	if unseen.StatusCode != http.StatusNotFound {
		t.Fatalf("unseen cover: %d", unseen.StatusCode)
	}
}

func gbURL(r *http.Request) string {
	return "http://" + r.Host
}
