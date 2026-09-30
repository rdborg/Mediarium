package api_test

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// conditionalGet asks for url the way a browser does, with the headers given.
func conditionalGet(t *testing.T, client *http.Client, url string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func TestLibraryListsAreCompressedAndRevalidated(t *testing.T) {
	server, base, client := loginNewServer(t)
	for i := 0; i < 40; i++ {
		if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 1000 + i, Title: fmt.Sprintf("A Fairly Long Movie Title Number %d", i), Year: 2000 + i%20, Monitored: true}); err != nil {
			t.Fatal(err)
		}
	}
	url := base + "/api/movies"

	// Compressed for a browser that accepts gzip, with a tag to ask again with.
	resp, body := conditionalGet(t, client, url, map[string]string{"Accept-Encoding": "gzip"})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("want a gzip 200, got %d encoding %q", resp.StatusCode, resp.Header.Get("Content-Encoding"))
	}
	etag := resp.Header.Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) {
		t.Fatalf("ETag = %q", etag)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "private, no-cache" {
		t.Fatalf("Cache-Control = %q, the browser must be told to ask again", cc)
	}
	if !strings.Contains(resp.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary = %q", resp.Header.Get("Vary"))
	}
	zr, err := gzip.NewReader(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if err := json.Unmarshal(plain, &list); err != nil || len(list) != 40 {
		t.Fatalf("the compressed body should hold 40 movies, got %d (%v)", len(list), err)
	}
	if len(body) >= len(plain) {
		t.Fatalf("compressed %d bytes is not smaller than the %d plain bytes", len(body), len(plain))
	}

	// Asking again with the tag: nothing changed, so no body.
	resp, body = conditionalGet(t, client, url, map[string]string{"Accept-Encoding": "gzip", "If-None-Match": etag})
	if resp.StatusCode != http.StatusNotModified || len(body) != 0 || resp.Header.Get("ETag") != etag {
		t.Fatalf("want an empty 304 with the same tag, got %d, %d bytes, tag %q", resp.StatusCode, len(body), resp.Header.Get("ETag"))
	}
	// A list of tags, and the star, work too.
	for _, inm := range []string{`"other", ` + etag, "*"} {
		if resp, _ = conditionalGet(t, client, url, map[string]string{"If-None-Match": inm}); resp.StatusCode != http.StatusNotModified {
			t.Fatalf("If-None-Match %q gave %d", inm, resp.StatusCode)
		}
	}

	// Without gzip the same content comes plain, with the same tag.
	resp, body = conditionalGet(t, client, url, map[string]string{"Accept-Encoding": "identity"})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("ETag") != etag {
		t.Fatalf("plain: %d, encoding %q, tag %q (want %q)", resp.StatusCode, resp.Header.Get("Content-Encoding"), resp.Header.Get("ETag"), etag)
	}
	if string(body) != string(plain) {
		t.Fatal("the plain body differs from the decompressed one")
	}

	// Something changes: the old tag no longer matches and the new answer has a new tag.
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 5000, Title: "One More", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	resp, _ = conditionalGet(t, client, url, map[string]string{"Accept-Encoding": "gzip", "If-None-Match": etag})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("ETag") == etag {
		t.Fatalf("after a change: %d, tag %q", resp.StatusCode, resp.Header.Get("ETag"))
	}

	// Signed out, there is nothing to revalidate.
	anon := &http.Client{}
	if resp, _ = conditionalGet(t, anon, url, map[string]string{"If-None-Match": etag}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", resp.StatusCode)
	}
}

func TestSeriesListRevalidates(t *testing.T) {
	_, base, client := loginNewServer(t)
	url := base + "/api/series"
	resp, _ := conditionalGet(t, client, url, nil)
	etag := resp.Header.Get("ETag")
	if resp.StatusCode != http.StatusOK || etag == "" {
		t.Fatalf("first answer: %d, tag %q", resp.StatusCode, etag)
	}
	if resp, body := conditionalGet(t, client, url, map[string]string{"If-None-Match": etag}); resp.StatusCode != http.StatusNotModified || len(body) != 0 {
		t.Fatalf("second answer: %d, %d bytes", resp.StatusCode, len(body))
	}
}

func TestOtherAnswersKeepNoStore(t *testing.T) {
	_, base, client := loginNewServer(t)
	resp, _ := conditionalGet(t, client, base+"/api/settings", nil)
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("ETag") != "" {
		t.Fatalf("settings: Cache-Control %q, ETag %q", resp.Header.Get("Cache-Control"), resp.Header.Get("ETag"))
	}
}
