package mediaservers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeBookServer records the scans it was asked for.
type fakeBookServer struct {
	*httptest.Server
	mu    sync.Mutex
	scans []string
	auths int
}

func (f *fakeBookServer) scanned() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.scans...)
}

func newFakeABS(t *testing.T) *fakeBookServer {
	t.Helper()
	f := &fakeBookServer{}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer abs-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"app":"audiobookshelf","serverVersion":"2.17.0"}`)
	})
	mux.HandleFunc("GET /api/libraries", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		fmt.Fprint(w, `{"libraries":[{"id":"lib1","name":"Audiobooks","mediaType":"book","folders":[{"fullPath":"/audiobooks"}]},{"id":"pod","name":"Podcasts","mediaType":"podcast","folders":[{"fullPath":"/podcasts"}]}]}`)
	})
	mux.HandleFunc("POST /api/libraries/{id}/scan", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		f.mu.Lock()
		f.scans = append(f.scans, r.PathValue("id"))
		f.mu.Unlock()
	})
	mux.HandleFunc("GET /api/libraries/lib1/search", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		fmt.Fprint(w, `{"book":[{"libraryItem":{"id":"li_9","media":{"metadata":{"title":"Project Hail Mary","authorName":"Andy Weir"}}}}]}`)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func newFakeKavita(t *testing.T) *fakeBookServer {
	t.Helper()
	f := &fakeBookServer{}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer jwt-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("POST /api/Plugin/authenticate", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apiKey") != "kavita-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		f.auths++
		f.mu.Unlock()
		fmt.Fprint(w, `{"username":"admin","token":"jwt-1"}`)
	})
	mux.HandleFunc("GET /api/Library/libraries", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		fmt.Fprint(w, `[{"id":3,"name":"Books","type":2,"folders":["/books"]},{"id":4,"name":"Comics","type":1,"folders":["/comics"]}]`)
	})
	mux.HandleFunc("POST /api/Library/scan", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		f.mu.Lock()
		f.scans = append(f.scans, r.URL.Query().Get("libraryId"))
		f.mu.Unlock()
	})
	mux.HandleFunc("GET /api/Search/search", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		fmt.Fprint(w, `{"series":[{"seriesId":12,"libraryId":3,"name":"The Hobbit"}]}`)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func TestAudiobookshelf(t *testing.T) {
	f := newFakeABS(t)
	c := &Client{HTTP: f.Client(), Version: "test"}
	s := Server{Name: "ABS", Kind: KindAudiobookshelf, BaseURL: f.URL, Token: "abs-token", PathMap: []PathMapping{{From: "/data/audiobooks", To: "/audiobooks"}}}

	res, err := c.Test(context.Background(), s)
	if err != nil || res.Version != "2.17.0" || len(res.Libraries) != 2 || res.Libraries[0].Locations[0] != "/audiobooks" {
		t.Fatalf("test: %+v %v", res, err)
	}
	if _, err := c.Test(context.Background(), Server{Kind: KindAudiobookshelf, BaseURL: f.URL, Token: "wrong"}); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("a wrong token is explained: %v", err)
	}

	// Only book refreshes reach it, and only the library holding the folder is scanned.
	if done, err := c.RefreshFolders(context.Background(), s, MediaMovie, []string{"/movies/X"}); err != nil || len(done) != 0 {
		t.Fatalf("movies are not for a book server: %v %v", done, err)
	}
	done, err := c.RefreshFolders(context.Background(), s, MediaBook, []string{"/data/audiobooks/Andy Weir/Project Hail Mary (2021)", "/data/ebooks/Other/Book"})
	if err != nil || len(done) != 1 || fmt.Sprint(f.scanned()) != "[lib1]" {
		t.Fatalf("refresh: %v %v scans %v", done, err, f.scanned())
	}

	item, found, err := c.FindBook(context.Background(), s, "Project Hail Mary", "Andy Weir")
	if err != nil || !found || item.URL != f.URL+"/item/li_9" {
		t.Fatalf("find: %+v %v %v", item, found, err)
	}
	if _, found, _ := c.FindBook(context.Background(), s, "Artemis", "Andy Weir"); found {
		t.Fatal("another title is not a match")
	}
}

func TestKavita(t *testing.T) {
	f := newFakeKavita(t)
	c := &Client{HTTP: f.Client(), Version: "test"}
	s := Server{Name: "Kavita", Kind: KindKavita, BaseURL: f.URL, Token: "kavita-key"}

	res, err := c.Test(context.Background(), s)
	if err != nil || len(res.Libraries) != 2 || res.Libraries[0].ID != "3" {
		t.Fatalf("test: %+v %v", res, err)
	}
	done, err := c.RefreshFolders(context.Background(), s, MediaBook, []string{"/books/J.R.R. Tolkien/The Hobbit (1937)"})
	if err != nil || len(done) != 1 || fmt.Sprint(f.scanned()) != "[3]" {
		t.Fatalf("refresh: %v %v scans %v", done, err, f.scanned())
	}
	item, found, err := c.FindBook(context.Background(), s, "The Hobbit", "")
	if err != nil || !found || item.URL != f.URL+"/library/3/series/12" {
		t.Fatalf("find: %+v %v %v", item, found, err)
	}
	if f.auths != 1 {
		t.Errorf("the API key is swapped for a token once and kept: %d sign-ins", f.auths)
	}
	if _, err := c.Test(context.Background(), Server{Kind: KindKavita, BaseURL: f.URL, Token: "nope"}); err == nil {
		t.Fatal("a wrong API key fails")
	}
}

func TestBookRefreshSkipsVideoServersWithoutABooksLibrary(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/Library/VirtualFolders" {
			fmt.Fprint(w, `[{"Name":"Movies","ItemId":"1","Locations":["/movies"]}]`)
			return
		}
		t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Version: "test"}
	done, err := c.RefreshFolders(context.Background(), Server{Kind: KindJellyfin, BaseURL: srv.URL, Token: "k"}, MediaBook, []string{"/books/A/B"})
	if err != nil || len(done) != 0 {
		t.Fatalf("a book folder outside every library is left alone: %v %v", done, err)
	}
}
