package books

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleBooksLanguage(t *testing.T) {
	if got := GoogleBooksLanguage("it-IT"); got != "it" {
		t.Fatalf("it-IT: %q", got)
	}
	if got := GoogleBooksLanguage("nb-NO"); got != "no" {
		t.Fatalf("nb-NO: %q", got)
	}
	if got := GoogleBooksLanguage(""); got != "en" {
		t.Fatalf("empty: %q", got)
	}
}

func TestGoogleBooksSearchKeepsOrderAndLanguage(t *testing.T) {
	var gotLang, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLang = r.URL.Query().Get("langRestrict")
		gotKey = r.URL.Query().Get("key")
		if r.URL.Query().Get("q") == "isbn:9780140328721" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"totalItems":1}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[
			{"id":"oldervolume1","volumeInfo":{"title":"Older","authors":["Ada"],"publishedDate":"1990","language":"it","canonicalVolumeLink":"https://books.google.com/books?id=oldervolume1","imageLinks":{"thumbnail":"http://books.google.com/books/content?id=oldervolume1"},"industryIdentifiers":[{"type":"ISBN_13","identifier":"9780000000001"}]}},
			{"id":"newervolume1","volumeInfo":{"title":"Newer","authors":["Ada"],"publishedDate":"2024","language":"it","infoLink":"https://books.google.com/books?id=newervolume1"}}
		]}`))
	}))
	defer srv.Close()
	g := NewGoogleBooks("test-key-value", "test")
	g.Base = srv.URL
	hits, err := g.Search(context.Background(), "ilaria tuti", "it-IT", 12)
	if err != nil {
		t.Fatal(err)
	}
	if gotLang != "it" || gotKey != "test-key-value" {
		t.Fatalf("query lang=%q key=%q", gotLang, gotKey)
	}
	if len(hits) != 2 || hits[0].Title != "Older" || hits[1].Title != "Newer" {
		t.Fatalf("order: %+v", hits)
	}
	if hits[0].Year != 1990 || hits[1].Year != 2024 {
		t.Fatalf("years: %+v", hits)
	}
	if !strings.HasPrefix(hits[0].CoverURL, "https://") || hits[0].ISBN != "9780000000001" {
		t.Fatalf("cover or isbn: %+v", hits[0])
	}
	if hits[0].InfoURL != "https://books.google.com/books?id=oldervolume1" || hits[1].InfoURL != "https://books.google.com/books?id=newervolume1" {
		t.Fatalf("links: %+v", hits)
	}
	if err := g.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleBooksRejectsABadKeyWithoutTheAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"API key not valid"}}`))
	}))
	defer srv.Close()
	g := NewGoogleBooks("secret-key-value", "test")
	g.Base = srv.URL
	_, err := g.Search(context.Background(), "quiet", "en-US", 5)
	if !errors.Is(err, ErrGoogleBooksKey) {
		t.Fatalf("err: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "secret-key-value") {
		t.Fatalf("key leaked: %v", err)
	}
}

func TestGoogleBooksNoKeyStillAsks(t *testing.T) {
	var called bool
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotKey = r.URL.Query().Get("key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"id":"vol1","volumeInfo":{"title":"Quiet","authors":["Ada"],"publishedDate":"2020","description":"<p>A note.</p>"}}]}`))
	}))
	defer srv.Close()
	g := NewGoogleBooks("", "test")
	g.Base = srv.URL
	hits, err := g.Search(context.Background(), "quiet", "en", 5)
	if err != nil || !called || gotKey != "" || len(hits) != 1 || hits[0].Description != "A note." {
		t.Fatalf("called=%v key=%q hits=%+v err=%v", called, gotKey, hits, err)
	}
}

func TestGoogleCoverKeepsTheThumbnailZoom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"elmovolume1","volumeInfo":{"title":"L'elmo di Scipio","authors":["Angelo Soro"],"publishedDate":"2025","imageLinks":{"thumbnail":"http://books.google.com/books/content?id=elmovolume1&printsec=frontcover&img=1&zoom=1&edge=curl&source=gbs_api"}}}]}`))
	}))
	defer srv.Close()
	g := NewGoogleBooks("", "test")
	g.Base = srv.URL
	hits, err := g.Search(context.Background(), "elmo", "it", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits: %+v err=%v", hits, err)
	}
	u := hits[0].CoverURL
	if !strings.HasPrefix(u, "https://") || !strings.Contains(u, "zoom=1") || strings.Contains(u, "zoom=3") || strings.Contains(u, "edge=curl") {
		t.Fatalf("cover url: %s", u)
	}
}

func TestGoogleBookUsesTheSnippetWhenTheVolumeHasNoDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"elmovolume1","volumeInfo":{"title":"L'elmo di Scipio","authors":["Angelo Soro"],"publishedDate":"2025"},"searchInfo":{"textSnippet":"Cagliari, 1980. Una giornata."}}`))
	}))
	defer srv.Close()
	g := NewGoogleBooks("", "test")
	g.Base = srv.URL
	info, err := g.Book(context.Background(), "elmovolume1")
	if err != nil || info.Description != "Cagliari, 1980. Una giornata." {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestMergeGoogleFillsGapsAndAppendsTheRest(t *testing.T) {
	open := []Found{{Key: "OL1W", Title: "Senza copertina", Author: "Qualcuno", Year: 2001}}
	goog := []Found{
		{GoogleID: "same", Title: "Senza copertina", Author: "Qualcuno", Year: 2001, GoogleCover: "https://example/c.jpg", GoogleBlurb: "La trama."},
		{GoogleID: "other", Title: "Solo qui", Author: "Ada", Year: 1990, GoogleCover: "https://example/o.jpg", GoogleBlurb: "Tutto."},
		{GoogleID: "later", Title: "Senza copertina", Author: "Qualcuno", Year: 2018, GoogleCover: "https://example/no.jpg"},
	}
	got := MergeGoogle(open, goog)
	if len(got) != 3 {
		t.Fatalf("len %d: %+v", len(got), got)
	}
	if got[0].Key != "OL1W" || got[0].GoogleCover != "https://example/c.jpg" || got[0].GoogleBlurb != "La trama." {
		t.Fatalf("filled: %+v", got[0])
	}
	if got[1].Key != "gb-other" || got[1].GoogleBlurb != "Tutto." {
		t.Fatalf("google only: %+v", got[1])
	}
	if got[2].Key != "gb-later" {
		t.Fatalf("different year stayed separate: %+v", got[2])
	}
}
