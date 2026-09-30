package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/queue"
)

// libraryFile makes a real file in the movie folder and returns its path.
func libraryFile(t *testing.T, s *api.Server, name string) string {
	t.Helper()
	p := filepath.Join(s.TestMoviesRoot(), name, name+".mkv")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func movieState(t *testing.T, s *api.Server, id int64) library.Status {
	t.Helper()
	m, err := s.MovieRepo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return m.Status
}

func TestStartupPutsStuckMoviesRight(t *testing.T) {
	server, _, _ := newControlServer(t, false)
	type row struct {
		name     string
		status   library.Status
		file     string // "" none, "real" a file on disk, "gone" a recorded file that is no longer there
		behind   queue.Status
		byPerson bool // the download behind it was paused by a person
		want     library.Status
	}
	rows := []row{
		{"stuck downloading, nothing behind it, no file", library.StatusDownloading, "", "", false, library.StatusMissing},
		{"stuck downloading, the old file is still there", library.StatusDownloading, "real", "", false, library.StatusDownloaded},
		{"stuck downloading, the old file is gone", library.StatusDownloading, "gone", "", false, library.StatusMissing},
		{"downloading with a paused download behind it", library.StatusDownloading, "", queue.StatusPaused, true, library.StatusDownloading},
		{"downloading with a download that was running at the restart", library.StatusDownloading, "", queue.StatusDownloading, false, library.StatusDownloading},
		{"missing, but its file is on disk", library.StatusMissing, "real", "", false, library.StatusDownloaded},
		{"missing, and its recorded file is gone", library.StatusMissing, "gone", "", false, library.StatusMissing},
		{"missing, nothing recorded", library.StatusMissing, "", "", false, library.StatusMissing},
		{"downloaded stays downloaded", library.StatusDownloaded, "real", "", false, library.StatusDownloaded},
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: 1000 + i, Title: fmt.Sprintf("Movie %d", i), Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = m.ID
		path := ""
		switch r.file {
		case "real":
			path = libraryFile(t, server, fmt.Sprintf("Movie %d", i))
		case "gone":
			path = filepath.Join(server.TestMoviesRoot(), "deleted", "gone.mkv")
		}
		if path != "" {
			_ = server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", path)
		}
		_ = server.MovieRepo.SetStatus(m.ID, r.status, "", "")
		if r.behind != "" {
			qid, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "r"})
			if r.byPerson {
				_ = server.QueueRepo.Pause(qid, false)
			} else {
				_ = server.QueueRepo.SetStatus(qid, r.behind, "")
			}
		}
	}

	reopened, err := server.TestReopen()
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		if got := movieState(t, reopened, ids[i]); got != r.want {
			t.Errorf("%s: got %s, want %s", r.name, got, r.want)
		}
	}
}

func TestStartupPutsStuckEpisodesAndAlbumsRight(t *testing.T) {
	server, _, _ := newControlServer(t, false)
	series, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 50, Title: "Fixture Show", Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1, Monitored: true}, {Season: 1, Episode: 2, Monitored: true}, {Season: 2, Episode: 1, Monitored: true}, {Season: 2, Episode: 2, Monitored: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := server.MovieRepo.ListEpisodes(series.ID)
	byKey := map[string]library.Episode{}
	for _, e := range eps {
		byKey[fmt.Sprintf("S%dE%d", e.Season, e.Episode)] = e
	}
	file := libraryFile(t, server, "episode")
	_ = server.MovieRepo.SetEpisodeStatus(byKey["S1E1"].ID, library.StatusDownloading, "", "")             // stuck
	_ = server.MovieRepo.SetEpisodeStatus(byKey["S1E2"].ID, library.StatusMissing, "", "")                 // fine
	_ = server.MovieRepo.SetEpisodeStatus(byKey["S2E1"].ID, library.StatusDownloading, "", "")             // held by a paused season pack
	_ = server.MovieRepo.SetEpisodeStatus(byKey["S2E2"].ID, library.StatusDownloaded, "WEBDL-1080p", file) // has a file...
	_ = server.MovieRepo.SetEpisodeStatus(byKey["S2E2"].ID, library.StatusMissing, "", "")                 // ...but was marked missing
	pack, _ := server.QueueRepo.Enqueue(queue.Item{SeriesID: series.ID, Season: 2, ReleaseTitle: "Fixture.Show.S02.1080p.WEB-DL-GRP"})
	_ = server.QueueRepo.Pause(pack, false)

	artist, albums, err := server.MusicRepo.AddArtist(music.Artist{MBID: "artist-1", Name: "Fixture Artist", Monitored: true},
		[]music.Album{{MBID: "album-1", Title: "Stuck", Monitored: true, Status: music.StatusDownloading}, {MBID: "album-2", Title: "Held", Monitored: true, Status: music.StatusDownloading}})
	if err != nil {
		t.Fatal(err)
	}
	_ = artist
	heldQueue, _ := server.QueueRepo.Enqueue(queue.Item{AlbumID: albums[1].ID, ReleaseTitle: "Artist - Held"})
	_ = server.QueueRepo.Pause(heldQueue, false)

	reopened, err := server.TestReopen()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]library.Status{
		"S1E1": library.StatusMissing,     // nothing was downloading it
		"S1E2": library.StatusMissing,     // untouched
		"S2E1": library.StatusDownloading, // the paused pack still holds it
		"S2E2": library.StatusDownloaded,  // its file is on disk
	}
	got, _ := reopened.MovieRepo.ListEpisodes(series.ID)
	for _, e := range got {
		k := fmt.Sprintf("S%dE%d", e.Season, e.Episode)
		if w := want[k]; e.Status != w {
			t.Errorf("%s: got %s, want %s", k, e.Status, w)
		}
	}
	if a, _ := reopened.MusicRepo.GetAlbum(albums[0].ID); a.Status != music.StatusMissing {
		t.Errorf("an album stuck downloading with nothing behind it should be missing, got %s", a.Status)
	}
	if a, _ := reopened.MusicRepo.GetAlbum(albums[1].ID); a.Status != music.StatusDownloading {
		t.Errorf("an album held by a paused download stays as it is, got %s", a.Status)
	}
}

func TestWaitingListNeverShowsATitleThatIsDownloaded(t *testing.T) {
	server, base, client := newControlServer(t, false)
	add := func(tmdb int, title string, monitored bool) library.Movie {
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: title, Monitored: monitored})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	staleWithFile := add(1, "Has A File", true)
	_ = server.MovieRepo.SetStatus(staleWithFile.ID, library.StatusDownloaded, "WEBDL-1080p", libraryFile(t, server, "Has A File"))
	_ = server.MovieRepo.SetStatus(staleWithFile.ID, library.StatusMissing, "", "") // a wrong status: the file is there
	_ = server.MovieRepo.SetNoUpgrade(staleWithFile.ID, true)
	add(2, "Really Waiting", true)
	add(3, "Not Monitored", false)

	titles := map[string]bool{}
	for _, it := range getJSON[[]map[string]any](t, client, base+"/api/wanted?kind=missing") {
		titles[it["title"].(string)] = true
	}
	if !titles["Really Waiting"] || titles["Has A File"] || titles["Not Monitored"] || len(titles) != 1 {
		t.Fatalf("only a monitored title with no file is waiting for a release: %v", titles)
	}
}

func TestTurningOffBetterVersionsClearsWaitingDownloads(t *testing.T) {
	server, base, client := newControlServer(t, false)
	dl, _ := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Downloaded Movie", Monitored: true})
	_ = server.MovieRepo.SetStatus(dl.ID, library.StatusDownloaded, "WEBDL-1080p", libraryFile(t, server, "Downloaded Movie"))

	add := func(movieID int64, st queue.Status, byPerson bool) int64 {
		id, err := server.QueueRepo.Enqueue(queue.Item{MovieID: movieID, ReleaseTitle: fmt.Sprintf("r-%s-%v", st, byPerson)})
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case st == queue.StatusPaused:
			_ = server.QueueRepo.Pause(id, !byPerson)
		case st != queue.StatusQueued:
			_ = server.QueueRepo.SetStatus(id, st, "")
		}
		_ = os.MkdirAll(server.TestWorkDirFor(id), 0o755)
		_ = os.WriteFile(filepath.Join(server.TestWorkDirFor(id), "part"), []byte("x"), 0o644)
		return id
	}
	waitingQueued := add(dl.ID, queue.StatusQueued, false)
	waitingInterrupted := add(dl.ID, queue.StatusPaused, false)
	pausedByPerson := add(dl.ID, queue.StatusPaused, true)
	running := add(dl.ID, queue.StatusDownloading, false)

	// Switching the search for better versions back on removes nothing.
	postJSONMethod[any](t, client, http.MethodPut, fmt.Sprintf("%s/api/movies/%d/no-upgrade", base, dl.ID), map[string]bool{"noUpgrade": false}, http.StatusOK)
	for _, id := range []int64{waitingQueued, waitingInterrupted, pausedByPerson, running} {
		if _, err := server.QueueRepo.Get(id); err != nil {
			t.Fatalf("turning better versions on must not remove anything: %v", err)
		}
	}

	postJSONMethod[any](t, client, http.MethodPut, fmt.Sprintf("%s/api/movies/%d/no-upgrade", base, dl.ID), map[string]bool{"noUpgrade": true}, http.StatusOK)
	for _, id := range []int64{waitingQueued, waitingInterrupted} {
		if _, err := server.QueueRepo.Get(id); err == nil {
			t.Errorf("item %d was only waiting and should be removed", id)
		}
		if exists(server.TestWorkDirFor(id)) {
			t.Errorf("item %d: its partial files should be deleted", id)
		}
	}
	for _, id := range []int64{pausedByPerson, running} {
		if _, err := server.QueueRepo.Get(id); err != nil {
			t.Errorf("item %d was paused on purpose or is downloading, and should stay: %v", id, err)
		}
	}
	if got := movieState(t, server, dl.ID); got != library.StatusDownloading && got != library.StatusDownloaded {
		t.Errorf("the movie itself is untouched, got %s", got)
	}

	// A first download is not an upgrade, so switching off better versions
	// does not remove it.
	fresh, _ := server.MovieRepo.Add(library.Movie{TMDBID: 2, Title: "Fresh Movie", Monitored: true})
	first := add(fresh.ID, queue.StatusQueued, false)
	postJSONMethod[any](t, client, http.MethodPut, fmt.Sprintf("%s/api/movies/%d/no-upgrade", base, fresh.ID), map[string]bool{"noUpgrade": true}, http.StatusOK)
	if _, err := server.QueueRepo.Get(first); err != nil {
		t.Fatalf("a download for a title with no file yet stays: %v", err)
	}
	// Stopping monitoring does clear what is only waiting.
	postJSONMethod[any](t, client, http.MethodPut, fmt.Sprintf("%s/api/movies/%d/monitored", base, fresh.ID), map[string]bool{"monitored": false}, http.StatusOK)
	if _, err := server.QueueRepo.Get(first); err == nil {
		t.Fatal("a waiting download of a title that is no longer monitored should be removed")
	}
}

func TestTurningOffBetterVersionsForAShowClearsItsWaitingDownloads(t *testing.T) {
	server, base, client := newControlServer(t, false)
	series, _ := server.MovieRepo.AddSeries(library.Series{TMDBID: 60, Title: "Fixture Show", Monitored: true}, []library.Episode{{Season: 1, Episode: 1, Monitored: true}})
	eps, _ := server.MovieRepo.ListEpisodes(series.ID)
	_ = server.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "WEBDL-720p", libraryFile(t, server, "Fixture Show S01E01"))
	id, _ := server.QueueRepo.Enqueue(queue.Item{SeriesID: series.ID, Season: 1, Episode: 1, ReleaseTitle: "Fixture.Show.S01E01.1080p.WEB-DL-GRP"})

	postJSONMethod[any](t, client, http.MethodPut, fmt.Sprintf("%s/api/series/%d/no-upgrade", base, series.ID), map[string]bool{"noUpgrade": true}, http.StatusOK)
	if _, err := server.QueueRepo.Get(id); err == nil {
		t.Fatal("the waiting upgrade of a show that is not looking for better versions should be removed")
	}
}

func TestStartupRemovesWaitingUpgradesForTitlesThatDoNotWantThem(t *testing.T) {
	server, _, _ := newControlServer(t, false)
	upgrades, _ := server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Wants Upgrades", Monitored: true})
	leftAlone, _ := server.MovieRepo.Add(library.Movie{TMDBID: 2, Title: "Left Alone", Monitored: true})
	for _, m := range []library.Movie{upgrades, leftAlone} {
		_ = server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", libraryFile(t, server, m.Title))
	}
	_ = server.MovieRepo.SetNoUpgrade(leftAlone.ID, true)
	keep, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: upgrades.ID, ReleaseTitle: "keep"})
	drop, _ := server.QueueRepo.Enqueue(queue.Item{MovieID: leftAlone.ID, ReleaseTitle: "drop"})

	reopened, err := server.TestReopen()
	if err != nil {
		t.Fatal(err)
	}
	if it, err := reopened.QueueRepo.Get(keep); err != nil || it.Status != queue.StatusQueued {
		t.Fatalf("a wanted upgrade stays waiting in line: %+v, %v", it, err)
	}
	if _, err := reopened.QueueRepo.Get(drop); err == nil {
		t.Fatal("an upgrade for a title that is downloaded and not looking for better versions should be gone")
	}
	if got := movieState(t, reopened, leftAlone.ID); got != library.StatusDownloaded {
		t.Fatalf("the movie stays downloaded, got %s", got)
	}
}
