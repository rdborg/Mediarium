package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
)

// listsMovies are the movies the fake TMDB knows: id -> title, release date, vote average.
var listsMovies = map[int][3]string{
	100: {"Alpha", "2010-05-01", "8.1"},
	101: {"Bravo", "1995-01-01", "8.0"},   // too old for a minimum year of 2000
	102: {"Charlie", "2020-01-01", "5.0"}, // below a minimum rating of 7
	103: {"Delta", "2022-01-01", "7.5"},
	104: {"Echo", "2023-06-01", "0"}, // nobody rated it yet
	105: {"Foxtrot", "2021-03-03", "9.0"},
}

// fakeListsTMDB is a TMDB with those movies, one show (200), a public list (555),
// the IMDb lookup, a title search and a few charts.
func fakeListsTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	movie := func(id int) map[string]any {
		m := listsMovies[id]
		v, _ := strconv.ParseFloat(m[2], 64)
		return map[string]any{"id": id, "title": m[0], "release_date": m[1], "vote_average": v, "poster_path": "/p.jpg", "overview": "x"}
	}
	show := map[string]any{"id": 200, "name": "Show One", "first_air_date": "2015-02-01", "vote_average": 8.5,
		"seasons":      []map[string]any{{"season_number": 1, "episode_count": 1}},
		"external_ids": map[string]any{"tvdb_id": 5000, "imdb_id": "tt0000200"}}
	page := func(ids ...int) map[string]any {
		var res []map[string]any
		for _, id := range ids {
			res = append(res, movie(id))
		}
		return map[string]any{"page": 1, "total_pages": 1, "total_results": len(res), "results": res}
	}
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/movie/top_rated", func(w http.ResponseWriter, r *http.Request) { reply(w, page(105, 100)) })
	mux.HandleFunc("/movie/popular", func(w http.ResponseWriter, r *http.Request) { reply(w, page(103)) })
	mux.HandleFunc("/trending/movie/week", func(w http.ResponseWriter, r *http.Request) { reply(w, page(100, 105)) })
	mux.HandleFunc("/discover/movie", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("with_original_language") == "ko" && r.URL.Query().Get("sort_by") == "vote_average.desc" {
			reply(w, page(103))
			return
		}
		reply(w, page(105))
	})
	mux.HandleFunc("/movie/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		if _, ok := listsMovies[id]; !ok {
			http.NotFound(w, r)
			return
		}
		reply(w, movie(id))
	})
	mux.HandleFunc("/tv/200", func(w http.ResponseWriter, r *http.Request) { reply(w, show) })
	mux.HandleFunc("/tv/200/season/1", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"episodes": []map[string]any{{"season_number": 1, "episode_number": 1, "name": "Pilot", "air_date": "2015-02-01"}}})
	})
	mux.HandleFunc("/list/555", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"item_count": 6, "total_pages": 1, "items": []map[string]any{
			{"id": 100, "media_type": "movie", "title": "Alpha", "release_date": "2010-05-01", "vote_average": 8.1},
			{"id": 101, "media_type": "movie", "title": "Bravo", "release_date": "1995-01-01", "vote_average": 8.0},
			{"id": 102, "media_type": "movie", "title": "Charlie", "release_date": "2020-01-01", "vote_average": 5.0},
			{"id": 103, "media_type": "movie", "title": "Delta", "release_date": "2022-01-01", "vote_average": 7.5},
			{"id": 104, "media_type": "movie", "title": "Echo", "release_date": "2023-06-01", "vote_average": 0},
			{"id": 200, "media_type": "tv", "name": "Show One", "first_air_date": "2015-02-01", "vote_average": 8.5},
		}})
	})
	mux.HandleFunc("/find/{imdb}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("imdb") {
		case "tt0000100":
			reply(w, map[string]any{"movie_results": []map[string]any{{"id": 100}}, "tv_results": []any{}})
		case "tt0000103":
			reply(w, map[string]any{"movie_results": []map[string]any{{"id": 103}}, "tv_results": []any{}})
		case "5000": // TheTVDB id of Show One
			reply(w, map[string]any{"movie_results": []any{}, "tv_results": []map[string]any{{"id": 200}}})
		case "tt0000200":
			reply(w, map[string]any{"movie_results": []any{}, "tv_results": []map[string]any{{"id": 200}}})
		default:
			reply(w, map[string]any{"movie_results": []any{}, "tv_results": []any{}})
		}
	})
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("query") {
		case "Delta":
			reply(w, map[string]any{"results": []map[string]any{{"id": 999, "title": "Delta Force", "release_date": "1986-01-01"}, movie(103)}})
		default:
			reply(w, map[string]any{"results": []any{}})
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func listsServer(t *testing.T) (*api.Server, string, *http.Client) {
	t.Helper()
	server, base, admin := loginNewServer(t)
	server.TestListsNoPause()
	server.TestSetTMDBBaseURL("fixture-tmdb-key", fakeListsTMDB(t).URL)
	return server, base, admin
}

// readLists returns the list with that id from GET /api/import-lists.
func readList(t *testing.T, client *http.Client, base string, id float64) map[string]any {
	t.Helper()
	for _, l := range getJSON[map[string]any](t, client, base+"/api/import-lists")["lists"].([]any) {
		if m := l.(map[string]any); m["id"] == id {
			return m
		}
	}
	t.Fatalf("list %v not found", id)
	return nil
}

func runList(t *testing.T, server *api.Server, client *http.Client, base string, id float64) map[string]any {
	t.Helper()
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/import-lists/%v/run", base, id), nil, http.StatusAccepted)
	waitBackground(t, server)
	return readList(t, client, base, id)
}

func libraryTMDBIDs(t *testing.T, client *http.Client, base, path string) map[int]bool {
	t.Helper()
	out := map[int]bool{}
	for _, m := range getJSON[[]map[string]any](t, client, base+path) {
		out[int(m["tmdbId"].(float64))] = true
	}
	return out
}

func TestImportListFromATMDBListWithFilters(t *testing.T) {
	server, base, admin := listsServer(t)

	// Alpha is in the library already; Delta is on the Not interested list.
	postJSON[map[string]any](t, admin, base+"/api/movies", map[string]any{"tmdbId": 100}, http.StatusCreated)
	postJSON[map[string]any](t, admin, base+"/api/exclusions", map[string]any{"kind": "movie", "tmdbId": 103, "title": "Delta"}, http.StatusOK)

	created := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"name": "Friends' picks", "source": "tmdb-list", "media": "both", "config": map[string]any{"url": "https://www.themoviedb.org/list/555-friends-picks"},
		"intervalHours": 24, "minRating": 7, "minYear": 2000, "maxPerRun": 25, "tags": []string{"From a list"}, "searchNow": false,
	}, http.StatusCreated)
	id := created["id"].(float64)
	waitBackground(t, server)

	l := readList(t, admin, base, id)
	if l["lastStatus"] != "ok" || l["lastFound"] != float64(6) || l["lastAdded"] != float64(1) || l["lastSkipped"] != float64(5) {
		t.Fatalf("first read: %v", l)
	}
	if got := libraryTMDBIDs(t, admin, base, "/api/series"); !got[200] {
		t.Fatalf("the show wasn't added: %v", got)
	}
	// Only Alpha (already there) is in the movie library: Bravo is too old,
	// Charlie and Echo don't reach the rating, Delta is excluded.
	if got := libraryTMDBIDs(t, admin, base, "/api/movies"); len(got) != 1 || !got[100] {
		t.Fatalf("movies in the library: %v", got)
	}
	tags := getJSON[[]map[string]any](t, admin, base+"/api/tags")
	if len(tags) != 1 || tags[0]["name"] != "From a list" {
		t.Fatalf("tags: %v", tags)
	}
	if msg := l["lastMessage"].(string); !strings.Contains(msg, "Found 6 titles") || !strings.Contains(msg, "Added 1") || !strings.Contains(msg, "on your Not interested list") {
		t.Fatalf("message: %q", msg)
	}

	// Reading again changes nothing.
	l = runList(t, server, admin, base, id)
	if l["lastAdded"] != float64(0) || l["totalAdded"] != float64(1) {
		t.Fatalf("second read: %v", l)
	}

	// Taking Delta off the Not interested list lets the next read add it.
	postJSONMethod[map[string]any](t, admin, http.MethodDelete, base+"/api/exclusions/movie/103", nil, http.StatusOK)
	l = runList(t, server, admin, base, id)
	if l["lastAdded"] != float64(1) || !libraryTMDBIDs(t, admin, base, "/api/movies")[103] {
		t.Fatalf("after the exclusion was lifted: %v", l)
	}

	// A title the person removed isn't added again, and nothing is ever
	// removed because it left the list.
	series := getJSON[[]map[string]any](t, admin, base+"/api/series")
	postJSONMethod[map[string]any](t, admin, http.MethodDelete, fmt.Sprintf("%s/api/series/%v", base, series[0]["id"]), nil, http.StatusOK)
	l = runList(t, server, admin, base, id)
	if l["lastAdded"] != float64(0) || libraryTMDBIDs(t, admin, base, "/api/series")[200] {
		t.Fatalf("a removed show came back: %v", l)
	}
	// "Forget" starts the list over, and the add limit spreads a big list out.
	postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/import-lists/%v/forget", base, id), nil, http.StatusOK)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, fmt.Sprintf("%s/api/import-lists/%v", base, id), map[string]any{
		"name": "Friends' picks", "source": "tmdb-list", "media": "both", "config": map[string]any{"url": "555"},
		"intervalHours": 24, "maxPerRun": 1, "searchNow": false,
	}, http.StatusOK)
	l = runList(t, server, admin, base, id)
	if l["lastAdded"] != float64(1) || !strings.Contains(l["lastMessage"].(string), "more wait for the next read") {
		t.Fatalf("with a limit of one: %v", l)
	}
	if n := len(getJSON[[]map[string]any](t, admin, base+"/api/movies")); n != 3 {
		t.Fatalf("expected Alpha, Delta and one more movie, got %d", n)
	}
}

func TestImportListSwitchAndUnknownList(t *testing.T) {
	server, base, admin := listsServer(t)
	created := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "tmdb-chart", "media": "movie", "config": map[string]any{"chart": "top-rated", "count": 20}, "intervalHours": 6, "enabled": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	id := created["id"].(float64)
	l := readList(t, admin, base, id)
	if l["enabled"] != false || l["name"] != "TMDB trending, popular, top rated or upcoming" || l["lastRunAt"] != nil && l["lastRunAt"] != "" {
		t.Fatalf("a list that is off must not be read when saved: %v", l)
	}
	on := postJSONMethod[map[string]any](t, admin, http.MethodPut, fmt.Sprintf("%s/api/import-lists/%v/enabled", base, id), map[string]any{"enabled": true}, http.StatusOK)
	if on["enabled"] != true {
		t.Fatalf("switch: %v", on)
	}
	// The scheduled job reads it now: it was never read, so it is due.
	server.TestImportListsJob(t.Context())
	waitBackground(t, server)
	l = readList(t, admin, base, id)
	if l["lastStatus"] != "ok" || l["lastAdded"] != float64(2) {
		t.Fatalf("after the job: %v", l)
	}
	if got := libraryTMDBIDs(t, admin, base, "/api/movies"); !got[105] || !got[100] {
		t.Fatalf("top rated movies: %v", got)
	}
	// Not due again for another six hours.
	server.TestImportListsJob(t.Context())
	waitBackground(t, server)

	postJSONMethod[map[string]any](t, admin, http.MethodDelete, fmt.Sprintf("%s/api/import-lists/%v", base, id), nil, http.StatusNoContent)
	if n := len(getJSON[map[string]any](t, admin, base+"/api/import-lists")["lists"].([]any)); n != 0 {
		t.Fatalf("list not removed: %d", n)
	}
	if got := libraryTMDBIDs(t, admin, base, "/api/movies"); !got[105] {
		t.Fatalf("removing a list took titles out of the library: %v", got)
	}
	postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/import-lists/%v/run", base, id), nil, http.StatusNotFound)
}

func TestImportListTMDBChartFilters(t *testing.T) {
	server, base, admin := listsServer(t)
	// Top rated Korean titles use TMDB's browse; trending can't be narrowed.
	postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "tmdb-chart", "media": "movie", "config": map[string]any{"chart": "trending", "language": "ko"}, "intervalHours": 24,
	}, http.StatusBadRequest)
	created := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "tmdb-chart", "media": "movie", "config": map[string]any{"chart": "top-rated", "language": "KO", "count": 20}, "intervalHours": 24, "searchNow": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	l := readList(t, admin, base, created["id"].(float64))
	if l["lastAdded"] != float64(1) || !libraryTMDBIDs(t, admin, base, "/api/movies")[103] {
		t.Fatalf("filtered chart: %v", l)
	}
	if cfg := l["config"].(map[string]any); cfg["language"] != "ko" {
		t.Fatalf("language should be saved in lower case: %v", cfg)
	}
}

func TestImportListFromCSVFiles(t *testing.T) {
	server, base, admin := listsServer(t)
	imdb := "Position,Const,Title,Title Type,Year\n1,tt0000100,Alpha,Movie,2010\n2,tt0000200,Show One,TV Series,2015\n3,tt0000999,Unknown,Movie,1999\n4,tt0000300,An Episode,TV Episode,2001\n"
	created := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "imdb", "media": "both", "config": map[string]any{"csv": imdb}, "intervalHours": 24, "searchNow": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	l := readList(t, admin, base, created["id"].(float64))
	if l["lastAdded"] != float64(2) || l["lastFound"] != float64(3) || l["hasCsv"] != true {
		t.Fatalf("imdb list: %v", l)
	}
	if cfg := l["config"].(map[string]any); cfg["csv"] != nil {
		t.Fatalf("the file text must not come back: %v", cfg)
	}
	if !strings.Contains(l["lastMessage"].(string), "not found on TMDB") {
		t.Fatalf("message: %v", l["lastMessage"])
	}
	// An edit that doesn't send the file keeps it.
	postJSONMethod[map[string]any](t, admin, http.MethodPut, fmt.Sprintf("%s/api/import-lists/%v", base, created["id"]), map[string]any{
		"source": "imdb", "media": "both", "config": map[string]any{}, "intervalHours": 12, "name": "Renamed",
	}, http.StatusOK)
	if l = readList(t, admin, base, created["id"].(float64)); l["hasCsv"] != true || l["intervalHours"] != float64(12) || l["name"] != "Renamed" {
		t.Fatalf("after the edit: %v", l)
	}

	// Letterboxd names titles only: they are searched for. Movies only.
	lb := "Date,Name,Year,Letterboxd URI\n2024-01-01,Delta,2022,https://boxd.it/a\n2024-01-02,Not A Real Film,1999,https://boxd.it/b\n"
	created = postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "letterboxd", "media": "both", "config": map[string]any{"csv": lb}, "intervalHours": 24, "searchNow": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	l = readList(t, admin, base, created["id"].(float64))
	if l["media"] != "movie" || l["lastAdded"] != float64(1) || !libraryTMDBIDs(t, admin, base, "/api/movies")[103] {
		t.Fatalf("letterboxd list: %v", l)
	}

	// An address that answers with a bot check is explained, not retried blindly.
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-amzn-waf-action", "challenge")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer blocked.Close()
	created = postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "imdb", "media": "movie", "config": map[string]any{"url": blocked.URL + "/export"}, "intervalHours": 24,
	}, http.StatusCreated)
	waitBackground(t, server)
	l = readList(t, admin, base, created["id"].(float64))
	if l["lastStatus"] != "error" || !strings.Contains(l["lastMessage"].(string), "only lets people in with a browser") {
		t.Fatalf("blocked address: %v", l)
	}
}

func TestImportListFromMDBList(t *testing.T) {
	server, base, admin := listsServer(t)
	var mu sync.Mutex
	var gotKey, gotPath string
	mdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotKey, gotPath = r.URL.Query().Get("apikey"), r.URL.Path
		mu.Unlock()
		_, _ = w.Write([]byte(`{"movies":[{"id":103,"title":"Delta","imdb_id":"tt0000103","mediatype":"movie","release_year":2022}],"shows":[{"id":200,"title":"Show One","imdb_id":"tt0000200","mediatype":"show","release_year":2015}]}`))
	}))
	defer mdb.Close()
	server.TestSetMDBListBase(mdb.URL)

	postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "mdblist", "media": "both", "config": map[string]any{"url": "https://mdblist.com/lists/someone/the-list"}, "intervalHours": 24,
	}, http.StatusBadRequest) // no key
	created := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "mdblist", "media": "movie", "config": map[string]any{"url": "https://mdblist.com/lists/someone/the-list"}, "key": "fixture-mdblist-key", "intervalHours": 24, "searchNow": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	id := created["id"].(float64)
	l := readList(t, admin, base, id)
	mu.Lock()
	defer mu.Unlock()
	if gotKey != "fixture-mdblist-key" || gotPath != "/lists/someone/the-list/items" {
		t.Fatalf("asked MDBList for %q with key %q", gotPath, gotKey)
	}
	// Movies only: the show on the list is left out.
	if l["hasKey"] != true || l["lastFound"] != float64(1) || l["lastAdded"] != float64(1) || len(libraryTMDBIDs(t, admin, base, "/api/series")) != 0 {
		t.Fatalf("mdblist list: %v", l)
	}
	raw, _ := json.Marshal(getJSON[map[string]any](t, admin, base+"/api/import-lists"))
	if strings.Contains(string(raw), "fixture-mdblist-key") {
		t.Fatal("the API key came back from the list of lists")
	}
}

func TestImportListFromTrakt(t *testing.T) {
	server, base, admin := listsServer(t)
	var mu sync.Mutex
	var paths []string
	tr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		switch r.URL.Path {
		case "/users/someone/lists/picks/items":
			_, _ = w.Write([]byte(`[{"type":"movie","movie":{"title":"Foxtrot","year":2021,"ids":{"tmdb":105}}},{"type":"show","show":{"title":"Show One","year":2015,"ids":{"tmdb":200}}}]`))
		case "/movies/trending":
			_, _ = w.Write([]byte(`[{"watchers":5,"movie":{"title":"Alpha","year":2010,"ids":{"tmdb":100}}}]`))
		case "/users/hidden/watchlist":
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer tr.Close()
	server.TestSetTraktBaseURL("fixture-client-id", tr.URL)

	mk := func(body map[string]any, want int) map[string]any {
		body["intervalHours"], body["searchNow"] = 24, false
		return postJSON[map[string]any](t, admin, base+"/api/import-lists", body, want)
	}
	list := mk(map[string]any{"source": "trakt-list", "media": "both", "config": map[string]any{"url": "https://trakt.tv/users/someone/lists/picks"}}, http.StatusCreated)
	chart := mk(map[string]any{"source": "trakt-chart", "media": "movie", "config": map[string]any{"chart": "trending", "count": 10}}, http.StatusCreated)
	hidden := mk(map[string]any{"source": "trakt-watchlist", "media": "both", "config": map[string]any{"url": "hidden"}}, http.StatusCreated)
	waitBackground(t, server)

	if l := readList(t, admin, base, list["id"].(float64)); l["lastAdded"] != float64(2) {
		t.Fatalf("trakt list: %v", l)
	}
	if l := readList(t, admin, base, chart["id"].(float64)); l["lastAdded"] != float64(1) {
		t.Fatalf("trakt chart: %v", l)
	}
	if l := readList(t, admin, base, hidden["id"].(float64)); l["lastStatus"] != "error" || !strings.Contains(l["lastMessage"].(string), "private") {
		t.Fatalf("private watchlist: %v", l)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(paths, " "), "/movies/trending?limit=10&page=1") {
		t.Fatalf("calls: %v", paths)
	}
	mk(map[string]any{"source": "trakt-chart", "media": "movie", "config": map[string]any{"chart": "best"}}, http.StatusBadRequest)
}

func TestImportListInputIsChecked(t *testing.T) {
	_, base, admin := listsServer(t)
	good := func() map[string]any {
		return map[string]any{"source": "tmdb-list", "media": "both", "config": map[string]any{"url": "555"}, "intervalHours": 24}
	}
	tests := []struct {
		name   string
		change func(m map[string]any)
		want   string
	}{
		{"no source", func(m map[string]any) { m["source"] = "" }, "where the list comes from"},
		{"unknown source", func(m map[string]any) { m["source"] = "rottentomatoes" }, "where the list comes from"},
		{"bad media", func(m map[string]any) { m["media"] = "music" }, "movies, shows or both"},
		{"no interval", func(m map[string]any) { delete(m, "intervalHours") }, "how often"},
		{"too long between reads", func(m map[string]any) { m["intervalHours"] = 500 }, "how often"},
		{"rating over ten", func(m map[string]any) { m["minRating"] = 11 }, "0 to 10"},
		{"silly year", func(m map[string]any) { m["minYear"] = 12 }, "isn't a year"},
		{"too many at a time", func(m map[string]any) { m["maxPerRun"] = 500 }, "between 1 and 100"},
		{"missing profile", func(m map[string]any) { m["profileId"] = 9999 }, "profile that exists"},
		{"folder that isn't one of yours", func(m map[string]any) { m["movieRoot"] = "/nowhere/else" }, "folders"},
		{"not a TMDB list", func(m map[string]any) { m["config"] = map[string]any{"url": "https://example.com/list/5"} }, "isn't a TMDB list"},
		{"not a trakt address", func(m map[string]any) {
			m["source"] = "trakt-list"
			m["config"] = map[string]any{"url": "https://example.com/x"}
		}, "Trakt list address"},
		{"imdb with nothing", func(m map[string]any) { m["source"] = "imdb"; m["config"] = map[string]any{} }, "Upload the exported CSV"},
		{"imdb watchlist address", func(m map[string]any) {
			m["source"] = "imdb"
			m["config"] = map[string]any{"url": "https://www.imdb.com/user/ur1/watchlist"}
		}, "watchlist"},
		{"csv that isn't a list", func(m map[string]any) { m["source"] = "imdb"; m["config"] = map[string]any{"csv": "a,b\n1,2\n"} }, "IMDb or Letterboxd list"},
		{"mdblist address", func(m map[string]any) {
			m["source"] = "mdblist"
			m["key"] = "k"
			m["config"] = map[string]any{"url": "nope"}
		}, "MDBList address"},
		{"chart count", func(m map[string]any) {
			m["source"] = "tmdb-chart"
			m["config"] = map[string]any{"chart": "popular", "count": 999}
		}, "200 titles"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := good()
			tc.change(body)
			res := postJSON[map[string]any](t, admin, base+"/api/import-lists", body, http.StatusBadRequest)
			if msg, _ := res["error"].(string); !strings.Contains(msg, tc.want) {
				t.Fatalf("error %q should mention %q", msg, tc.want)
			}
		})
	}
}

func TestImportListPreviewAndAccess(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	// familyServer builds its own server; give it the fake TMDB through admin's settings route is not possible,
	// so the preview here is of an uploaded file (no outside service involved).
	res := postJSON[map[string]any](t, admin, base+"/api/import-lists/preview", map[string]any{
		"source": "letterboxd", "media": "movie", "intervalHours": 24,
		"config": map[string]any{"csv": "Name,Year\nAlien,1979\nAliens,1986\n"},
	}, http.StatusOK)
	if res["found"] != float64(2) || len(res["sample"].([]any)) != 2 {
		t.Fatalf("preview: %v", res)
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/import-lists"}, {http.MethodPost, "/api/import-lists"}, {http.MethodPost, "/api/import-lists/preview"},
		{http.MethodPut, "/api/import-lists/1"}, {http.MethodDelete, "/api/import-lists/1"}, {http.MethodPost, "/api/import-lists/1/run"},
	} {
		if status, _ := doStatus(t, member, c.method, base+c.path); status != http.StatusForbidden {
			t.Errorf("%s %s as a member: %d", c.method, c.path, status)
		}
	}
}

func TestPlexWatchlistsShowInTheListView(t *testing.T) {
	server, base, admin := listsServer(t)
	plex := newFakePlexWatchlist(t)
	server.TestSetPlexTVURL(plex.URL)
	server.TestSetPlexDiscoverURL(plex.URL)
	if got := getJSON[map[string]any](t, admin, base+"/api/import-lists")["plexWatchlists"].([]any); len(got) != 0 {
		t.Fatalf("no watchlist is connected yet: %v", got)
	}
	connectWatchlist(t, admin, base)
	plex.put("new1", "movie", "Foxtrot", 2021, "tmdb://105")
	got := getJSON[map[string]any](t, admin, base+"/api/import-lists")["plexWatchlists"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["account"] != "Ryan Tester" || got[0].(map[string]any)["plexName"] != "plexfan" {
		t.Fatalf("plex watchlists: %v", got)
	}
	uid := got[0].(map[string]any)["userId"]
	run := postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/import-lists/plex/%v/run", base, uid), nil, http.StatusOK)
	if run["added"] != float64(1) || !libraryTMDBIDs(t, admin, base, "/api/movies")[105] {
		t.Fatalf("run now: %v", run)
	}
	postJSON[map[string]any](t, admin, base+"/api/import-lists/plex/9999/run", nil, http.StatusNotFound)
}

// An RSS feed list adds the titles its entries name, by id or by name, and
// with "also download" on it grabs a wanted release from the feed (#160).
func TestImportListFromAnRSSFeed(t *testing.T) {
	server, base, admin := listsServer(t)
	var feedURL string
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".nzb") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>
<item><title>Alpha.2010.1080p.BluRay.x264-GRP</title><guid>a</guid><enclosure url="%[1]s/a.nzb" length="8000000000" type="application/x-nzb"/><newznab:attr name="imdb" value="0000100"/></item>
<item><title>Delta.2022.720p.WEB-DL.x264-GRP</title><guid>d</guid></item>
<item><title>Show.One.S01E01.1080p.WEB.h264-GRP</title><guid>s</guid><newznab:attr name="tvdbid" value="5000"/></item>
</channel></rss>`, feedURL)
	}))
	t.Cleanup(feed.Close)
	feedURL = feed.URL

	created := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "rss", "media": "both", "config": map[string]any{"url": feed.URL + "/rss?apikey=K", "grab": true}, "intervalHours": 24, "searchNow": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	l := readList(t, admin, base, created["id"].(float64))
	if l["lastAdded"] != float64(3) {
		t.Fatalf("rss list: %v", l)
	}
	if got := libraryTMDBIDs(t, admin, base, "/api/movies"); !got[100] || !got[103] {
		t.Errorf("movies from the feed: %v", got)
	}
	if got := libraryTMDBIDs(t, admin, base, "/api/series"); !got[200] {
		t.Errorf("show from the feed: %v", got)
	}
	queue := getJSON[[]map[string]any](t, admin, base+"/api/queue")
	grabbed := false
	for _, q := range queue {
		if q["releaseTitle"] == "Alpha.2010.1080p.BluRay.x264-GRP" {
			grabbed = true
		}
	}
	if !grabbed {
		t.Errorf("the wanted release from the feed was not grabbed: %v (list: %v)", queue, l["lastMessage"])
	}

	// A web page is not a feed.
	postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "rss", "media": "both", "config": map[string]any{"url": "ftp://nope"}, "intervalHours": 24,
	}, http.StatusBadRequest)
}
