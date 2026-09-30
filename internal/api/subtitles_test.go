package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

type fakeOpenSubtitles struct {
	srv         *httptest.Server
	searches    atomic.Int32
	quota       atomic.Bool  // refuse every download with 406 and no figures
	dlLimit     atomic.Int32 // when > 0: allow this many downloads, report the quota, then refuse with figures
	downloads   atomic.Int32 // downloads allowed so far
	lastQueries chan map[string]string
}

// newFakeOpenSubtitles serves an "en" subtitle for everything, none in any
// other language, and a downloadable file.
func newFakeOpenSubtitles(t *testing.T) *fakeOpenSubtitles {
	t.Helper()
	f := &fakeOpenSubtitles{lastQueries: make(chan map[string]string, 32)}
	mux := http.NewServeMux()
	mux.HandleFunc("/subtitles", func(w http.ResponseWriter, r *http.Request) {
		f.searches.Add(1)
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		select {
		case f.lastQueries <- q:
		default:
		}
		var data []map[string]any
		if q["languages"] == "en" {
			data = []map[string]any{
				{"attributes": map[string]any{"language": "en", "release": "Some.Movie.2001.1080p.BluRay.x264-GRP", "download_count": 100, "ratings": 8.0, "files": []map[string]any{{"file_id": 7}}}},
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		if f.quota.Load() {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		resp := map[string]any{"link": "http://" + r.Host + "/file.srt"}
		if limit := f.dlLimit.Load(); limit > 0 {
			reset := time.Now().Add(5 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
			n := f.downloads.Add(1)
			if n > limit {
				f.downloads.Add(-1)
				w.WriteHeader(http.StatusNotAcceptable)
				json.NewEncoder(w).Encode(map[string]any{"requests": limit, "remaining": 0, "message": "limit reached", "reset_time_utc": reset})
				return
			}
			resp["requests"], resp["remaining"], resp["reset_time_utc"] = n, limit-n, reset
		}
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/file.srt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "1\n00:00:01,000 --> 00:00:02,000\nHello\n")
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func TestSubtitleSweepFillsConfiguredLanguagesAndBacksOff(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	os_ := newFakeOpenSubtitles(t)
	env.server.TestSetSubtitlesBaseURL("fixture-key", os_.srv.URL)
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleLanguages": []string{"en", "it"}}, http.StatusOK)

	dir := t.TempDir()
	video := filepath.Join(dir, "Some Movie (2001).mkv")
	if err := os.WriteFile(video, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = env.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", video)

	statusURL := fmt.Sprintf("%s/api/movies/%d/subtitles/status", env.baseURL, m.ID)
	st := getJSON[map[string]any](t, env.client, statusURL)
	if len(st["languages"].([]any)) != 2 || len(st["present"].([]any)) != 0 {
		t.Fatalf("nothing on disk yet: %+v", st)
	}
	wanted := getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/subtitles/wanted")
	if len(wanted) != 1 || len(wanted[0]["missing"].([]any)) != 2 {
		t.Fatalf("expected the movie to be missing both languages: %+v", wanted)
	}

	res := postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/sweep", nil, http.StatusOK)
	if res["downloaded"] != float64(1) {
		t.Fatalf("expected the English subtitle only, got %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "Some Movie (2001).en.srt")); err != nil {
		t.Fatalf("English subtitle should be next to the video: %v", err)
	}
	st = getJSON[map[string]any](t, env.client, statusURL)
	if present := st["present"].([]any); len(present) != 1 || present[0] != "en" {
		t.Fatalf("status should now show en: %+v", st)
	}
	wanted = getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/subtitles/wanted")
	if len(wanted) != 1 || len(wanted[0]["missing"].([]any)) != 1 || wanted[0]["missing"].([]any)[0] != "it" {
		t.Fatalf("only Italian should remain wanted: %+v", wanted)
	}

	// The scheduled sweep (only active when automatic downloading is on)
	// remembers the Italian miss and does not ask again.
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleAutoDownload": true}, http.StatusOK)
	before := os_.searches.Load()
	env.server.TestSubtitleSweepJob(context.Background())
	if after := os_.searches.Load(); after != before {
		t.Fatalf("scheduled sweep re-searched a recent miss (%d -> %d searches)", before, after)
	}
}

func TestSubtitleQuotaStopsTheSweep(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	fake.quota.Store(true)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)

	video := filepath.Join(t.TempDir(), "Some Movie (2001).mkv")
	_ = os.WriteFile(video, []byte("v"), 0o644)
	m, _ := env.server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Monitored: true})
	_ = env.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", video)

	res := postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/sweep", nil, http.StatusOK)
	if res["downloaded"] != float64(0) || !strings.Contains(fmt.Sprint(res["message"]), "download limit") {
		t.Fatalf("expected a quota message, got %+v", res)
	}
}

func TestEpisodeSubtitleSearchUsesSeriesAndEpisodeNumbers(t *testing.T) {
	env := newTVAutoEnv(t, nil, []episodeSpec{{2, 5, "2020-01-01", library.StatusMissing, ""}})
	fake := newFakeOpenSubtitles(t)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)

	eps, _ := env.server.MovieRepo.ListEpisodes(env.seriesID)
	results := getJSON[[]map[string]any](t, env.client, fmt.Sprintf("%s/api/episodes/%d/subtitles?lang=en", env.baseURL, eps[0].ID))
	if len(results) != 1 {
		t.Fatalf("expected the fake English subtitle, got %+v", results)
	}
	q := <-fake.lastQueries
	if q["parent_tmdb_id"] != "1399" || q["season_number"] != "2" || q["episode_number"] != "5" || q["type"] != "episode" {
		t.Fatalf("wrong episode query: %v", q)
	}
}
