package api_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestReadListenAndProgress(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	root := t.TempDir()
	ebooks, audiobooks := filepath.Join(root, "ebooks"), filepath.Join(root, "audiobooks")
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true, "audiobooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": ebooks, "audiobooksPath": audiobooks}, http.StatusOK)
	files := map[string]string{
		filepath.Join(ebooks, "J.R.R. Tolkien", "The Hobbit (1937)", "The Hobbit.epub"): "EPUB-BYTES",
		filepath.Join(audiobooks, "J.R.R. Tolkien", "The Hobbit", "2 - Two.mp3"):        "two",
		filepath.Join(audiobooks, "J.R.R. Tolkien", "The Hobbit", "10 - Ten.mp3"):       "ten-ten-ten",
		filepath.Join(audiobooks, "J.R.R. Tolkien", "The Hobbit", "1 - One.mp3"):        "one",
	}
	for p, body := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{}, http.StatusAccepted)
	waitBackground(t, server)
	list := getJSON[[]map[string]any](t, client, base+"/api/books")
	if len(list) != 1 {
		t.Fatalf("both formats land on one book: %v", list)
	}
	bookURL := fmt.Sprintf("%s/api/books/%v", base, list[0]["id"])

	get := func(url string, header map[string]string) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	resp, body := get(bookURL+"/read", nil)
	if resp.StatusCode != http.StatusOK || body != "EPUB-BYTES" || resp.Header.Get("Content-Type") != "application/epub+zip" {
		t.Fatalf("read: %d %q %s", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}

	tracks := getJSON[map[string]any](t, client, bookURL+"/tracks")["tracks"].([]any)
	var names []string
	for _, tr := range tracks {
		names = append(names, tr.(map[string]any)["name"].(string))
	}
	if fmt.Sprint(names) != "[1 - One 2 - Two 10 - Ten]" {
		t.Fatalf("tracks in counting order: %v", names)
	}
	resp, body = get(bookURL+"/listen/2", map[string]string{"Range": "bytes=0-2"})
	if resp.StatusCode != http.StatusPartialContent || body != "ten" || resp.Header.Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("listen with a range: %d %q %s", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
	if resp, _ := get(bookURL+"/listen/3", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("no such track: %d", resp.StatusCode)
	}

	postJSONMethod[any](t, client, http.MethodPut, bookURL+"/progress", map[string]any{"format": "audiobook", "position": "1:93.5", "percent": 40}, http.StatusOK)
	p := getJSON[map[string]any](t, client, bookURL+"/progress?format=audiobook")
	if p["position"] != "1:93.5" || p["percent"] != float64(40) {
		t.Fatalf("progress: %v", p)
	}
	all := getJSON[[]map[string]any](t, client, base+"/api/books/progress")
	if len(all) != 1 || all[0]["format"] != "audiobook" {
		t.Fatalf("all progress: %v", all)
	}
	if empty := getJSON[map[string]any](t, client, bookURL+"/progress?format=ebook"); empty["position"] != "" {
		t.Fatalf("never opened: %v", empty)
	}
}
