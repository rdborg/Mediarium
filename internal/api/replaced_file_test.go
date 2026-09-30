package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// putSettings saves settings the way the Settings page does.
func putSettings(t *testing.T, e *controlEnv, body map[string]any) {
	t.Helper()
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/settings", body, http.StatusOK)
}

// When a new release lands under another file name than the one the movie
// has, the old file goes only if the person allowed replacing files.
func TestUpgradeUnderAnotherNameRemovesTheOldFileOnlyWhenReplacingIsAllowed(t *testing.T) {
	tests := []struct {
		name string
		// policy is the "If a file already exists" setting ("" leaves the default, skip).
		policy string
		// oldQuality is what the movie's current file is recorded as.
		oldQuality string
		// old says where the movie's current file is, relative to the movies
		// folder ("outside:" puts it in a folder that is not the library).
		old string
		// keepOld says the old file must still be there afterwards.
		keepOld bool
	}{
		{"overwrite, other name in the movie's folder", "overwrite", "HDTV-720p", "The Fixture Movie (1999)/The Fixture Movie (1999) [720p].mp4", false},
		{"overwrite if better, and it is better", "overwrite_if_better", "HDTV-720p", "The Fixture Movie (1999)/The Fixture Movie (1999) [720p].mp4", false},
		{"overwrite if better, but it is not better", "overwrite_if_better", "Remux-2160p", "The Fixture Movie (1999)/The Fixture Movie (1999) [2160p].mkv", true},
		{"default policy never replaces", "", "HDTV-720p", "The Fixture Movie (1999)/The Fixture Movie (1999) [720p].mp4", true},
		{"skip never replaces", "skip", "HDTV-720p", "The Fixture Movie (1999)/The Fixture Movie (1999) [720p].mp4", true},
		{"a file outside the library folder is left alone", "overwrite", "HDTV-720p", "outside:old.mp4", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newControlEnv(t, -1)
			if tc.policy != "" {
				putSettings(t, e, map[string]any{"importConflictPolicy": tc.policy})
			}
			oldPath := filepath.Join(e.movies, filepath.FromSlash(tc.old))
			if len(tc.old) > 8 && tc.old[:8] == "outside:" {
				oldPath = filepath.Join(t.TempDir(), tc.old[8:])
			}
			putFile(t, oldPath, "the old version")
			if err := e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, tc.oldQuality, oldPath); err != nil {
				t.Fatal(err)
			}

			id := e.grab(t)
			waitFor(t, "the download to end", func() bool { s := e.status(t, id); return s == "completed" || s == "failed" })
			if got := e.status(t, id); got != "completed" {
				t.Fatalf("the download ended %s: %v", got, e.item(t, id)["error"])
			}

			newPath := filepath.Join(e.movies, "The Fixture Movie (1999)", "The Fixture Movie (1999).mkv")
			if got, err := os.ReadFile(newPath); err != nil || string(got) != string(e.wantFile) {
				t.Fatalf("the new file: %q, %v", got, err)
			}
			m, _ := e.server.MovieRepo.Get(e.movie.ID)
			if m.FilePath != newPath {
				t.Errorf("the movie points at %s, want %s", m.FilePath, newPath)
			}
			if kept := exists(oldPath); kept != tc.keepOld {
				t.Errorf("old file still there = %v, want %v (library: %s)", kept, tc.keepOld, tree(t, e.movies))
			}
		})
	}
}

// The file is replaced under the same name: nothing else to remove, and
// nothing goes wrong.
func TestUpgradeUnderTheSameNameLeavesOneFile(t *testing.T) {
	e := newControlEnv(t, -1)
	putSettings(t, e, map[string]any{"importConflictPolicy": "overwrite"})
	oldPath := filepath.Join(e.movies, "The Fixture Movie (1999)", "The Fixture Movie (1999).mkv")
	putFile(t, oldPath, "the old version")
	if err := e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, "HDTV-720p", oldPath); err != nil {
		t.Fatal(err)
	}

	id := e.grab(t)
	waitFor(t, "the download to end", func() bool { s := e.status(t, id); return s == "completed" || s == "failed" })
	if got := e.status(t, id); got != "completed" {
		t.Fatalf("the download ended %s: %v", got, e.item(t, id)["error"])
	}
	if got := tree(t, e.movies); got != "The Fixture Movie (1999),The Fixture Movie (1999)/The Fixture Movie (1999).mkv" {
		t.Fatalf("library: %s", got)
	}
	if got, _ := os.ReadFile(oldPath); string(got) != string(e.wantFile) {
		t.Fatalf("the file was not replaced: %q", got)
	}
}

// A movie whose recorded file is gone from the disk imports normally.
func TestUpgradeWhenTheOldFileIsAlreadyGone(t *testing.T) {
	e := newControlEnv(t, -1)
	putSettings(t, e, map[string]any{"importConflictPolicy": "overwrite"})
	gone := filepath.Join(e.movies, "The Fixture Movie (1999)", "The Fixture Movie (1999) [720p].mp4")
	if err := e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, "HDTV-720p", gone); err != nil {
		t.Fatal(err)
	}

	id := e.grab(t)
	waitFor(t, "the download to end", func() bool { s := e.status(t, id); return s == "completed" || s == "failed" })
	if got := e.status(t, id); got != "completed" {
		t.Fatalf("the download ended %s: %v", got, e.item(t, id)["error"])
	}
	if got := tree(t, e.movies); got != "The Fixture Movie (1999),The Fixture Movie (1999)/The Fixture Movie (1999).mkv" {
		t.Fatalf("library: %s", got)
	}
}

// A file another movie still points at is never removed.
func TestUpgradeKeepsAFileAnotherTitleUses(t *testing.T) {
	e := newControlEnv(t, -1)
	putSettings(t, e, map[string]any{"importConflictPolicy": "overwrite"})
	shared := filepath.Join(e.movies, "Collection", "Both Films.mkv")
	putFile(t, shared, "one file, two titles")
	if err := e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, "HDTV-720p", shared); err != nil {
		t.Fatal(err)
	}
	other := seedMovie(t, e.server.MovieRepo, 999, "Other Film", 1999, shared)
	_ = other

	id := e.grab(t)
	waitFor(t, "the download to end", func() bool { s := e.status(t, id); return s == "completed" || s == "failed" })
	if got := e.status(t, id); got != "completed" {
		t.Fatalf("the download ended %s: %v", got, e.item(t, id)["error"])
	}
	if !exists(shared) {
		t.Fatal("a file another movie uses was removed")
	}
}

// Episodes: a new file under another name replaces the old one when replacing
// is allowed, but a file another episode still uses stays.
func TestEpisodeUpgradeUnderAnotherNameRemovesTheOldFile(t *testing.T) {
	content := []byte("episode two bytes " + strings.Repeat("x", 500))
	env := newTVEnv(t, map[string]nntpArticle{"ep-two@example": {fileName: "release-file.mkv", content: content}})
	postJSONMethod[map[string]any](t, env.client, http.MethodPut, env.httpSrv.URL+"/api/settings", map[string]any{"importConflictPolicy": "overwrite"}, http.StatusOK)

	season := filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01")
	oldE2 := filepath.Join(season, "Fixture Show - S01E02 - Second [720p].mp4")
	putFile(t, oldE2, "old e2")
	pair := filepath.Join(season, "Fixture Show - S01E03.mkv") // holds two episodes, one of them still wanted
	putFile(t, pair, "old pair")
	e2, err := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetEpisodeStatus(e2.ID, library.StatusDownloaded, "HDTV-720p", oldE2); err != nil {
		t.Fatal(err)
	}

	env.grab(t, "Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the TV grab to complete, got status=%q error=%q", status, errMsg)
	}
	if exists(oldE2) {
		t.Errorf("the old episode file was left next to the new one: %s", tree(t, season))
	}
	if !exists(filepath.Join(season, "Fixture Show - S01E02 - Second.mkv")) {
		t.Errorf("the new episode is missing: %s", tree(t, season))
	}
	if !exists(pair) {
		t.Error("an unrelated file was removed")
	}
	// The finished download reads 100%, whatever the transfer ended at.
	if list := getJSON[[]map[string]any](t, env.client, env.httpSrv.URL+"/api/queue"); len(list) != 1 || list[0]["progressPct"] != float64(100) {
		t.Errorf("a completed download should read 100%%: %+v", list)
	}
}

func TestEpisodeUpgradeKeepsAFileAnotherEpisodeStillUses(t *testing.T) {
	content := []byte("episode two bytes " + strings.Repeat("x", 500))
	env := newTVEnv(t, map[string]nntpArticle{"ep-two@example": {fileName: "release-file.mkv", content: content}})
	postJSONMethod[map[string]any](t, env.client, http.MethodPut, env.httpSrv.URL+"/api/settings", map[string]any{"importConflictPolicy": "overwrite"}, http.StatusOK)

	season := filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01")
	both := filepath.Join(season, "Fixture Show - S01E01-E02.mkv")
	putFile(t, both, "one file, two episodes")
	for _, n := range []int{1, 2} {
		ep, err := env.server.MovieRepo.GetEpisode(env.seriesID, 1, n)
		if err != nil {
			t.Fatal(err)
		}
		if err := env.server.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, "HDTV-720p", both); err != nil {
			t.Fatal(err)
		}
	}

	env.grab(t, "Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the TV grab to complete, got status=%q error=%q", status, errMsg)
	}
	if !exists(both) {
		t.Fatalf("episode 1 still lives in the two-episode file, which was removed: %s", tree(t, season))
	}
}
