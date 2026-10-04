package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
)

type tvAutoEnv struct {
	server   *api.Server
	baseURL  string
	client   *http.Client
	seriesID int64
	release  func() // lets the fixture's release downloads answer
}

// episodeSpec seeds one episode: airDate "" means unannounced.
type episodeSpec struct {
	season, episode int
	airDate         string
	status          library.Status
	quality         string
}

// newGatedTVIndexer is newTVIndexerWith whose release downloads (/nzb/...)
// wait until release is called and then answer with nzbStatus/nzbBody. While
// the gate is shut every grabbed pipeline is still "downloading", so a test
// sees exactly what one automation run queued, whatever the timing; and a
// 404 afterwards fails the download without blaming the release, so no
// automatic retry queues anything else.
func newGatedTVIndexer(t *testing.T, titles []string, nzbStatus int, nzbBody string) (srv *httptest.Server, release func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/nzb/") {
			<-gate
			w.WriteHeader(nzbStatus)
			fmt.Fprint(w, nzbBody)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
		for i, title := range titles {
			fmt.Fprintf(&b, `<item><title>%s</title><guid>g%d</guid><enclosure url="http://%s/nzb/%d.nzb" length="21474836480" type="application/x-nzb"/><newznab:attr name="size" value="21474836480"/><newznab:attr name="category" value="5000"/></item>`,
				title, i, r.Host, i)
		}
		b.WriteString(`</channel></rss>`)
		fmt.Fprint(w, b.String())
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(release) // runs first: Close waits for the blocked downloads
	return srv, release
}

func newTVAutoEnv(t *testing.T, titles []string, specs []episodeSpec) *tvAutoEnv {
	t.Helper()
	return newTVAutoEnvWith(t, titles, specs, http.StatusNotFound, "")
}

func newTVAutoEnvWith(t *testing.T, titles []string, specs []episodeSpec, nzbStatus int, nzbBody string) *tvAutoEnv {
	t.Helper()
	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	indexer, release := newGatedTVIndexer(t, titles, nzbStatus, nzbBody)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "TV Indexer", "definitionId": "fixture", "baseUrl": indexer.URL, "apiKey": "k",
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
				// A real file: a download that ends without importing gives the
				// episode back as downloaded only while its file is still there.
				file := filepath.Join(t.TempDir(), fmt.Sprintf("S%02dE%02d.mkv", e.Season, e.Episode))
				if err := os.WriteFile(file, []byte("old file"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := server.MovieRepo.SetEpisodeStatus(e.ID, library.StatusDownloaded, sp.quality, file); err != nil {
					t.Fatalf("seed episode status: %v", err)
				}
			}
		}
	}
	return &tvAutoEnv{server: server, baseURL: httpSrv.URL, client: client, seriesID: series.ID, release: release}
}

// queued returns the queue as it is now, oldest first. Automation runs queue
// their grabs before returning, and the fixture holds every download until
// release, so this is exactly what the runs so far have grabbed.
func (e *tvAutoEnv) queued(t *testing.T) []map[string]any {
	t.Helper()
	list := getJSON[[]map[string]any](t, e.client, e.baseURL+"/api/queue")
	sort.Slice(list, func(i, j int) bool { return list[i]["id"].(float64) < list[j]["id"].(float64) })
	return list
}

// grabbedTitles returns the release titles queued so far, sorted.
func (e *tvAutoEnv) grabbedTitles(t *testing.T) []string {
	t.Helper()
	var titles []string
	for _, it := range e.queued(t) {
		titles = append(titles, it["releaseTitle"].(string))
	}
	sort.Strings(titles)
	return titles
}

// finish lets the held downloads answer and waits until every pipeline and
// automatic retry has run its course.
func (e *tvAutoEnv) finish(t *testing.T) {
	t.Helper()
	e.release()
	waitBackground(t, e.server)
}

func (e *tvAutoEnv) episodeStatuses(t *testing.T) map[string]library.Status {
	t.Helper()
	eps, err := e.server.MovieRepo.ListEpisodes(e.seriesID)
	if err != nil {
		t.Fatalf("list episodes: %v", err)
	}
	out := map[string]library.Status{}
	for _, ep := range eps {
		out[fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode)] = ep.Status
	}
	return out
}

func requireTitles(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	got = append([]string(nil), got...) // the order downloads are listed in does not matter
	sort.Strings(got)
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

var wholeSeasonMissing = []episodeSpec{
	{1, 1, "2020-01-01", library.StatusMissing, ""},
	{1, 2, "2020-01-08", library.StatusMissing, ""},
	{1, 3, "2020-01-15", library.StatusMissing, ""},
}

func TestHuntTVGrabsSeasonPackWhenWholeSeasonMissing(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB, sPack, otherE01}, wholeSeasonMissing)
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t), sPack)

	// The pack claims every episode it will deliver the moment it is grabbed.
	for ep, status := range env.episodeStatuses(t) {
		if status != library.StatusDownloading {
			t.Fatalf("%s is %q right after the pack was grabbed, want downloading", ep, status)
		}
	}

	// Later runs while the pack is still downloading leave its episodes alone.
	env.server.TestHunt(context.Background())
	env.server.TestRSSSync(context.Background())
	requireTitles(t, env.grabbedTitles(t), sPack)

	env.finish(t)
	requireTitles(t, env.grabbedTitles(t), sPack)
	for ep, status := range env.episodeStatuses(t) {
		if status != library.StatusMissing {
			t.Fatalf("%s is %q after the pack's download failed, want missing again", ep, status)
		}
	}
}

// TestTVAutomationRunsThatOverlapGrabOnce runs the hunt and RSS sync at the
// same time, many times over: whichever claims the season first grabs the
// pack, and the other must not grab it again or any of its episodes.
func TestTVAutomationRunsThatOverlapGrabOnce(t *testing.T) {
	for i := 0; i < 10; i++ {
		env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB, sPack, otherE01}, wholeSeasonMissing)
		var wg sync.WaitGroup
		for _, run := range []func(context.Context){env.server.TestHunt, env.server.TestRSSSync, env.server.TestHunt} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				run(context.Background())
			}()
		}
		wg.Wait()
		requireTitles(t, env.grabbedTitles(t), sPack)
		env.finish(t)
	}
}

// TestHuntTVFallsBackToEpisodesWhenThePackIsBad covers the one case where
// single episodes follow a pack: the pack itself turned out to be unusable,
// was blocklisted, and the automatic retry looks for something else. The
// episodes are grabbed only after the pack has failed, never alongside it.
func TestHuntTVFallsBackToEpisodesWhenThePackIsBad(t *testing.T) {
	env := newTVAutoEnvWith(t, []string{s1e1WEB, s1e2WEB, sPack, otherE01}, wholeSeasonMissing, http.StatusOK, "this is not an nzb")
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t), sPack)

	env.finish(t)
	items := env.queued(t)
	if len(items) == 0 || items[0]["releaseTitle"] != sPack {
		t.Fatalf("the pack must be the first grab: %+v", items)
	}
	requireTitles(t, env.grabbedTitles(t), sPack, s1e1WEB, s1e2WEB)
	packDone := items[0]["completedAt"].(string)
	for _, it := range items[1:] {
		if added := it["addedAt"].(string); added < packDone {
			t.Fatalf("%s was grabbed at %s, before the pack failed at %s", it["releaseTitle"], added, packDone)
		}
	}
}

func TestHuntTVGrabsEpisodeNotPackWhenSeasonPartiallyDownloaded(t *testing.T) {
	env := newTVAutoEnv(t, []string{s2Pack, s2e1WEB, otherE01}, []episodeSpec{
		{2, 1, "2021-01-01", library.StatusMissing, ""},
		{2, 2, "2021-01-08", library.StatusDownloaded, "WEBDL-1080p"},
	})
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t), s2e1WEB)
	env.finish(t)
}

func TestHuntTVSkipsUnairedEpisodes(t *testing.T) {
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB, sPack}, []episodeSpec{
		{1, 1, future, library.StatusMissing, ""},
		{1, 2, "", library.StatusMissing, ""},
	})
	env.server.TestHunt(context.Background())
	env.finish(t)
	requireTitles(t, env.grabbedTitles(t))
}

func TestRSSSyncTVGrabsMissingEpisode(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB, otherE01}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusDownloaded, "Bluray-1080p"},
		{1, 2, "2020-01-08", library.StatusMissing, ""},
	})
	env.server.TestRSSSync(context.Background())
	requireTitles(t, env.grabbedTitles(t), s1e2WEB)
	env.finish(t)
}

func TestHuntTVUpgradesDownloadedEpisodeBelowCutoff(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e1Blu}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusDownloaded, "WEBDL-1080p"},
	})
	enableUpgrades(t, env.server)
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t), s1e1Blu)
	env.finish(t)
	// The upgrade's download failed: the episode still has its old file.
	if got := env.episodeStatuses(t)["S01E01"]; got != library.StatusDownloaded {
		t.Fatalf("after a failed upgrade the episode is %q, want downloaded", got)
	}
}

func TestHuntTVLeavesEpisodeAtCutoffAlone(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e1Blu}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusDownloaded, "Bluray-1080p"},
	})
	env.server.TestHunt(context.Background())
	env.finish(t)
	requireTitles(t, env.grabbedTitles(t))
}
