package api_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// The download apps send the strangest query strings (negative or huge start
// and limit, ids that are not ours, empty and repeated values). None of it may
// give a server error, and what SABnzbd's queue says as a whole is there.
func TestDownloaderLookAlikesToleratOddQueries(t *testing.T) {
	server, base, admin, _, _ := familyServer(t)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "probe"}, http.StatusCreated)["key"].(string)
	putJSON(t, admin, base+"/api/system/downloader-api", map[string]any{"enabled": true}, http.StatusOK)
	plain := &http.Client{Timeout: 20 * time.Second}

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 801, Title: "Probe Movie", Year: 2020, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	dlSeed(t, server.QueueRepo, movie.ID, "Probe.Movie.2020.1080p.WEB-DL-GRP", queue.ProtocolUsenet, 1<<30, queue.StatusDownloading, 50)
	dlSeed(t, server.QueueRepo, movie.ID, "Probe.Movie.2020.720p.WEB-DL-GRP", queue.ProtocolTorrent, 1<<29, queue.StatusDownloading, 10)
	dlSeed(t, server.QueueRepo, movie.ID, "Probe.Movie.2020.DVD-GRP", queue.ProtocolUsenet, 1<<28, queue.StatusFailed, 1)

	// The whole queue's state word is there while one is running.
	_, body := dlGet(t, plain, base+"/sabnzbd/api?mode=queue&output=json&apikey="+key)
	if q := dlJSON(t, body)["queue"].(map[string]any); q["status"] != "Downloading" {
		t.Errorf("queue status = %v", q["status"])
	}

	odd := []string{"-1", "0", "1", "99999999999999999999", "abc", "", "%00", "1,2,,3", strings.Repeat("9", 400), "../../x", "all", "failed", "purge", url.QueryEscape("😀")}
	modes := []string{"queue", "history", "pause", "resume", "get_cats", "get_config", "fullstatus", "warnings", "server_stats", "addurl", "addfile", "addlocalfile", "version", "auth", "nothing", ""}
	failed := func(label string, code int, b string) {
		if code >= 500 {
			t.Errorf("%s: %d %.200s", label, code, b)
		}
		if strings.Contains(b, key) {
			t.Errorf("%s: the key is in the answer", label)
		}
	}
	for _, mode := range modes {
		for _, v := range odd {
			for _, extra := range []string{"&start=" + v + "&limit=" + v, "&name=delete&value=" + v, "&name=" + v + "&value=" + v, "&nzo_ids=" + v + "&search=" + v + "&category=" + v + "&failed_only=" + v} {
				u := base + "/sabnzbd/api?output=json&apikey=" + key + "&mode=" + mode + extra
				code, b := dlGet(t, plain, u)
				failed("sab "+mode+extra, code, b)
			}
		}
	}

	// qBittorrent: the key as a Bearer token, then the calls with odd values.
	post := func(path string, form url.Values) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+"/qbittorrent/api/v2/"+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+key)
		return dlDo(t, plain, req)
	}
	get := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, base+"/qbittorrent/api/v2/"+path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		return dlDo(t, plain, req)
	}
	for _, v := range odd {
		for _, path := range []string{
			"torrents/info?hashes=" + v, "torrents/info?filter=" + v + "&category=" + v + "&sort=" + v + "&offset=" + v + "&limit=" + v + "&reverse=" + v,
			"torrents/properties?hash=" + v, "torrents/files?hash=" + v, "sync/maindata?rid=" + v, "torrents/info?tag=" + v,
			"app/version", "app/webapiVersion", "app/preferences", "transfer/info", "torrents/categories", "torrents/tags",
		} {
			code, b := get(path)
			failed("qb GET "+path, code, b)
		}
		for _, path := range []string{"torrents/pause", "torrents/resume", "torrents/stop", "torrents/start", "torrents/delete", "torrents/add", "torrents/setCategory", "torrents/recheck", "auth/login", "torrents/topPrio"} {
			code, b := post(path, url.Values{"hashes": {v}, "hash": {v}, "urls": {v}, "deleteFiles": {v}, "username": {v}, "password": {v}, "category": {v}})
			failed("qb POST "+path+" "+v, code, b)
		}
	}
	// A wrong key is refused by every way.
	if code, _ := dlGet(t, plain, base+"/sabnzbd/api?mode=queue&output=json&apikey=wrong"); code != http.StatusForbidden {
		t.Errorf("wrong key on SABnzbd: %d", code)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/qbittorrent/api/v2/torrents/info", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	if code, _ := dlDo(t, plain, req); code != http.StatusForbidden {
		t.Errorf("wrong key on qBittorrent: %d", code)
	}
}
