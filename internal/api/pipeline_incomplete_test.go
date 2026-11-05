package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediainfo/mediainfotest"
)

// waitForBlocklist polls until the release is on the blocklist and returns the
// reason it was given (the pipeline blocklists just after it marks the
// download failed).
func waitForBlocklist(t *testing.T, e *tvEnv, release string) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reason, listed, err := e.server.Blocklist.Reason(release); err == nil && listed {
			return reason, true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return "", false
}

func TestEpisodeLengthCheckAtImport(t *testing.T) {
	const release = "Fixture.Show.S01E02.2160p.WEB-DL.x265-GRP"
	tests := []struct {
		name     string
		seconds  float64
		expected float64 // minutes TMDB gives, 0 = TMDB doesn't know
		off      bool    // the check is switched off
		reject   bool
		wantText string
	}{
		{"one minute for a 45 minute episode", 60, 45, false, true, "The file is only 1 min long, the episode is about 45 min. This looks like a fake or broken release."},
		{"a bit under half", 22 * 60, 45, false, true, "only 22 min long"},
		{"half is enough", 23 * 60, 45, false, false, ""},
		{"a full episode", 44 * 60, 45, false, false, ""},
		{"a short show is never judged", 90, 10, false, false, ""},
		{"unknown runtime, two minutes", 120, 0, false, true, "The file is only 2 min long. This looks like a fake or broken release."},
		{"unknown runtime, twenty minutes", 20 * 60, 0, false, false, ""},
		{"check switched off", 60, 45, true, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTVEnv(t, map[string]nntpArticle{"ep@example": {fileName: "release-file.mkv", content: mediainfotest.MKV(tt.seconds)}})
			if err := env.server.MovieRepo.SaveRuntime(library.RuntimeTV, 1399, tt.expected, time.Now()); err != nil {
				t.Fatal(err)
			}
			if tt.off {
				putJSONStatus(t, env.client, env.httpSrv.URL+"/api/settings", map[string]any{"checkDuration": false}, http.StatusOK)
			}
			env.grab(t, release)
			status, errMsg := env.waitForTerminal(t)
			target := filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01", "Fixture Show - S01E02 - Second.mkv")
			if !tt.reject {
				if status != "completed" {
					t.Fatalf("expected the download to complete, got %q: %s", status, errMsg)
				}
				if _, err := os.Stat(target); err != nil {
					t.Fatalf("expected the episode to be imported: %v", err)
				}
				return
			}
			if status != "failed" || !strings.Contains(errMsg, tt.wantText) {
				t.Fatalf("expected a failure saying %q, got %q: %q", tt.wantText, status, errMsg)
			}
			if _, err := os.Stat(target); err == nil {
				t.Fatal("the short file was imported")
			}
			if reason, ok := waitForBlocklist(t, env, release); !ok || reason != "incomplete file" {
				t.Fatalf("expected the release blocklisted as an incomplete file, got %q (listed %v)", reason, ok)
			}
			if got := env.episodeStatuses(t)["S01E02"]; got != "missing" {
				t.Fatalf("the episode should be wanted again, it is %q", got)
			}
			if !hasMessage(activityMessages(t, env), fmt.Sprintf("Blocklisted \"%s\": incomplete file", release)) {
				t.Fatalf("missing the blocklist line in Activity: %v", activityMessages(t, env))
			}
		})
	}
}

// A season pack with one fake file among real ones: the real episodes are
// imported, the fake one is left out, and the pack itself is not blocklisted.
func TestSeasonPackLeavesOutAShortEpisode(t *testing.T) {
	env := newTVEnv(t, map[string]nntpArticle{
		"e1@example": {fileName: "Fixture.Show.S01E01.1080p.BluRay.x264-GRP.mkv", content: mediainfotest.MKV(44 * 60)},
		"e2@example": {fileName: "Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv", content: mediainfotest.MKV(70)},
	})
	if err := env.server.MovieRepo.SaveRuntime(library.RuntimeTV, 1399, 45, time.Now()); err != nil {
		t.Fatal(err)
	}
	const release = "Fixture.Show.S01.1080p.BluRay.x264-GRP"
	env.grab(t, release)
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the pack to complete, got %q: %s", status, errMsg)
	}
	statuses := env.episodeStatuses(t)
	if statuses["S01E01"] != "downloaded" || statuses["S01E02"] != "missing" {
		t.Fatalf("expected only the full episode imported, got %v", statuses)
	}
	if _, listed, _ := env.server.Blocklist.Reason(release); listed {
		t.Fatal("a pack with real episodes in it must not be blocklisted")
	}
	if !hasMessage(activityMessages(t, env), "Fixture Show: the file Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv was left out because it looks like a fake or broken release (it is only 1 min long).") {
		t.Fatalf("missing the left-out line in Activity: %v", activityMessages(t, env))
	}
}

// A season pack where every file is short is a bad release as a whole.
func TestSeasonPackOfShortFilesIsBlocklisted(t *testing.T) {
	env := newTVEnv(t, map[string]nntpArticle{
		"e1@example": {fileName: "Fixture.Show.S01E01.1080p.BluRay.x264-GRP.mkv", content: mediainfotest.MKV(60)},
		"e2@example": {fileName: "Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv", content: mediainfotest.MKV(60)},
	})
	if err := env.server.MovieRepo.SaveRuntime(library.RuntimeTV, 1399, 45, time.Now()); err != nil {
		t.Fatal(err)
	}
	const release = "Fixture.Show.S01.1080p.BluRay.x264-GRP"
	env.grab(t, release)
	if status, _ := env.waitForTerminal(t); status != "failed" {
		t.Fatalf("expected the pack to fail, got %q", status)
	}
	if reason, ok := waitForBlocklist(t, env, release); !ok || reason != "incomplete file" {
		t.Fatalf("expected the pack blocklisted as an incomplete file, got %q (listed %v)", reason, ok)
	}
}

func TestMovieLengthCheckAtImport(t *testing.T) {
	tests := []struct {
		name     string
		seconds  float64
		expected float64
		reject   bool
	}{
		{"two minutes of a 100 minute movie", 120, 100, true},
		{"full movie", 98 * 60, 100, false},
		{"unknown runtime, five minutes", 5 * 60, 0, true},
		{"unknown runtime, long enough", 40 * 60, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTVEnv(t, map[string]nntpArticle{"mv@example": {fileName: "Fixture.Movie.2001.1080p.BluRay.x264-GRP.mkv", content: mediainfotest.MKV(tt.seconds)}})
			moviesRoot := filepath.Join(t.TempDir(), "movies")
			if err := os.MkdirAll(moviesRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			putJSONStatus(t, env.client, env.httpSrv.URL+"/api/settings", map[string]any{"moviesPath": moviesRoot}, http.StatusOK)
			m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 5, Title: "Fixture Movie", Year: 2001, Monitored: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := env.server.MovieRepo.SaveRuntime(library.RuntimeMovie, 5, tt.expected, time.Now()); err != nil {
				t.Fatal(err)
			}
			const release = "Fixture.Movie.2001.1080p.BluRay.x264-GRP"
			postJSON[map[string]any](t, env.client, fmt.Sprintf("%s/api/movies/%d/grab", env.httpSrv.URL, m.ID), map[string]any{
				"releaseTitle": release, "downloadUrl": env.nzbURL, "sizeBytes": 1000,
			}, http.StatusAccepted)
			status, errMsg := env.waitForTerminal(t)
			done, _ := env.server.MovieRepo.Get(m.ID)
			if !tt.reject {
				if status != "completed" || done.Status != library.StatusDownloaded {
					t.Fatalf("expected the movie to import, got %q %q (movie %s)", status, errMsg, done.Status)
				}
				return
			}
			if status != "failed" || !strings.Contains(errMsg, "This looks like a fake or broken release.") {
				t.Fatalf("expected a failure, got %q: %q", status, errMsg)
			}
			// The queue row turns failed just before the movie goes back to wanted.
			waitFor(t, "the movie to be wanted again", func() bool {
				done, _ = env.server.MovieRepo.Get(m.ID)
				return done.Status == library.StatusMissing
			})
			if done.Status != library.StatusMissing || done.FilePath != "" {
				t.Fatalf("the movie should be wanted again, it is %s with %q", done.Status, done.FilePath)
			}
			if reason, ok := waitForBlocklist(t, env, release); !ok || reason != "incomplete file" {
				t.Fatalf("expected the release blocklisted as an incomplete file, got %q (listed %v)", reason, ok)
			}
		})
	}
}
