package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/queue"
)

// arrTMDB is a fake TMDB with one made-up movie (701) and one made-up show
// (9001, TVDB id 777001, two seasons).
func arrTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/movie/701", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 701, "title": "Night Harbour", "release_date": "2021-05-14", "poster_path": "/nh.jpg", "overview": "A ferry, a storm.", "credits": map[string]any{"cast": []map[string]any{{"id": 11, "name": "Ada Reef", "character": "Captain", "profile_path": "/ada.jpg", "order": 0}}, "crew": []map[string]any{{"id": 12, "name": "Bo Quay", "job": "Director", "profile_path": "/bo.jpg"}, {"id": 13, "name": "Cy Dock", "job": "Editor", "department": "Editing"}}}})
	})
	mux.HandleFunc("/trending/movie/week", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": 701, "title": "Night Harbour", "release_date": "2021-05-14"}, {"id": 702, "title": "Low Tide", "release_date": "2022-01-02", "poster_path": "/lt.jpg"}}})
	})
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": 701, "title": "Night Harbour", "release_date": "2021-05-14"}}})
	})
	mux.HandleFunc("/tv/9001", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id": 9001, "name": "Salt & Static", "first_air_date": "2019-02-01", "status": "Returning Series",
			"seasons":      []map[string]any{{"season_number": 1, "episode_count": 2}, {"season_number": 2, "episode_count": 1}},
			"external_ids": map[string]any{"tvdb_id": 777001, "imdb_id": "tt0000001"},
		})
	})
	mux.HandleFunc("/tv/9001/season/1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
			{"season_number": 1, "episode_number": 1, "name": "Static", "air_date": "2019-02-01"},
			{"season_number": 1, "episode_number": 2, "name": "Salt", "air_date": "2019-02-08"},
		}})
	})
	mux.HandleFunc("/tv/9001/season/2", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{{"season_number": 2, "episode_number": 1, "name": "Tide", "air_date": "2020-03-01"}}})
	})
	mux.HandleFunc("/find/777001", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tv_results": []map[string]any{{"id": 9001, "name": "Salt & Static", "first_air_date": "2019-02-01"}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// arrCall makes one request the way the arr apps do and decodes the answer.
func arrCall(t *testing.T, method, url, key string, body any, want int) any {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, url, resp.StatusCode, want, raw)
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func list(v any) []any         { l, _ := v.([]any); return l }

func TestArrAPIForOverseerr(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	server.TestSetTMDBBaseURL("fixture-key", arrTMDB(t).URL)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "Overseerr"}, http.StatusCreated)["key"].(string)
	memberKey := postJSON[map[string]any](t, member, base+"/api/auth/api-keys", map[string]any{"name": "mine"}, http.StatusCreated)["key"].(string)
	radarr, sonarr := base+"/radarr/api/v3", base+"/sonarr/api/v3"

	// Signing in: a key is needed, it must be an administrator's, and the
	// query parameter works like the header.
	arrCall(t, "GET", radarr+"/system/status", "", nil, http.StatusUnauthorized)
	arrCall(t, "GET", radarr+"/system/status", "not-a-key", nil, http.StatusUnauthorized)
	arrCall(t, "GET", radarr+"/system/status", memberKey, nil, http.StatusForbidden)
	st := obj(arrCall(t, "GET", radarr+"/system/status?apikey="+key, "", nil, http.StatusOK))
	if st["appName"] != "Radarr" || st["urlBase"] != "/radarr" || !strings.HasPrefix(st["version"].(string), "5.") {
		t.Fatalf("radarr status: %v", st)
	}
	if st := obj(arrCall(t, "GET", sonarr+"/system/status", key, nil, http.StatusOK)); st["appName"] != "Sonarr" || !strings.HasPrefix(st["version"].(string), "4.") {
		t.Fatalf("sonarr status: %v", st)
	}
	if root := obj(arrCall(t, "GET", base+"/radarr/api", key, nil, http.StatusOK)); root["current"] != "v3" {
		t.Fatalf("api root: %v", root)
	}

	// What Overseerr's connection test reads (note the capital P).
	profiles := list(arrCall(t, "GET", radarr+"/qualityProfile", key, nil, http.StatusOK))
	if len(profiles) < 3 || obj(profiles[0])["name"] == nil {
		t.Fatalf("profiles: %v", profiles)
	}
	profileID := obj(profiles[1])["id"].(float64)
	if roots := list(arrCall(t, "GET", radarr+"/rootfolder", key, nil, http.StatusOK)); len(roots) != 1 || obj(roots[0])["path"] == "" {
		t.Fatalf("root folders: %v", roots)
	}
	if lp := list(arrCall(t, "GET", sonarr+"/languageprofile", key, nil, http.StatusOK)); len(lp) != 1 {
		t.Fatalf("language profiles: %v", lp)
	}
	tag := obj(arrCall(t, "POST", radarr+"/tag", key, map[string]any{"label": "1 - Ann"}, http.StatusCreated))
	tagID := tag["id"].(float64)
	if again := obj(arrCall(t, "POST", radarr+"/tag", key, map[string]any{"label": "1 - ann"}, http.StatusCreated)); again["id"] != tag["id"] {
		t.Fatalf("the same label should get the same number: %v vs %v", again, tag)
	}

	// A movie request: look it up (not in the library: no id), add it, then
	// it's found with its id.
	look := list(arrCall(t, "GET", radarr+"/movie/lookup?term=tmdb:701", key, nil, http.StatusOK))
	if len(look) != 1 || obj(look[0])["id"] != nil || obj(look[0])["title"] != "Night Harbour" {
		t.Fatalf("lookup before adding: %v", look)
	}
	added := obj(arrCall(t, "POST", radarr+"/movie", key, map[string]any{
		"title": "Night Harbour", "tmdbId": 701, "year": 2021, "qualityProfileId": profileID, "profileId": profileID,
		"titleSlug": "701", "minimumAvailability": "released", "rootFolderPath": "/movies", "monitored": true,
		"tags": []float64{tagID}, "addOptions": map[string]any{"searchForMovie": false},
	}, http.StatusCreated))
	movieID := added["id"].(float64)
	if movieID == 0 || added["qualityProfileId"] != profileID || len(list(added["tags"])) != 1 || added["titleSlug"] != "701" {
		t.Fatalf("added movie: %v", added)
	}
	// Adding it again is refused with Radarr's own code, which some apps read as "done".
	dup := list(arrCall(t, "POST", radarr+"/movie", key, map[string]any{"tmdbId": 701, "qualityProfileId": profileID}, http.StatusBadRequest))
	if len(dup) != 1 || obj(dup[0])["errorCode"] != "MovieExistsValidator" {
		t.Fatalf("adding twice: %v", dup)
	}
	look = list(arrCall(t, "GET", radarr+"/movie/lookup?term=tmdb:701", key, nil, http.StatusOK))
	if obj(look[0])["id"] != movieID || obj(look[0])["hasFile"] != false {
		t.Fatalf("lookup after adding: %v", look)
	}
	all := list(arrCall(t, "GET", radarr+"/movie", key, nil, http.StatusOK))
	if len(all) != 1 || obj(all[0])["tmdbId"] != float64(701) || obj(all[0])["monitored"] != true {
		t.Fatalf("movie list: %v", all)
	}
	if tags := getJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/movies/%v", base, movieID))["tags"]; fmt.Sprint(tags) != "[1 - Ann]" {
		t.Fatalf("the tag should be on the movie in Mediarium: %v", tags)
	}

	// Overseerr's PUT carries the id in the body; Ruddarr's in the path.
	put := obj(arrCall(t, "PUT", radarr+"/movie", key, map[string]any{"id": movieID, "monitored": false, "tmdbId": 701}, http.StatusAccepted))
	if put["monitored"] != false {
		t.Fatalf("unmonitor: %v", put)
	}
	put = obj(arrCall(t, "PUT", fmt.Sprintf("%s/movie/%v", radarr, movieID), key, map[string]any{"monitored": true}, http.StatusAccepted))
	if put["monitored"] != true {
		t.Fatalf("monitor again: %v", put)
	}
	arrCall(t, "GET", radarr+"/movie/99999", key, nil, http.StatusNotFound)
	if cmd := obj(arrCall(t, "POST", radarr+"/command", key, map[string]any{"name": "RefreshMonitoredDownloads"}, http.StatusCreated)); cmd["name"] != "RefreshMonitoredDownloads" {
		t.Fatalf("command: %v", cmd)
	}

	// Dashboards: queue, queue status, wanted and calendar.
	q := obj(arrCall(t, "GET", radarr+"/queue?includeEpisode=true", key, nil, http.StatusOK))
	if q["totalRecords"] != float64(0) || q["records"] == nil {
		t.Fatalf("queue: %v", q)
	}
	if qs := obj(arrCall(t, "GET", radarr+"/queue/status", key, nil, http.StatusOK)); qs["totalCount"] != float64(0) {
		t.Fatalf("queue status: %v", qs)
	}
	if d := arrCall(t, "GET", radarr+"/queue/details", key, nil, http.StatusOK); d == nil {
		t.Fatal("queue details should be a list")
	}
	if wm := obj(arrCall(t, "GET", radarr+"/wanted/missing", key, nil, http.StatusOK)); wm["totalRecords"] != float64(1) {
		t.Fatalf("wanted: %v", wm)
	}
	if cal := list(arrCall(t, "GET", radarr+"/calendar?start=2021-05-01&end=2021-05-31", key, nil, http.StatusOK)); len(cal) != 1 {
		t.Fatalf("calendar: %v", cal)
	}

	// A show request by TVDB id, two seasons asked for one.
	sl := list(arrCall(t, "GET", sonarr+"/series/lookup?term=tvdb:777001", key, nil, http.StatusOK))
	if len(sl) != 1 || obj(sl[0])["id"] != nil || obj(sl[0])["tvdbId"] != float64(777001) || len(list(obj(sl[0])["seasons"])) != 2 {
		t.Fatalf("series lookup: %v", sl)
	}
	sr := obj(arrCall(t, "POST", sonarr+"/series", key, map[string]any{
		"tvdbId": 777001, "title": "Salt & Static", "qualityProfileId": profileID, "languageProfileId": 1,
		"seasons": []map[string]any{{"seasonNumber": 1, "monitored": true}, {"seasonNumber": 2, "monitored": false}},
		"tags":    []float64{}, "seasonFolder": true, "monitored": true, "rootFolderPath": "/tv", "seriesType": "standard",
		"addOptions": map[string]any{"ignoreEpisodesWithFiles": true, "searchForMissingEpisodes": false},
	}, http.StatusCreated))
	seriesID := sr["id"].(float64)
	if seriesID == 0 || sr["tvdbId"] != float64(777001) || sr["titleSlug"] != "salt-static" {
		t.Fatalf("added series: %v", sr)
	}
	monitored := map[float64]bool{}
	for _, s := range list(sr["seasons"]) {
		monitored[obj(s)["seasonNumber"].(float64)] = obj(s)["monitored"].(bool)
	}
	if !monitored[1] || monitored[2] {
		t.Fatalf("only season 1 should be monitored: %v", sr["seasons"])
	}
	if got := list(arrCall(t, "GET", sonarr+"/series?tvdbId=777001", key, nil, http.StatusOK)); len(got) != 1 {
		t.Fatalf("series by tvdb id: %v", got)
	}
	// Sonarr v4 apps such as Arrmatey refuse a show without originalLanguage.
	if obj(sr["originalLanguage"])["name"] != "English" || obj(obj(sl[0])["originalLanguage"])["name"] != "English" {
		t.Fatalf("a series needs an originalLanguage: %v / %v", sr["originalLanguage"], obj(sl[0])["originalLanguage"])
	}
	arrCall(t, "POST", sonarr+"/series", key, map[string]any{"tvdbId": 777001}, http.StatusBadRequest)

	// A later request for season 2: Overseerr PUTs the whole show back.
	sr["seasons"] = []map[string]any{{"seasonNumber": 1, "monitored": true}, {"seasonNumber": 2, "monitored": true}}
	updated := obj(arrCall(t, "PUT", sonarr+"/series", key, sr, http.StatusAccepted))
	for _, s := range list(updated["seasons"]) {
		if !obj(s)["monitored"].(bool) {
			t.Fatalf("both seasons should be monitored now: %v", updated["seasons"])
		}
	}
	eps := list(arrCall(t, "GET", fmt.Sprintf("%s/episode?seriesId=%v", sonarr, seriesID), key, nil, http.StatusOK))
	if len(eps) != 3 || obj(eps[0])["airDateUtc"] != "2019-02-01T00:00:00Z" {
		t.Fatalf("episodes: %v", eps)
	}
	// One season: only that season's episodes come back (Maintainerr acts on all of them).
	for season, want := range map[int]int{1: 2, 2: 1, 9: 0} {
		got := list(arrCall(t, "GET", fmt.Sprintf("%s/episode?seriesId=%v&seasonNumber=%d", sonarr, seriesID, season), key, nil, http.StatusOK))
		if len(got) != want {
			t.Fatalf("season %d: %d episodes, want %d: %v", season, len(got), want, got)
		}
		for _, e := range got {
			if obj(e)["seasonNumber"] != float64(season) {
				t.Fatalf("season %d answer holds %v", season, obj(e))
			}
		}
	}
	arrCall(t, "GET", fmt.Sprintf("%s/episode?seriesId=%v&seasonNumber=x", sonarr, seriesID), key, nil, http.StatusBadRequest)
	one := obj(eps[0])["id"].(float64)
	if got := list(arrCall(t, "GET", fmt.Sprintf("%s/episode?seriesId=%v&episodeIds=%v", sonarr, seriesID, one), key, nil, http.StatusOK)); len(got) != 1 || obj(got[0])["id"] != one {
		t.Fatalf("episodeIds filter: %v", got)
	}
	if got := list(arrCall(t, "GET", fmt.Sprintf("%s/episode?episodeIds=%v&includeSeries=true", sonarr, one), key, nil, http.StatusOK)); len(got) != 1 || obj(got[0])["id"] != one || obj(got[0])["series"] == nil {
		t.Fatalf("episodeIds without a show: %v", got)
	}
	if cmd := obj(arrCall(t, "POST", sonarr+"/command", key, map[string]any{"name": "SeriesSearch", "seriesId": seriesID}, http.StatusCreated)); cmd["status"] == nil {
		t.Fatalf("command: %v", cmd)
	}
	// One command may name at most 200 titles.
	many := make([]int64, 201)
	for i := range many {
		many[i] = int64(i + 1)
	}
	arrCall(t, "POST", sonarr+"/command", key, map[string]any{"name": "EpisodeSearch", "episodeIds": many}, http.StatusBadRequest)
	waitBackground(t, server)
	if wm := obj(arrCall(t, "GET", sonarr+"/wanted/missing?includeSeries=true", key, nil, http.StatusOK)); wm["totalRecords"] != float64(3) {
		t.Fatalf("wanted episodes: %v", wm)
	}

	// A season pack downloading: one record per episode, as Sonarr shows it.
	qid, err := server.QueueRepo.Enqueue(queue.Item{SeriesID: int64(seriesID), Season: 1, ReleaseTitle: "Salt.and.Static.S01.1080p.WEB-DL.x264-GRP",
		NZBURL: "http://127.0.0.1:1/x.nzb", SizeBytes: 2000, Protocol: queue.ProtocolUsenet})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.QueueRepo.SetStatus(qid, queue.StatusDownloading, "")
	_ = server.QueueRepo.SetProgress(qid, 50)
	sq := obj(arrCall(t, "GET", sonarr+"/queue?includeEpisode=true&includeSeries=true", key, nil, http.StatusOK))
	recs := list(sq["records"])
	if sq["totalRecords"] != float64(2) || len(recs) != 2 {
		t.Fatalf("pack queue: %v", sq)
	}
	r0 := obj(recs[0])
	if r0["status"] != "downloading" || r0["sizeleft"] != float64(500) || r0["episodeId"] == nil || obj(r0["episode"])["seasonNumber"] != float64(1) ||
		obj(r0["series"])["title"] != "Salt & Static" || obj(obj(r0["quality"])["quality"])["source"] != "web" {
		t.Fatalf("pack record: %v", r0)
	}
	if obj(recs[0])["id"] == obj(recs[1])["id"] {
		t.Fatal("each record needs its own id")
	}
	if qs := obj(arrCall(t, "GET", sonarr+"/queue/status", key, nil, http.StatusOK)); qs["totalCount"] != float64(2) {
		t.Fatalf("sonarr queue status: %v", qs)
	}
	if rq := obj(arrCall(t, "GET", radarr+"/queue", key, nil, http.StatusOK)); rq["totalRecords"] != float64(0) {
		t.Fatalf("a TV download isn't Radarr's: %v", rq)
	}
	arrCall(t, "DELETE", fmt.Sprintf("%s/queue/%v", sonarr, obj(recs[1])["id"]), key, nil, http.StatusOK)
	if sq := obj(arrCall(t, "GET", sonarr+"/queue", key, nil, http.StatusOK)); sq["totalRecords"] != float64(0) {
		t.Fatalf("after removing: %v", sq)
	}

	// Removing through the API.
	arrCall(t, "DELETE", fmt.Sprintf("%s/movie/%v?deleteFiles=false&addImportExclusion=false", radarr, movieID), key, nil, http.StatusOK)
	arrCall(t, "GET", fmt.Sprintf("%s/movie/%v", radarr, movieID), key, nil, http.StatusNotFound)
}

func TestArrAPIOffWithItsModule(t *testing.T) {
	_, base, admin, _, _ := familyServer(t)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "dash"}, http.StatusCreated)["key"].(string)
	// Music is off until switched on: Lidarr answers 404.
	arrCall(t, "GET", base+"/lidarr/api/v1/system/status", key, nil, http.StatusNotFound)
	putJSONStatus(t, admin, base+"/api/modules", map[string]any{"music": true}, http.StatusOK)
	if st := obj(arrCall(t, "GET", base+"/lidarr/api/v1/system/status", key, nil, http.StatusOK)); st["appName"] != "Lidarr" {
		t.Fatalf("lidarr status: %v", st)
	}
	if a := list(arrCall(t, "GET", base+"/lidarr/api/v1/artist", key, nil, http.StatusOK)); len(a) != 0 {
		t.Fatalf("artists: %v", a)
	}
	if mp := list(arrCall(t, "GET", base+"/lidarr/api/v1/metadataprofile", key, nil, http.StatusOK)); len(mp) != 1 {
		t.Fatalf("metadata profiles: %v", mp)
	}
}
