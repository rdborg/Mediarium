package api_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
)

// A torrent release goes to Real-Debrid, which "downloads" it; the finished
// file then comes home over HTTPS and is imported like any other download.
func TestDebridGrabDownloadImport(t *testing.T) {
	movieContent := []byte("Fixture movie bytes from the debrid service. " + strings.Repeat("padding-", 300))
	var mu sync.Mutex
	polls, deleted := 0, false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rd-key" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"bad_token","error_code":8}`)
			return
		}
		fmt.Fprint(w, `{"username":"rduser","type":"premium","expiration":"2027-01-01T00:00:00.000Z"}`)
	})
	mux.HandleFunc("POST /torrents/addMagnet", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"T1"}`)
	})
	var base string
	mux.HandleFunc("GET /torrents/info/T1", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		polls++
		status, links := "downloading", "[]"
		if polls > 2 {
			status, links = "downloaded", `["https://real-debrid.com/d/T1"]`
		}
		mu.Unlock()
		fmt.Fprintf(w, `{"status":%q,"progress":60,"seeders":4,"files":[],"links":%s}`, status, links)
	})
	mux.HandleFunc("POST /unrestrict/link", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"filename":"The.Fixture.Movie.1999.1080p.WEB-DL.x264-FIXTURE.mkv","filesize":%d,"download":%q}`, len(movieContent), base+"/files/movie.mkv")
	})
	// The first link has already run out when it is used: the service is asked
	// for a new one and the download carries on.
	fileHits := 0
	mux.HandleFunc("GET /files/movie.mkv", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fileHits++
		expired := fileHits == 1
		mu.Unlock()
		if expired {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write(movieContent)
	})
	mux.HandleFunc("DELETE /torrents/delete/T1", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deleted = true
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	rd := httptest.NewServer(mux)
	defer rd.Close()
	base = rd.URL
	defer api.SetDebridURLForTest(rd.URL)()
	defer api.SetDebridPollForTest(20 * time.Millisecond)()

	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
	}
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := api.OpenTestDB(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer server.TestWaitBackground()
	httpSrv := httptest.NewServer(server.Routes())
	defer httpSrv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]string{"moviesPath": cfg.MoviesDir, "namingPreset": "minimal"}, http.StatusOK)

	// Test, then save the service. A wrong key is caught by Test.
	bad := postJSON[map[string]any](t, client, httpSrv.URL+"/api/debrid/test", map[string]any{"service": "realdebrid", "apiKey": "wrong-key"}, http.StatusOK)
	if bad["ok"] != false || !strings.Contains(bad["error"].(string), "refused the API key") {
		t.Fatalf("bad key test = %v", bad)
	}
	good := postJSON[map[string]any](t, client, httpSrv.URL+"/api/debrid/test", map[string]any{"service": "realdebrid", "apiKey": "rd-key"}, http.StatusOK)
	if good["ok"] != true {
		t.Fatalf("test = %v", good)
	}
	saved := postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/debrid", map[string]any{"service": "realdebrid", "apiKey": "rd-key"}, http.StatusOK)
	if saved["service"] != "realdebrid" || saved["hasKey"] != true || saved["apiKey"] != nil {
		t.Fatalf("saved = %v", saved)
	}

	// The built-in torrent client can stay off: the service does the torrenting.
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{"torrentEnabled": false}, http.StatusOK)

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", httpSrv.URL, movie.ID), map[string]any{
		"releaseTitle": "The.Fixture.Movie.1999.1080p.WEB-DL.x264-FIXTURE",
		"downloadUrl":  "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=fixture",
		"sizeBytes":    len(movieContent),
		"protocol":     "torrent",
	}, http.StatusAccepted)

	deadline := time.Now().Add(20 * time.Second)
	status := ""
	for time.Now().Before(deadline) {
		q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(q) == 1 {
			status, _ = q[0]["status"].(string)
			if status == "failed" {
				t.Fatalf("failed: %v", q[0])
			}
			if status == "completed" {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "completed" {
		t.Fatalf("not completed in time, status %q", status)
	}
	final := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", httpSrv.URL, movie.ID))
	got, err := os.ReadFile(final["filePath"].(string))
	if err != nil || !bytes.Equal(got, movieContent) {
		t.Fatalf("imported file: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !deleted {
		t.Error("the finished torrent should be removed from the service")
	}
	if fileHits != 2 {
		t.Errorf("the file was fetched %d times, want 2 (an expired link, then a fresh one)", fileHits)
	}
}
