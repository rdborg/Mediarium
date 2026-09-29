package api_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/store"
)

// fakeTracker is a small private torrent site with a login form, the kind
// of site a definition-based indexer drives.
type fakeTracker struct {
	*httptest.Server
	password  atomic.Value // the account's current password
	logins    atomic.Int32
	downloads atomic.Int32
	mu        sync.Mutex
	sessions  map[string]bool
}

const fakeTorrentFile = "d8:announce30:http://tracker.test/announce4:infod6:lengthi1e4:name1:x12:piece lengthi16384e6:pieces0:ee"

func newFakeTracker(t *testing.T) *fakeTracker {
	ft := &fakeTracker{sessions: map[string]bool{}}
	ft.password.Store("s3cret")
	session := func(r *http.Request) bool {
		c, err := r.Cookie("sid")
		if err != nil {
			return false
		}
		ft.mu.Lock()
		defer ft.mu.Unlock()
		return ft.sessions[c.Value]
	}
	ft.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login.php":
			io.WriteString(w, `<form id="loginform" action="takelogin.php" method="post"><input type="hidden" name="csrf" value="tok"><input name="username"><input name="password" type="password"></form>`)
		case "/takelogin.php":
			r.ParseForm()
			if r.PostForm.Get("username") != "alice" || r.PostForm.Get("password") != ft.password.Load().(string) {
				io.WriteString(w, `<div class="error">Invalid username or password</div>`)
				return
			}
			sid := fmt.Sprintf("s%d", ft.logins.Add(1))
			ft.mu.Lock()
			ft.sessions[sid] = true
			ft.mu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: sid, Path: "/"})
			http.Redirect(w, r, "/index.php", http.StatusFound)
		case "/index.php":
			if session(r) {
				io.WriteString(w, `<a href="logout.php">Logout</a>`)
			}
		case "/browse.php":
			if !session(r) {
				http.Redirect(w, r, "/login.php", http.StatusFound)
				return
			}
			io.WriteString(w, `<a href="logout.php">Logout</a><table id="torrents">
<tr class="torrent"><td><a href="browse.php?cat=10">HD</a></td><td><a class="title" href="details.php?id=1">The.Tracker.Movie.2021.1080p.BluRay.x264-TRK</a></td>
<td class="size">8 GB</td><td class="seeders">25</td><td class="leechers">2</td><td class="snatched">1</td></tr>
<tr class="extra"><td><a href="download.php?id=1">DL</a></td><td class="added">2024-05-01 10:00</td></tr>
</table>`)
		case "/download.php":
			if !session(r) {
				io.WriteString(w, `<html>please log in</html>`)
				return
			}
			ft.downloads.Add(1)
			w.Header().Set("Content-Type", "application/x-bittorrent")
			io.WriteString(w, fakeTorrentFile)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ft.Close)
	return ft
}

// definitionsArchive serves a GitHub-style archive of the test
// definitions, with the fixture site's address pointing at the tracker.
func definitionsArchive(t *testing.T, siteURL string) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"fixtureprivate", "fixturepublic"} {
		data, err := os.ReadFile(filepath.Join("..", "indexers", "testdata", "cardigann", name+".yml"))
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.ReplaceAll(data, []byte("https://private.fixture.test/"), []byte(siteURL+"/"))
		tw.WriteHeader(&tar.Header{Name: "Indexers-master/definitions/v11/" + name + ".yml", Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg})
		tw.Write(data)
	}
	unsupported := []byte("id: needsxpath\nname: Needs XPath\nlinks: [https://x.test/]\nsearch:\n  paths: [{path: x}]\n  rows: {selector: 'tr:xpath(1)'}\n  fields: {title: {selector: a}}\n")
	tw.WriteHeader(&tar.Header{Name: "Indexers-master/definitions/v11/needsxpath.yml", Mode: 0o644, Size: int64(len(unsupported)), Typeflag: tar.TypeReg})
	tw.Write(unsupported)
	tw.Close()
	gz.Close()
	archive := buf.Bytes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	t.Cleanup(srv.Close)
	return srv
}

// TestDefinitionIndexerEndToEnd adds a login-required site from the
// definition list through the API, searches it, and grabs a result: the
// grab fetches the .torrent through the indexer's signed-in session.
func TestDefinitionIndexerEndToEnd(t *testing.T) {
	server, base, client := loginNewServer(t)
	tracker := newFakeTracker(t)
	server.Definitions.SourceURL = definitionsArchive(t, tracker.URL).URL
	server.Cardigann.DefaultDelay = 0

	// --- the "add a site" list ---
	defs := getJSON[map[string]any](t, client, base+"/api/indexer-definitions")
	if defs["licence"] == "" || !strings.Contains(defs["source"].(string), "Prowlarr/Indexers") || defs["updatedAt"] == "" {
		t.Fatalf("missing source/licence/updatedAt: %+v", defs)
	}
	var private map[string]any
	for _, d := range defs["definitions"].([]any) {
		if m := d.(map[string]any); m["id"] == "fixtureprivate" {
			private = m
		}
	}
	if private == nil || private["type"] != "private" || private["protocol"] != "torrent" || private["supported"] != true {
		t.Fatalf("fixtureprivate not listed properly: %+v", private)
	}
	settingTypes := map[string]string{}
	for _, s := range private["settings"].([]any) {
		m := s.(map[string]any)
		settingTypes[m["name"].(string)] = m["type"].(string)
	}
	if settingTypes["username"] != "text" || settingTypes["password"] != "password" || settingTypes["freeleech"] != "checkbox" {
		t.Fatalf("settings not as declared: %v", settingTypes)
	}

	// --- validation ---
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"definitionId": "nope", "settings": map[string]any{}}, http.StatusBadRequest)
	msg := postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"definitionId": "needsxpath", "settings": map[string]any{}}, http.StatusBadRequest)
	if !strings.Contains(msg["error"].(string), "can't run yet") {
		t.Fatalf("unsupported definition should say so: %v", msg)
	}
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"definitionId": "fixtureprivate", "baseUrl": "https://elsewhere.test/", "settings": map[string]any{}}, http.StatusBadRequest)

	// --- add the site (base URL defaults to the definition's first link) ---
	created := postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"definitionId": "fixtureprivate", "name": "My Tracker", "protocol": "torrent",
		"settings": map[string]any{"username": "alice", "password": "s3cret", "freeleech": false, "info_x": "ignored"},
	}, http.StatusCreated)
	id := int64(created["id"].(float64))
	if created["kind"] != "cardigann" || created["baseUrl"] != tracker.URL+"/" || created["protocol"] != "torrent" {
		t.Fatalf("unexpected created indexer %+v", created)
	}
	st := created["settings"].(map[string]any)
	if st["username"] != "alice" || st["freeleech"] != false || st["password"] != nil {
		t.Fatalf("settings in the response must not include secrets: %+v", st)
	}
	if fmt.Sprint(created["storedSecrets"]) != "[password]" {
		t.Fatalf("storedSecrets = %v", created["storedSecrets"])
	}
	// the password is encrypted at rest
	db, err := store.Open(filepath.Join(filepath.Dir(server.Definitions.Dir), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	var plain, enc string
	if err := db.QueryRow(`SELECT settings_json, COALESCE(secrets_encrypted, '') FROM indexers WHERE id = ?`, id).Scan(&plain, &enc); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if strings.Contains(plain+enc, "s3cret") || enc == "" || !strings.Contains(plain, "alice") {
		t.Fatalf("secret not encrypted: plain=%q enc=%q", plain, enc)
	}

	list := getJSON[[]map[string]any](t, client, base+"/api/indexers")
	if len(list) != 1 || list[0]["kind"] != "cardigann" || list[0]["definitionId"] != "fixtureprivate" {
		t.Fatalf("list = %+v", list)
	}

	// --- test ---
	res := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/indexers/%d/test", base, id), nil, http.StatusOK)
	if res["ok"] != true {
		t.Fatalf("test failed: %+v", res)
	}
	// testing an unsaved configuration
	res = postJSON[map[string]any](t, client, base+"/api/indexers/test", map[string]any{
		"kind": "cardigann", "definitionId": "fixtureprivate", "settings": map[string]any{"username": "alice", "password": "nope"},
	}, http.StatusOK)
	if res["ok"] != false || !strings.Contains(res["message"].(string), "Invalid username or password") {
		t.Fatalf("unsaved test with a bad password should fail clearly: %+v", res)
	}

	// --- search ---
	results := getJSON[[]map[string]any](t, client, base+"/api/search?q=tracker+movie")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %+v", results)
	}
	r := results[0]
	if r["indexerName"] != "My Tracker" || r["protocol"] != "torrent" || r["resolution"] != "1080p" || r["seeders"] != float64(25) {
		t.Fatalf("unexpected result %+v", r)
	}
	// The browser gets an opaque reference, never the real link (which can
	// carry an indexer's key); the server turns it back into the link.
	downloadURL := r["downloadUrl"].(string)
	if !strings.HasPrefix(downloadURL, "rel_") {
		t.Fatalf("download url should be an opaque reference, got %q", downloadURL)
	}
	if real := server.OfferedURL(downloadURL); !strings.HasPrefix(real, "mediarium-indexer://") {
		t.Fatalf("the reference should stand for an indexer reference, got %q", real)
	}

	// --- grab: the pipeline fetches the .torrent through the session. The
	// VPN kill switch then stops it before any torrent traffic starts, so
	// the test needs no network.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"requireVpnForTorrents": true}, http.StatusOK)
	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 777, Title: "The Tracker Movie", Year: 2021, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", base, movie.ID), map[string]any{
		"releaseTitle": r["title"], "downloadUrl": downloadURL, "sizeBytes": r["sizeBytes"], "protocol": "torrent",
	}, http.StatusAccepted)
	item := waitForQueueStatus(t, client, base, "failed")
	if !strings.Contains(item["error"].(string), "VPN required") {
		t.Fatalf("expected the kill switch to stop the grab after the fetch: %+v", item)
	}
	if tracker.downloads.Load() != 1 {
		t.Fatalf("the .torrent should have been fetched once with the session, got %d", tracker.downloads.Load())
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(server.Definitions.Dir)), "downloads", "incomplete", "queue-*", "release.torrent"))
	if len(matches) != 1 {
		t.Fatalf("expected the fetched .torrent in the queue folder, got %v", matches)
	}
	if data, _ := os.ReadFile(matches[0]); string(data) != fakeTorrentFile {
		t.Fatalf("wrong .torrent content %q", data)
	}

	// --- edit: blank secrets keep their value ---
	upd := putJSONStatus(t, client, fmt.Sprintf("%s/api/indexers/%d", base, id), map[string]any{
		"name": "Renamed", "settings": map[string]any{"username": "alice", "password": "", "freeleech": true},
	}, http.StatusOK)
	if upd["name"] != "Renamed" || upd["settings"].(map[string]any)["freeleech"] != true {
		t.Fatalf("update = %+v", upd)
	}
	if res := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/indexers/%d/test", base, id), nil, http.StatusOK); res["ok"] != true {
		t.Fatalf("password should have been kept: %+v", res)
	}

	// --- a failing test shows on the dashboard until the next good one ---
	tracker.password.Store("changed")
	putJSONStatus(t, client, fmt.Sprintf("%s/api/indexers/%d", base, id), map[string]any{"settings": map[string]any{"password": "stale"}}, http.StatusOK)
	res = postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/indexers/%d/test", base, id), nil, http.StatusOK)
	if res["ok"] != false {
		t.Fatalf("expected the test to fail: %+v", res)
	}
	h := healthByID(t, client, base)
	item = h[fmt.Sprintf("indexer-test-failed-%d", id)]
	if item == nil || item["level"] != "info" || !strings.Contains(item["impact"].(string), "Invalid username or password") {
		t.Fatalf("expected an info item for the failed test: %v", keysOf(h))
	}
	if l := getJSON[[]map[string]any](t, client, base+"/api/indexers"); l[0]["lastTestError"] == nil || strings.Contains(fmt.Sprint(l[0]), "stale") {
		t.Fatalf("list should carry the last error but never secrets: %+v", l[0])
	}
	tracker.password.Store("stale")
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/indexers/%d/test", base, id), nil, http.StatusOK)
	if h := healthByID(t, client, base); h[fmt.Sprintf("indexer-test-failed-%d", id)] != nil {
		t.Fatalf("a passing test should clear the item: %v", keysOf(h))
	}

	// --- delete ---
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/indexers/%d", base, id), nil)
	if resp, err := client.Do(req); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: %v", err)
	}
}

func waitForQueueStatus(t *testing.T, client *http.Client, base, status string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last []map[string]any
	for time.Now().Before(deadline) {
		last = getJSON[[]map[string]any](t, client, base+"/api/queue")
		for _, it := range last {
			if it["status"] == status {
				return it
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no queue item reached %q: %+v", status, last)
	return nil
}

func TestDefinitionListUnavailable(t *testing.T) {
	server, base, client := loginNewServer(t)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()
	server.Definitions.SourceURL = down.URL
	resp, err := client.Get(base + "/api/indexer-definitions?refresh=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "Could not download the indexer definitions") {
		t.Fatalf("expected a clear 502, got %d %s", resp.StatusCode, body)
	}
}

func TestFlareSolverrSetting(t *testing.T) {
	_, base, client := loginNewServer(t)
	st := getJSON[map[string]any](t, client, base+"/api/settings")
	if st["flareSolverrUrl"] != "" {
		t.Fatalf("no FlareSolverr by default, got %v", st["flareSolverrUrl"])
	}
	st = putJSONStatus(t, client, base+"/api/settings", map[string]any{"flareSolverrUrl": " http://flaresolverr:8191/ "}, http.StatusOK)
	if st["flareSolverrUrl"] != "http://flaresolverr:8191" {
		t.Fatalf("got %v", st["flareSolverrUrl"])
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"flareSolverrUrl": "ftp://nope"}, http.StatusBadRequest)
	st = putJSONStatus(t, client, base+"/api/settings", map[string]any{"namingPreset": "plex"}, http.StatusOK)
	if st["flareSolverrUrl"] != "http://flaresolverr:8191" {
		t.Fatalf("a partial update must not clear it: %v", st["flareSolverrUrl"])
	}
	st = putJSONStatus(t, client, base+"/api/settings", map[string]any{"flareSolverrUrl": ""}, http.StatusOK)
	if st["flareSolverrUrl"] != "" {
		t.Fatalf("empty should clear it: %v", st["flareSolverrUrl"])
	}
}

func TestNewznabIndexersKeepWorking(t *testing.T) {
	_, base, client := loginNewServer(t)
	idx := newTVIndexerWith(t, []string{"A.Release.2001.1080p-GRP"})
	created := postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "U", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	if created["kind"] != "newznab" || created["apiKey"] != nil {
		t.Fatalf("unexpected %+v", created)
	}
	created = postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "T", "baseUrl": idx.URL, "apiKey": "k", "protocol": "torrent"}, http.StatusCreated)
	if created["kind"] != "torznab" {
		t.Fatalf("unexpected %+v", created)
	}
	id := int64(created["id"].(float64))
	upd := putJSONStatus(t, client, fmt.Sprintf("%s/api/indexers/%d", base, id), map[string]any{"name": "Torznab", "apiKey": ""}, http.StatusOK)
	if upd["name"] != "Torznab" {
		t.Fatalf("update = %+v", upd)
	}
	if res := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/indexers/%d/test", base, id), nil, http.StatusOK); res["ok"] != true {
		t.Fatalf("api key should be kept on a blank update: %+v", res)
	}
	putJSONStatus(t, client, base+"/api/indexers/9999", map[string]any{"name": "x"}, http.StatusNotFound)
}
