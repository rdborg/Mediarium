package api_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKindleBooksOpenAsEPUB(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	ebooks := filepath.Join(t.TempDir(), "ebooks")
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": ebooks}, http.StatusOK)
	azw3, err := os.ReadFile("../ebookconv/testdata/test.azw3")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(ebooks, "J.R.R. Tolkien", "The Hobbit (1937)", "The Hobbit.azw3")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, azw3, 0o644); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{}, http.StatusAccepted)
	waitBackground(t, server)
	list := getJSON[[]map[string]any](t, client, base+"/api/books")
	if len(list) != 1 || list[0]["ebook"].(map[string]any)["format"] != "azw3" {
		t.Fatalf("books: %v", list)
	}
	readURL := fmt.Sprintf("%s/api/books/%v/read", base, list[0]["id"])

	get := func(url string) (*http.Response, []byte) {
		t.Helper()
		resp, err := client.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, b
	}

	// Downloading gives the original file.
	if resp, body := get(readURL + "?download=1"); resp.StatusCode != http.StatusOK || !bytes.Equal(body, azw3) {
		t.Fatalf("download: %d, %d bytes", resp.StatusCode, len(body))
	}
	// The reader gets an EPUB.
	resp, body := get(readURL + "?as=epub")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/epub+zip" {
		t.Fatalf("as epub: %d %s %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || z.File[0].Name != "mimetype" {
		t.Fatalf("not an EPUB: %v", err)
	}
	cache, _ := filepath.Glob(filepath.Join(server.TestConfigDir(), "cache", "epub", "*.epub"))
	if len(cache) != 1 {
		t.Fatalf("the copy is kept in the cache: %v", cache)
	}
	// The library folder is left as it was.
	if entries, _ := os.ReadDir(filepath.Dir(file)); len(entries) != 1 {
		t.Fatalf("the library folder changed: %v", entries)
	}
	// A second open uses the copy.
	if resp, again := get(readURL + "?as=epub"); resp.StatusCode != http.StatusOK || !bytes.Equal(again, body) {
		t.Fatal("the second open made a different copy")
	}

	// A file that isn't a Kindle book gets a plain message, not a crash.
	if err := os.WriteFile(file, []byte(strings.Repeat("not a book ", 50)), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(file, later, later)
	resp, body = get(readURL + "?as=epub")
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "couldn't be turned into") {
		t.Fatalf("broken file: %d %s", resp.StatusCode, body)
	}
}
