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
)

// fakeWatchPlex is a Plex with one movie library: The Matrix (603) watched
// 40 days ago, Heat (949) watched yesterday, Alien (348) watched long ago.
func fakeWatchPlex(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var c map[string]any
		switch r.URL.Path {
		case "/library/sections":
			c = map[string]any{"Directory": []map[string]any{{"key": "1", "type": "movie", "title": "Films"}}}
		case "/library/sections/1/all":
			c = map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "1", "Guid": []map[string]any{{"id": "tmdb://603"}}, "viewCount": 1, "lastViewedAt": time.Now().AddDate(0, 0, -40).Unix()},
				{"ratingKey": "2", "Guid": []map[string]any{{"id": "tmdb://949"}}, "viewCount": 3, "lastViewedAt": time.Now().AddDate(0, 0, -1).Unix()},
				{"ratingKey": "3", "Guid": []map[string]any{{"id": "tmdb://348"}}, "viewCount": 1, "lastViewedAt": time.Now().AddDate(-1, 0, 0).Unix()},
			}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWatchedAndCleanup(t *testing.T) {
	server, base, client := loginNewServer(t)
	if _, err := server.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: fakeWatchPlex(t).URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	root := server.TestMoviesRoot()
	add := func(tmdb int, title string) (library.Movie, string) {
		t.Helper()
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: title, Year: 2000, Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(root, title+" (2000)", title+" (2000).mkv")
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", file); err != nil {
			t.Fatal(err)
		}
		return m, file
	}
	matrix, matrixFile := add(603, "The Matrix")
	_, heatFile := add(949, "Heat")
	alien, alienFile := add(348, "Alien")
	if _, _, err := server.MovieRepo.SetTitleTags(library.TagMovie, alien.ID, []string{"Keep"}); err != nil {
		t.Fatal(err)
	}

	// Off by default: nothing is read and nothing is shown.
	st := getJSON[map[string]any](t, client, base+"/api/watched/settings")
	if st["sync"] != false || st["cleanup"].(map[string]any)["enabled"] != false {
		t.Fatalf("defaults: %v", st)
	}
	if w := getJSON[map[string]map[string]any](t, client, base+"/api/watched"); len(w["movies"]) != 0 {
		t.Fatalf("watched while off: %v", w)
	}

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/watched/settings", map[string]any{"sync": true}, http.StatusOK)
	synced := postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusOK)
	if synced["movies"] != float64(3) || synced["servers"] != float64(1) {
		t.Fatalf("sync: %v", synced)
	}
	w := getJSON[map[string]map[string]map[string]any](t, client, base+"/api/watched")
	if len(w["movies"]) != 3 || w["movies"][jsonID(matrix.ID)]["plays"] != float64(1) {
		t.Fatalf("watched: %v", w)
	}

	// Rules: needs at least one; then the preview lists only The Matrix
	// (watched 40 days ago); Heat is too recent and Alien carries "Keep".
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/watched/settings", map[string]any{"cleanup": map[string]any{"enabled": true}}, http.StatusBadRequest)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/watched/settings", map[string]any{"cleanup": map[string]any{"enabled": false, "moviesWatchedDays": 30, "keepTags": []string{"keep", " "}}}, http.StatusOK)
	preview := getJSON[map[string]any](t, client, base+"/api/watched/cleanup/preview")["items"].([]any)
	if len(preview) != 1 || preview[0].(map[string]any)["id"] != float64(matrix.ID) {
		t.Fatalf("preview: %v", preview)
	}
	for _, f := range []string{matrixFile, heatFile, alienFile} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("a preview deleted %s", f)
		}
	}

	res := postJSON[map[string]any](t, client, base+"/api/watched/cleanup/run", nil, http.StatusOK)
	if len(res["removed"].([]any)) != 1 {
		t.Fatalf("run: %v", res)
	}
	if _, err := os.Stat(matrixFile); !os.IsNotExist(err) {
		t.Fatalf("the watched movie's file is still there: %v", err)
	}
	for _, f := range []string{heatFile, alienFile} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("%s was removed: %v", f, err)
		}
	}
	m, _ := server.MovieRepo.Get(matrix.ID)
	if m.Status != library.StatusMissing || m.Monitored || m.FilePath != "" {
		t.Fatalf("a cleaned movie isn't looked for again: %+v", m)
	}

	// Switching reading off switches cleanup off too.
	got := postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/watched/settings", map[string]any{"sync": false}, http.StatusOK)
	if got["cleanup"].(map[string]any)["enabled"] != false {
		t.Fatalf("cleanup left on without watch data: %v", got)
	}
}

func jsonID(id int64) string { b, _ := json.Marshal(id); return string(b) }
