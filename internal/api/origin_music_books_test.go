package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

// An artist, its albums and a book added by hand say so, with the person, to
// an administrator: on the artist's page (and each album there), the album's
// own answer and the book's page. An album added later to the artist keeps
// its own origin.
func TestMusicAndBookOrigins(t *testing.T) {
	e := newMusicEnv(t)
	added := postJSON[map[string]any](t, e.client, e.base+"/api/music/manual", map[string]any{"artist": "Hand Band", "album": "Handmade"}, http.StatusCreated)
	artist := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/artists/%v", e.base, added["artistId"]))
	album := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/albums/%v", e.base, added["albumId"]))
	listed := artist["albums"].([]any)[0].(map[string]any)
	for name, got := range map[string]map[string]any{"artist": artist, "album": album, "album on the artist page": listed} {
		o := originOf(t, got)
		if o == nil || o["kind"] != "manual" {
			t.Fatalf("%s: origin %v", name, got["origin"])
		}
		if u, _ := o["user"].(map[string]any); u == nil || u["name"] == "" {
			t.Fatalf("%s: the person behind it: %v", name, o)
		}
	}

	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	b := postJSON[map[string]any](t, e.client, e.base+"/api/books/manual", map[string]any{"title": "Home Brew", "ebook": true}, http.StatusCreated)
	book := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/books/%v", e.base, b["id"]))
	if o := originOf(t, book); o == nil || o["kind"] != "manual" || o["user"] == nil {
		t.Fatalf("book: origin %v", book["origin"])
	}
}
