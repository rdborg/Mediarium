package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/queue"
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
	_, err := os.Lstat(path)
	return err == nil
}

// tree lists every path under dir, relative and sorted.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, _ os.FileInfo, err error) error {
		if err == nil && p != dir {
			r, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(r))
		}
		return nil
	})
	sort.Strings(out)
	return strings.Join(out, ",")
}

func seedMovie(t *testing.T, repo *library.Repo, tmdbID int, title string, year int, file string) int64 {
	t.Helper()
	m, err := repo.Add(library.Movie{TMDBID: tmdbID, Title: title, Year: year, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if file != "" {
		if err := repo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", file); err != nil {
			t.Fatal(err)
		}
	}
	return m.ID
}

func TestDeleteMovieKeepsOrRemovesItsFolder(t *testing.T) {
	server, base, client := loginNewServer(t)
	movies := server.TestMoviesRoot()
	movieURL := func(id int64) string { return base + "/api/movies/" + strconv.FormatInt(id, 10) }

	// Without deleteFiles the library is left alone.
	keepFile := filepath.Join(movies, "Keep (2001)", "Keep (2001).mkv")
	putFile(t, keepFile, "keep")
	keepID := seedMovie(t, server.MovieRepo, 1, "Keep", 2001, keepFile)
	if code := deleteReq(t, client, movieURL(keepID)); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if !exists(keepFile) {
		t.Fatal("file was deleted although deleteFiles wasn't requested")
	}

	// With it the movie's whole folder goes: video, subtitles, .nfo, artwork.
	folder := filepath.Join(movies, "Gone (2002)")
	for _, f := range []string{"Gone (2002).mkv", "Gone (2002).en.srt", "Gone (2002).nfo", "poster.jpg", "Featurettes/Making of.mkv"} {
		putFile(t, filepath.Join(folder, f), "x")
	}
	goneID := seedMovie(t, server.MovieRepo, 2, "Gone", 2002, filepath.Join(folder, "Gone (2002).mkv"))
	if code := deleteReq(t, client, movieURL(goneID)+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if exists(folder) {
		t.Fatalf("the movie's folder should be gone, left: %s", tree(t, folder))
	}
	if !exists(movies) || !exists(keepFile) {
		t.Fatal("the library folder and other titles must stay")
	}

	// A file loose in the library folder: only it and the files named after
	// it go.
	for _, f := range []string{"Loose (2003).mkv", "Loose (2003).nfo", "Loose (2003).en.srt", "Other (2003).mkv"} {
		putFile(t, filepath.Join(movies, f), "x")
	}
	looseID := seedMovie(t, server.MovieRepo, 3, "Loose", 2003, filepath.Join(movies, "Loose (2003).mkv"))
	if code := deleteReq(t, client, movieURL(looseID)+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if got := tree(t, movies); got != "Keep (2001),Keep (2001)/Keep (2001).mkv,Other (2003).mkv" {
		t.Fatalf("library after removing a loose movie: %s", got)
	}

	// A folder shared with another title in the library: only this title's
	// files go.
	shared := filepath.Join(movies, "Collection")
	putFile(t, filepath.Join(shared, "First (2004).mkv"), "x")
	putFile(t, filepath.Join(shared, "Second (2005).mkv"), "x")
	firstID := seedMovie(t, server.MovieRepo, 4, "First", 2004, filepath.Join(shared, "First (2004).mkv"))
	seedMovie(t, server.MovieRepo, 5, "Second", 2005, filepath.Join(shared, "Second (2005).mkv"))
	if code := deleteReq(t, client, movieURL(firstID)+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if exists(filepath.Join(shared, "First (2004).mkv")) || !exists(filepath.Join(shared, "Second (2005).mkv")) {
		t.Fatalf("shared folder after removing one title: %s", tree(t, shared))
	}

	if code := deleteReq(t, client, base+"/api/movies/99999"); code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestDeleteMovieRefusesFilesOutsideTheLibrary(t *testing.T) {
	server, base, client := loginNewServer(t)
	movies := server.TestMoviesRoot()
	outside := t.TempDir()
	putFile(t, filepath.Join(outside, "Escape (2020).mkv"), "not yours")
	link := filepath.Join(movies, "Escape (2020)")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
	id := seedMovie(t, server.MovieRepo, 6, "Escape", 2020, filepath.Join(link, "Escape (2020).mkv"))
	if code := deleteReq(t, client, base+"/api/movies/"+strconv.FormatInt(id, 10)+"?deleteFiles=true"); code != http.StatusConflict {
		t.Fatalf("expected 409 for a file reached through a symlink leading out, got %d", code)
	}
	if !exists(filepath.Join(outside, "Escape (2020).mkv")) || !exists(link) {
		t.Fatal("nothing outside the library may be deleted")
	}
	if len(getJSON[[]map[string]any](t, client, base+"/api/movies")) != 1 {
		t.Fatal("a refused delete must leave the movie in the library")
	}
}

// TestDeleteMovieCancelsDownloadsAndClearsLeftovers removes a movie that is
// still being downloaded and has an older failed download: both queue
// entries and both working folders go, with or without deleteFiles.
func TestDeleteMovieCancelsDownloadsAndClearsLeftovers(t *testing.T) {
	server, base, client := loginNewServer(t)
	work := server.TestWorkDir()
	id := seedMovie(t, server.MovieRepo, 7, "Busy", 2007, "")
	_ = server.MovieRepo.SetStatus(id, library.StatusDownloading, "", "")

	var dirs []string
	for _, status := range []queue.Status{queue.StatusFailed, queue.StatusDownloading} {
		qid, err := server.QueueRepo.Enqueue(queue.Item{MovieID: id, ReleaseTitle: "Busy.2007.1080p"})
		if err != nil {
			t.Fatal(err)
		}
		_ = server.QueueRepo.SetStatus(qid, status, "")
		dir := queueDir(work, qid)
		putFile(t, filepath.Join(dir, "part.mkv"), "x")
		dirs = append(dirs, dir)
	}
	// Another title's download is left alone.
	otherID := seedMovie(t, server.MovieRepo, 8, "Other", 2008, "")
	otherQ, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: otherID, ReleaseTitle: "Other.2008.1080p"})
	putFile(t, filepath.Join(queueDir(work, otherQ), "part.mkv"), "x")

	if code := deleteReq(t, client, base+"/api/movies/"+strconv.FormatInt(id, 10)); code != http.StatusOK {
		t.Fatalf("a downloading movie can be removed now, got %d", code)
	}
	for _, d := range dirs {
		if exists(d) {
			t.Fatalf("%s should be gone", d)
		}
	}
	list := getJSON[[]map[string]any](t, client, base+"/api/queue")
	if len(list) != 1 || list[0]["id"] != float64(otherQ) {
		t.Fatalf("only the other title's download should be left: %+v", list)
	}
	if !exists(queueDir(work, otherQ)) {
		t.Fatal("another title's download folder was removed")
	}
}

func TestDeleteSeriesRemovesItsWholeFolder(t *testing.T) {
	env := newTVAutoEnv(t, nil, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusMissing, ""},
		{2, 1, "2021-01-01", library.StatusMissing, ""},
	})
	tvRoot := filepath.Join(t.TempDir(), "tv")
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"tvPath": tvRoot}, http.StatusOK)
	show := filepath.Join(tvRoot, "Fixture Show (2020)")
	e1 := filepath.Join(show, "Season 01", "Fixture Show (2020) - S01E01 - Pilot.mkv")
	e2 := filepath.Join(show, "Season 02", "Fixture Show (2020) - S02E01 - Return.mkv")
	for _, f := range []string{e1, e2, filepath.Join(show, "Season 01", "Fixture Show (2020) - S01E01 - Pilot.en.srt"), filepath.Join(show, "poster.jpg"), filepath.Join(show, "tvshow.nfo")} {
		putFile(t, f, "x")
	}
	putFile(t, filepath.Join(tvRoot, "Another Show (2019)", "Season 01", "Another Show (2019) - S01E01.mkv"), "keep")
	eps, _ := env.server.MovieRepo.ListEpisodes(env.seriesID)
	for i, f := range []string{e1, e2} {
		if err := env.server.MovieRepo.SetEpisodeStatus(eps[i].ID, library.StatusDownloaded, "WEBDL-1080p", f); err != nil {
			t.Fatal(err)
		}
	}
	// A failed download of the show leaves a folder in the downloads area.
	qid, _ := env.server.QueueRepo.Enqueue(queue.Item{SeriesID: env.seriesID, Season: 1, ReleaseTitle: "Fixture.Show.S01"})
	_ = env.server.QueueRepo.SetStatus(qid, queue.StatusFailed, "")
	leftover := queueDir(env.server.TestWorkDir(), qid)
	putFile(t, filepath.Join(leftover, "a.rar"), "x")

	url := env.baseURL + "/api/series/" + strconv.FormatInt(env.seriesID, 10)
	if code := deleteReq(t, env.client, url+"?deleteFiles=true"); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if exists(show) {
		t.Fatalf("the show's folder should be gone, left: %s", tree(t, show))
	}
	if exists(leftover) {
		t.Fatal("the failed download's folder should be gone")
	}
	if !exists(tvRoot) || !exists(filepath.Join(tvRoot, "Another Show (2019)", "Season 01", "Another Show (2019) - S01E01.mkv")) {
		t.Fatal("the TV folder and other shows must stay")
	}
	if list := getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/series"); len(list) != 0 {
		t.Fatalf("series still listed: %+v", list)
	}
	if list := getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/queue"); len(list) != 0 {
		t.Fatalf("the show's downloads should be gone from the queue: %+v", list)
	}
}
