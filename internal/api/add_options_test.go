package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/library"
)

// newTitleTMDB serves a combined search (a movie, a show, and a person that
// must be dropped) plus details for two movies: one long released, one not out
// yet.
func newTitleTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	future := time.Now().UTC().AddDate(0, 3, 0).Format("2006-01-02")
	mux := http.NewServeMux()
	mux.HandleFunc("/search/multi", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"media_type": "movie", "id": 603, "title": "The Matrix", "release_date": "1999-03-31", "poster_path": "/m.jpg"},
			{"media_type": "tv", "id": 1399, "name": "Matrix Show", "first_air_date": "2011-04-17"},
			{"media_type": "person", "id": 6384, "name": "Keanu Reeves"},
		}})
	})
	mux.HandleFunc("/movie/603", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 603, "title": "The Matrix", "release_date": "1999-03-31"})
	})
	mux.HandleFunc("/movie/700", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 700, "title": "Coming Soon", "release_date": future})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestTitleSearchMergesMoviesAndShowsAndFlagsLibrary(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newTitleTMDB(t).URL)

	short := getJSON[[]map[string]any](t, client, base+"/api/discover/search?q=m")
	if len(short) != 0 {
		t.Fatalf("a one-letter query should not hit TMDB: %+v", short)
	}

	postJSON[map[string]any](t, client, base+"/api/movies", map[string]any{"tmdbId": 603}, http.StatusCreated)
	res := getJSON[[]map[string]any](t, client, base+"/api/discover/search?q=matrix")
	if len(res) != 2 {
		t.Fatalf("expected the movie and the show (person dropped), got %+v", res)
	}
	if res[0]["kind"] != "movie" || res[0]["inLibrary"] != true || res[0]["status"] != "missing" || res[0]["year"] != float64(1999) {
		t.Fatalf("movie hit should be flagged as in the library: %+v", res[0])
	}
	if res[1]["kind"] != "tv" || res[1]["inLibrary"] != false || res[1]["title"] != "Matrix Show" {
		t.Fatalf("show hit: %+v", res[1])
	}
}

func TestAddMovieAppliesTheDialogChoices(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newTitleTMDB(t).URL)
	profile := postJSON[map[string]any](t, client, base+"/api/quality-profiles", map[string]any{
		"name": "Mine", "allowed": []string{"Bluray-1080p"}, "cutoff": "Bluray-1080p", "upgradeAllowed": true,
	}, http.StatusCreated)

	// Bad choices are refused before anything is added.
	postJSON[map[string]any](t, client, base+"/api/movies", map[string]any{"tmdbId": 603, "sources": "carrier-pigeon"}, http.StatusBadRequest)
	postJSON[map[string]any](t, client, base+"/api/movies", map[string]any{"tmdbId": 603, "profileId": 9999}, http.StatusBadRequest)
	if movies := getJSON[[]map[string]any](t, client, base+"/api/movies"); len(movies) != 0 {
		t.Fatalf("a refused add must not leave a movie behind: %+v", movies)
	}

	added := postJSON[map[string]any](t, client, base+"/api/movies", map[string]any{
		"tmdbId": 603, "profileId": profile["id"], "monitored": false, "sources": "usenet",
	}, http.StatusCreated)
	if added["profileId"] != profile["id"] || added["monitored"] != false || added["sources"] != "usenet" {
		t.Fatalf("choices not applied: %+v", added)
	}
	postJSON[map[string]any](t, client, base+"/api/movies", map[string]any{"tmdbId": 603}, http.StatusConflict)

	// Changing the download sources later.
	id := int64(added["id"].(float64))
	putJSONStatus(t, client, fmt.Sprintf("%s/api/movies/%d/sources", base, id), map[string]any{"sources": "torrent"}, http.StatusOK)
	if got := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", base, id)); got["sources"] != "torrent" {
		t.Fatalf("sources not updated: %+v", got)
	}
	putJSONStatus(t, client, fmt.Sprintf("%s/api/movies/%d/sources", base, id), map[string]any{"sources": "nope"}, http.StatusBadRequest)

	// The default lives in Settings.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"defaultSources": "usenet"}, http.StatusOK)
	if s := getJSON[map[string]any](t, client, base+"/api/settings"); s["defaultSources"] != "usenet" {
		t.Fatalf("default sources: %+v", s["defaultSources"])
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"defaultSources": "nope"}, http.StatusBadRequest)
}

// "Start searching now" runs the search in the background and respects which
// downloaders the movie may use.
func TestAddMovieCanStartSearchingAndHonoursSources(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newTitleTMDB(t).URL)

	usenet := newTVIndexerWith(t, []string{"The.Matrix.1999.1080p.WEB-DL.x264-U"})
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "U", "definitionId": "x", "baseUrl": usenet.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	// A torrent indexer with a *better* release that must be ignored: this movie is usenet-only.
	torrent := newTVIndexerWith(t, []string{"The.Matrix.1999.1080p.BluRay.x264-T"})
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "T", "definitionId": "x", "baseUrl": torrent.URL, "apiKey": "k", "protocol": "torrent"}, http.StatusCreated)

	postJSON[map[string]any](t, client, base+"/api/movies", map[string]any{"tmdbId": 603, "sources": "usenet", "searchNow": true}, http.StatusCreated)

	items := waitForQueue(t, client, base, 1)
	if len(items) != 1 || items[0]["releaseTitle"] != "The.Matrix.1999.1080p.WEB-DL.x264-U" {
		t.Fatalf("expected only the Usenet release to be grabbed, got %+v", items)
	}
}

func TestUnreleasedMoviesAreNotHuntedUntilYouAskForIt(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newTitleTMDB(t).URL)
	future := time.Now().UTC().AddDate(0, 3, 0)
	idx := newTVIndexerWith(t, []string{fmt.Sprintf("Coming.Soon.%d.1080p.WEB-DL.x264-U", future.Year())})
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "U", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 700, Title: "Coming Soon", Year: future.Year(), Monitored: true, ReleaseDate: future.Format("2006-01-02")})
	if err != nil {
		t.Fatal(err)
	}

	server.TestHunt(context.Background())
	server.TestRSSSync(context.Background())
	time.Sleep(300 * time.Millisecond)
	if q := getJSON[[]map[string]any](t, client, base+"/api/queue"); len(q) != 0 {
		t.Fatalf("automation searched for a movie that is not out yet: %+v", q)
	}

	// An explicit "search now" still goes ahead.
	got := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/search-now", base, m.ID), nil, http.StatusOK)
	if got["grabbed"] != float64(1) {
		t.Fatalf("search now should ignore the release date: %+v", got)
	}
	waitForQueue(t, client, base, 1)
}

func TestAddSeriesMonitorChoices(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", newTVTMDBServer(t, nil).URL)

	monitoredCount := func(id float64) (monitored, total int) {
		detail := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/series/%d", base, int64(id)))
		for _, raw := range detail["episodes"].([]any) {
			total++
			if raw.(map[string]any)["monitored"] == true {
				monitored++
			}
		}
		return
	}
	for _, tc := range []struct {
		monitor       string
		wantMonitored int // the fixture show's episodes all aired years ago
	}{{"all", 3}, {"future", 0}, {"none", 0}, {"", 3}} {
		added := postJSON[map[string]any](t, client, base+"/api/series", map[string]any{"tmdbId": 1399, "monitor": tc.monitor}, http.StatusCreated)
		if m, total := monitoredCount(added["id"].(float64)); m != tc.wantMonitored || total != 3 {
			t.Fatalf("monitor=%q: %d/%d episodes monitored, want %d", tc.monitor, m, total, tc.wantMonitored)
		}
		if code := deleteReq(t, client, fmt.Sprintf("%s/api/series/%d", base, int64(added["id"].(float64)))); code != http.StatusOK {
			t.Fatalf("cleanup delete: %d", code)
		}
	}
	postJSON[map[string]any](t, client, base+"/api/series", map[string]any{"tmdbId": 1399, "monitor": "sometimes"}, http.StatusBadRequest)

	added := postJSON[map[string]any](t, client, base+"/api/series", map[string]any{"tmdbId": 1399, "sources": "torrent"}, http.StatusCreated)
	if added["sources"] != "torrent" || !strings.HasPrefix(fmt.Sprint(added["title"]), "Fixture") {
		t.Fatalf("series sources: %+v", added)
	}
	putJSONStatus(t, client, fmt.Sprintf("%s/api/series/%d/sources", base, int64(added["id"].(float64))), map[string]any{"sources": ""}, http.StatusOK)
}
