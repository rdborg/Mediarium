package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/subtitles"
)

func (e *locateEnv) status(t *testing.T, method, path string, client *http.Client) int {
	t.Helper()
	req, _ := http.NewRequest(method, e.base+path, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// The case from the report: a special named S02E09 was linked to episode 9 of
// season 2. Unlinking frees the file for the special, and leaves it on the disk.
func TestUnlinkEpisodeFileFreesItForAnotherEpisode(t *testing.T) {
	e := newLocateEnv(t)
	_, eps := e.addShow(t, 99, "Show", false, [2]int{2, 9}, [2]int{0, 9})
	var linked, special int64
	for _, ep := range eps {
		if ep.Season == 0 {
			special = ep.ID
		} else {
			linked = ep.ID
		}
	}
	file := filepath.Join(e.tv, "Show", "Show.S02E09.Special.mkv")
	writeBody(t, file, "video")
	if err := e.server.MovieRepo.SetEpisodeStatus(linked, library.StatusDownloaded, "", file); err != nil {
		t.Fatal(err)
	}
	// While it is linked, the special can't take it.
	postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/episodes/%d/locate-file", e.base, special), map[string]string{"path": file}, http.StatusConflict)

	if err := e.server.SubtitleFiles.Record(subtitles.SavedFile{Kind: "episode", MediaID: linked, Language: "en", Path: file + ".en.srt"}); err != nil {
		t.Fatal(err)
	}
	got := postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/episodes/%d/unlink-file", e.base, linked), nil, http.StatusOK)
	if got["file"] != "Show.S02E09.Special.mkv" {
		t.Fatalf("answer = %v", got)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the file must stay on the disk: %v", err)
	}
	after, _ := e.server.MovieRepo.GetEpisodeByID(linked)
	if after.Status != library.StatusMissing || after.FilePath != "" || after.Quality != "" {
		t.Fatalf("episode after unlink: %+v", after)
	}
	if cands, _ := e.server.SubtitleFiles.UpgradeCandidates(time.Time{}, time.Now().Add(time.Hour), 10); len(cands) != 0 {
		t.Fatalf("the subtitle record should be gone: %v", cands)
	}
	// Now the file can be linked to the special.
	postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/episodes/%d/locate-file", e.base, special), map[string]string{"path": file}, http.StatusOK)
	// An episode with no file, or one that doesn't exist, is refused.
	postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/episodes/%d/unlink-file", e.base, linked), nil, http.StatusConflict)
	postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/episodes/%d/unlink-file", e.base, 99999), nil, http.StatusNotFound)
}

func TestUnlinkMovieFile(t *testing.T) {
	e := newLocateEnv(t)
	m := e.addMovie(t, 603, "The Matrix")
	other := e.addMovie(t, 604, "The Matrix Reloaded")
	file := filepath.Join(e.movies, "The Matrix (1999)", "matrix.mkv")
	writeBody(t, file, "video")
	if err := e.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", file); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/movies/%d/locate-file", e.base, other.ID), map[string]string{"path": file}, http.StatusConflict)
	postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/movies/%d/unlink-file", e.base, m.ID), nil, http.StatusOK)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the file must stay on the disk: %v", err)
	}
	after, _ := e.server.MovieRepo.Get(m.ID)
	if after.Status != library.StatusMissing || after.FilePath != "" || !after.Monitored {
		t.Fatalf("movie after unlink: %+v", after)
	}
	postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/movies/%d/locate-file", e.base, other.ID), map[string]string{"path": file}, http.StatusOK)
	postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/movies/%d/unlink-file", e.base, m.ID), nil, http.StatusConflict)
}

func TestUnlinkIsAdminOnly(t *testing.T) {
	e := newLocateEnv(t)
	m := e.addMovie(t, 603, "The Matrix")
	file := filepath.Join(e.movies, "matrix.mkv")
	writeBody(t, file, "video")
	if err := e.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "", file); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{fmt.Sprintf("/api/movies/%d/unlink-file", m.ID), "/api/episodes/1/unlink-file"} {
		if got := e.status(t, http.MethodPost, p, e.member); got != http.StatusForbidden {
			t.Fatalf("member POST %s = %d, want 403", p, got)
		}
	}
	if got, _ := e.server.MovieRepo.Get(m.ID); got.FilePath != file {
		t.Fatalf("a member unlinked the file: %+v", got)
	}
}
