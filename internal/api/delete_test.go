package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ryanborg/mediarium/internal/library"
)

func deleteReq(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete %s: %v", url, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestDeleteMovieKeepsOrRemovesFile(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	seed := func(tmdbID int, title string) (int64, string) {
		file := filepath.Join(t.TempDir(), title+".mkv")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: tmdbID, Title: title, Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", file); err != nil {
			t.Fatal(err)
		}
		return m.ID, file
	}

	keepID, keepFile := seed(1, "Keep")
	if code := deleteReq(t, client, httpSrv.URL+"/api/movies/"+strconv.FormatInt(keepID, 10)); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if !exists(keepFile) {
		t.Fatal("file was deleted although deleteFiles wasn't requested")
	}

	goneID, goneFile := seed(2, "Gone")
	if code := deleteReq(t, client, httpSrv.URL+"/api/movies/"+strconv.FormatInt(goneID, 10)+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if exists(goneFile) {
		t.Fatal("file should have been deleted")
	}
	if movies := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/movies"); len(movies) != 0 {
		t.Fatalf("expected no movies left, got %+v", movies)
	}

	busy, err := server.MovieRepo.Add(library.Movie{TMDBID: 3, Title: "Busy", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.MovieRepo.SetStatus(busy.ID, library.StatusDownloading, "", "")
	if code := deleteReq(t, client, httpSrv.URL+"/api/movies/"+strconv.FormatInt(busy.ID, 10)); code != http.StatusConflict {
		t.Fatalf("expected 409 for a downloading movie, got %d", code)
	}
	if code := deleteReq(t, client, httpSrv.URL+"/api/movies/99999"); code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestDeleteSeriesWithFiles(t *testing.T) {
	env := newTVAutoEnv(t, nil, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusMissing, ""},
		{1, 2, "2020-01-08", library.StatusMissing, ""},
	})
	file := filepath.Join(t.TempDir(), "e1.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	eps, _ := env.server.MovieRepo.ListEpisodes(env.seriesID)
	if err := env.server.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "WEBDL-1080p", file); err != nil {
		t.Fatal(err)
	}

	url := env.baseURL + "/api/series/" + strconv.FormatInt(env.seriesID, 10)
	if code := deleteReq(t, env.client, url+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if exists(file) {
		t.Fatal("episode file should have been deleted")
	}
	if list := getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/series"); len(list) != 0 {
		t.Fatalf("series still listed: %+v", list)
	}
}
