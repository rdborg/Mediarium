package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestTorrentSwitchDefaultsOnAndPersists(t *testing.T) {
	_, base, client := loginNewServer(t)

	st := getJSON[map[string]any](t, client, base+"/api/settings")
	if st["torrentEnabled"] != true {
		t.Fatalf("torrents should default to on: %+v", st)
	}
	status := getJSON[map[string]any](t, client, base+"/api/downloads/status")["torrent"].(map[string]any)
	if status["state"] != "ready" || status["enabled"] != true {
		t.Fatalf("unexpected torrent status while on: %+v", status)
	}

	st = postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"torrentEnabled": false}, http.StatusOK)
	if st["torrentEnabled"] != false {
		t.Fatalf("switch should read back off: %+v", st)
	}
	// An unrelated partial update must not flip it back.
	st = postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"namingPreset": "plex"}, http.StatusOK)
	if st["torrentEnabled"] != false {
		t.Fatalf("a partial update changed the torrent switch: %+v", st)
	}
	status = getJSON[map[string]any](t, client, base+"/api/downloads/status")["torrent"].(map[string]any)
	if status["state"] != "disabled" || status["ready"] != false || status["blockedByVpn"] != false {
		t.Fatalf("unexpected torrent status while off: %+v", status)
	}

	st = postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"torrentEnabled": true}, http.StatusOK)
	if st["torrentEnabled"] != true {
		t.Fatalf("switch should read back on: %+v", st)
	}
}

func TestTorrentGrabsRefusedWhileOff(t *testing.T) {
	server, base, client := loginNewServer(t)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"torrentEnabled": false}, http.StatusOK)

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 604, Title: "Some Movie", Year: 2000, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	got := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", base, movie.ID), map[string]any{
		"releaseTitle": "Some Movie 2000 1080p",
		"downloadUrl":  "magnet:?xt=urn:btih:0000000000000000000000000000000000000000&dn=x",
		"protocol":     "torrent",
	}, http.StatusConflict)
	msg, _ := got["error"].(string)
	if !strings.Contains(msg, "Torrents are switched off") {
		t.Fatalf("expected a clear explanation, got %q", msg)
	}
	if list := getJSON[[]map[string]any](t, client, base+"/api/queue"); len(list) != 0 {
		t.Fatalf("a refused grab must not be queued: %+v", list)
	}

	series, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 1, Title: "Some Show", Year: 2000, Monitored: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/series/%d/grab", base, series.ID), map[string]any{
		"releaseTitle": "Some.Show.S01.1080p", "downloadUrl": "magnet:?xt=urn:btih:1", "season": 1, "protocol": "torrent",
	}, http.StatusConflict)
}

func TestTorrentIndexersSkippedWhileOff(t *testing.T) {
	_, base, client := loginNewServer(t)
	var hits atomic.Int32
	idx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`)
	}))
	defer idx.Close()
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Tracker", "definitionId": "fixture", "baseUrl": idx.URL, "apiKey": "k", "protocol": "torrent",
	}, http.StatusCreated)

	getJSON[[]map[string]any](t, client, base+"/api/search?q=anything")
	if hits.Load() == 0 {
		t.Fatal("with torrents on, the torrent indexer should be searched")
	}

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"torrentEnabled": false}, http.StatusOK)
	before := hits.Load()
	getJSON[[]map[string]any](t, client, base+"/api/search?q=anything")
	if hits.Load() != before {
		t.Fatal("with torrents off, the torrent indexer must not be searched")
	}
}

func TestHealthIgnoresVPNAndTorrentIndexersWhileOff(t *testing.T) {
	_, base, client := loginNewServer(t)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Tracker", "definitionId": "fixture", "baseUrl": "http://127.0.0.1:1", "protocol": "torrent",
	}, http.StatusCreated)

	h := healthByID(t, client, base)
	if _, ok := h["no-vpn"]; !ok {
		t.Fatalf("with torrents on and a torrent indexer, the no-VPN advice should show: %+v", h)
	}

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"torrentEnabled": false, "requireVpnForTorrents": true}, http.StatusOK)
	h = healthByID(t, client, base)
	for _, id := range []string{"no-vpn", "vpn-required"} {
		if _, ok := h[id]; ok {
			t.Fatalf("%s must not be reported while torrents are off: %+v", id, h)
		}
	}
}
