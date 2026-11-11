package slskd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake is enough of slskd for the client: a search that completes on the
// second look, a batch endpoint that can be missing (an older slskd), and a
// download list.
type fake struct {
	mu       sync.Mutex
	noBatch  bool
	numState bool
	looks    int
	queued   []Want
	deleted  []string
	// searchTimeout is what the last search asked for; slskd counts it in milliseconds.
	searchTimeout int
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") != "key" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := strings.TrimPrefix(r.URL.Path, "/api/v0")
	switch {
	case p == "/application/version":
		_, _ = w.Write([]byte(`"0.22.5"`))
	case p == "/server":
		_, _ = w.Write([]byte(`{"isConnected":true,"isLoggedIn":true,"state":"Connected, LoggedIn"}`))
	case p == "/searches" && r.Method == http.MethodPost:
		var b struct {
			SearchTimeout int `json:"searchTimeout"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.searchTimeout = b.SearchTimeout
		_, _ = w.Write([]byte(`{"id":"x","isComplete":false}`))
	case strings.HasSuffix(p, "/responses"):
		_, _ = w.Write([]byte(`[{"username":"alice","hasFreeUploadSlot":true,"queueLength":0,"uploadSpeed":900000,"files":[
			{"filename":"Music\\Artist\\Album (2001)\\01 One.flac","size":30000000,"extension":"flac","bitDepth":16,"sampleRate":44100,"length":200}]}]`))
	case strings.HasPrefix(p, "/searches/") && r.Method == http.MethodGet:
		f.looks++
		_, _ = w.Write([]byte(`{"id":"x","isComplete":` + map[bool]string{true: "true", false: "false"}[f.looks >= 2] + `}`))
	case strings.HasPrefix(p, "/searches/") && r.Method == http.MethodDelete:
		f.deleted = append(f.deleted, p)
		w.WriteHeader(http.StatusNoContent)
	case p == "/transfers/downloads/batches":
		if f.noBatch {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var b struct {
			Username string `json:"username"`
			Files    []Want `json:"files"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.queued = append(f.queued, b.Files...)
		w.WriteHeader(http.StatusCreated)
	case p == "/transfers/downloads/alice" && r.Method == http.MethodPost:
		var files []Want
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &files)
		f.queued = append(f.queued, files...)
		w.WriteHeader(http.StatusCreated)
	case p == "/transfers/downloads/alice":
		state := `"Completed, Succeeded"`
		if f.numState {
			state = "4098" // Queued, Remotely
		}
		_, _ = w.Write([]byte(`{"username":"alice","directories":[{"directory":"Album (2001)","files":[{"id":"t1","filename":"Music\\Artist\\Album (2001)\\01 One.flac","size":30000000,"state":` + state + `,"bytesTransferred":30000000}]}]}`))
	case p == "/transfers/downloads/bob":
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestClient(t *testing.T) {
	f := &fake{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	ctx := context.Background()
	c := New(srv.URL+"/", "key")

	if v, err := c.Version(ctx); err != nil || v != "0.22.5" {
		t.Fatalf("Version = %q, %v", v, err)
	}
	if _, err := New(srv.URL, "wrong").Version(ctx); !errors.Is(err, ErrBadKey) {
		t.Errorf("a wrong key: %v", err)
	}

	res, err := c.Search(ctx, "Artist Album", 10*time.Second)
	if err != nil || len(res) != 1 || res[0].Username != "alice" || len(res[0].Files) != 1 || res[0].Files[0].BitDepth != 16 {
		t.Fatalf("Search = %+v, %v", res, err)
	}
	// slskd reads searchTimeout in milliseconds: 10 would end the search at once.
	if f.searchTimeout < 5000 || f.searchTimeout > 10000 {
		t.Errorf("searchTimeout = %d, want about 8000 for a 10 s wait", f.searchTimeout)
	}
	if len(f.deleted) != 0 {
		t.Errorf("the search should stay in slskd's Searches page for now: %v", f.deleted)
	}
	// An hour on, the next search tidies the old one away.
	ownMu.Lock()
	for i := range ownSearches {
		ownSearches[i].at = time.Now().Add(-2 * time.Hour)
	}
	ownMu.Unlock()
	if _, err := c.Search(ctx, "Artist Album", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 1 {
		t.Errorf("the old search wasn't removed from slskd: %v", f.deleted)
	}
	if st, err := c.Server(ctx); err != nil || !st.Online() || st.State != "Connected, LoggedIn" {
		t.Errorf("Server = %+v, %v", st, err)
	}

	want := []Want{{Filename: res[0].Files[0].Filename, Size: res[0].Files[0].Size}}
	if err := c.Enqueue(ctx, "alice", want); err != nil || len(f.queued) != 1 {
		t.Fatalf("Enqueue (batch) = %v, %v", err, f.queued)
	}
	f.noBatch = true
	if err := c.Enqueue(ctx, "alice", want); err != nil || len(f.queued) != 2 {
		t.Fatalf("Enqueue (older slskd) = %v, %v", err, f.queued)
	}

	got, err := c.Downloads(ctx, "alice")
	if err != nil || len(got) != 1 || !got[0].State.Succeeded() {
		t.Fatalf("Downloads = %+v, %v", got, err)
	}
	f.numState = true
	if got, _ := c.Downloads(ctx, "alice"); got[0].State.Done() || got[0].State&StateQueued == 0 {
		t.Errorf("state as a number = %v", got[0].State)
	}
	if got, err := c.Downloads(ctx, "bob"); err != nil || got != nil {
		t.Errorf("no downloads = %v, %v", got, err)
	}

	if BaseName(`Music\Artist\Album\01 One.flac`) != "01 One.flac" || Dir(`Music/Artist/Album/01.flac`) != "Music/Artist/Album" {
		t.Error("path helpers")
	}
}
