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

// A book release whose name says no language counts as the language of the
// category the indexer filed it under ("EBook (DE)"), and with German wanted
// the German one is listed first and is the one Mediarium takes (#128).
func TestBookLanguageComesFromTheIndexerCategory(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	ebooks := filepath.Join(t.TempDir(), "ebooks")
	if err := os.MkdirAll(ebooks, 0o755); err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": ebooks, "qualityLanguage": "German", "qualityLanguageOnly": true}, http.StatusOK)

	idx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("t") == "caps" {
			fmt.Fprint(w, `<caps><categories><category id="7000" name="Books"><subcat id="7020" name="Books/EBook"/><subcat id="7120" name="Books/EBook (DE)"/></category></categories></caps>`)
			return
		}
		item := func(title, cat, guid string) string {
			return fmt.Sprintf(`<item><title>%s</title><guid>%s</guid><enclosure url="http://%s/nzb/%s.nzb" length="5000000" type="application/x-nzb"/><newznab:attr name="category" value="%s"/><newznab:attr name="size" value="5000000"/></item>`, title, guid, r.Host, guid, cat)
		}
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`+
			item("J.R.R. Tolkien - The Hobbit (1937) [EPUB]", "7020", "en")+
			item("J.R.R. Tolkien - The Hobbit (1937) (retail) [EPUB]", "7120", "de")+`</channel></rss>`)
	}))
	t.Cleanup(idx.Close)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "Treasure", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)

	book := postJSON[map[string]any](t, client, base+"/api/books", map[string]any{"olKey": "OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "year": 1937, "ebook": true}, http.StatusCreated)
	bookURL := fmt.Sprintf("%s/api/books/%v", base, book["id"])
	releases := getJSON[[]map[string]any](t, client, bookURL+"/releases?format=ebook")
	if len(releases) != 2 {
		t.Fatalf("releases: %v", releases)
	}
	if !strings.Contains(releases[0]["title"].(string), "retail") || releases[0]["language"] != "German" || releases[1]["languageFit"] != "other" {
		t.Fatalf("the German release should come first and the other be marked as another language: %v", releases)
	}

	postJSON[map[string]any](t, client, bookURL+"/search?format=ebook", nil, http.StatusOK)
	waitBackground(t, server)
	q := getJSON[[]map[string]any](t, client, base+"/api/queue")
	if len(q) != 1 || !strings.Contains(q[0]["releaseTitle"].(string), "retail") {
		t.Fatalf("Mediarium should have taken the German release: %v", q)
	}
}
