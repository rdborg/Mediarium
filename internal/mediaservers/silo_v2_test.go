package mediaservers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeSiloGens answers like a Silo build of one API generation: "v2" is a
// current build (every /api/v1 call is 410 Gone, the native API is under
// /api/v2 with string library ids), "v1" is an old build that has no /api/v2
// (404).
type fakeSiloGens struct {
	*httptest.Server
	mu    sync.Mutex
	hits  []string
	scans []string
}

func (f *fakeSiloGens) hitsOf(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, h := range f.hits {
		if strings.HasPrefix(h, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeSiloGens) scanned() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.scans...)
}

func newFakeSiloGens(t *testing.T, gen string) *fakeSiloGens {
	t.Helper()
	f := &fakeSiloGens{}
	h := func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.URL.Path)
		f.mu.Unlock()
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/api/v1/") && gen == "v2":
			w.WriteHeader(http.StatusGone)
			fmt.Fprint(w, `{"error":"gone","message":"API v1 has been retired"}`)
			return
		case strings.HasPrefix(path, "/api/v2/") && gen == "v1":
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sa_admin" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"unauthorized"}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && path == "/api/v2/admin/system/build":
			fmt.Fprint(w, `{"display":"build-2210","revision":"abc","dirty":false,"build_number":2210,"available":true}`)
		case r.Method == http.MethodGet && path == "/api/v2/libraries":
			fmt.Fprint(w, `{"items":[{"id":"1","name":"Movies","type":"movies","paths":["/mnt/media/Movies"],"enabled":true},{"id":"2","name":"TV","type":"series","paths":["/mnt/media/TV"],"enabled":true},{"id":"3","name":"Off","type":"movies","paths":["/mnt/media/Off"],"enabled":false}],"page":{"has_more":false}}`)
		case r.Method == http.MethodPost && path == "/api/v2/scan":
			var req struct {
				LibraryID string `json:"library_id"`
				Path      string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			f.mu.Lock()
			if req.LibraryID != "" {
				f.scans = append(f.scans, "library:"+req.LibraryID)
			} else {
				f.scans = append(f.scans, "path:"+req.Path)
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"status":"accepted","mode":"library","library_id":"1"}`)
		case r.Method == http.MethodGet && path == "/api/v1/admin/system/build":
			fmt.Fprint(w, `{"display":"74158b4a","build_number":1144,"available":true}`)
		case r.Method == http.MethodGet && path == "/api/v1/libraries/":
			fmt.Fprint(w, `[{"id":1,"paths":["/mnt/media/Movies"],"type":"movie","name":"Movies","enabled":true},{"id":2,"paths":["/mnt/media/TV"],"type":"tv","name":"TV","enabled":true}]`)
		case r.Method == http.MethodPost && path == "/api/v1/scan":
			var req struct {
				LibraryID int    `json:"library_id"`
				Path      string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			if req.LibraryID > 0 {
				f.scans = append(f.scans, fmt.Sprintf("library:%d", req.LibraryID))
			} else {
				f.scans = append(f.scans, "path:"+req.Path)
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"status":"accepted"}`)
		default:
			http.NotFound(w, r)
		}
	}
	f.Server = httptest.NewServer(http.HandlerFunc(h))
	t.Cleanup(f.Close)
	return f
}

func TestSiloCurrentBuildUsesV2(t *testing.T) {
	f := newFakeSiloGens(t, "v2")
	c := &Client{HTTP: f.Client(), Version: "test"}
	s := Server{Kind: KindSilo, BaseURL: f.URL, Token: "sa_admin", PathMap: []PathMapping{{From: "/movies", To: "/mnt/media/Movies"}}}

	res, err := c.Test(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "build 2210" || len(res.Libraries) != 3 || res.Libraries[1].ID != "2" || res.Libraries[1].Locations[0] != "/mnt/media/TV" {
		t.Fatalf("result = %+v", res)
	}

	// Refresh now: the disabled library is skipped, ids go out as strings.
	if err := c.RefreshAll(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if got := f.scanned(); len(got) != 2 || got[0] != "library:1" || got[1] != "library:2" {
		t.Fatalf("scans = %v", got)
	}
	// One folder, and a folder outside every library (whole-library fallback).
	if _, err := c.RefreshFolders(context.Background(), s, MediaMovie, []string{"/movies/Film (2008)"}); err != nil {
		t.Fatal(err)
	}
	if got := f.scanned(); got[len(got)-1] != "path:/mnt/media/Movies/Film (2008)" {
		t.Fatalf("scans = %v", got)
	}
	if _, err := c.RefreshFolders(context.Background(), s, MediaTV, []string{"/elsewhere/Show"}); err != nil {
		t.Fatal(err)
	}
	if got := f.scanned(); len(got) != 5 {
		t.Fatalf("scans = %v", got)
	}
}

func TestSiloOldBuildFallsBackToV1(t *testing.T) {
	f := newFakeSiloGens(t, "v1")
	c := &Client{HTTP: f.Client(), Version: "test"}
	s := Server{Kind: KindSilo, BaseURL: f.URL, Token: "sa_admin"}

	res, err := c.Test(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "build 1144" || len(res.Libraries) != 2 || res.Libraries[0].ID != "1" {
		t.Fatalf("result = %+v", res)
	}
	if err := c.RefreshAll(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if got := f.scanned(); len(got) != 2 || got[0] != "library:1" {
		t.Fatalf("scans = %v", got)
	}
}

func TestSiloRemembersWhichAPIAnswered(t *testing.T) {
	f := newFakeSiloGens(t, "v1")
	c := &Client{HTTP: f.Client(), Version: "test"}
	s := Server{Kind: KindSilo, BaseURL: f.URL, Token: "sa_admin"}
	if _, err := c.Test(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if f.hitsOf("/api/v2/") != 1 {
		t.Fatalf("the first test should try v2 once, got %d", f.hitsOf("/api/v2/"))
	}
	// The library list inside the same test already skipped v2, and so does
	// everything after it.
	if err := c.RefreshAll(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if f.hitsOf("/api/v2/") != 1 {
		t.Fatalf("v2 was tried again after v1 was remembered: %d", f.hitsOf("/api/v2/"))
	}

	// A server that is updated to a current build is found again: the
	// remembered v1 answers 410, so v2 is tried and remembered.
	g := newFakeSiloGens(t, "v2")
	c2 := &Client{HTTP: g.Client(), Version: "test"}
	s2 := Server{Kind: KindSilo, BaseURL: g.URL, Token: "sa_admin"}
	c2.siloAPI.Store(s2.BaseURL, siloV1)
	if _, err := c2.Test(context.Background(), s2); err != nil {
		t.Fatal(err)
	}
	if v, _ := c2.siloAPI.Load(s2.BaseURL); v != siloV2 {
		t.Fatalf("remembered = %v", v)
	}
	before := g.hitsOf("/api/v1/")
	if err := c2.RefreshAll(context.Background(), s2); err != nil {
		t.Fatal(err)
	}
	if g.hitsOf("/api/v1/") != before {
		t.Fatal("v1 was tried again after v2 was remembered")
	}
}

func TestSiloRejectedKeyOnV2(t *testing.T) {
	for _, gen := range []string{"v2", "v1"} {
		f := newFakeSiloGens(t, gen)
		c := &Client{HTTP: f.Client(), Version: "test"}
		_, err := c.Test(context.Background(), Server{Kind: KindSilo, BaseURL: f.URL, Token: "sa_wrong"})
		if err == nil || !strings.Contains(err.Error(), "refused the API key") {
			t.Fatalf("%s: a wrong key: %v", gen, err)
		}
	}
	// A 401 on v2 is final: the old API is not tried, it would only say 410.
	f := newFakeSiloGens(t, "v2")
	c := &Client{HTTP: f.Client(), Version: "test"}
	_, _ = c.Test(context.Background(), Server{Kind: KindSilo, BaseURL: f.URL, Token: "sa_wrong"})
	if f.hitsOf("/api/v1/") != 0 {
		t.Fatal("v1 was tried after v2 said 401")
	}
}

func TestSiloFindOnACurrentBuildIsQuiet(t *testing.T) {
	f := newFakeSiloGens(t, "v2")
	c := &Client{HTTP: f.Client(), Version: "test"}
	_, found, err := c.Find(context.Background(), Server{Kind: KindSilo, BaseURL: f.URL, Token: "sa_admin"}, MediaMovie, 10378)
	if err != nil || found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
}
