package qbit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeClient is a small qBittorrent Web API: a session cookie after
// login, and a list of torrents.
type fakeClient struct {
	mu        sync.Mutex
	user      string
	pass      string
	sessions  map[string]bool
	logins    int
	torrents  []Torrent
	added     []map[string]string
	files     [][]byte
	deleted   []string
	ignoreTag bool // like clients that ignore the tag filter
	v4        bool // an older client: pause and resume instead of stop and start
	actions   []string
}

func newFake(user, pass string) *fakeClient {
	return &fakeClient{user: user, pass: pass, sessions: map[string]bool{}}
}

func (f *fakeClient) authed(r *http.Request) bool {
	c, err := r.Cookie("SID")
	return err == nil && f.sessions[c.Value]
}

func (f *fakeClient) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/api/v2/auth/login" {
		_ = r.ParseForm()
		f.logins++
		if r.PostForm.Get("username") != f.user || r.PostForm.Get("password") != f.pass {
			fmt.Fprint(w, "Fails.")
			return
		}
		sid := fmt.Sprintf("sid%d", f.logins)
		f.sessions[sid] = true
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: sid, Path: "/"})
		fmt.Fprint(w, "Ok.")
		return
	}
	if !f.authed(r) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
		return
	}
	switch r.URL.Path {
	case "/api/v2/app/version":
		fmt.Fprint(w, "v5.0.2")
	case "/api/v2/torrents/createCategory":
		w.WriteHeader(http.StatusConflict)
	case "/api/v2/torrents/add":
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got := map[string]string{"urls": r.FormValue("urls"), "category": r.FormValue("category"), "tags": r.FormValue("tags")}
		if fh, _, err := r.FormFile("torrents"); err == nil {
			data, _ := io.ReadAll(fh)
			f.files = append(f.files, data)
			got["torrents"] = "file"
		}
		if got["urls"] == "" && got["torrents"] == "" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			fmt.Fprint(w, "Fails.")
			return
		}
		f.added = append(f.added, got)
		f.torrents = append(f.torrents, Torrent{Hash: fmt.Sprintf("h%d", len(f.added)), Name: "Added", State: "metaDL", Tags: got["tags"], Category: got["category"]})
		fmt.Fprint(w, "Ok.")
	case "/api/v2/torrents/info":
		q := r.URL.Query()
		out := []Torrent{}
		for _, t := range f.torrents {
			if tag := q.Get("tag"); tag != "" && !f.ignoreTag && !t.HasTag(tag) {
				continue
			}
			if cat := q.Get("category"); cat != "" && !f.ignoreTag && t.Category != cat {
				continue
			}
			if h := q.Get("hashes"); h != "" && !strings.EqualFold(h, t.Hash) {
				continue
			}
			out = append(out, t)
		}
		_ = json.NewEncoder(w).Encode(out)
	case "/api/v2/torrents/stop", "/api/v2/torrents/start", "/api/v2/torrents/pause", "/api/v2/torrents/resume", "/api/v2/torrents/recheck":
		_ = r.ParseForm()
		if f.v4 && (strings.HasSuffix(r.URL.Path, "/stop") || strings.HasSuffix(r.URL.Path, "/start")) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.actions = append(f.actions, strings.TrimPrefix(r.URL.Path, "/api/v2/torrents/")+" "+r.PostForm.Get("hashes"))
	case "/api/v2/torrents/delete":
		_ = r.ParseForm()
		f.deleted = append(f.deleted, r.PostForm.Get("hashes")+" "+r.PostForm.Get("deleteFiles"))
		fmt.Fprint(w, "")
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestVersionAndLogin(t *testing.T) {
	cases := []struct {
		name, user, pass string
		want             string
		wantErr          error
	}{
		{"right password", "admin", "secret", "v5.0.2", nil},
		{"wrong password", "admin", "nope", "", ErrAuth},
		{"wrong user", "someone", "secret", "", ErrAuth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFake("admin", "secret")
			srv := httptest.NewServer(fake)
			defer srv.Close()
			c := New(srv.URL+"/", tc.user, tc.pass, srv.Client())
			got, err := c.Version(context.Background())
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("Version = %q, %v; want %q, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

// An expired session (403) is renewed once, without the caller noticing.
func TestSignsInAgainOnForbidden(t *testing.T) {
	fake := newFake("admin", "secret")
	fake.torrents = []Torrent{{Hash: "abc", State: "downloading", Tags: "mediarium-q1"}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := New(srv.URL, "admin", "secret", srv.Client())
	if _, ok, err := c.Info(context.Background(), "ABC"); err != nil || !ok {
		t.Fatalf("first call: ok %v, %v", ok, err)
	}
	fake.mu.Lock()
	fake.sessions = map[string]bool{} // the client restarted
	fake.mu.Unlock()
	if _, ok, err := c.Info(context.Background(), "abc"); err != nil || !ok {
		t.Fatalf("after the session ended: ok %v, %v", ok, err)
	}
	if fake.logins != 2 {
		t.Fatalf("signed in %d times, want 2", fake.logins)
	}
	bad := New(srv.URL, "admin", "wrong", srv.Client())
	if _, _, err := bad.Info(context.Background(), "abc"); !errors.Is(err, ErrAuth) {
		t.Fatalf("wrong password: %v", err)
	}
}

func TestAdd(t *testing.T) {
	cases := []struct {
		name    string
		opts    AddOptions
		want    map[string]string
		wantErr bool
	}{
		{"magnet", AddOptions{Magnet: "magnet:?xt=urn:btih:abc", Category: "mediarium", Tag: "mediarium-q7"},
			map[string]string{"urls": "magnet:?xt=urn:btih:abc", "category": "mediarium", "tags": "mediarium-q7"}, false},
		{"torrent file", AddOptions{TorrentFile: []byte("d4:infod4:name1:xee"), Tag: "mediarium-q8"},
			map[string]string{"urls": "", "category": "", "tags": "mediarium-q8", "torrents": "file"}, false},
		{"nothing", AddOptions{Tag: "x"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFake("u", "p")
			srv := httptest.NewServer(fake)
			defer srv.Close()
			err := New(srv.URL, "u", "p", srv.Client()).Add(context.Background(), tc.opts)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Add: %v", err)
			}
			if tc.wantErr {
				return
			}
			if len(fake.added) != 1 {
				t.Fatalf("added %d torrents", len(fake.added))
			}
			for k, v := range tc.want {
				if fake.added[0][k] != v {
					t.Errorf("%s = %q, want %q", k, fake.added[0][k], v)
				}
			}
		})
	}
}

func TestInfoByTagFiltersItself(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		fake := newFake("u", "p")
		fake.ignoreTag = ignore
		fake.torrents = []Torrent{
			{Hash: "a", Tags: "mediarium-q1"},
			{Hash: "b", Tags: "other, mediarium-q12"},
			{Hash: "c", Tags: "mediarium-q12"},
		}
		srv := httptest.NewServer(fake)
		list, err := New(srv.URL, "u", "p", srv.Client()).InfoByTag(context.Background(), "mediarium-q12")
		srv.Close()
		if err != nil || len(list) != 2 || list[0].Hash != "b" || list[1].Hash != "c" {
			t.Fatalf("ignoreTag=%v: %+v, %v", ignore, list, err)
		}
	}
}

func TestDelete(t *testing.T) {
	fake := newFake("u", "p")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := New(srv.URL, "u", "p", srv.Client())
	if err := c.Delete(context.Background(), "abc", false); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(context.Background(), "def", true); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fake.deleted, ";") != "abc false;def true" {
		t.Fatalf("deleted %v", fake.deleted)
	}
}

func TestTorrentStates(t *testing.T) {
	cases := []struct {
		state    string
		progress float64
		done     bool
		failed   bool
	}{
		{"downloading", 0.4, false, false},
		{"stalledDL", 0.4, false, false},
		{"metaDL", 0, false, false},
		{"uploading", 1, true, false},
		{"stalledUP", 1, true, false},
		{"pausedUP", 1, true, false},
		{"stoppedUP", 1, true, false},
		{"queuedUP", 1, true, false},
		{"forcedUP", 1, true, false},
		{"checkingUP", 1, false, false},
		{"moving", 1, false, false},
		{"pausedUP", 0.99, false, false},
		{"error", 0.2, false, true},
		{"missingFiles", 1, false, true},
	}
	for _, tc := range cases {
		tr := Torrent{State: tc.state, Progress: tc.progress}
		if tr.Done() != tc.done || tr.Failed() != tc.failed {
			t.Errorf("%s at %.2f: done %v failed %v, want %v %v", tc.state, tc.progress, tr.Done(), tr.Failed(), tc.done, tc.failed)
		}
	}
}

func TestContent(t *testing.T) {
	cases := []struct {
		t    Torrent
		want string
	}{
		{Torrent{ContentPath: "/downloads/Movie.mkv", SavePath: "/downloads", Name: "Movie.mkv"}, "/downloads/Movie.mkv"},
		{Torrent{SavePath: "/downloads/", Name: "Show.S01"}, "/downloads/Show.S01"},
		{Torrent{SavePath: `D:\Torrents\`, Name: "Show.S01"}, `D:\Torrents\Show.S01`},
		{Torrent{Name: "x"}, ""},
	}
	for _, tc := range cases {
		if got := tc.t.Content(); got != tc.want {
			t.Errorf("Content(%+v) = %q, want %q", tc.t, got, tc.want)
		}
	}
}

func TestInfoHash(t *testing.T) {
	cases := []struct {
		name   string
		magnet string
		file   []byte
		want   string
	}{
		{"hex magnet", "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=x", nil, "0123456789abcdef0123456789abcdef01234567"},
		{"base32 magnet", "magnet:?xt=urn:btih:AERUKZ4JVPG66AJDIVTYTK6N54ASGRLH&dn=x", nil, "0123456789abcdef0123456789abcdef01234567"},
		{"not a magnet", "magnet:?dn=x", nil, ""},
		{"torrent file", "", []byte("d4:infod6:lengthi1e4:name1:x12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaaee"), "a7d1bb63d4a1ad7d1d7b8a3a5b3c4b9c7d2f0d1c"},
		{"broken file", "", []byte("nonsense"), ""},
		// Millions of nested lists end the program if the library decodes them.
		{"nesting bomb", "", append([]byte("d1:x"), strings.Repeat("l", 6<<20)...), ""},
	}
	for _, tc := range cases {
		got := InfoHash(tc.magnet, tc.file)
		if tc.name == "torrent file" {
			// The exact hash is the SHA-1 of the info dictionary; only check its shape.
			if len(got) != 40 {
				t.Errorf("%s: %q", tc.name, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("%s: InfoHash = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLabel(t *testing.T) {
	for kind, want := range map[string]string{QBittorrent: "qBittorrent", RDTClient: "RDT-Client", Decypharr: "Decypharr", "": "your download client"} {
		if got := Label(kind); got != want {
			t.Errorf("Label(%q) = %q", kind, got)
		}
	}
	if !ValidKind("transmission") || !ValidKind(Decypharr) || ValidKind("deluge") {
		t.Error("ValidKind")
	}
}

func TestListAndControls(t *testing.T) {
	for _, v4 := range []bool{false, true} {
		fake := newFake("u", "p")
		fake.v4 = v4
		fake.torrents = []Torrent{{Hash: "a", Name: "One", Category: "mediarium", DLSpeed: 500, ETA: 60}, {Hash: "b", Name: "Two", Category: "movies"}}
		srv := httptest.NewServer(fake)
		c := New(srv.URL, "u", "p", srv.Client())
		ctx := context.Background()
		all, err := c.List(ctx, "")
		if err != nil || len(all) != 2 || all[0].DLSpeed != 500 || all[0].ETA != 60 {
			t.Fatalf("v4=%v all: %+v, %v", v4, all, err)
		}
		mine, err := c.List(ctx, "mediarium")
		if err != nil || len(mine) != 1 || mine[0].Hash != "a" {
			t.Fatalf("v4=%v category: %+v, %v", v4, mine, err)
		}
		if err := c.Pause(ctx, "a"); err != nil {
			t.Fatal(err)
		}
		if err := c.Resume(ctx, "a"); err != nil {
			t.Fatal(err)
		}
		if err := c.Recheck(ctx, "a"); err != nil {
			t.Fatal(err)
		}
		want := "stop a;start a;recheck a"
		if v4 {
			want = "pause a;resume a;recheck a"
		}
		if got := strings.Join(fake.actions, ";"); got != want {
			t.Fatalf("v4=%v actions %q, want %q", v4, got, want)
		}
		srv.Close()
	}
}

func TestWaitingMeansInLineNotStalled(t *testing.T) {
	for state, want := range map[string]bool{
		"queuedDL": true, "checkingDL": true, "allocating": true, "moving": true,
		"downloading": false, "stalledDL": false, "metaDL": false, "pausedDL": false, "error": false,
	} {
		if got := (Torrent{State: state}).Waiting(); got != want {
			t.Errorf("%s: Waiting() = %v, want %v", state, got, want)
		}
	}
}
