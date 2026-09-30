package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// newRichTMDB serves genre tables, list endpoints carrying genre_ids and vote
// data, and the appended detail responses for one movie and one show.
func newRichTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	reply := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/genre/movie/list", reply(`{"genres":[{"id":28,"name":"Action"},{"id":878,"name":"Science Fiction"}]}`))
	mux.HandleFunc("/genre/tv/list", reply(`{"genres":[{"id":18,"name":"Drama"},{"id":80,"name":"Crime"}]}`))
	mux.HandleFunc("/trending/movie/week", reply(`{"results":[{"id":603,"title":"The Matrix","release_date":"1999-03-31","vote_average":8.2654,"vote_count":25000,"genre_ids":[28,878,7777]}]}`))
	mux.HandleFunc("/tv/popular", reply(`{"results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20","vote_average":8.9,"vote_count":12000,"genre_ids":[18,80]}]}`))
	mux.HandleFunc("/search/multi", reply(`{"results":[
		{"media_type":"movie","id":603,"title":"The Matrix","release_date":"1999-03-31","vote_average":8.2,"vote_count":10,"genre_ids":[28]},
		{"media_type":"tv","id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20","vote_average":8.9,"vote_count":20,"genre_ids":[18]}]}`))
	mux.HandleFunc("/movie/603", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("append_to_response") == "" {
			json.NewEncoder(w).Encode(map[string]any{"id": 603, "title": "The Matrix", "release_date": "1999-03-31"})
			return
		}
		w.Write([]byte(`{"id":603,"title":"The Matrix","release_date":"1999-03-31","overview":"o","vote_average":8.2654,"vote_count":25000,
			"runtime":136,"tagline":"Welcome","status":"Released","original_language":"en","homepage":"https://x.test","imdb_id":"tt0133093",
			"genres":[{"id":28,"name":"Action"}],
			"release_dates":{"results":[{"iso_3166_1":"US","release_dates":[{"certification":"R"}]}]},
			"videos":{"results":[{"name":"Official Trailer","site":"YouTube","key":"K1","type":"Trailer","official":true}]},
			"credits":{"cast":[{"name":"Keanu Reeves","character":"Neo","order":0,"profile_path":"/k.jpg"}]}}`))
	})
	mux.HandleFunc("/tv/1396", reply(`{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20","overview":"o","vote_average":8.9,"vote_count":12000,
		"status":"Ended","tagline":"Change","original_language":"en","number_of_seasons":5,"number_of_episodes":62,
		"genres":[{"id":18,"name":"Drama"}],"networks":[{"name":"AMC"}],
		"content_ratings":{"results":[{"iso_3166_1":"US","rating":"TV-MA"}]},
		"videos":{"results":[{"name":"Trailer","site":"YouTube","key":"T1","type":"Trailer","official":true}]},
		"credits":{"cast":[{"name":"Bryan Cranston","character":"Walter White","order":0}]}}`))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverListsCarryGenresAndRatings(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newRichTMDB(t).URL)

	movies := getJSON[[]map[string]any](t, client, base+"/api/discover/trending")
	m := movies[0]
	genres, _ := m["genres"].([]any)
	if m["mediaType"] != "movie" || m["rating"] != 8.3 || m["voteCount"] != float64(25000) ||
		len(genres) != 2 || genres[0] != "Action" || genres[1] != "Science Fiction" {
		t.Fatalf("unexpected movie list item: %+v", m)
	}

	shows := getJSON[[]map[string]any](t, client, base+"/api/discover/tv/popular")
	sh := shows[0]
	if sh["mediaType"] != "tv" || sh["rating"] != 8.9 || strings.Join(toStrings(sh["genres"]), ",") != "Drama,Crime" {
		t.Fatalf("unexpected show list item: %+v", sh)
	}

	found := getJSON[[]map[string]any](t, client, base+"/api/discover/search?q=matrix")
	if len(found) != 2 || found[0]["mediaType"] != "movie" || found[1]["mediaType"] != "tv" ||
		strings.Join(toStrings(found[1]["genres"]), ",") != "Drama" || found[0]["voteCount"] != float64(10) {
		t.Fatalf("unexpected search results: %+v", found)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestMovieAndTVDetailEndpoints(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newRichTMDB(t).URL)

	movie := getJSON[map[string]any](t, client, base+"/api/tmdb/movies/603")
	trailers := movie["trailers"].([]any)
	cast := movie["cast"].([]any)
	if movie["certification"] != "R" || movie["runtime"] != float64(136) || movie["releaseStatus"] != "Released" ||
		movie["imdbId"] != "tt0133093" || movie["rating"] != 8.3 || movie["language"] != "en" || movie["tagline"] != "Welcome" ||
		movie["releaseDate"] != "1999-03-31" || movie["homepage"] != "https://x.test" {
		t.Fatalf("unexpected movie detail: %+v", movie)
	}
	if _, has := movie["status"]; has {
		t.Fatalf("status is the library state and must stay unset for a movie not in the library: %+v", movie)
	}
	if len(trailers) != 1 || trailers[0].(map[string]any)["url"] != "https://www.youtube.com/watch?v=K1" {
		t.Fatalf("trailers: %+v", trailers)
	}
	if len(cast) != 1 || cast[0].(map[string]any)["profileUrl"] != "https://image.tmdb.org/t/p/w185/k.jpg" {
		t.Fatalf("cast: %+v", cast)
	}

	// In the library, the library state still comes back as `status`.
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	movie = getJSON[map[string]any](t, client, base+"/api/tmdb/movies/603")
	if movie["status"] != "missing" || movie["libraryId"] == nil || movie["releaseStatus"] != "Released" {
		t.Fatalf("library state should be kept separate from TMDB's status: %+v", movie)
	}

	tv := getJSON[map[string]any](t, client, base+"/api/tmdb/tv/1396")
	if tv["title"] != "Breaking Bad" || tv["seasons"] != float64(5) || tv["episodes"] != float64(62) ||
		tv["contentRating"] != "TV-MA" || tv["releaseStatus"] != "Ended" || tv["firstAirDate"] != "2008-01-20" ||
		strings.Join(toStrings(tv["networks"]), ",") != "AMC" || strings.Join(toStrings(tv["genres"]), ",") != "Drama" {
		t.Fatalf("unexpected tv detail: %+v", tv)
	}
	if tv["libraryId"] != nil || tv["status"] != nil {
		t.Fatalf("a show outside the library has no library state: %+v", tv)
	}
	if len(tv["trailers"].([]any)) != 1 || len(tv["cast"].([]any)) != 1 {
		t.Fatalf("tv trailers/cast: %+v", tv)
	}

	if _, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 1396, Title: "Breaking Bad", Year: 2008, Monitored: true}, nil); err != nil {
		t.Fatal(err)
	}
	tv = getJSON[map[string]any](t, client, base+"/api/tmdb/tv/1396")
	if tv["libraryId"] == nil || tv["status"] != "missing" {
		t.Fatalf("library state for a show in the library: %+v", tv)
	}
}
