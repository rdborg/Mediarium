package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// newPreviewTMDB serves the appended detail answers the preview pages need for
// one movie and one show, and counts how often each was asked for.
func newPreviewTMDB(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/genre/movie/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"genres":[]}`))
	})
	mux.HandleFunc("/genre/tv/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"genres":[]}`))
	})
	mux.HandleFunc("/movie/597", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.Contains(r.URL.Query().Get("append_to_response"), "credits") {
			t.Errorf("the movie detail must ask for credits: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"id":597,"title":"Titanic","release_date":"1997-11-18","overview":"A seventeen-year-old aristocrat falls in love.",
			"runtime":194,"tagline":"Nothing on Earth could come between them.","status":"Released","imdb_id":"tt0120338",
			"genres":[{"id":18,"name":"Drama"},{"id":10749,"name":"Romance"}],
			"release_dates":{"results":[{"iso_3166_1":"US","release_dates":[{"certification":"PG-13"}]}]},
			"videos":{"results":[
				{"name":"Teaser","site":"YouTube","key":"TE1","type":"Teaser","official":true},
				{"name":"Trailer A","site":"YouTube","key":"TR1","type":"Trailer","official":true},
				{"name":"Trailer B","site":"YouTube","key":"TR2","type":"Trailer","official":true},
				{"name":"On Vimeo","site":"Vimeo","key":"V1","type":"Trailer","official":true}]},
			"credits":{"cast":[
				{"name":"Kate Winslet","character":"Rose","order":1,"profile_path":"/kate.jpg"},
				{"name":"Leonardo DiCaprio","character":"Jack","order":0,"profile_path":"/leo.jpg"}],
			"crew":[
				{"name":"James Cameron","job":"Director"},
				{"name":"James Cameron","job":"Director"},
				{"name":"James Cameron","job":"Writer"},
				{"name":"Russell Carpenter","job":"Director of Photography"}]}}`))
	})
	mux.HandleFunc("/tv/95396", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"id":95396,"name":"Severance","first_air_date":"2022-02-17","overview":"Mark leads a team.",
			"status":"Returning Series","tagline":"","number_of_seasons":2,"number_of_episodes":19,
			"created_by":[{"name":"Dan Erickson"},{"name":""}],
			"external_ids":{"imdb_id":"tt11280740"},
			"genres":[{"id":18,"name":"Drama"}],"networks":[{"name":"Apple TV+"}],
			"seasons":[
				{"season_number":0,"name":"Specials","episode_count":3},
				{"season_number":1,"name":"Season 1","episode_count":9,"air_date":"2022-02-18"},
				{"season_number":2,"name":"","episode_count":10,"air_date":"2025-01-17"},
				{"season_number":3,"name":"Season 3","episode_count":0}],
			"content_ratings":{"results":[{"iso_3166_1":"US","rating":"TV-MA"}]},
			"videos":{"results":[{"name":"Official Trailer","site":"YouTube","key":"S1","type":"Trailer","official":true}]},
			"credits":{"cast":[{"name":"Adam Scott","character":"Mark","order":0,"profile_path":"/adam.jpg"}]}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestMoviePreviewHasEverythingTheInfoViewShows(t *testing.T) {
	server, base, client := loginNewServer(t)
	tmdb, hits := newPreviewTMDB(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	m := getJSON[map[string]any](t, client, base+"/api/tmdb/movies/597")
	if m["title"] != "Titanic" || m["year"] != float64(1997) || m["runtime"] != float64(194) || m["certification"] != "PG-13" ||
		m["tagline"] != "Nothing on Earth could come between them." || m["overview"] == "" || m["imdbId"] != "tt0120338" {
		t.Fatalf("basics: %+v", m)
	}
	if got := strings.Join(toStrings(m["genres"]), ","); got != "Drama,Romance" {
		t.Errorf("genres %q", got)
	}
	if got := strings.Join(toStrings(m["directors"]), ","); got != "James Cameron" {
		t.Errorf("directors %q, want the director once and only the director", got)
	}

	cast := m["cast"].([]any)
	if len(cast) != 2 || cast[0].(map[string]any)["name"] != "Leonardo DiCaprio" || cast[0].(map[string]any)["profileUrl"] != "https://image.tmdb.org/t/p/w185/leo.jpg" {
		t.Errorf("cast should be in billing order with photos: %+v", cast)
	}

	trailers := m["trailers"].([]any)
	var keys []string
	for _, tr := range trailers {
		keys = append(keys, tr.(map[string]any)["key"].(string))
	}
	if strings.Join(keys, ",") != "TR1,TR2,TE1" {
		t.Errorf("trailers %v: YouTube only, trailers before teasers", keys)
	}
	if m["libraryId"] != nil || m["status"] != nil {
		t.Errorf("a movie outside the library has no library state: %+v", m)
	}

	// Asking again, as the open page does every few seconds, does not go back to TMDB.
	for i := 0; i < 3; i++ {
		getJSON[map[string]any](t, client, base+"/api/tmdb/movies/597")
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("TMDB was asked %d times for one movie, want 1", n)
	}
}

func TestShowPreviewHasSeasonsAndCreators(t *testing.T) {
	server, base, client := loginNewServer(t)
	tmdb, hits := newPreviewTMDB(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	tv := getJSON[map[string]any](t, client, base+"/api/tmdb/tv/95396")
	if tv["title"] != "Severance" || tv["seasons"] != float64(2) || tv["episodes"] != float64(19) || tv["contentRating"] != "TV-MA" || tv["imdbId"] != "tt11280740" {
		t.Fatalf("basics: %+v", tv)
	}
	if got := strings.Join(toStrings(tv["creators"]), ","); got != "Dan Erickson" {
		t.Errorf("creators %q", got)
	}
	list := tv["seasonList"].([]any)
	if len(list) != 2 {
		t.Fatalf("seasons %+v: no specials, no empty seasons", list)
	}
	first, second := list[0].(map[string]any), list[1].(map[string]any)
	if first["number"] != float64(1) || first["name"] != "Season 1" || first["episodes"] != float64(9) || first["airDate"] != "2022-02-18" {
		t.Errorf("first season %+v", first)
	}
	if second["name"] != "Season 2" || second["episodes"] != float64(10) {
		t.Errorf("a season without a name is called Season N: %+v", second)
	}
	if len(tv["cast"].([]any)) != 1 || len(tv["trailers"].([]any)) != 1 {
		t.Errorf("cast or trailers missing: %+v", tv)
	}

	getJSON[map[string]any](t, client, base+"/api/tmdb/tv/95396")
	if n := hits.Load(); n != 1 {
		t.Errorf("TMDB was asked %d times for one show, want 1", n)
	}
}

func TestPreviewWithoutAKeyExplainsWhat(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("", "http://127.0.0.1:1")
	for _, path := range []string{"/api/tmdb/movies/597", "/api/tmdb/tv/95396"} {
		status, res := doJSONStatus(t, client, http.MethodGet, base+path, nil)
		if status != http.StatusPreconditionFailed || !strings.Contains(res["error"].(string), "TMDB API key") {
			t.Errorf("%s: %d %+v", path, status, res)
		}
	}
}
