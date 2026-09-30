package api_test

import (
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestDiskUsageSaysWhatRemovingWouldDelete(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	movies := server.TestMoviesRoot()

	// A movie with its own folder: everything in the folder counts.
	folder := filepath.Join(movies, "Gone (2002)")
	putFile(t, filepath.Join(folder, "Gone (2002).mkv"), "12345678")
	putFile(t, filepath.Join(folder, "Gone (2002).en.srt"), "12")
	putFile(t, filepath.Join(folder, "poster.jpg"), "123")
	id := seedMovie(t, server.MovieRepo, 2, "Gone", 2002, filepath.Join(folder, "Gone (2002).mkv"))
	got := getJSON[map[string]any](t, admin, base+"/api/movies/"+strconv.FormatInt(id, 10)+"/disk-usage")
	if got["files"] != float64(3) || got["bytes"] != float64(13) {
		t.Fatalf("own folder: %v", got)
	}

	// A movie sharing its folder with another title: only its own file and the
	// files named after it count.
	shared := filepath.Join(movies, "Shared")
	putFile(t, filepath.Join(shared, "Mine (2001).mkv"), "1234")
	putFile(t, filepath.Join(shared, "Mine (2001).srt"), "12")
	putFile(t, filepath.Join(shared, "Other (2003).mkv"), "123456789012")
	mine := seedMovie(t, server.MovieRepo, 3, "Mine", 2001, filepath.Join(shared, "Mine (2001).mkv"))
	seedMovie(t, server.MovieRepo, 4, "Other", 2003, filepath.Join(shared, "Other (2003).mkv"))
	got = getJSON[map[string]any](t, admin, base+"/api/movies/"+strconv.FormatInt(mine, 10)+"/disk-usage")
	if got["files"] != float64(2) || got["bytes"] != float64(6) {
		t.Fatalf("shared folder: %v", got)
	}

	// Nothing downloaded: zero, not an error.
	none, err := server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Nothing", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	got = getJSON[map[string]any](t, admin, base+"/api/movies/"+strconv.FormatInt(none.ID, 10)+"/disk-usage")
	if got["files"] != float64(0) || got["bytes"] != float64(0) {
		t.Fatalf("nothing on disk: %v", got)
	}

	// Removing is for administrators, and so is asking what it would delete.
	resp, err := member.Get(base + "/api/movies/" + strconv.FormatInt(id, 10) + "/disk-usage")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a member got %d", resp.StatusCode)
	}
	resp, err = admin.Get(base + "/api/movies/99999/disk-usage")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown movie gave %d", resp.StatusCode)
	}
}

func TestSeriesDiskUsage(t *testing.T) {
	env := newTVAutoEnv(t, nil, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusMissing, ""},
		{2, 1, "2021-01-01", library.StatusMissing, ""},
	})
	tvRoot := filepath.Join(t.TempDir(), "tv")
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"tvPath": tvRoot}, http.StatusOK)
	show := filepath.Join(tvRoot, "Fixture Show (2020)")
	e1 := filepath.Join(show, "Season 01", "Fixture Show (2020) - S01E01 - Pilot.mkv")
	e2 := filepath.Join(show, "Season 02", "Fixture Show (2020) - S02E01 - Return.mkv")
	putFile(t, e1, "1234")
	putFile(t, e2, "12345")
	putFile(t, filepath.Join(show, "poster.jpg"), "12")
	eps, _ := env.server.MovieRepo.ListEpisodes(env.seriesID)
	for i, f := range []string{e1, e2} {
		if err := env.server.MovieRepo.SetEpisodeStatus(eps[i].ID, library.StatusDownloaded, "WEBDL-1080p", f); err != nil {
			t.Fatal(err)
		}
	}
	got := getJSON[map[string]any](t, env.client, env.baseURL+"/api/series/"+strconv.FormatInt(env.seriesID, 10)+"/disk-usage")
	if got["files"] != float64(3) || got["bytes"] != float64(11) {
		t.Fatalf("series: %v", got)
	}
}
