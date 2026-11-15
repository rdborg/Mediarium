package debrid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/speed"
)

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"Movie.2020.1080p/Movie.2020.1080p.mkv": filepath.Join("Movie.2020.1080p", "Movie.2020.1080p.mkv"),
		"../../etc/passwd":                      filepath.Join("etc", "passwd"),
		"/abs/path/file.mkv":                    filepath.Join("abs", "path", "file.mkv"),
		`..\..\win\file.mkv`:                    filepath.Join("win", "file.mkv"),
		"a/./b/../c.mkv":                        filepath.Join("a", "b", "c.mkv"),
		"bad\x00name\n.mkv":                     "badname.mkv",
		"":                                      "download",
		"../..":                                 "download",
	}
	for in, want := range cases {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// fileServer serves one file, honouring Range, and can cut the first answer
// short to test carrying on.
func fileServer(t *testing.T, content string, cutFirst int) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	first := true
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cut := first && cutFirst > 0
		first = false
		mu.Unlock()
		start := 0
		if rg := r.Header.Get("Range"); rg != "" {
			fmt.Sscanf(rg, "bytes=%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content)-1, len(content)))
			w.WriteHeader(http.StatusPartialContent)
		}
		body := content[start:]
		if cut {
			body = body[:cutFirst]
			w.Header().Set("Content-Length", fmt.Sprint(len(content)))
			_, _ = io.WriteString(w, body)
			// Pretend the connection dropped.
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
			}
			return
		}
		_, _ = io.WriteString(w, body)
	}))
}

func TestFetchCarriesOn(t *testing.T) {
	content := strings.Repeat("0123456789", 5000)
	srv := fileServer(t, content, 12000)
	defer srv.Close()
	dir := t.TempDir()
	f := File{Name: "Show/S01/ep1.mkv", Size: int64(len(content)), URL: srv.URL}
	if err := Fetch(context.Background(), srv.Client(), f, dir, func(int64) {}); err == nil {
		t.Fatal("a dropped connection should fail the first try")
	}
	part, err := os.Stat(filepath.Join(dir, "Show", "S01", "ep1.mkv.part"))
	if err != nil || part.Size() == 0 {
		t.Fatalf("the part should be kept: %v", err)
	}
	var last int64
	if err := Fetch(context.Background(), srv.Client(), f, dir, func(n int64) { last = n }); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "Show", "S01", "ep1.mkv"))
	if string(got) != content {
		t.Fatalf("got %d bytes, want %d", len(got), len(content))
	}
	_ = last
	// A finished file isn't fetched again.
	if err := Fetch(context.Background(), srv.Client(), File{Name: f.Name, Size: f.Size, URL: "http://127.0.0.1:1/gone"}, dir, func(int64) {}); err != nil {
		t.Fatalf("a complete file was downloaded again: %v", err)
	}
}

// A service that sends far more than the file's size is stopped one byte
// past it, and the part is thrown away, so it can't fill the disk.
func TestFetchStopsAtTheFileSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := []byte(strings.Repeat("x", 32<<10))
		for i := 0; i < 1024; i++ { // 32 MB, unless the reader stops early
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	f := File{Name: "big.mkv", Size: 1000, URL: srv.URL}
	err := Fetch(context.Background(), srv.Client(), f, dir, func(int64) {})
	if err == nil || !strings.Contains(err.Error(), "more than the file's size") {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "big.mkv.part")); err == nil {
		t.Fatal("the oversized part should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "big.mkv")); err == nil {
		t.Fatal("nothing should be kept under the file's name")
	}
}

// The download speed limit covers files fetched from a debrid service.
func TestFetchKeepsToTheSpeedLimit(t *testing.T) {
	content := strings.Repeat("x", 300_000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, content)
	}))
	defer srv.Close()
	speed.Set(200_000) // bytes per second; the first 4 MB burst is used up below
	defer speed.Set(0)
	// Use up the burst so the limit shows at once.
	_ = speed.Wait(context.Background(), 4<<20)
	start := time.Now()
	if err := Fetch(context.Background(), srv.Client(), File{Name: "a.mkv", Size: int64(len(content)), URL: srv.URL}, t.TempDir(), func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < time.Second {
		t.Fatalf("300 kB at 200 kB/s took %v: the limit was ignored", took)
	}
}

func TestRealDebridFlow(t *testing.T) {
	var mu sync.Mutex
	status, selected, deleted := "magnet_conversion", "", false
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer rd-key" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"bad_token","error_code":8}`)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			fmt.Fprint(w, `{"username":"rduser","type":"premium","expiration":"2027-01-01T00:00:00.000Z"}`)
		}
	})
	mux.HandleFunc("POST /torrents/addMagnet", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) && strings.HasPrefix(r.FormValue("magnet"), "magnet:?xt=") {
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":"ABC","uri":"x"}`)
		}
	})
	mux.HandleFunc("GET /torrents/info/ABC", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		links := []string{}
		if status == "downloaded" {
			links = []string{"https://real-debrid.com/d/one"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "progress": 40, "seeders": 7, "files": []map[string]any{{"id": 1, "path": "/Film.mkv", "bytes": 5, "selected": 0}}, "links": links})
		switch status {
		case "magnet_conversion":
			status = "waiting_files_selection"
		}
	})
	mux.HandleFunc("POST /torrents/selectFiles/ABC", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			mu.Lock()
			selected, status = r.FormValue("files"), "downloaded"
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("POST /unrestrict/link", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			fmt.Fprint(w, `{"filename":"Film.mkv","filesize":5,"download":"https://cdn.example/Film.mkv"}`)
		}
	})
	mux.HandleFunc("DELETE /torrents/delete/ABC", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			mu.Lock()
			deleted = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	bad := &realDebrid{key: "nope", http: srv.Client(), base: srv.URL}
	if _, err := bad.Account(context.Background()); err != ErrAuth {
		t.Fatalf("bad key: %v", err)
	}
	rd := &realDebrid{key: "rd-key", http: srv.Client(), base: srv.URL}
	acc, err := rd.Account(context.Background())
	if err != nil || acc.Name != "rduser" || !acc.Premium || acc.Expires.Year() != 2027 {
		t.Fatalf("account: %+v %v", acc, err)
	}
	id, err := rd.Add(context.Background(), "magnet:?xt=urn:btih:abc", nil)
	if err != nil || id != "ABC" {
		t.Fatalf("add: %q %v", id, err)
	}
	st, _ := rd.Status(context.Background(), id)
	if st.State != Waiting || st.Seeders != 7 {
		t.Fatalf("converting: %+v", st)
	}
	st, _ = rd.Status(context.Background(), id) // waiting_files_selection: picks every file
	if selected != "all" {
		t.Fatalf("selected %q", selected)
	}
	st, _ = rd.Status(context.Background(), id)
	if st.State != Ready || st.Progress != 1 {
		t.Fatalf("done: %+v", st)
	}
	files, err := rd.Files(context.Background(), id)
	if err != nil || len(files) != 1 || files[0].URL != "https://cdn.example/Film.mkv" || files[0].Size != 5 {
		t.Fatalf("files: %+v %v", files, err)
	}
	if err := rd.Remove(context.Background(), id); err != nil || !deleted {
		t.Fatalf("remove: %v", err)
	}
}

func TestTorBoxFlow(t *testing.T) {
	var mu sync.Mutex
	finished, deleted := false, false
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, data any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}
	mux.HandleFunc("GET /user/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tb-key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		ok(w, map[string]any{"email": "me@example.com", "plan": 2, "premium_expires_at": "2027-02-01T00:00:00Z"})
	})
	mux.HandleFunc("POST /torrents/createtorrent", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		if r.FormValue("magnet") == "" || r.FormValue("seed") != "3" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ok(w, map[string]any{"torrent_id": 99, "hash": "h"})
	})
	mux.HandleFunc("GET /torrents/mylist", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Query().Get("id") != "99" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		state := "downloading"
		if finished {
			state = "completed"
		}
		ok(w, map[string]any{"download_state": state, "download_finished": finished, "download_present": finished, "progress": 0.5, "seeds": 3,
			"files": []map[string]any{{"id": 1, "name": "Show/ep1.mkv", "size": 10}, {"id": 2, "name": "Show/ep1.srt", "size": 2}}})
		finished = true
	})
	mux.HandleFunc("GET /torrents/requestdl", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "tb-key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		ok(w, "https://cdn.example/"+r.URL.Query().Get("file_id"))
	})
	mux.HandleFunc("POST /torrents/controltorrent", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["operation"] == "delete" && body["torrent_id"].(float64) == 99 {
			mu.Lock()
			deleted = true
			mu.Unlock()
		}
		ok(w, nil)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tb := &torBox{key: "tb-key", http: srv.Client(), base: srv.URL}
	acc, err := tb.Account(context.Background())
	if err != nil || acc.Name != "me@example.com" || !acc.Premium {
		t.Fatalf("account: %+v %v", acc, err)
	}
	id, err := tb.Add(context.Background(), "magnet:?xt=urn:btih:abc", nil)
	if err != nil || id != "99" {
		t.Fatalf("add: %q %v", id, err)
	}
	if st, _ := tb.Status(context.Background(), id); st.State != Downloading || st.Progress != 0.5 {
		t.Fatalf("downloading: %+v", st)
	}
	if st, _ := tb.Status(context.Background(), id); st.State != Ready {
		t.Fatalf("ready: %+v", st)
	}
	files, err := tb.Files(context.Background(), id)
	if err != nil || len(files) != 2 || files[0].Name != "Show/ep1.mkv" || files[1].URL != "https://cdn.example/2" {
		t.Fatalf("files: %+v %v", files, err)
	}
	// A link that ran out is replaced by asking for just that one file again.
	if l, err := FreshLink(context.Background(), tb, id, files[1]); err != nil || l != "https://cdn.example/2" {
		t.Fatalf("fresh link: %q %v", l, err)
	}
	if _, err := FreshLink(context.Background(), tb, id, File{Name: "Show/gone.mkv"}); err == nil {
		t.Fatal("a file that is no longer on the account got a link")
	}
	if err := tb.Remove(context.Background(), id); err != nil || !deleted {
		t.Fatalf("remove: %v %v", err, deleted)
	}
	if _, err := (&torBox{key: "x", http: srv.Client(), base: srv.URL}).Account(context.Background()); err != ErrAuth {
		t.Fatalf("bad key: %v", err)
	}
}

// What is on the account is listed in the same few states for both services.
func TestListingTheAccount(t *testing.T) {
	rd := http.NewServeMux()
	rd.HandleFunc("GET /torrents", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "A1", "filename": "Show.S01", "bytes": 5000, "progress": 40, "status": "downloading", "speed": 1234, "seeders": 7, "added": "2026-10-09T08:00:00.000Z"},
			{"id": "B2", "filename": "Movie", "bytes": 900, "progress": 100, "status": "downloaded", "added": "2026-10-08T08:00:00.000Z"},
			{"id": "C3", "filename": "Dead", "bytes": 1, "progress": 0, "status": "dead"},
			{"id": "D4", "filename": "Queued", "bytes": 1, "progress": 0, "status": "queued"},
		})
	})
	rsrv := httptest.NewServer(rd)
	defer rsrv.Close()
	got, err := (&realDebrid{key: "k", http: rsrv.Client(), base: rsrv.URL}).List(context.Background())
	if err != nil || len(got) != 4 {
		t.Fatalf("real-debrid: %+v %v", got, err)
	}
	if got[0].State != Downloading || got[0].Progress != 0.4 || got[0].Speed != 1234 || got[0].Seeders != 7 || got[0].Added.IsZero() ||
		got[1].State != Ready || got[1].Progress != 1 || got[2].State != Failed || got[3].State != Waiting || got[3].Seeders != -1 {
		t.Fatalf("real-debrid states: %+v", got)
	}

	tb := http.NewServeMux()
	tb.HandleFunc("GET /torrents/mylist", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]any{
			{"id": 7, "name": "Show", "size": 100, "progress": 0.25, "download_state": "downloading", "download_speed": 99, "seeds": 4, "created_at": "2026-10-09T08:00:00Z"},
			{"id": 8, "name": "Done", "size": 10, "progress": 1, "download_state": "completed", "download_finished": true, "download_present": true},
			{"id": 9, "name": "Bad", "size": 10, "download_state": "error"},
		}})
	})
	tsrv := httptest.NewServer(tb)
	defer tsrv.Close()
	got, err = (&torBox{key: "k", http: tsrv.Client(), base: tsrv.URL}).List(context.Background())
	if err != nil || len(got) != 3 || got[0].ID != "7" || got[0].State != Downloading || got[0].Speed != 99 || got[1].State != Ready || got[2].State != Failed {
		t.Fatalf("torbox: %+v %v", got, err)
	}
}
