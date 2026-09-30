package api_test

import (
	"context"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// An imported movie that is below the profile's cutoff is left alone when it
// is marked no-upgrade, and is searched for a better version otherwise.
func TestHuntLeavesNoUpgradeMoviesAlone(t *testing.T) {
	tests := []struct {
		name      string
		noUpgrade bool
		run       string // hunt or rss
		wantGrabs int
	}{
		{"hunt skips a no-upgrade movie", true, "hunt", 0},
		{"hunt still upgrades a normal movie", false, "hunt", 1},
		{"rss sync skips a no-upgrade movie", true, "rss", 0},
		{"rss sync still upgrades a normal movie", false, "rss", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, httpSrv, client := newHuntTestServer(t)
			enableUpgrades(t, server)
			indexerSrv := newTwoQualityIndexerServer(t)
			signIn(t, client, httpSrv.URL)
			postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
				"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
			}, http.StatusCreated)

			movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "The Fixture Movie", Year: 1999, Monitored: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := server.MovieRepo.SetStatus(movie.ID, library.StatusDownloaded, "WEBDL-1080p", "/movies/fixture.mkv"); err != nil {
				t.Fatal(err)
			}
			if err := server.MovieRepo.SetNoUpgrade(movie.ID, tc.noUpgrade); err != nil {
				t.Fatal(err)
			}

			cutoff := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/wanted?kind=cutoff")
			if want := tc.wantGrabs; len(cutoff) != want {
				t.Fatalf("the upgrade list has %d entries, want %d", len(cutoff), want)
			}

			if tc.run == "hunt" {
				server.TestHunt(context.Background())
			} else {
				server.TestRSSSync(context.Background())
			}
			time.Sleep(200 * time.Millisecond)
			if q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue"); len(q) != tc.wantGrabs {
				t.Fatalf("queued %d downloads, want %d: %+v", len(q), tc.wantGrabs, q)
			}
			if tc.wantGrabs > 0 {
				waitForQueueTerminal(t, client, httpSrv.URL)
			}
		})
	}
}

func TestHuntLeavesNoUpgradeShowsAlone(t *testing.T) {
	tests := []struct {
		name      string
		noUpgrade bool
		want      []string
	}{
		{"a no-upgrade show keeps its episodes", true, nil},
		{"a normal show gets the better version", false, []string{s1e1Blu}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newTVAutoEnv(t, []string{s1e1WEB, s1e1Blu}, []episodeSpec{
				{1, 1, "2020-01-01", library.StatusDownloaded, "WEBDL-1080p"},
			})
			enableUpgrades(t, env.server)
			if err := env.server.MovieRepo.SetSeriesNoUpgrade(env.seriesID, tc.noUpgrade); err != nil {
				t.Fatal(err)
			}
			env.server.TestHunt(context.Background())
			requireTitles(t, env.grabbedTitles(t), tc.want...)
			env.finish(t)
		})
	}
}

// Importing a show must never start a download by itself: the show is not
// monitored, the episodes that are missing are only wanted when the person
// asks for that, and the ones already on disk are only replaced when the show
// is watched for better versions.
func TestImportedShowOnlyDownloadsWhatWasAskedFor(t *testing.T) {
	const (
		s2e2 = "Fixture.Show.S02E02.1080p.WEB-DL.x264-GRP"
	)
	tests := []struct {
		name           string
		options        map[string]any
		wantMonitored  bool
		wantNoUpgrade  bool
		wantDownloaded []string
	}{
		{"defaults: not monitored, nothing downloads", map[string]any{}, false, true, nil},
		{"missing episodes wanted, nothing replaced", map[string]any{"monitorMissing": true}, true, true, []string{s2e2}},
		{"watched: better versions, missing episodes left alone", map[string]any{"monitor": true}, true, false, []string{s1e1Blu}},
		{"everything on", map[string]any{"monitor": true, "monitorMissing": true}, true, false, []string{s1e1Blu, s2e2}},
		{"an older client asking for better versions", map[string]any{"noUpgrade": false, "monitorMissing": false}, true, false, []string{s1e1Blu}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, httpSrv, client := newHuntTestServer(t)
			enableUpgrades(t, server)
			var extra atomic.Bool
			extra.Store(true) // the show has a second episode in season 2 that is not on disk
			server.TestSetTMDBBaseURL("fixture-tmdb-key", newTVTMDBServer(t, &extra).URL)
			signIn(t, client, httpSrv.URL)
			indexer, release := newGatedTVIndexer(t, []string{s1e1Blu, s2e2}, http.StatusNotFound, "")
			postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
				"name": "TV Indexer", "definitionId": "fixture", "baseUrl": indexer.URL, "apiKey": "k",
			}, http.StatusCreated)

			root := t.TempDir()
			writeFile(t, root, "Fixture Show (2011)/Season 01/Fixture.Show.S01E01.720p.HDTV.x264-GRP.mkv")
			writeFile(t, root, "Fixture Show (2011)/Season 01/Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv")
			writeFile(t, root, "Fixture Show (2011)/Season 02/Fixture.Show.S02E01.1080p.BluRay.x264-GRP.mkv")

			started := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/scan", map[string]any{"path": root, "kind": "tv"}, http.StatusAccepted)
			job := waitForImportPhase(t, client, httpSrv.URL, started["jobId"].(string), "ready")
			item := job["items"].([]any)[0].(map[string]any)
			body := map[string]any{"jobId": started["jobId"], "selections": []map[string]any{{"key": item["key"], "tmdbId": 1399}}}
			for k, v := range tc.options {
				body[k] = v
			}
			confirmed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/library/import", body, http.StatusAccepted)
			waitForImportBatch(t, client, httpSrv.URL, confirmed["batchId"].(float64))

			series := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/series")
			if len(series) != 1 || series[0]["monitored"] != tc.wantMonitored || series[0]["noUpgrade"] != tc.wantNoUpgrade || series[0]["downloadedCount"] != float64(3) || series[0]["episodeCount"] != float64(4) {
				t.Fatalf("unexpected show after the import: %+v", series)
			}

			server.TestHunt(context.Background())
			got := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
			var titles []string
			for _, q := range got {
				titles = append(titles, q["releaseTitle"].(string))
			}
			requireTitles(t, titles, tc.wantDownloaded...)
			release()
			waitBackground(t, server)
		})
	}
}

// Titles that an earlier version imported get the age of their files as the
// date they were added, once.
func TestBackfillOfImportedAddedDates(t *testing.T) {
	server, _, _ := newHuntTestServer(t)
	dir := t.TempDir()
	old := time.Date(2019, 3, 4, 10, 0, 0, 0, time.UTC)
	older := old.AddDate(0, -1, 0)
	future := time.Now().Add(-time.Minute) // a file newer than the recorded date
	touch := func(name string, at time.Time) string {
		path := writeFile(t, dir, name)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
		return path
	}
	addedNow := time.Now().Add(-time.Hour)

	movie := func(tmdb int, title, path string, logged bool) library.Movie {
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: title, Year: 2000, Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", path); err != nil {
			t.Fatal(err)
		}
		if err := server.MovieRepo.SetAddedAt("movie", m.ID, addedNow); err != nil {
			t.Fatal(err)
		}
		if logged {
			if err := server.QueueRepo.LogActivity(m.ID, "imported", title+": registered existing file "+path); err != nil {
				t.Fatal(err)
			}
		}
		return m
	}
	imported := movie(1, "Imported Movie", touch("imported.mkv", old), true)
	downloaded := movie(2, "Downloaded Movie", touch("downloaded.mkv", old), false)
	replaced := movie(3, "Replaced Movie", touch("replaced.mkv", future), true)
	// A download logs an "imported" line too, but with different words.
	other := movie(4, "Grabbed Movie", touch("grabbed.mkv", old), false)
	if err := server.QueueRepo.LogActivity(other.ID, "imported", "Grabbed Movie imported to /movies/x.mkv"); err != nil {
		t.Fatal(err)
	}

	// An older version logged a show's import without its id.
	e1 := touch("show-e1.mkv", older)
	e2 := touch("show-e2.mkv", old)
	show, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 9, Title: "Imported Show", Year: 2001, Monitored: true}, []library.Episode{{Season: 1, Episode: 1}, {Season: 1, Episode: 2}})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := server.MovieRepo.ListEpisodes(show.ID)
	for i, p := range []string{e1, e2} {
		if err := server.MovieRepo.SetEpisodeStatus(eps[i].ID, library.StatusDownloaded, "HDTV-720p", p); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.MovieRepo.SetAddedAt("series", show.ID, addedNow); err != nil {
		t.Fatal(err)
	}
	if err := server.QueueRepo.LogActivity(0, "imported", "Imported Show: registered 2 episodes that were already on disk"); err != nil {
		t.Fatal(err)
	}

	server.TestBackfillImportedAddedDates()

	added := func(kind string, id int64) time.Time {
		t.Helper()
		items, err := server.MovieRepo.RecentlyAdded(50)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if it.Kind == kind && it.ID == id {
				at, err := time.Parse("2006-01-02T15:04:05.000Z", it.AddedAt)
				if err != nil {
					t.Fatalf("added date %q: %v", it.AddedAt, err)
				}
				return at
			}
		}
		t.Fatalf("%s %d not listed", kind, id)
		return time.Time{}
	}
	checks := []struct {
		name string
		kind string
		id   int64
		want time.Time
	}{
		{"an imported movie takes its file's date", "movie", imported.ID, old},
		{"a movie that was downloaded is not touched", "movie", downloaded.ID, addedNow},
		{"a newer file never moves the date forward", "movie", replaced.ID, addedNow},
		{"a download's own log line is not an import", "movie", other.ID, addedNow},
		{"an imported show takes its newest episode's date", "series", show.ID, old},
	}
	for _, c := range checks {
		if got := added(c.kind, c.id); !got.Truncate(time.Second).Equal(c.want.Truncate(time.Second)) {
			t.Errorf("%s: added %v, want %v", c.name, got, c.want)
		}
	}

	// It runs once: a date changed later is left as it is.
	if err := server.MovieRepo.SetAddedAt("movie", imported.ID, addedNow); err != nil {
		t.Fatal(err)
	}
	server.TestBackfillImportedAddedDates()
	if got := added("movie", imported.ID); !got.Truncate(time.Second).Equal(addedNow.Truncate(time.Second)) {
		t.Errorf("the backfill ran again: %v", got)
	}
}
