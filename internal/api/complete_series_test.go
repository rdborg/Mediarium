package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// Three seasons of two episodes each, all long aired, none downloaded.
var threeSeasonsMissing = []episodeSpec{
	{1, 1, "2020-01-01", library.StatusMissing, ""},
	{1, 2, "2020-01-08", library.StatusMissing, ""},
	{2, 1, "2021-01-01", library.StatusMissing, ""},
	{2, 2, "2021-01-08", library.StatusMissing, ""},
	{3, 1, "2022-01-01", library.StatusMissing, ""},
	{3, 2, "2022-01-08", library.StatusMissing, ""},
}

const (
	fixS1       = "Fixture.Show.S01.1080p.BluRay.x264-GRP"
	fixS2       = "Fixture.Show.S02.1080p.BluRay.x264-GRP"
	fixS3       = "Fixture.Show.S03.1080p.BluRay.x264-GRP"
	fixRange    = "Fixture.Show.S01-S03.1080p.BluRay.x264-GRP"
	fixComplete = "Fixture.Show.Complete.Series.1080p.BluRay.x264-GRP"
	fixShort    = "Fixture.Show.S01-S02.1080p.BluRay.x264-GRP"
)

func (e *tvAutoEnv) searchComplete(t *testing.T) (grabbed int, message string) {
	t.Helper()
	out := postJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/series/%d/search-complete", e.baseURL, e.seriesID), map[string]any{}, http.StatusOK)
	return int(out["grabbed"].(float64)), out["message"].(string)
}

// A pack for each season that has one comes first; the pack of the whole series
// is only wanted when some season has none, and then it replaces them all.
func TestSearchCompleteSeries(t *testing.T) {
	cases := []struct {
		name     string
		titles   []string
		specs    []episodeSpec
		want     []string
		message  string // a part of what the person is told
		statuses map[string]library.Status
	}{
		{
			name: "a pack for every season", titles: []string{fixS1, fixS2, fixS3, fixComplete}, specs: threeSeasonsMissing,
			want: []string{fixS1, fixS2, fixS3}, message: "Started 3 downloads",
		},
		{
			name: "a season without a pack: the whole series is taken instead", titles: []string{fixS1, fixS2, fixComplete}, specs: threeSeasonsMissing,
			want: []string{fixComplete}, message: "pack of the whole series",
			statuses: map[string]library.Status{"S01E01": library.StatusDownloading, "S02E02": library.StatusDownloading, "S03E01": library.StatusDownloading},
		},
		{
			name: "a range of seasons counts as a whole-series pack", titles: []string{fixS2, fixRange}, specs: threeSeasonsMissing,
			want: []string{fixRange}, message: "pack of the whole series",
			statuses: map[string]library.Status{"S01E01": library.StatusDownloading, "S03E02": library.StatusDownloading},
		},
		{
			name: "a pack that leaves out a season is no use: the season packs are taken", titles: []string{fixS1, fixS2, fixShort}, specs: threeSeasonsMissing,
			want: []string{fixS1, fixS2}, message: "Started 2 downloads",
			statuses: map[string]library.Status{"S01E01": library.StatusDownloading, "S03E01": library.StatusMissing},
		},
		{
			name: "only the whole series is there", titles: []string{fixComplete}, specs: threeSeasonsMissing,
			want: []string{fixComplete}, message: "pack of the whole series",
		},
		{
			name: "another show's complete series is left alone", titles: []string{"Other.Show.Complete.Series.1080p.BluRay.x264-GRP", otherE01}, specs: threeSeasonsMissing,
			want: nil, message: "Nothing suitable was found",
		},
		{
			name: "nothing on the indexers", titles: nil, specs: threeSeasonsMissing,
			want: nil, message: "Nothing suitable was found",
		},
		{
			name: "only seasons with gaps are searched for", titles: []string{fixS1, fixS2, fixS3}, specs: []episodeSpec{
				{1, 1, "2020-01-01", library.StatusDownloaded, "Bluray-1080p"},
				{1, 2, "2020-01-08", library.StatusDownloaded, "Bluray-1080p"},
				{2, 1, "2021-01-01", library.StatusMissing, ""},
				{2, 2, "2021-01-08", library.StatusMissing, ""},
			},
			want: []string{fixS2}, message: "Started 1 download",
		},
		{
			name: "nothing is missing", titles: []string{fixS1, fixComplete}, specs: []episodeSpec{
				{1, 1, "2020-01-01", library.StatusDownloaded, "Bluray-1080p"},
				{1, 2, "2020-01-08", library.StatusDownloaded, "Bluray-1080p"},
			},
			want: nil, message: "Nothing is missing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTVAutoEnv(t, tc.titles, tc.specs)
			grabbed, message := env.searchComplete(t)
			if !strings.Contains(message, tc.message) {
				t.Errorf("message %q, want it to say %q", message, tc.message)
			}
			requireTitles(t, env.grabbedTitles(t), tc.want...)
			if grabbed != len(tc.want) {
				t.Errorf("grabbed = %d, want %d", grabbed, len(tc.want))
			}
			if tc.statuses != nil {
				got := env.episodeStatuses(t)
				for ep, want := range tc.statuses {
					if got[ep] != want {
						t.Errorf("%s is %q, want %q", ep, got[ep], want)
					}
				}
			}
			env.finish(t)
		})
	}
}

// Whatever the search did is in the show's history, with the reason when it took
// nothing.
func TestSearchCompleteSeriesWritesTheHistory(t *testing.T) {
	env := newTVAutoEnv(t, []string{otherE01}, threeSeasonsMissing)
	env.searchComplete(t)
	events := getJSON[[]map[string]any](t, env.client, fmt.Sprintf("%s/api/series/%d/events", env.baseURL, env.seriesID))
	var text []string
	for _, ev := range events {
		if m, ok := ev["message"].(string); ok {
			text = append(text, m)
		}
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{"Looking for the complete series", "Searched for a pack of the whole series", "Searched for the complete series: nothing taken"} {
		if !strings.Contains(all, want) {
			t.Errorf("the history does not say %q:\n%s", want, all)
		}
	}
}

// A profile that never takes season packs has nothing to look for.
func TestSearchCompleteSeriesRespectsNoSeasonPacks(t *testing.T) {
	env := newTVAutoEnv(t, []string{fixS1, fixComplete}, threeSeasonsMissing)
	profiles, err := env.server.QualityRepo.List()
	if err != nil || len(profiles) == 0 {
		t.Fatalf("profiles: %v (%d)", err, len(profiles))
	}
	for _, p := range profiles {
		p.NoSeasonPacks = true
		if _, err := env.server.QualityRepo.Update(p); err != nil {
			t.Fatal(err)
		}
	}
	grabbed, message := env.searchComplete(t)
	if grabbed != 0 || !strings.Contains(message, "never downloads season packs") {
		t.Fatalf("grabbed %d, message %q", grabbed, message)
	}
	requireTitles(t, env.grabbedTitles(t))
	env.finish(t)
}

// A show that is not in the library, and a body that is not a show.
func TestSearchCompleteSeriesUnknownShow(t *testing.T) {
	env := newTVAutoEnv(t, nil, threeSeasonsMissing)
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/series/99999/search-complete", map[string]any{}, http.StatusNotFound)
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/series/abc/search-complete", map[string]any{}, http.StatusBadRequest)
}

// A whole-series pack imports into the season folders its files belong in.
func TestCompleteSeriesPackImportsEverySeason(t *testing.T) {
	e1 := []byte("episode one " + strings.Repeat("a", 400))
	e2 := []byte("episode two " + strings.Repeat("b", 400))
	s2 := []byte("season two " + strings.Repeat("c", 400))
	env := newTVEnv(t, map[string]nntpArticle{
		"e1@example": {fileName: "Season 1/Fixture.Show.S01E01.1080p.BluRay.x264-GRP.mkv", content: e1},
		"e2@example": {fileName: "Season 1/Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv", content: e2},
		"s2@example": {fileName: "Season 2/Fixture.Show.S02E01.1080p.BluRay.x264-GRP.mkv", content: s2},
	})

	env.grab(t, "Fixture.Show.Complete.Series.1080p.BluRay.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the pack to complete, got status=%q error=%q", status, errMsg)
	}
	for _, want := range []struct {
		folder, name string
		content      []byte
	}{
		{"Season 01", "Fixture Show - S01E01 - Pilot.mkv", e1},
		{"Season 01", "Fixture Show - S01E02 - Second.mkv", e2},
		{"Season 02", "Fixture Show - S02E01 - Return.mkv", s2},
	} {
		path := filepath.Join(env.tvRoot, "Fixture Show (2011)", want.folder, want.name)
		got, err := os.ReadFile(path)
		if err != nil {
			entries, _ := filepath.Glob(filepath.Join(env.tvRoot, "*", "*", "*"))
			t.Fatalf("expected %s: %v (found %v)", path, err, entries)
		}
		if string(got) != string(want.content) {
			t.Fatalf("%s: content mismatch", want.name)
		}
	}
	for ep, status := range env.episodeStatuses(t) {
		if status != "downloaded" {
			t.Errorf("%s is %q, want downloaded", ep, status)
		}
	}
}
