package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// An audiobook Open Library doesn't carry but Audible does should import
// through the Audnexus fallback, with the narrator and length it reported.
func TestImportAudiobookViaAudnexusFallback(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL) // answers, but nothing matches our title

	audible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("title") != "Legion's Fifth Vault" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"products":[{"asin":"B0TEST0001"}]}`)
	}))
	defer audible.Close()
	audnexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/books/B0TEST0001" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"asin":"B0TEST0001","title":"Legion's Fifth Vault","formatType":"unabridged","narrators":[{"name":"Test Narrator"}],"runtimeLengthMin":600,"releaseDate":"2023-05-04T00:00:00.000Z"}`)
	}))
	defer audnexus.Close()
	server.TestSetAudnexus(audible.URL, audnexus.URL)

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"audiobooks": true}, http.StatusOK)
	audios := filepath.Join(t.TempDir(), "audio")
	bookDir := filepath.Join(audios, "A. F Kay", "Legion's Fifth Vault")
	if err := os.MkdirAll(bookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bookDir, "01 - Legion's Fifth Vault.m4b"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"audiobooksPath": audios}, http.StatusOK)

	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{}, http.StatusAccepted)
	waitBackground(t, server)
	st := getJSON[map[string]any](t, client, base+"/api/books/import")
	if st["phase"] != "done" || st["summary"].(map[string]any)["imported"] != float64(1) {
		t.Fatalf("import: %v", st)
	}

	list := getJSON[[]map[string]any](t, client, base+"/api/books")
	if len(list) != 1 {
		t.Fatalf("books: %v", list)
	}
	b := list[0]
	if b["title"] != "Legion's Fifth Vault" || b["asin"] != "B0TEST0001" || b["narrators"] != "Test Narrator" || b["runtimeMin"] != float64(600) {
		t.Fatalf("imported book: %v", b)
	}
	a := b["audiobook"].(map[string]any)
	if a["status"] != "downloaded" || a["format"] != "m4b" || a["wanted"] != true {
		t.Fatalf("imported audiobook: %v", b)
	}
}
