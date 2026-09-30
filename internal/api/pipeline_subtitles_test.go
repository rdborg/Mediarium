package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func activityMessages(t *testing.T, e *tvEnv) []string {
	t.Helper()
	var out []string
	for _, a := range getJSON[[]map[string]any](t, e.client, e.httpSrv.URL+"/api/activity") {
		out = append(out, a["message"].(string))
	}
	return out
}

func hasMessage(msgs []string, want string) bool {
	for _, m := range msgs {
		if m == want {
			return true
		}
	}
	return false
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// A season pack that ships subtitles: each episode gets the ones that name it
// (by SxxEyy, in the file name or its folder), placed next to it as
// "<episode>.<lang>.srt"; a subtitle that names no episode is left out.
func TestTVSeasonPackImportsSubtitlesThatCameWithIt(t *testing.T) {
	env := newTVEnv(t, map[string]nntpArticle{
		"e1@example":    {fileName: "Fixture.Show.S01E01.1080p.BluRay.x264-GRP.mkv", content: []byte("episode one " + strings.Repeat("a", 400))},
		"e2@example":    {fileName: "Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv", content: []byte("episode two " + strings.Repeat("b", 400))},
		"s1en@example":  {fileName: "Fixture.Show.S01E01.1080p.BluRay.x264-GRP.English.srt", content: []byte("1\n00:00:01,000 --> 00:00:02,000\nHello one\n")},
		"s1fr@example":  {fileName: "Fixture.Show.S01E01.1080p.BluRay.x264-GRP.fre.srt", content: []byte("1\n00:00:01,000 --> 00:00:02,000\nBonjour un\n")},
		"s2en@example":  {fileName: "Fixture.Show.S01E02.1080p.BluRay.x264-GRP.en.srt", content: []byte("1\n00:00:01,000 --> 00:00:02,000\nHello two\n")},
		"s2fc@example":  {fileName: "Fixture.Show.S01E02.1080p.BluRay.x264-GRP.fr.forced.srt", content: []byte("1\n00:00:01,000 --> 00:00:02,000\nForced\n")},
		"stray@example": {fileName: "English.srt", content: []byte("1\n00:00:01,000 --> 00:00:02,000\nCannot tell which episode\n")},
	})

	env.grab(t, "Fixture.Show.S01.1080p.BluRay.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the season pack to complete, got status=%q error=%q", status, errMsg)
	}

	season := filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01")
	got := dirNames(t, season)
	want := []string{
		"Fixture Show - S01E01 - Pilot.en.srt",
		"Fixture Show - S01E01 - Pilot.fr.srt",
		"Fixture Show - S01E01 - Pilot.mkv",
		"Fixture Show - S01E02 - Second.en.srt",
		"Fixture Show - S01E02 - Second.fr.forced.srt",
		"Fixture Show - S01E02 - Second.mkv",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Season 01 contains %v, want %v", got, want)
	}
	b, err := os.ReadFile(filepath.Join(season, "Fixture Show - S01E02 - Second.en.srt"))
	if err != nil || !strings.Contains(string(b), "Hello two") {
		t.Fatalf("episode 2's English subtitle: %q, %v", b, err)
	}

	msgs := activityMessages(t, env)
	if !hasMessage(msgs, "Fixture Show S01E01 imported with 2 subtitle files: en, fr") {
		t.Fatalf("missing activity line for episode 1: %v", msgs)
	}
	if !hasMessage(msgs, "Fixture Show S01E02 imported with 2 subtitle files: en, fr forced") {
		t.Fatalf("missing activity line for episode 2: %v", msgs)
	}

	// English counts as present; a forced-only French track does not count as French.
	// (The status page answers only while the subtitles switch is on.)
	putJSONStatus(t, env.client, env.httpSrv.URL+"/api/settings", map[string]any{"subtitlesEnabled": true}, http.StatusOK)
	e2, _ := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 2)
	st := getJSON[map[string]any](t, env.client, fmt.Sprintf("%s/api/episodes/%d/subtitles/status", env.httpSrv.URL, e2.ID))
	if present := st["present"].([]any); len(present) != 1 || present[0] != "en" {
		t.Fatalf("episode 2 status: %+v", st)
	}
	putJSONStatus(t, env.client, env.httpSrv.URL+"/api/settings", map[string]any{"subtitleLanguages": []string{"en", "fr"}}, http.StatusOK)
	wanted := getJSON[[]map[string]any](t, env.client, env.httpSrv.URL+"/api/subtitles/wanted")
	byID := map[float64][]any{}
	for _, row := range wanted {
		byID[row["id"].(float64)] = row["missing"].([]any)
	}
	if _, ok := byID[float64(e2.ID)]; !ok || fmt.Sprint(byID[float64(e2.ID)]) != "[fr]" {
		t.Fatalf("episode 2 should still want French (the forced track does not count), got %+v", wanted)
	}
	e1, _ := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 1)
	if _, ok := byID[float64(e1.ID)]; ok {
		t.Fatalf("episode 1 has English and French and should not be wanted: %+v", wanted)
	}
}

// A single-episode grab: the subtitle that names no episode belongs to the one
// video in the download.
func TestTVSingleEpisodeImportsItsSubtitle(t *testing.T) {
	env := newTVEnv(t, map[string]nntpArticle{
		"ep@example":  {fileName: "release-file.mkv", content: []byte("episode two " + strings.Repeat("x", 500))},
		"sub@example": {fileName: "release-file.English.srt", content: []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n")},
	})
	env.grab(t, "Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected completion, got status=%q error=%q", status, errMsg)
	}
	season := filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01")
	got := dirNames(t, season)
	if strings.Join(got, "|") != "Fixture Show - S01E02 - Second.en.srt|Fixture Show - S01E02 - Second.mkv" {
		t.Fatalf("Season 01 contains %v", got)
	}
}

// A movie whose release ships subtitles in several languages.
func TestMovieImportsSubtitlesThatCameWithIt(t *testing.T) {
	env := newTVEnv(t, map[string]nntpArticle{
		"mv@example":  {fileName: "Fixture.Movie.2001.1080p.BluRay.x264-GRP.mkv", content: []byte("movie bytes " + strings.Repeat("m", 500))},
		"en@example":  {fileName: "2_English.srt", content: []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n")},
		"fr@example":  {fileName: "French.srt", content: []byte("1\n00:00:01,000 --> 00:00:02,000\nBonjour\n")},
		"smp@example": {fileName: "sample.en.srt", content: []byte("skip me, I belong to a sample")},
		"unk@example": {fileName: "track.srt", content: []byte("unknown language")},
	})
	moviesRoot := filepath.Join(t.TempDir(), "movies")
	if err := os.MkdirAll(moviesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	putJSONStatus(t, env.client, env.httpSrv.URL+"/api/settings", map[string]any{"moviesPath": moviesRoot}, http.StatusOK)
	m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 5, Title: "Fixture Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, env.client, fmt.Sprintf("%s/api/movies/%d/grab", env.httpSrv.URL, m.ID), map[string]any{
		"releaseTitle": "Fixture.Movie.2001.1080p.BluRay.x264-GRP", "downloadUrl": env.nzbURL, "sizeBytes": 1000,
	}, http.StatusAccepted)
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected completion, got status=%q error=%q", status, errMsg)
	}

	done, _ := env.server.MovieRepo.Get(m.ID)
	base := strings.TrimSuffix(done.FilePath, filepath.Ext(done.FilePath))
	got := dirNames(t, filepath.Dir(done.FilePath))
	want := []string{filepath.Base(done.FilePath), filepath.Base(base) + ".en.srt", filepath.Base(base) + ".fr.srt"}
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("movie folder contains %v, want %v", got, want)
	}
	if !hasMessage(activityMessages(t, env), "Fixture Movie imported with 2 subtitle files: en, fr") {
		t.Fatalf("missing activity line: %v", activityMessages(t, env))
	}
	// Subtitles that come inside a download are imported even though the
	// subtitles switch is off (the default). The status page only answers once
	// it is on; both are English/French files already, so nothing is wanted for English.
	putJSONStatus(t, env.client, env.httpSrv.URL+"/api/settings", map[string]any{"subtitlesEnabled": true}, http.StatusOK)
	st := getJSON[map[string]any](t, env.client, fmt.Sprintf("%s/api/movies/%d/subtitles/status", env.httpSrv.URL, m.ID))
	if present := st["present"].([]any); len(present) != 1 || present[0] != "en" {
		t.Fatalf("movie status: %+v", st)
	}
}
