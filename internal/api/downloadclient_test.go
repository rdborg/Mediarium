package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
)

const fixtureHash = "0123456789abcdef0123456789abcdef01234567"

// fakeQbit is enough of the qBittorrent Web API for the pipeline: one
// torrent, which downloads in two polls and then seeds.
type fakeQbit struct {
	mu      sync.Mutex
	content string // content_path as the client reports it
	tags    []string
	cats    []string
	polls   int
	deleted []string
	// extra are torrents that are in the client already, listed as they are; actions are the controls pressed.
	extra   []map[string]any
	actions []string
	// state, when set, is the state the torrent reports instead of downloading.
	state string
	// adds records the share limit fields of every add call ("-" when a field
	// was not sent); limitCalls records every setShareLimits call.
	adds       []map[string]string
	limitCalls []map[string]string
	// hold keeps the torrent half way down for ever; failDelete makes the
	// delete call fail; infos counts the status checks.
	hold       bool
	failDelete bool
	infos      int
}

// formField is a multipart or url-encoded form value, or "-" when it is absent.
func formField(r *http.Request, name string) string {
	if r.MultipartForm != nil {
		if v, ok := r.MultipartForm.Value[name]; ok && len(v) > 0 {
			return v[0]
		}
		return "-"
	}
	if v, ok := r.PostForm[name]; ok && len(v) > 0 {
		return v[0]
	}
	return "-"
}

func (f *fakeQbit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/api/v2/auth/login" {
		_ = r.ParseForm()
		if r.PostForm.Get("username") != "admin" || r.PostForm.Get("password") != "adminpass" {
			fmt.Fprint(w, "Fails.")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "ok", Path: "/"})
		fmt.Fprint(w, "Ok.")
		return
	}
	if c, err := r.Cookie("SID"); err != nil || c.Value != "ok" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/api/v2/app/version":
		fmt.Fprint(w, "v5.0.2")
	case "/api/v2/torrents/createCategory":
	case "/api/v2/torrents/add":
		_ = r.ParseMultipartForm(1 << 20)
		f.tags = append(f.tags, r.FormValue("tags"))
		f.cats = append(f.cats, r.FormValue("category"))
		f.adds = append(f.adds, map[string]string{"ratioLimit": formField(r, "ratioLimit"), "seedingTimeLimit": formField(r, "seedingTimeLimit")})
		fmt.Fprint(w, "Ok.")
	case "/api/v2/torrents/setShareLimits":
		_ = r.ParseForm()
		f.limitCalls = append(f.limitCalls, map[string]string{
			"hashes": formField(r, "hashes"), "ratioLimit": formField(r, "ratioLimit"), "seedingTimeLimit": formField(r, "seedingTimeLimit"),
		})
	case "/api/v2/torrents/info":
		f.infos++
		list := []map[string]any{}
		if len(f.tags) > 0 && len(f.deleted) == 0 {
			f.polls++
			state, progress := "downloading", 0.5
			if f.polls > 2 && !f.hold {
				state, progress = "stalledUP", 1.0
			}
			if f.state != "" {
				state = f.state
			}
			list = append(list, map[string]any{"hash": fixtureHash, "name": "x", "progress": progress, "state": state, "content_path": f.content, "tags": f.tags[0]})
		}
		list = append(list, f.extra...)
		_ = json.NewEncoder(w).Encode(list)
	case "/api/v2/torrents/stop", "/api/v2/torrents/start", "/api/v2/torrents/recheck":
		_ = r.ParseForm()
		f.actions = append(f.actions, strings.TrimPrefix(r.URL.Path, "/api/v2/torrents/")+" "+r.PostForm.Get("hashes"))
	case "/api/v2/torrents/delete":
		if f.failDelete {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = r.ParseForm()
		f.deleted = append(f.deleted, r.PostForm.Get("hashes")+" "+r.PostForm.Get("deleteFiles"))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type clientEnv struct {
	dir    string
	server *api.Server
	url    string
	client *http.Client
	qb     *fakeQbit
	qbURL  string
}

func newClientEnv(t *testing.T) *clientEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DBPath:              filepath.Join(dir, "config", "app.db"),
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
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := httptest.NewServer(server.Routes())
	qb := &fakeQbit{}
	qbSrv := httptest.NewServer(qb)
	t.Cleanup(func() {
		server.TestWaitBackground()
		httpSrv.Close()
		qbSrv.Close()
	})
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{"moviesPath": cfg.MoviesDir, "namingPreset": "minimal", "torrentEnabled": false}, http.StatusOK)
	return &clientEnv{dir: dir, server: server, url: httpSrv.URL, client: client, qb: qb, qbURL: qbSrv.URL}
}

// grab sends a torrent release for a new movie and waits for it to finish.
func (e *clientEnv) grab(t *testing.T) (queueItem, movie map[string]any) {
	t.Helper()
	m, err := e.server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/movies/%d/grab", e.url, m.ID), map[string]any{
		"releaseTitle": "The.Fixture.Movie.1999.1080p.WEB-DL.x264-FIXTURE",
		"downloadUrl":  "magnet:?xt=urn:btih:" + fixtureHash + "&dn=fixture",
		"sizeBytes":    1000,
		"protocol":     "torrent",
	}, http.StatusAccepted)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		q := getJSON[[]map[string]any](t, e.client, e.url+"/api/queue")
		if len(q) == 1 && (q[0]["status"] == "completed" || q[0]["status"] == "failed") {
			e.server.TestWaitBackground()
			return q[0], getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/movies/%d", e.url, m.ID))
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("the download didn't finish in time")
	return nil, nil
}

func (e *clientEnv) save(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	body["kind"] = "qbittorrent"
	body["url"] = e.qbURL
	body["username"] = "admin"
	body["password"] = "adminpass"
	return postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.url+"/api/download-client", body, http.StatusOK)
}

func writeClientFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

var movieBytes = "Fixture movie from the download client. " + strings.Repeat("padding-", 300)

// A torrent folder in the client, seen by Mediarium under another path: the
// movie is imported from it and the client's files stay where they are.
func TestDownloadClientFolderTorrent(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newClientEnv(t)
	local := filepath.Join(e.dir, "client-downloads")
	folder := filepath.Join(local, "The.Fixture.Movie.1999")
	writeClientFile(t, filepath.Join(folder, "The.Fixture.Movie.1999.mkv"), movieBytes)
	writeClientFile(t, filepath.Join(folder, "Sample", "sample.mkv"), "sample")
	writeClientFile(t, filepath.Join(folder, "The.Fixture.Movie.1999.en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nHello\n")
	e.qb.content = "/downloads/The.Fixture.Movie.1999"
	e.save(t, map[string]any{"remotePath": "/downloads", "localPath": local, "removeAfterImport": true, "category": "films"})

	item, movie := e.grab(t)
	if item["status"] != "completed" {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
	got, err := os.ReadFile(movie["filePath"].(string))
	if err != nil || string(got) != movieBytes {
		t.Fatalf("imported file: %v", err)
	}
	if info, _ := os.Lstat(movie["filePath"].(string)); !info.Mode().IsRegular() {
		t.Fatal("a real file is imported as a real file")
	}
	// The client's files are untouched and the download's own folder is gone.
	for _, f := range []string{"The.Fixture.Movie.1999.mkv", "Sample/sample.mkv", "The.Fixture.Movie.1999.en.srt"} {
		if _, err := os.Stat(filepath.Join(folder, f)); err != nil {
			t.Errorf("the client's %s is gone: %v", f, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(e.dir, "downloads", "incomplete")); len(entries) != 0 {
		t.Errorf("the download's folder should be cleaned up, found %v", entries)
	}
	srt := strings.TrimSuffix(movie["filePath"].(string), filepath.Ext(movie["filePath"].(string))) + ".en.srt"
	if _, err := os.Stat(srt); err != nil {
		t.Errorf("the subtitle that came with it should be imported: %v", err)
	}
	e.qb.mu.Lock()
	defer e.qb.mu.Unlock()
	if len(e.qb.tags) != 1 || !strings.HasPrefix(e.qb.tags[0], "mediarium-q") || e.qb.cats[0] != "films" {
		t.Errorf("added with tags %v, categories %v", e.qb.tags, e.qb.cats)
	}
	if len(e.qb.deleted) != 1 || e.qb.deleted[0] != fixtureHash+" true" {
		t.Errorf("removed from the client after import: %v", e.qb.deleted)
	}
}

// A single-file torrent saved next to other files: only its own file counts,
// even when a bigger video sits beside it.
func TestDownloadClientSingleFileTorrent(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newClientEnv(t)
	save := filepath.Join(e.dir, "client-downloads")
	writeClientFile(t, filepath.Join(save, "Someone.Elses.Movie.2020.mkv"), strings.Repeat("bigger ", 2000))
	writeClientFile(t, filepath.Join(save, "The.Fixture.Movie.1999.mkv"), movieBytes)
	e.qb.content = filepath.Join(save, "The.Fixture.Movie.1999.mkv")
	e.save(t, map[string]any{})

	item, movie := e.grab(t)
	if item["status"] != "completed" {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
	got, _ := os.ReadFile(movie["filePath"].(string))
	if string(got) != movieBytes {
		t.Fatal("the wrong file was imported")
	}
	if _, err := os.Stat(filepath.Join(save, "Someone.Elses.Movie.2020.mkv")); err != nil {
		t.Fatal("the other file must stay")
	}
	e.qb.mu.Lock()
	defer e.qb.mu.Unlock()
	if len(e.qb.deleted) != 0 {
		t.Errorf("the torrent must keep seeding: %v", e.qb.deleted)
	}
}

// The client's own trouble (a full or read-only disk) is not the release's
// fault: the release is not blocklisted and the torrent is left in the client.
func TestDownloadClientErrorStateKeepsTheTorrent(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newClientEnv(t)
	e.qb.state = "error"
	e.save(t, map[string]any{})
	item, _ := e.grab(t)
	if item["status"] != "failed" || !strings.Contains(fmt.Sprint(item["error"]), "reports a problem") {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
	if len(e.qb.deleted) != 0 {
		t.Errorf("the torrent must stay in the client: %v", e.qb.deleted)
	}
	if list, err := e.server.Blocklist.List(); err != nil || len(list) != 0 {
		t.Errorf("the release must not be blocklisted: %v, %v", list, err)
	}
}

// A path the client reports that Mediarium can't see fails with a hint about
// the path mapping.
func TestDownloadClientUnseenPath(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newClientEnv(t)
	e.qb.content = "/somewhere/only/the/client/sees"
	e.save(t, map[string]any{})
	item, _ := e.grab(t)
	if item["status"] != "failed" || !strings.Contains(fmt.Sprint(item["error"]), "path mapping") {
		t.Fatalf("status %v: %v", item["status"], item["error"])
	}
}

// Decypharr hands over symlinks into its cloud mount: refused until the
// mount is set as the folder links may point into, then imported as links.
func TestDownloadClientSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	for _, allowed := range []bool{false, true} {
		t.Run(fmt.Sprintf("allowed=%v", allowed), func(t *testing.T) {
			e := newClientEnv(t)
			mount := filepath.Join(e.dir, "mnt", "remote")
			onMount := filepath.Join(mount, "__all__", "The.Fixture.Movie.1999", "The.Fixture.Movie.1999.mkv")
			writeClientFile(t, onMount, movieBytes)
			folder := filepath.Join(e.dir, "symlinks", "The.Fixture.Movie.1999")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(onMount, filepath.Join(folder, "The.Fixture.Movie.1999.mkv")); err != nil {
				t.Skip("cannot make a symlink here")
			}
			e.qb.content = folder
			body := map[string]any{"removeAfterImport": true}
			if allowed {
				body["linkRoot"] = mount
			}
			e.save(t, body)

			item, movie := e.grab(t)
			if !allowed {
				if item["status"] != "failed" || !strings.Contains(fmt.Sprint(item["error"]), "Symlinks may point into") {
					t.Fatalf("status %v: %v", item["status"], item["error"])
				}
				return
			}
			if item["status"] != "completed" {
				t.Fatalf("status %v: %v", item["status"], item["error"])
			}
			dest := movie["filePath"].(string)
			if target, err := os.Readlink(dest); err != nil || target != onMount {
				t.Fatalf("the library should get a link to %s: %q, %v", onMount, target, err)
			}
			if _, err := os.Lstat(filepath.Join(folder, "The.Fixture.Movie.1999.mkv")); err != nil {
				t.Fatal("the client's link must stay")
			}
			e.qb.mu.Lock()
			defer e.qb.mu.Unlock()
			if len(e.qb.deleted) != 1 || e.qb.deleted[0] != fixtureHash+" false" {
				t.Errorf("links in the library still need the client's files: %v", e.qb.deleted)
			}
		})
	}
}

// Settings: Test, save, the password never comes back, validation in plain
// words, and one place for torrents (a client turns the debrid service off
// and the other way round).
func TestDownloadClientSettings(t *testing.T) {
	e := newClientEnv(t)
	bad := postJSON[map[string]any](t, e.client, e.url+"/api/download-client/test", map[string]any{"kind": "qbittorrent", "url": e.qbURL, "username": "admin", "password": "wrong"}, http.StatusOK)
	if bad["ok"] != false || !strings.Contains(fmt.Sprint(bad["error"]), "refused the user name or password") {
		t.Fatalf("wrong password test = %v", bad)
	}
	good := postJSON[map[string]any](t, e.client, e.url+"/api/download-client/test", map[string]any{"kind": "qbittorrent", "url": e.qbURL, "username": "admin", "password": "adminpass"}, http.StatusOK)
	if good["ok"] != true || good["version"] != "v5.0.2" {
		t.Fatalf("test = %v", good)
	}

	for _, tc := range []struct {
		body map[string]any
		want string
	}{
		{map[string]any{"kind": "deluge", "url": e.qbURL}, "Choose qBittorrent"},
		{map[string]any{"kind": "qbittorrent", "url": ""}, "Add the address"},
		{map[string]any{"kind": "qbittorrent", "url": "ftp://x"}, "http://"},
		{map[string]any{"kind": "qbittorrent", "url": e.qbURL, "remotePath": "/downloads"}, "Fill in both folders"},
		{map[string]any{"kind": "qbittorrent", "url": e.qbURL, "remotePath": "/downloads", "localPath": "data"}, "isn't complete"},
		{map[string]any{"kind": "qbittorrent", "url": e.qbURL, "category": "a//b"}, "category"},
		{map[string]any{"kind": "qbittorrent", "url": e.qbURL, "categoryMovies": "/films"}, "The movies category"},
		{map[string]any{"kind": "qbittorrent", "url": e.qbURL, "categoryTv": `a\b`}, "The TV shows category"},
		{map[string]any{"kind": "qbittorrent", "url": e.qbURL, "categoryMusic": strings.Repeat("m", 101)}, "The music category"},
		{map[string]any{"kind": "qbittorrent", "url": e.qbURL, "categoryBooks": "books/"}, "The books category"},
		{map[string]any{"kind": "decypharr", "url": e.qbURL, "linkRoot": "mnt"}, "isn't complete"},
	} {
		got := postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.url+"/api/download-client", tc.body, http.StatusBadRequest)
		if !strings.Contains(fmt.Sprint(got["error"]), tc.want) {
			t.Errorf("%v: %v, want %q", tc.body, got, tc.want)
		}
	}

	// A debrid service first; saving the client turns it off.
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.url+"/api/debrid", map[string]any{"service": "torbox", "apiKey": "tb-key"}, http.StatusOK)
	saved := e.save(t, map[string]any{"category": ""})
	if saved["kind"] != "qbittorrent" || saved["hasPassword"] != true || saved["password"] != nil || saved["category"] != "" {
		t.Fatalf("saved = %v", saved)
	}
	raw, _ := json.Marshal(getJSON[map[string]any](t, e.client, e.url+"/api/download-client"))
	if bytes.Contains(raw, []byte("adminpass")) {
		t.Fatal("the password must never be sent back")
	}
	if d := getJSON[map[string]any](t, e.client, e.url+"/api/debrid"); d["service"] != "" || d["hasKey"] != false {
		t.Fatalf("debrid should be off now: %v", d)
	}
	// The saved password is used by Test when none is typed.
	if r := postJSON[map[string]any](t, e.client, e.url+"/api/download-client/test", map[string]any{"kind": "qbittorrent", "url": e.qbURL, "username": "admin"}, http.StatusOK); r["ok"] != true {
		t.Fatalf("test with the saved password = %v", r)
	}
	// Torrent releases can be grabbed with the built-in client off.
	if s := getJSON[map[string]any](t, e.client, e.url+"/api/settings"); s["torrentEnabled"] != false {
		t.Fatalf("built-in client should still be off: %v", s["torrentEnabled"])
	}

	// A debrid service again: the client goes off.
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.url+"/api/debrid", map[string]any{"service": "torbox", "apiKey": "tb-key"}, http.StatusOK)
	if c := getJSON[map[string]any](t, e.client, e.url+"/api/download-client"); c["kind"] != "" {
		t.Fatalf("the download client should be off now: %v", c)
	}
}

// A category per kind: a movie is added under the movies category, a torrent
// added earlier under the main category is still listed as Mediarium's, and
// leaving them empty changes nothing.
func TestDownloadClientCategoryPerKind(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newClientEnv(t)
	local := filepath.Join(e.dir, "client-downloads")
	e.qb.content = "/downloads/Fixture.Movie"
	writeClientFile(t, filepath.Join(local, "Fixture.Movie", "movie.mkv"), movieBytes)
	e.qb.extra = []map[string]any{
		{"hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "name": "Added.Before", "progress": 0.4, "state": "downloading", "category": "reads", "added_on": 1},
		{"hash": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "name": "Other.Film", "progress": 0.4, "state": "downloading", "category": "films/new", "added_on": 2},
		{"hash": "cccccccccccccccccccccccccccccccccccccccc", "name": "Not.Mine", "progress": 1.0, "state": "stalledUP", "category": "linux", "added_on": 3},
	}
	saved := e.save(t, map[string]any{"categoryMovies": " films/new ", "categoryBooks": "reads", "remotePath": "/downloads", "localPath": local})
	if saved["category"] != "" || saved["categoryMovies"] != "films/new" || saved["categoryBooks"] != "reads" || saved["categoryTv"] != "" {
		t.Fatalf("saved = %v", saved)
	}
	if item, _ := e.grab(t); item["status"] != "completed" {
		t.Fatalf("grab = %v", item)
	}
	e.qb.mu.Lock()
	cats := append([]string(nil), e.qb.cats...)
	e.qb.mu.Unlock()
	if len(cats) != 1 || cats[0] != "films/new" {
		t.Fatalf("the movie should be added under its own category, got %v", cats)
	}
	list := getJSON[map[string]any](t, e.client, e.url+"/api/download-client/torrents")["torrents"].([]any)
	names := map[string]bool{}
	for _, it := range list {
		names[it.(map[string]any)["name"].(string)] = true
	}
	if len(list) < 2 || !names["Added.Before"] || !names["Other.Film"] || names["Not.Mine"] {
		t.Fatalf("Mediarium's torrents in either category, and no one else's: %v", names)
	}
}

// Clearing the main category stays cleared: the form is not refilled with the
// default. With nothing set anywhere torrents still go in "mediarium".
func TestDownloadClientEmptyCategoryStaysEmpty(t *testing.T) {
	defer api.SetDownloadClientPollForTest(10 * time.Millisecond)()
	e := newClientEnv(t)
	local := filepath.Join(e.dir, "client-downloads")
	e.qb.content = "/downloads/Fixture.Movie"
	writeClientFile(t, filepath.Join(local, "Fixture.Movie", "movie.mkv"), movieBytes)
	saved := e.save(t, map[string]any{"category": "", "remotePath": "/downloads", "localPath": local})
	if saved["category"] != "" {
		t.Fatalf("saved category = %v, want it empty", saved["category"])
	}
	if got := getJSON[map[string]any](t, e.client, e.url+"/api/download-client"); got["category"] != "" {
		t.Fatalf("loaded category = %v, want it empty", got["category"])
	}
	if item, _ := e.grab(t); item["status"] != "completed" {
		t.Fatalf("grab = %v", item)
	}
	e.qb.mu.Lock()
	defer e.qb.mu.Unlock()
	if len(e.qb.cats) != 1 || e.qb.cats[0] != "mediarium" {
		t.Fatalf("with nothing set the default category is used, got %v", e.qb.cats)
	}
}
