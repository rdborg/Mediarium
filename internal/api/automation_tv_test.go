package api_test

import (
	"context"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/api"
	"github.com/ryanborg/mediarium/internal/library"
)

type tvAutoEnv struct {
	server   *api.Server
	baseURL  string
	client   *http.Client
	seriesID int64
}

// episodeSpec seeds one episode: airDate "" means unannounced.
type episodeSpec struct {
	season, episode int
	airDate         string
	status          library.Status
	quality         string
}

func newTVAutoEnv(t *testing.T, titles []string, specs []episodeSpec) *tvAutoEnv {
	t.Helper()
	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "TV Indexer", "definitionId": "fixture", "baseUrl": newTVIndexerWith(t, titles).URL, "apiKey": "k",
	}, http.StatusCreated)

	var eps []library.Episode
	for _, sp := range specs {
		eps = append(eps, library.Episode{Season: sp.season, Episode: sp.episode, AirDate: sp.airDate})
	}
	series, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 1399, Title: "Fixture Show", Year: 2020, Monitored: true}, eps)
	if err != nil {
		t.Fatalf("seed series: %v", err)
	}
	stored, err := server.MovieRepo.ListEpisodes(series.ID)
	if err != nil {
		t.Fatalf("list episodes: %v", err)
	}
	for _, e := range stored {
		for _, sp := range specs {
			if sp.season == e.Season && sp.episode == e.Episode && sp.status == library.StatusDownloaded {
				if err := server.MovieRepo.SetEpisodeStatus(e.ID, library.StatusDownloaded, sp.quality, "/tv/x.mkv"); err != nil {
					t.Fatalf("seed episode status: %v", err)
				}
			}
		}
	}
	return &tvAutoEnv{server: server, baseURL: httpSrv.URL, client: client, seriesID: series.ID}
}

// grabbedTitles waits for at least want queue items (or the short grace
// period when want is 0), then for every item to reach a terminal state so
// no pipeline goroutine outlives the test's DB, and returns the release
// titles sorted.
func (e *tvAutoEnv) grabbedTitles(t *testing.T, want int) []string {
	t.Helper()
	if want == 0 {
		time.Sleep(300 * time.Millisecond)
	}
	deadline := time.Now().Add(10 * time.Second)
	var list []map[string]any
	for time.Now().Before(deadline) {
		list = getJSON[[]map[string]any](t, e.client, e.baseURL+"/api/queue")
		done := len(list) >= want
		for _, it := range list {
			if s, _ := it["status"].(string); s != "completed" && s != "failed" {
				done = false
			}
		}
		if done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var titles []string
	for _, it := range list {
		titles = append(titles, it["releaseTitle"].(string))
	}
	sort.Strings(titles)
	return titles
}

// firstGrabbedTitle waits for the first queue item and returns its release
// title: the oldest entry, whatever the retry logic queued after it.
func (e *tvAutoEnv) firstGrabbedTitle(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		list := getJSON[[]map[string]any](t, e.client, e.baseURL+"/api/queue")
		if len(list) > 0 {
			oldest := list[0]
			for _, it := range list {
				if it["id"].(float64) < oldest["id"].(float64) {
					oldest = it
				}
			}
			return oldest["releaseTitle"].(string)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("nothing was grabbed")
	return ""
}

func requireTitles(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("grabbed %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("grabbed %v, want %v", got, want)
		}
	}
}

const (
	sPack    = "Fixture.Show.S01.1080p.BluRay.x264-GRP"
	s1e1WEB  = "Fixture.Show.S01E01.1080p.WEB-DL.x264-GRP"
	s1e1Blu  = "Fixture.Show.S01E01.1080p.BluRay.x264-GRP"
	s1e2WEB  = "Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP"
	s2Pack   = "Fixture.Show.S02.1080p.BluRay.x264-GRP"
	s2e1WEB  = "Fixture.Show.S02E01.1080p.WEB-DL.x264-GRP"
	otherE01 = "Other.Show.S01E01.1080p.BluRay.x264-GRP"
)

func TestHuntTVGrabsSeasonPackWhenWholeSeasonMissing(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB, sPack, otherE01}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusMissing, ""},
		{1, 2, "2020-01-08", library.StatusMissing, ""},
		{1, 3, "2020-01-15", library.StatusMissing, ""},
	})
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t, 1), sPack)
}

func TestHuntTVGrabsEpisodeNotPackWhenSeasonPartiallyDownloaded(t *testing.T) {
	env := newTVAutoEnv(t, []string{s2Pack, s2e1WEB, otherE01}, []episodeSpec{
		{2, 1, "2021-01-01", library.StatusMissing, ""},
		{2, 2, "2021-01-08", library.StatusDownloaded, "WEBDL-1080p"},
	})
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t, 1), s2e1WEB)
}

func TestHuntTVSkipsUnairedEpisodes(t *testing.T) {
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB, sPack}, []episodeSpec{
		{1, 1, future, library.StatusMissing, ""},
		{1, 2, "", library.StatusMissing, ""},
	})
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t, 0))
}

func TestRSSSyncTVGrabsMissingEpisode(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB, otherE01}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusDownloaded, "Bluray-1080p"},
		{1, 2, "2020-01-08", library.StatusMissing, ""},
	})
	env.server.TestRSSSync(context.Background())
	requireTitles(t, env.grabbedTitles(t, 1), s1e2WEB)
}

func TestHuntTVUpgradesDownloadedEpisodeBelowCutoff(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e1Blu}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusDownloaded, "WEBDL-1080p"},
	})
	env.server.TestHunt(context.Background())
	// The fixture's NZB is not a real NZB, so the grab fails and the automatic
	// retry may go on to queue the next release. What this test is about is which
	// release the hunt itself picked first.
	if got := env.firstGrabbedTitle(t); got != s1e1Blu {
		t.Fatalf("hunt grabbed %q first, want the upgrade %q", got, s1e1Blu)
	}
}

func TestHuntTVLeavesEpisodeAtCutoffAlone(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e1Blu}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusDownloaded, "Bluray-1080p"},
	})
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t, 0))
}
