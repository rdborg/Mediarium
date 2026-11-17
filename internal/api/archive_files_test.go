package api_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediainfo/mediainfotest"
	"github.com/rdborg/mediarium/internal/settings"
)

// rarVolume is the start of a RAR 5 volume, padded: what an obfuscated
// release that was never unpacked looks like when it is filed as a video.
func rarVolume() []byte {
	return append([]byte("Rar!\x1a\x07\x01\x00"), make([]byte, 4096)...)
}

// A file named like a video that is really an archive is listed with the files
// that look incomplete, says what it is, and is deleted, blocklisted and
// searched again like them. A real video next to it is left alone.
func TestLibraryFilesThatAreArchives(t *testing.T) {
	restore := api.SetMediaInfoSettle(0)
	defer restore()
	e := newMCPEnv(t)
	e.enable(t)
	s := e.server
	root := t.TempDir()
	movies, tv := filepath.Join(root, "movies"), filepath.Join(root, "tv")
	for k, v := range map[string]string{settings.KeyMoviesPath: movies, settings.KeyTVPath: tv} {
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, data []byte) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	m, err := s.MovieRepo.Add(library.Movie{TMDBID: 10, Title: "Packed Film", Year: 2020, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	moviePath := write(filepath.Join(movies, "Packed Film (2020)", "Packed.Film.2020.mkv"), rarVolume())
	if err := s.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", moviePath); err != nil {
		t.Fatal(err)
	}
	_ = s.MovieRepo.SetReleaseTitle(m.ID, "Packed.Film.2020.1080p.WEB-DL-OBFUSCATED")
	_ = s.MovieRepo.SaveRuntime(library.RuntimeMovie, 10, 100, time.Now())

	sr, err := s.MovieRepo.AddSeries(library.Series{TMDBID: 77, Title: "Number Show", Year: 2017, Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1, Title: "One", AirDate: "2017-01-01", Monitored: true},
		{Season: 1, Episode: 2, Title: "Two", AirDate: "2017-01-08", Monitored: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.MovieRepo.SaveRuntime(library.RuntimeTV, 77, 5, time.Now())
	eps, _ := s.MovieRepo.ListEpisodes(sr.ID)
	season := filepath.Join(tv, "Number Show (2017)", "Season 01")
	var packed, real library.Episode
	for _, ep := range eps {
		var path string
		if ep.Episode == 1 {
			path, packed = write(filepath.Join(season, "S01E01.mkv"), rarVolume()), ep
		} else {
			path, real = write(filepath.Join(season, "S01E02.mkv"), mediainfotest.MKV(5*60)), ep
		}
		if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, "WEBDL-1080p", path); err != nil {
			t.Fatal(err)
		}
		_ = s.MovieRepo.SetEpisodeReleaseTitle(ep.ID, fmt.Sprintf("Number.Show.S01E0%d.1080p.WEB-DL-OBFUSCATED", ep.Episode))
	}
	s.RunMediaInfoJob(context.Background())

	// The pages and lists say what the file is.
	page := getJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/movies/%d", e.base, m.ID))
	note, _ := page["shortFile"].(map[string]any)
	if note == nil || note["archive"] != "rar" {
		t.Fatalf("the movie page note = %v, want archive rar", page["shortFile"])
	}
	shows := getJSON[[]map[string]any](t, e.admin, e.base+"/api/series?brief=1")
	if len(shows) != 1 || shows[0]["shortFiles"] != float64(1) {
		t.Fatalf("shows = %v, want one file flagged", shows)
	}
	detail := getJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/series/%d", e.base, sr.ID))
	for _, raw := range detail["episodes"].([]any) {
		ep := raw.(map[string]any)
		n, _ := ep["shortFile"].(map[string]any)
		switch ep["episode"] {
		case float64(1):
			if n == nil || n["archive"] != "rar" {
				t.Errorf("the packed episode note = %v", ep["shortFile"])
			}
		case float64(2):
			if n != nil {
				t.Errorf("the real episode is flagged: %v", n)
			}
		}
	}

	// Redo deletes the archive, blocklists its release with its own reason and
	// leaves the real episode.
	redo := postJSON[map[string]any](t, e.admin, e.base+"/api/library/incomplete/redo", map[string]any{
		"movieIds": []int64{m.ID}, "seriesIds": []int64{sr.ID},
	}, http.StatusOK)
	if redo["titles"] != float64(2) || redo["skipped"] != float64(0) {
		t.Fatalf("redo answered %v, want the packed film and the packed episode", redo)
	}
	if got, _ := s.MovieRepo.Get(m.ID); got.Status != library.StatusMissing {
		t.Errorf("the packed film should be wanted again: %+v", got)
	}
	if got, _ := s.MovieRepo.GetEpisodeByID(packed.ID); got.Status != library.StatusMissing {
		t.Errorf("the packed episode should be wanted again: %+v", got)
	}
	if got, _ := s.MovieRepo.GetEpisodeByID(real.ID); got.Status != library.StatusDownloaded {
		t.Errorf("the real episode was touched: %+v", got)
	}
	for _, release := range []string{"Packed.Film.2020.1080p.WEB-DL-OBFUSCATED", "Number.Show.S01E01.1080p.WEB-DL-OBFUSCATED"} {
		if reason, listed, _ := s.Blocklist.Reason(release); !listed || reason != "not a video file" {
			t.Errorf("release %s: blocklist reason %q, listed %v", release, reason, listed)
		}
	}
	if _, listed, _ := s.Blocklist.Reason("Number.Show.S01E02.1080p.WEB-DL-OBFUSCATED"); listed {
		t.Error("the release of the real episode was blocklisted")
	}
}
