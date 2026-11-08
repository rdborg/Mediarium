package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// The SABnzbd and qBittorrent look-alike addresses, called the way nzb360
// does: SABnzbd with everything in the query string, qBittorrent with a
// form-encoded body and the SID cookie from the login.

func dlDo(t *testing.T, client *http.Client, req *http.Request) (int, string) {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func dlGet(t *testing.T, client *http.Client, u string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	return dlDo(t, client, req)
}

func dlPost(t *testing.T, client *http.Client, u string, form url.Values) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, u, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return dlDo(t, client, req)
}

func dlJSON(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("not a JSON object: %q", body)
	}
	return out
}

func dlItoa(n int64) string { return strconv.FormatInt(n, 10) }

// dlSeed adds one download of a movie to the queue in the given state.
func dlSeed(t *testing.T, queueRepo *queue.Repo, movieID int64, title string, proto queue.Protocol, size int64, status queue.Status, pct float64) int64 {
	t.Helper()
	id, err := queueRepo.Enqueue(queue.Item{MovieID: movieID, ReleaseTitle: title, SizeBytes: size, NZBURL: "http://127.0.0.1:1/x", Protocol: proto})
	if err != nil {
		t.Fatal(err)
	}
	msg := ""
	if status == queue.StatusFailed {
		msg = "No articles found"
	}
	if err := queueRepo.SetStatus(id, status, msg); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusCompleted {
		if err := queueRepo.SetProgress(id, pct); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestSABnzbdLookAlike(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "nzb360"}, http.StatusCreated)["key"].(string)
	roKey := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "look", "readOnly": true}, http.StatusCreated)["key"].(string)
	memberKey := postJSON[map[string]any](t, member, base+"/api/auth/api-keys", map[string]any{"name": "mine"}, http.StatusCreated)["key"].(string)
	api := base + "/sabnzbd/api"
	plain := &http.Client{Timeout: 20 * time.Second}

	// Switched off by default: nothing is there, not even with a good key.
	if code, _ := dlGet(t, plain, api+"?mode=version&output=json"); code != http.StatusNotFound {
		t.Fatalf("off: status %d, want 404", code)
	}
	if code, _ := dlGet(t, plain, api+"?mode=queue&output=json&apikey="+key); code != http.StatusNotFound {
		t.Fatalf("off with a key: status %d, want 404", code)
	}
	// Only an administrator switches it on.
	if code, _ := doStatus(t, member, http.MethodPut, base+"/api/system/downloader-api"); code != http.StatusForbidden {
		t.Fatalf("a member switched it on: %d", code)
	}
	if got := putJSON(t, admin, base+"/api/system/downloader-api", map[string]any{"enabled": true}, http.StatusOK); got["enabled"] != true {
		t.Fatalf("switching on: %v", got)
	}

	// The version and the way to sign in are answered without a key, as SABnzbd does.
	if code, body := dlGet(t, plain, api+"?mode=version&output=json"); code != http.StatusOK || dlJSON(t, body)["version"] == "" {
		t.Fatalf("version: %d %s", code, body)
	}
	if code, body := dlGet(t, plain, api+"?mode=version"); code != http.StatusOK || strings.Contains(body, "{") {
		t.Fatalf("plain version: %d %s", code, body)
	}
	if _, body := dlGet(t, plain, api+"?mode=auth&output=json"); dlJSON(t, body)["auth"] != "apikey" {
		t.Fatalf("auth: %s", body)
	}

	// Keys: none, wrong and a member's are refused; a read-only key may look but not change.
	for name, k := range map[string]string{"none": "", "wrong": "not-a-key", "member": memberKey} {
		code, body := dlGet(t, plain, api+"?mode=queue&output=json&apikey="+k)
		if code != http.StatusForbidden || dlJSON(t, body)["status"] != false {
			t.Fatalf("%s key: %d %s", name, code, body)
		}
	}
	if code, _ := dlGet(t, plain, api+"?mode=pause&output=json&apikey="+roKey); code != http.StatusForbidden {
		t.Fatalf("read-only key paused: %d", code)
	}
	if code, _ := dlGet(t, plain, api+"?mode=queue&output=json&apikey="+roKey); code != http.StatusOK {
		t.Fatalf("read-only key could not read: %d", code)
	}
	if code, _ := dlGet(t, plain, base+"/sabnzbd/nothing?mode=queue&apikey="+key); code != http.StatusNotFound {
		t.Fatalf("another address: %d", code)
	}

	// A running, a paused, a finished and a failed Usenet download, and a torrent that must not show up here.
	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 701, Title: "Night Harbour", Year: 2021, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	running := dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.1080p.BluRay.x264-GRP", queue.ProtocolUsenet, 2<<30, queue.StatusDownloading, 40)
	paused := dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.720p.WEB-DL.x264-GRP", queue.ProtocolUsenet, 1<<30, queue.StatusPaused, 10)
	dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.2160p.REMUX-GRP", queue.ProtocolUsenet, 3<<30, queue.StatusCompleted, 100)
	dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.DVDRip.XviD-GRP", queue.ProtocolUsenet, 700<<20, queue.StatusFailed, 5)
	dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.Torrent.1080p-GRP", queue.ProtocolTorrent, 1<<30, queue.StatusDownloading, 20)

	q := func() map[string]any {
		t.Helper()
		code, body := dlGet(t, plain, api+"?mode=queue&start=0&limit=50&output=json&apikey="+key)
		if code != http.StatusOK {
			t.Fatalf("queue: %d %s", code, body)
		}
		return dlJSON(t, body)["queue"].(map[string]any)
	}
	snap := q()
	slots := snap["slots"].([]any)
	if len(slots) != 2 || snap["noofslots"].(float64) != 2 {
		t.Fatalf("queue slots: %v", snap)
	}
	first := slots[0].(map[string]any)
	if first["nzo_id"] != "SABnzbd_nzo_mediarium"+dlItoa(running) || first["status"] != "Downloading" || first["cat"] != "movies" ||
		first["filename"] != "Night.Harbour.2021.1080p.BluRay.x264-GRP" || first["percentage"] != "40" || first["priority"] != "Normal" ||
		first["mb"] != "2048.00" || first["mbleft"] != "1228.80" || first["size"] != "2.0 GB" {
		t.Fatalf("first slot: %v", first)
	}
	if slots[1].(map[string]any)["status"] != "Paused" {
		t.Fatalf("second slot: %v", slots[1])
	}
	for _, k := range []string{"kbpersec", "speed", "paused", "size", "sizeleft", "timeleft", "diskspace1", "diskspace2", "mb", "mbleft", "version"} {
		if _, ok := snap[k]; !ok {
			t.Errorf("queue lacks %q", k)
		}
	}
	if snap["paused"] != false {
		t.Errorf("the queue says it is paused while a download runs")
	}

	// The speed comes from how far a download got between two looks.
	time.Sleep(1100 * time.Millisecond)
	if err := server.QueueRepo.SetProgress(running, 50); err != nil {
		t.Fatal(err)
	}
	snap = q()
	if kb, _ := strconv.ParseFloat(snap["kbpersec"].(string), 64); kb <= 0 {
		t.Fatalf("no speed worked out: %v", snap["kbpersec"])
	}
	if snap["timeleft"] == "0:00:00" || snap["slots"].([]any)[0].(map[string]any)["timeleft"] == "0:00:00" {
		t.Fatalf("no time left worked out: %v", snap)
	}

	// The history holds the finished and the failed one, and not the torrent.
	_, body := dlGet(t, plain, api+"?mode=history&limit=10&output=json&apikey="+key)
	hist := dlJSON(t, body)["history"].(map[string]any)
	hs := hist["slots"].([]any)
	if len(hs) != 2 {
		t.Fatalf("history: %v", hist)
	}
	byStatus := map[string]map[string]any{}
	for _, h := range hs {
		byStatus[h.(map[string]any)["status"].(string)] = h.(map[string]any)
	}
	if c := byStatus["Completed"]; c == nil || c["name"] != "Night.Harbour.2021.2160p.REMUX-GRP" || c["category"] != "movies" || c["completed"].(float64) <= 0 || c["size"] != "3.0 GB" || c["nzo_id"] == "" {
		t.Fatalf("completed entry: %v", c)
	}
	if f := byStatus["Failed"]; f == nil || f["fail_message"] != "No articles found" {
		t.Fatalf("failed entry: %v", f)
	}
	_, body = dlGet(t, plain, api+"?mode=history&failed_only=1&output=json&apikey="+key)
	if n := len(dlJSON(t, body)["history"].(map[string]any)["slots"].([]any)); n != 1 {
		t.Fatalf("failed only: %d entries", n)
	}

	// Categories, config and status.
	_, body = dlGet(t, plain, api+"?mode=get_cats&output=json&apikey="+key)
	if cats := dlJSON(t, body)["categories"].([]any); len(cats) < 3 || cats[0] != "*" {
		t.Fatalf("categories: %s", body)
	}
	_, body = dlGet(t, plain, api+"?mode=get_config&output=json&apikey="+key)
	if cfg := dlJSON(t, body)["config"].(map[string]any); len(cfg["categories"].([]any)) < 3 || strings.Contains(body, key) {
		t.Fatalf("config: %s", body)
	}
	_, body = dlGet(t, plain, api+"?mode=fullstatus&skip_dashboard=1&output=json&apikey="+key)
	if dlJSON(t, body)["status"].(map[string]any)["version"] == nil {
		t.Fatalf("fullstatus: %s", body)
	}

	// Pausing and resuming one download, then removing another.
	nzo := "SABnzbd_nzo_mediarium" + dlItoa(running)
	_, body = dlGet(t, plain, api+"?mode=queue&name=pause&value="+nzo+"&output=json&apikey="+key)
	if r := dlJSON(t, body); r["status"] != true || len(r["nzo_ids"].([]any)) != 1 {
		t.Fatalf("pause: %s", body)
	}
	if it, _ := server.QueueRepo.Get(running); it.Status != queue.StatusPaused {
		t.Fatalf("after pause: %s", it.Status)
	}
	_, body = dlGet(t, plain, api+"?mode=queue&name=resume&value="+nzo+"&output=json&apikey="+key)
	if dlJSON(t, body)["status"] != true {
		t.Fatalf("resume: %s", body)
	}
	if it, _ := server.QueueRepo.Get(running); it.Status == queue.StatusPaused {
		t.Fatalf("still paused after resume")
	}
	_, body = dlGet(t, plain, api+"?mode=queue&name=delete&value=SABnzbd_nzo_mediarium"+dlItoa(paused)+"&del_files=0&output=json&apikey="+key)
	if r := dlJSON(t, body); r["status"] != true || len(r["nzo_ids"].([]any)) != 1 {
		t.Fatalf("delete: %s", body)
	}
	if _, err := server.QueueRepo.Get(paused); err == nil {
		t.Fatalf("the deleted download is still in the list")
	}

	// Adding something that cannot be used is refused in SABnzbd's shape.
	_, body = dlGet(t, plain, api+"?mode=addurl&name="+url.QueryEscape("ftp://example.invalid/x.nzb")+"&output=json&apikey="+key)
	if r := dlJSON(t, body); r["status"] != false || r["error"] == "" {
		t.Fatalf("addurl with a bad address: %s", body)
	}
	_, body = dlGet(t, plain, api+"?mode=somethingelse&output=json&apikey="+key)
	if dlJSON(t, body)["status"] != false {
		t.Fatalf("unknown mode: %s", body)
	}

	// Switched off again: gone, with the same key.
	putJSON(t, admin, base+"/api/system/downloader-api", map[string]any{"enabled": false}, http.StatusOK)
	if code, _ := dlGet(t, plain, api+"?mode=queue&output=json&apikey="+key); code != http.StatusNotFound {
		t.Fatalf("after switching off: %d", code)
	}
}

func TestQBittorrentLookAlike(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "nzb360"}, http.StatusCreated)["key"].(string)
	roKey := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "look", "readOnly": true}, http.StatusCreated)["key"].(string)
	memberKey := postJSON[map[string]any](t, member, base+"/api/auth/api-keys", map[string]any{"name": "mine"}, http.StatusCreated)["key"].(string)
	v2 := base + "/qbittorrent/api/v2"
	newApp := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, Timeout: 20 * time.Second}
	}
	app := newApp()

	if code, _ := dlGet(t, app, v2+"/app/version"); code != http.StatusNotFound {
		t.Fatalf("off: status %d, want 404", code)
	}
	putJSON(t, admin, base+"/api/system/downloader-api", map[string]any{"enabled": true}, http.StatusOK)

	// Before signing in everything is forbidden, like the real program.
	if code, _ := dlGet(t, app, v2+"/torrents/info"); code != http.StatusForbidden {
		t.Fatalf("unsigned: %d", code)
	}
	for name, k := range map[string]string{"wrong": "not-a-key", "member": memberKey} {
		code, body := dlPost(t, app, v2+"/auth/login", url.Values{"username": {"admin"}, "password": {k}})
		if code == http.StatusOK && body == "Ok." {
			t.Fatalf("%s key signed in", name)
		}
	}
	if code, _ := dlGet(t, app, v2+"/torrents/info"); code != http.StatusForbidden {
		t.Fatalf("still unsigned: %d", code)
	}
	// The API key is the password.
	if code, body := dlPost(t, app, v2+"/auth/login", url.Values{"username": {"admin"}, "password": {key}}); code != http.StatusOK || body != "Ok." {
		t.Fatalf("login: %d %q", code, body)
	}
	if code, body := dlGet(t, app, v2+"/app/version"); code != http.StatusOK || !strings.HasPrefix(body, "v4.") {
		t.Fatalf("version: %d %q", code, body)
	}
	if code, body := dlGet(t, app, v2+"/app/webapiVersion"); code != http.StatusOK || !strings.HasPrefix(body, "2.") {
		t.Fatalf("api version: %d %q", code, body)
	}
	// An API key sent as a Bearer token needs no login.
	req, _ := http.NewRequest(http.MethodGet, v2+"/app/version", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	if code, _ := dlDo(t, &http.Client{Timeout: 20 * time.Second}, req); code != http.StatusOK {
		t.Fatalf("bearer: %d", code)
	}

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 701, Title: "Night Harbour", Year: 2021, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	running := dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.1080p.BluRay.x264-GRP", queue.ProtocolTorrent, 2<<30, queue.StatusDownloading, 40)
	paused := dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.720p.WEB-DL.x264-GRP", queue.ProtocolTorrent, 1<<30, queue.StatusPaused, 10)
	dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.2160p.REMUX-GRP", queue.ProtocolTorrent, 3<<30, queue.StatusCompleted, 100)
	dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.DVDRip.XviD-GRP", queue.ProtocolTorrent, 700<<20, queue.StatusFailed, 5)
	dlSeed(t, server.QueueRepo, movie.ID, "Night.Harbour.2021.Usenet.1080p-GRP", queue.ProtocolUsenet, 1<<30, queue.StatusDownloading, 20)

	info := func(extra string) []any {
		t.Helper()
		code, body := dlGet(t, app, v2+"/torrents/info"+extra)
		if code != http.StatusOK {
			t.Fatalf("info: %d %s", code, body)
		}
		var out []any
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("info is not a list: %s", body)
		}
		return out
	}
	list := info("")
	if len(list) != 4 {
		t.Fatalf("torrents: %d listed, want 4 (the Usenet one is not one of them): %v", len(list), list)
	}
	states := map[string]map[string]any{}
	for _, x := range list {
		m := x.(map[string]any)
		states[m["state"].(string)] = m
	}
	d := states["downloading"]
	if d == nil {
		d = states["stalledDL"] // no peers and no speed on the first look
	}
	if d == nil {
		t.Fatalf("no downloading torrent: %v", states)
	}
	if d["name"] != "Night.Harbour.2021.1080p.BluRay.x264-GRP" || d["progress"].(float64) != 0.4 || d["size"].(float64) != float64(2<<30) ||
		d["category"] != "movies" || len(d["hash"].(string)) != 40 || d["completion_on"].(float64) != -1 || d["eta"].(float64) != 8640000 || d["added_on"].(float64) <= 0 {
		t.Fatalf("running torrent: %v", d)
	}
	if states["pausedDL"] == nil || states["error"] == nil || states["pausedUP"] == nil {
		t.Fatalf("states: %v", states)
	}
	if p := states["pausedUP"]; p["progress"].(float64) != 1 || p["completion_on"].(float64) <= 0 {
		t.Fatalf("finished torrent: %v", p)
	}
	hash := d["hash"].(string)
	if got := info("?filter=downloading"); len(got) != 1 {
		t.Fatalf("downloading filter: %v", got)
	}
	if got := info("?filter=paused"); len(got) != 2 {
		t.Fatalf("paused filter: %v", got)
	}
	if got := info("?hashes=" + hash); len(got) != 1 {
		t.Fatalf("hash filter: %v", got)
	}
	if got := info("?category=music"); len(got) != 0 {
		t.Fatalf("category filter: %v", got)
	}

	// The speed and time left come from how far it got between two looks.
	time.Sleep(1100 * time.Millisecond)
	if err := server.QueueRepo.SetProgress(running, 50); err != nil {
		t.Fatal(err)
	}
	var speedy map[string]any
	for _, x := range info("?hashes=" + hash) {
		speedy = x.(map[string]any)
	}
	if speedy["dlspeed"].(float64) <= 0 || speedy["eta"].(float64) >= 8640000 || speedy["state"] != "downloading" {
		t.Fatalf("no speed worked out: %v", speedy)
	}
	_, body := dlGet(t, app, v2+"/transfer/info")
	if tr := dlJSON(t, body); tr["dl_info_speed"].(float64) <= 0 || tr["connection_status"] != "connected" {
		t.Fatalf("transfer info: %s", body)
	}
	_, body = dlGet(t, app, v2+"/sync/maindata?rid=0")
	if md := dlJSON(t, body); md["full_update"] != true || len(md["torrents"].(map[string]any)) != 4 || md["rid"].(float64) != 1 {
		t.Fatalf("maindata: %s", body)
	}
	_, body = dlGet(t, app, v2+"/torrents/properties?hash="+hash)
	if dlJSON(t, body)["save_path"] == nil {
		t.Fatalf("properties: %s", body)
	}
	if code, _ := dlGet(t, app, v2+"/torrents/properties?hash="+strings.Repeat("0", 40)); code != http.StatusNotFound {
		t.Fatalf("properties of nothing: %d", code)
	}
	_, body = dlGet(t, app, v2+"/torrents/categories")
	if dlJSON(t, body)["movies"] == nil {
		t.Fatalf("categories: %s", body)
	}

	// Pause, resume, delete: form-encoded POSTs, with a pipe between hashes.
	if code, _ := dlGet(t, app, v2+"/torrents/pause?hashes="+hash); code != http.StatusMethodNotAllowed {
		t.Fatalf("pause by GET: %d", code)
	}
	if code, _ := dlPost(t, app, v2+"/torrents/pause", url.Values{"hashes": {hash}}); code != http.StatusOK {
		t.Fatalf("pause: %d", code)
	}
	if it, _ := server.QueueRepo.Get(running); it.Status != queue.StatusPaused {
		t.Fatalf("after pause: %s", it.Status)
	}
	if code, _ := dlPost(t, app, v2+"/torrents/resume", url.Values{"hashes": {hash}}); code != http.StatusOK {
		t.Fatalf("resume: %d", code)
	}
	if it, _ := server.QueueRepo.Get(running); it.Status == queue.StatusPaused {
		t.Fatalf("still paused after resume")
	}
	pausedHash := ""
	for _, x := range info("") {
		if m := x.(map[string]any); m["state"] == "pausedDL" {
			pausedHash = m["hash"].(string)
		}
	}
	if pausedHash == "" {
		t.Fatal("the paused torrent is not listed")
	}
	if code, _ := dlPost(t, app, v2+"/torrents/delete", url.Values{"hashes": {pausedHash}, "deleteFiles": {"false"}}); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if _, err := server.QueueRepo.Get(paused); err == nil {
		t.Fatal("the deleted torrent is still in the list")
	}
	if code, _ := dlPost(t, app, v2+"/torrents/delete", url.Values{"deleteFiles": {"true"}}); code != http.StatusBadRequest {
		t.Fatalf("delete without hashes: %d", code)
	}

	// Adding: a name that is in no library is refused ("Fails."), a request with nothing in it is a 400.
	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("ab", 20) + "&dn=Unknown.Film.1999.1080p-GRP"
	if code, body := dlPost(t, app, v2+"/torrents/add", url.Values{"urls": {magnet}, "category": {"movies"}}); code != http.StatusOK || body != "Fails." {
		t.Fatalf("add of an unknown title: %d %q", code, body)
	}
	if code, _ := dlPost(t, app, v2+"/torrents/add", url.Values{}); code != http.StatusBadRequest {
		t.Fatalf("add of nothing: %d", code)
	}
	if code, _ := dlGet(t, app, v2+"/torrents/nothinghere"); code != http.StatusNotFound {
		t.Fatalf("unknown call: %d", code)
	}

	// A read-only key reads but does not change anything.
	ro := newApp()
	if code, body := dlPost(t, ro, v2+"/auth/login", url.Values{"username": {"x"}, "password": {roKey}}); code != http.StatusOK || body != "Ok." {
		t.Fatalf("read-only login: %d %q", code, body)
	}
	if code, _ := dlGet(t, ro, v2+"/torrents/info"); code != http.StatusOK {
		t.Fatalf("read-only read: %d", code)
	}
	if code, _ := dlPost(t, ro, v2+"/torrents/pause", url.Values{"hashes": {"all"}}); code != http.StatusForbidden {
		t.Fatalf("read-only pause: %d", code)
	}

	// Logging out ends the sign-in; switching the feature off closes everything.
	dlPost(t, app, v2+"/auth/logout", url.Values{})
	if code, _ := dlGet(t, app, v2+"/torrents/info"); code != http.StatusForbidden {
		t.Fatalf("after logout: %d", code)
	}
	putJSON(t, admin, base+"/api/system/downloader-api", map[string]any{"enabled": false}, http.StatusOK)
	if code, _ := dlGet(t, ro, v2+"/torrents/info"); code != http.StatusNotFound {
		t.Fatalf("after switching off: %d", code)
	}
}

func TestDownloaderAPIMatchesReleasesToTheLibrary(t *testing.T) {
	server, _, _, _, _ := familyServer(t)
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 701, Title: "Night Harbour", Year: 2021}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 9001, Title: "Salt & Static", Year: 2019}, nil); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		release string
		kind    string
		season  int
		episode int
		fails   bool
	}{
		{"movie with a year", "Night.Harbour.2021.1080p.BluRay.x264-GRP", "movie", 0, 0, false},
		{"movie without a year", "Night Harbour 1080p WEB-DL", "movie", 0, 0, false},
		{"another year is another movie", "Night.Harbour.1984.1080p.BluRay-GRP", "", 0, 0, true},
		{"an episode", "Salt.and.Static.S02E05.1080p.WEB-DL.x264-GRP", "show", 2, 5, false},
		{"a whole season", "Salt.and.Static.S01.1080p.WEB-DL.x264-GRP", "show", 1, 0, false},
		{"a show that is not here", "Other.Show.S01E01.720p-GRP", "", 0, 0, true},
		{"a movie that is not here", "Unknown.Film.1999.1080p-GRP", "", 0, 0, true},
		{"a dated episode names no season", "Salt.and.Static.2024.03.15.720p-GRP", "", 0, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, _, season, episode, err := server.TestMatchLibraryTitle(tc.release)
			if tc.fails {
				if err == nil {
					t.Fatalf("%q matched a %s", tc.release, kind)
				}
				return
			}
			if err != nil || kind != tc.kind || season != tc.season || episode != tc.episode {
				t.Fatalf("%q: got %s S%dE%d, %v; want %s S%dE%d", tc.release, kind, season, episode, err, tc.kind, tc.season, tc.episode)
			}
		})
	}
}
