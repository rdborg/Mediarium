package library_test

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// A refresh that finds episodes TMDB had not listed before must not start the
// old ones monitored for a show that follows only what is still to come.
func TestRefreshKeepsTheBackCatalogueUnmonitoredForFutureOnlyShows(t *testing.T) {
	day := func(offset int) string { return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02") }
	base := []library.Episode{
		{Season: 1, Episode: 1, AirDate: day(-400)},
		{Season: 2, Episode: 1, AirDate: day(5)},
	}
	incoming := append(append([]library.Episode{}, base...),
		library.Episode{Season: 1, Episode: 2, AirDate: day(-399)}, // old, new to us
		library.Episode{Season: 1, Episode: 3, AirDate: day(-3)},   // aired days ago, listed late
		library.Episode{Season: 2, Episode: 2, AirDate: day(12)},   // still to come
		library.Episode{Season: 2, Episode: 3},                     // no date yet
		library.Episode{Season: 0, Episode: 1, AirDate: day(-2)},   // a special
	)
	cases := []struct {
		name       string
		futureOnly bool
		want       map[[2]int]bool
	}{
		{"future only show", true, map[[2]int]bool{{1, 1}: false, {1, 2}: false, {1, 3}: true, {2, 1}: true, {2, 2}: true, {2, 3}: true, {0, 1}: false}},
		{"a show that follows everything", false, map[[2]int]bool{{1, 1}: true, {1, 2}: true, {1, 3}: true, {2, 1}: true, {2, 2}: true, {2, 3}: true, {0, 1}: false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newRepo(t)
			s, err := repo.AddSeries(library.Series{TMDBID: 1, Title: "Show", Year: 2020, Monitored: true}, base)
			if err != nil {
				t.Fatal(err)
			}
			if c.futureOnly {
				if _, err := repo.ApplyMonitorMode(s.ID, library.MonitorFuture, day(0), false); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.UpsertEpisodes(s.ID, incoming, true); err != nil {
				t.Fatal(err)
			}
			got, err := repo.ListEpisodes(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %d episodes, want %d", len(got), len(c.want))
			}
			for _, e := range got {
				if w := c.want[[2]int{e.Season, e.Episode}]; e.Monitored != w {
					t.Errorf("S%02dE%02d monitored = %v, want %v", e.Season, e.Episode, e.Monitored, w)
				}
			}
		})
	}
}
