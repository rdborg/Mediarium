package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// importMovies scans a folder of n movies and confirms it with the given
// options, then waits for the details. It returns the batch id.
func importMovies(t *testing.T, base string, client *http.Client, root string, options map[string]any) float64 {
	t.Helper()
	started := postJSON[map[string]any](t, client, base+"/api/library/scan", map[string]any{"path": root, "kind": "movie"}, http.StatusAccepted)
	job := waitForImportPhase(t, client, base, started["jobId"].(string), "ready")
	var selections []map[string]any
	for _, raw := range job["items"].([]any) {
		item := raw.(map[string]any)
		selections = append(selections, map[string]any{"key": item["key"], "tmdbId": item["candidates"].([]any)[0].(map[string]any)["tmdbId"]})
	}
	body := map[string]any{"jobId": started["jobId"], "selections": selections}
	for k, v := range options {
		body[k] = v
	}
	confirmed := postJSON[map[string]any](t, client, base+"/api/library/import", body, http.StatusAccepted)
	batchID := confirmed["batchId"].(float64)
	waitForImportBatch(t, client, base, batchID)
	return batchID
}

// What an import sets on the titles it adds, for the choices on the review page.
func TestImportedMoviesAreAddedSafeUnlessAskedOtherwise(t *testing.T) {
	tests := []struct {
		name          string
		options       map[string]any
		wantMonitored bool
		wantNoUpgrade bool
	}{
		{"defaults", map[string]any{}, false, true},
		{"watch switched on", map[string]any{"monitor": true}, true, false},
		{"watch switched on, better versions still off", map[string]any{"monitor": true, "noUpgrade": true}, true, true},
		{"an older client asking for better versions", map[string]any{"noUpgrade": false}, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, httpSrv, client := newHuntTestServer(t)
			server.TestSetTMDBBaseURL("fixture-tmdb-key", newFakeTMDB(t).srv.URL)
			signIn(t, client, httpSrv.URL)
			batchID := importMovies(t, httpSrv.URL, client, makeMovieFolder(t, 3), tc.options)

			movies := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies")
			if len(movies) != 3 {
				t.Fatalf("expected 3 movies, got %d", len(movies))
			}
			for _, m := range movies {
				if m["monitored"] != tc.wantMonitored || m["noUpgrade"] != tc.wantNoUpgrade || m["status"] != "downloaded" {
					t.Fatalf("movie %v: monitored %v no-upgrade %v, want %v and %v", m["title"], m["monitored"], m["noUpgrade"], tc.wantMonitored, tc.wantNoUpgrade)
				}
			}
			batch := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/library/import/batches/%.0f", httpSrv.URL, batchID))
			if batch["monitor"] != tc.wantMonitored || batch["noUpgrade"] != tc.wantNoUpgrade {
				t.Fatalf("the report should say what was set: %+v", batch)
			}
		})
	}
}

// "Start monitoring these titles" selects exactly the titles of one import.
func TestStartMonitoringTheTitlesOfAnImport(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newFakeTMDB(t).srv.URL)
	signIn(t, client, httpSrv.URL)

	// A title from somewhere else, not monitored and left alone. It is not
	// part of the import and must stay as it is.
	other, err := server.MovieRepo.Add(library.Movie{TMDBID: 9999, Title: "Somewhere Else", Year: 2001, Monitored: false, NoUpgrade: true})
	if err != nil {
		t.Fatal(err)
	}

	batchID := importMovies(t, httpSrv.URL, client, makeMovieFolder(t, 3), nil)
	url := fmt.Sprintf("%s/api/library/import/batches/%.0f/watch", httpSrv.URL, batchID)

	// Nothing chosen, or a choice that does not fit, is refused.
	postJSON[map[string]any](t, client, url, map[string]any{}, http.StatusBadRequest)
	refused := postJSON[map[string]any](t, client, url, map[string]any{"missing": true}, http.StatusBadRequest)
	if !strings.Contains(fmt.Sprint(refused["error"]), "TV shows") {
		t.Fatalf("expected a plain reason, got %+v", refused)
	}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import/batches/9999/watch", map[string]any{"watch": true}, http.StatusNotFound)

	res := postJSON[map[string]any](t, client, url, map[string]any{"watch": true}, http.StatusOK)
	if res["movies"] != float64(3) || res["message"] != "Done: 3 movies monitored now." {
		t.Fatalf("unexpected answer: %+v", res)
	}
	for _, m := range getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies") {
		wantMon, wantNoUpg := true, false
		if m["title"] == "Somewhere Else" {
			wantMon, wantNoUpg = false, true
		}
		if m["monitored"] != wantMon || m["noUpgrade"] != wantNoUpg {
			t.Fatalf("%v: monitored %v no-upgrade %v, want %v and %v", m["title"], m["monitored"], m["noUpgrade"], wantMon, wantNoUpg)
		}
	}
	if got, _ := server.MovieRepo.Get(other.ID); got.Monitored || !got.NoUpgrade {
		t.Fatalf("a title outside the import was changed: %+v", got)
	}
	batch := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/library/import/batches/%.0f", httpSrv.URL, batchID))
	if batch["monitor"] != true || batch["noUpgrade"] != false {
		t.Fatalf("the report should now say the titles are watched: %+v", batch)
	}
}

// The same click for a show import: watching, and looking for missing episodes.
func TestStartMonitoringAnImportedShow(t *testing.T) {
	for _, tc := range []struct {
		name          string
		body          map[string]any
		wantMonitored bool
		wantNoUpgrade bool
		wantMessage   string
	}{
		{"watch", map[string]any{"watch": true}, true, false, "Done: 1 show monitored now."},
		{"missing episodes", map[string]any{"missing": true}, true, true, "Done: looking for missing episodes of 1 show."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, httpSrv, client := newHuntTestServer(t)
			server.TestSetTMDBBaseURL("fixture-tmdb-key", newTVTMDBServer(t, nil).URL)
			signIn(t, client, httpSrv.URL)
			root := t.TempDir()
			writeFile(t, root, "Fixture Show (2011)/Season 01/Fixture.Show.S01E01.720p.HDTV.x264-GRP.mkv")

			started := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "tv"}, http.StatusAccepted)
			job := waitForImportPhase(t, client, httpSrv.URL, started["jobId"].(string), "ready")
			item := job["items"].([]any)[0].(map[string]any)
			confirmed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", map[string]any{
				"jobId": started["jobId"], "selections": []map[string]any{{"key": item["key"], "tmdbId": 1399}},
			}, http.StatusAccepted)
			batchID := confirmed["batchId"].(float64)
			waitForImportBatch(t, client, httpSrv.URL, batchID)

			series := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/series")
			if series[0]["monitored"] != false || series[0]["noUpgrade"] != true {
				t.Fatalf("a show is added safe: %+v", series[0])
			}
			res := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/library/import/batches/%.0f/watch", httpSrv.URL, batchID), tc.body, http.StatusOK)
			if res["message"] != tc.wantMessage {
				t.Fatalf("message %v, want %q", res["message"], tc.wantMessage)
			}
			series = getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/series")
			if series[0]["monitored"] != tc.wantMonitored || series[0]["noUpgrade"] != tc.wantNoUpgrade {
				t.Fatalf("monitored %v no-upgrade %v, want %v and %v", series[0]["monitored"], series[0]["noUpgrade"], tc.wantMonitored, tc.wantNoUpgrade)
			}
		})
	}
}

// A title added from the Add dialog is monitored, but it is not searched again
// for better versions unless that is asked for.
func TestAddedTitlesLeaveBetterVersionsOffUnlessAsked(t *testing.T) {
	server, base, client := loginNewServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/movie/", func(w http.ResponseWriter, r *http.Request) {
		var id int
		fmt.Sscanf(r.URL.Path, "/movie/%d", &id)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "title": fmt.Sprintf("Movie %d", id), "release_date": "1999-03-31"})
	})
	tmdb := httptest.NewServer(mux)
	t.Cleanup(tmdb.Close)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	for _, tc := range []struct {
		name string
		body map[string]any
		want bool // no-upgrade
	}{
		{"field left out", map[string]any{"tmdbId": 603}, true},
		{"better versions off", map[string]any{"tmdbId": 700, "noUpgrade": true}, true},
	} {
		added := postJSON[map[string]any](t, client, base+"/api/movies", tc.body, http.StatusCreated)
		if added["monitored"] != true || added["noUpgrade"] != tc.want {
			t.Fatalf("%s: %+v", tc.name, added)
		}
	}

	on := postJSON[map[string]any](t, client, base+"/api/movies", map[string]any{"tmdbId": 605, "noUpgrade": false}, http.StatusCreated)
	if on["noUpgrade"] != false || on["monitored"] != true {
		t.Fatalf("better versions asked for: %+v", on)
	}
}
