package books

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatches(t *testing.T) {
	b := Book{Title: "The Hobbit", Author: "J. R. R. Tolkien"}
	cases := []struct {
		release string
		ok      bool
	}{
		{"J.R.R. Tolkien - The Hobbit (1937) [EPUB]", true},
		{"Tolkien.The.Hobbit.Unabridged.M4B", true},
		{"The Hobbit - Some Other Author (epub)", false},
		{"Tolkien - The Silmarillion epub", false},
		{"Hobbit", false},
		{"Tolkien - Hobbit epub", true},
		{"J.R.R.Tolkien-The.Hobbit.Retail.EPUB.eBook-GROUP", true},
		{"Tolkien - The Hobbit and the Ring epub", false},
	}
	for _, tc := range cases {
		if got := Matches(tc.release, b); got != tc.ok {
			t.Errorf("%q: %v, want %v", tc.release, got, tc.ok)
		}
	}
	// A short title must not match a longer one that contains it.
	it := Book{Title: "It", Author: "Stephen King"}
	short := []struct {
		release string
		ok      bool
	}{
		{"Stephen.King-If.It.Bleeds.2020.Retail.EPUB.eBook-BitBook", false},
		{"Stephen King - It Bleeds epub", false},
		{"Stephen King - It (1986) [EPUB]", true},
		{"Stephen.King-It.Retail.EPUB.eBook-BitBook", true},
		{"It - Stephen King.epub", true},
		{"Stephen_King-It-AUDIOBOOK-WEB-EN-2017-GROUP", true},
		{"It by Stephen King unabridged m4b", true},
	}
	for _, tc := range short {
		if got := Matches(tc.release, it); got != tc.ok {
			t.Errorf("%q for It: %v, want %v", tc.release, got, tc.ok)
		}
	}
	series := Book{Title: "The Gunslinger", Author: "Stephen King"}
	if !Matches("Stephen King - The Dark Tower 01 - The Gunslinger (1982) epub", series) {
		t.Error("a title after a series number should match")
	}
	if Matches("anything", Book{Title: "The"}) {
		t.Error("a title of only small words must not match everything")
	}
}

func TestReleaseRank(t *testing.T) {
	cases := []struct {
		release string
		f       Format
		want    int
	}{
		{"Author - Book (2020) EPUB", Ebook, 14},
		{"Author - Book PDF", Ebook, 11},
		{"Author - Book", Ebook, 5},
		{"Author - Book M4B", Ebook, 0},
		{"Author - Book M4B", Audiobook, 14},
		{"Author - Book MP3 64kbps", Audiobook, 12},
		{"Author - Book EPUB", Audiobook, 0},
	}
	for _, tc := range cases {
		if got := ReleaseRank(tc.release, tc.f); got != tc.want {
			t.Errorf("%q %s: %d, want %d", tc.release, tc.f, got, tc.want)
		}
	}
}

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFilesInADownload(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "book.pdf"), 900)
	write(t, filepath.Join(dir, "sub", "book.epub"), 100)
	write(t, filepath.Join(dir, "cover.jpg"), 50)
	if p, f := EbookFile(dir); filepath.Base(p) != "book.epub" || f != "epub" {
		t.Errorf("ebook: %s %s, want the epub", p, f)
	}
	audio := t.TempDir()
	write(t, filepath.Join(audio, "02 - Chapter.mp3"), 10)
	write(t, filepath.Join(audio, "01 - Chapter.mp3"), 10)
	write(t, filepath.Join(audio, "sample.ogg"), 10)
	files, f := AudioFiles(audio)
	if f != "mp3" || len(files) != 2 || filepath.Base(files[0]) != "01 - Chapter.mp3" {
		t.Errorf("audio: %v %s", files, f)
	}
	if p, _ := EbookFile(audio); p != "" {
		t.Error("no ebook in an audio folder")
	}
}

func TestBrowseQuery(t *testing.T) {
	cases := []struct {
		q    BrowseQuery
		want string
	}{
		{BrowseQuery{Subject: "Fantasy"}, `subject:"fantasy"`},
		{BrowseQuery{Subject: "science fiction", YearFrom: 2000, YearTo: 2010}, `subject:"science fiction" first_publish_year:[2000 TO 2010]`},
		{BrowseQuery{YearFrom: 1990}, `first_publish_year:[1990 TO *]`},
		{BrowseQuery{}, `first_publish_year:[1000 TO 3000]`},
	}
	for _, tc := range cases {
		if got := tc.q.SearchQuery(); got != tc.want {
			t.Errorf("%+v: %s, want %s", tc.q, got, tc.want)
		}
	}
}

func TestTrendingAndBrowse(t *testing.T) {
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending/weekly.json":
			_, _ = w.Write([]byte(`{"works":[{"key":"/works/OL1W","title":"Atomic Habits","author_name":["James Clear"],"cover_i":5},{"key":"/works/OL2W","title":"No cover"},{"key":"/works/OL1W","title":"Atomic Habits again","cover_i":5}]}`))
		case "/search.json":
			if strings.HasPrefix(r.URL.Query().Get("q"), "key:(") {
				_, _ = w.Write([]byte(`{"docs":[{"key":"/works/OL1W","format":["Kindle Edition","Audio CD"]}]}`))
				return
			}
			lastQuery = r.URL.Query().Get("q") + "|" + r.URL.Query().Get("sort")
			_, _ = w.Write([]byte(`{"docs":[{"key":"/works/OL3W","title":"The Hunger Games","author_name":["Suzanne Collins"],"first_publish_year":2008,"cover_i":7}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	tr, err := c.Trending(context.Background(), "weekly", 1, 20)
	if err != nil || len(tr) != 1 || tr[0].Key != "OL1W" || !tr[0].HasEbook || !tr[0].HasAudio {
		t.Fatalf("trending (no cover and duplicates left out, editions filled in): %+v %v", tr, err)
	}
	if _, err := c.Trending(context.Background(), "hourly", 1, 20); err == nil {
		t.Error("an unknown period is refused")
	}
	br, err := c.Browse(context.Background(), BrowseQuery{Subject: "fantasy", Sort: "newest"})
	if err != nil || len(br) != 1 || br[0].Year != 2008 || lastQuery != `subject:"fantasy"|new` {
		t.Fatalf("browse: %+v %v (%s)", br, err, lastQuery)
	}
}

func TestCleanDescription(t *testing.T) {
	cases := []struct{ in, want string }{
		{"A hobbit goes on a trip.", "A hobbit goes on a trip."},
		{"First.\r\n\r\nSecond.", "First.\n\nSecond."},
		{"Story text.\r\n\r\n----------\r\nAlso contained in:\r\n\r\n - [The Hobbit / The Lord of the Rings](https://openlibrary.org/works/OL1W)", "Story text."},
		{"See [the author][1] for more.\n\n([source][1])\n\n[1]: https://example.org/", "See the author for more."},
		{"Read [this](https://example.org) now.", "Read this now."},
		{"One.\n\n\n\nTwo.", "One.\n\nTwo."},
	}
	for _, tc := range cases {
		if got := CleanDescription(tc.in); got != tc.want {
			t.Errorf("CleanDescription(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOpenLibraryClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search.json":
			_, _ = w.Write([]byte(`{"docs":[{"key":"/works/OL1W","title":"The Hobbit","author_name":["J.R.R. Tolkien"],"author_key":["OL26320A"],"first_publish_year":1937,"cover_i":123},{"key":"","title":"broken"}]}`))
		case "/works/OL1W.json":
			_, _ = w.Write([]byte(`{"title":"The Hobbit","description":{"type":"/type/text","value":"A hobbit goes on a trip."},"covers":[123],"authors":[{"author":{"key":"/authors/OL26320A"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	found, err := c.Search(context.Background(), "hobbit", 5)
	if err != nil || len(found) != 1 || found[0].Key != "OL1W" || found[0].Author != "J.R.R. Tolkien" || found[0].Year != 1937 || found[0].CoverID != 123 {
		t.Fatalf("search: %+v (err %v)", found, err)
	}
	w, err := c.GetWork(context.Background(), "OL1W")
	if err != nil || w.Description != "A hobbit goes on a trip." || w.CoverID != 123 || w.AuthorKey != "OL26320A" {
		t.Fatalf("work: %+v (err %v)", w, err)
	}
	if _, err := c.GetWork(context.Background(), "OL9W"); err != ErrNotFound {
		t.Fatalf("missing work: %v", err)
	}
	if _, err := c.GetWork(context.Background(), "../x"); err != ErrNotFound {
		t.Fatalf("odd key: %v", err)
	}
	if CoverURL(123, "M") != "https://covers.openlibrary.org/b/id/123-M.jpg" || CoverURL(0, "M") != "" {
		t.Error("cover url")
	}
}

func TestEditions(t *testing.T) {
	cases := []struct {
		formats     []string
		ebook, audo bool
	}{
		{[]string{"Paperback", "Hardcover"}, false, false},
		{[]string{"Paperback", "eBook"}, true, false},
		{[]string{"Kindle Edition", "Audio CD"}, true, true},
		{[]string{"Audible eAudiobook"}, false, true},
		{[]string{"[sound recording] /", "Epub"}, true, true},
		{[]string{"preloaded digital audio player"}, false, true},
		{[]string{"[electronic resource]"}, true, false},
		{nil, false, false},
	}
	for _, tc := range cases {
		e, a := Editions(tc.formats)
		if e != tc.ebook || a != tc.audo {
			t.Errorf("Editions(%q) = %v, %v; want %v, %v", tc.formats, e, a, tc.ebook, tc.audo)
		}
	}
}
