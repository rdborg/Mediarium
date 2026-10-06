package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/settings"
)

// fakeCleanupPlex is a Plex with a movie library and a TV library. Every
// play it reports was 40 days ago, except a "lastViewedAt" of 0 (unknown).
func fakeCleanupPlex(t *testing.T) *httptest.Server {
	t.Helper()
	old := time.Now().AddDate(0, 0, -40).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var c map[string]any
		switch {
		case r.URL.Path == "/library/sections":
			c = map[string]any{"Directory": []map[string]any{{"key": "1", "type": "movie", "title": "Films"}, {"key": "2", "type": "show", "title": "TV"}}}
		case r.URL.Path == "/library/sections/1/all":
			c = map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "1", "Guid": []map[string]any{{"id": "tmdb://603"}}, "viewCount": 1, "lastViewedAt": old}, // The Matrix
				{"ratingKey": "2", "Guid": []map[string]any{{"id": "tmdb://949"}}, "viewCount": 1, "lastViewedAt": old}, // Heat
				{"ratingKey": "3", "Guid": []map[string]any{{"id": "tmdb://680"}}, "viewCount": 1, "lastViewedAt": 0},   // Pulp Fiction
			}}
		case r.URL.Path == "/library/sections/2/all" && r.URL.Query().Get("type") == "4":
			ep := func(n int) map[string]any {
				return map[string]any{"ratingKey": "e" + string(rune('0'+n)), "grandparentRatingKey": "50", "parentIndex": 1, "index": n, "viewCount": 1, "lastViewedAt": old}
			}
			c = map[string]any{"Metadata": []map[string]any{ep(1), ep(2), ep(3)}} // E04 never watched
		case r.URL.Path == "/library/sections/2/all":
			c = map[string]any{"Metadata": []map[string]any{{"ratingKey": "50", "Guid": []map[string]any{{"id": "tmdb://95396"}}}}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCleanupKeepsWhatIsStillWanted(t *testing.T) {
	server, base, client := loginNewServer(t)
	plex, err := server.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: fakeCleanupPlex(t).URL, Token: "tok", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	longAgo := time.Now().AddDate(0, 0, -90)
	write := func(path string, mtime time.Time) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return path
	}
	movie := func(tmdb int, title string, fileTime, added time.Time) (library.Movie, string) {
		t.Helper()
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: title, Year: 2000, Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		file := write(filepath.Join(server.TestMoviesRoot(), title+" (2000)", title+" (2000).mkv"), fileTime)
		if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", file); err != nil {
			t.Fatal(err)
		}
		server.TestSetMovieAddedAt(m.ID, added)
		return m, file
	}
	matrix, matrixFile := movie(603, "The Matrix", longAgo, longAgo)                                 // watched 40 days ago: goes
	_, heatFile := movie(949, "Heat", time.Now().AddDate(0, 0, -2), longAgo)                         // watched, then downloaded again: stays
	_, pulpFile := movie(680, "Pulp Fiction", longAgo, longAgo)                                      // watched, date unknown: stays
	alien, alienFile := movie(348, "Alien", longAgo, longAgo)                                        // never watched for 90 days: goes
	_, roninFile := movie(8195, "Ronin", time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, -6, 0)) // wanted for months, downloaded yesterday: stays

	// A show with a two-episode file (both watched), and a file holding a
	// watched and an unwatched episode.
	tvRoot := t.TempDir()
	if err := server.Settings.Set(settings.KeyTVPath, tvRoot, false); err != nil {
		t.Fatal(err)
	}
	showDir := filepath.Join(tvRoot, "Severance")
	poster := write(filepath.Join(showDir, "poster.jpg"), longAgo)
	both := write(filepath.Join(showDir, "Season 01", "Severance - S01E01-E02.mkv"), longAgo)
	half := write(filepath.Join(showDir, "Season 01", "Severance - S01E03-E04.mkv"), longAgo)
	sh, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 95396, Title: "Severance", Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1}, {Season: 1, Episode: 2}, {Season: 1, Episode: 3}, {Season: 1, Episode: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := server.MovieRepo.ListEpisodes(sh.ID)
	for _, e := range eps {
		file := both
		if e.Episode > 2 {
			file = half
		}
		if err := server.MovieRepo.SetEpisodeStatus(e.ID, library.StatusDownloaded, "WEBDL-1080p", file); err != nil {
			t.Fatal(err)
		}
	}

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/watched/settings", map[string]any{"sync": true}, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/watched/settings", map[string]any{"cleanup": map[string]any{"moviesWatchedDays": 30, "moviesUnwatchedDays": 60, "episodesWatchedDays": 30}}, http.StatusOK)

	preview := getJSON[map[string]any](t, client, base+"/api/watched/cleanup/preview")["items"].([]any)
	var titles []string
	for _, it := range preview {
		titles = append(titles, it.(map[string]any)["title"].(string))
	}
	// Oldest first: Alien arrived 90 days ago, the others were watched 40 days ago.
	want := []string{"Alien (2000)", "Severance S01E01-E02", "The Matrix (2000)"}
	if len(titles) != len(want) {
		t.Fatalf("preview %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("preview %v, want %v", titles, want)
		}
	}

	// A media server that doesn't answer makes the watch list incomplete:
	// nothing is removed until every server answers again.
	broken, err := server.MediaServers.Create(mediaservers.Server{Name: "Jellyfin", Kind: mediaservers.KindJellyfin, BaseURL: "http://127.0.0.1:1", Token: "k", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/watched/cleanup/run", nil, http.StatusConflict)
	if err := server.MediaServers.Delete(broken.ID); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusOK)

	res := postJSON[map[string]any](t, client, base+"/api/watched/cleanup/run", nil, http.StatusOK)
	if len(res["removed"].([]any)) != 3 {
		t.Fatalf("run: %v", res)
	}
	for _, f := range []string{matrixFile, alienFile, both} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s should be gone: %v", f, err)
		}
	}
	for _, f := range []string{heatFile, pulpFile, roninFile, half, poster} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s should still be there: %v", f, err)
		}
	}
	eps, _ = server.MovieRepo.ListEpisodes(sh.ID)
	for _, e := range eps {
		gone := e.Episode <= 2
		if gone != (e.FilePath == "") || gone == e.Monitored {
			t.Errorf("S01E%02d: file %q, monitored %v", e.Episode, e.FilePath, e.Monitored)
		}
	}
	for _, id := range []int64{matrix.ID, alien.ID} {
		if m, _ := server.MovieRepo.Get(id); m.FilePath != "" || m.Monitored {
			t.Errorf("%s still has a file or is still looked for: %+v", m.Title, m)
		}
	}

	// With no media server switched on, reading fails instead of looking
	// like nobody watched anything.
	if err := server.MediaServers.Delete(plex.ID); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusBadGateway)
	postJSON[map[string]any](t, client, base+"/api/watched/cleanup/run", nil, http.StatusOK)
}
