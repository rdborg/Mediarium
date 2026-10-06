package library_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestSpecialsStartUnmonitored(t *testing.T) {
	repo := newRepo(t)
	s, err := repo.AddSeries(library.Series{TMDBID: 1, Title: "Show", Monitored: true}, []library.Episode{
		{Season: 0, Episode: 1, Title: "Behind the Scenes"},
		{Season: 1, Episode: 1, Title: "Pilot"},
		{Season: 1, Episode: 2, Title: "Second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.EpisodeCount != 2 {
		t.Fatalf("an unwanted special is left out of the count: %d", s.EpisodeCount)
	}
	monitoredOf := func() map[int]bool {
		eps, _ := repo.ListEpisodes(s.ID)
		out := map[int]bool{}
		for _, e := range eps {
			out[e.Season*100+e.Episode] = e.Monitored
		}
		return out
	}
	if m := monitoredOf(); m[1] || !m[101] || !m[102] {
		t.Fatalf("specials start unmonitored, the rest monitored: %v", m)
	}
	_ = repo.SetAllEpisodesMonitored(s.ID, false)
	_ = repo.SetAllEpisodesMonitored(s.ID, true)
	if m := monitoredOf(); m[1] || !m[101] {
		t.Fatalf("monitoring the whole show leaves specials alone: %v", m)
	}
	_ = repo.SetSeasonMonitored(s.ID, 0, true)
	if m := monitoredOf(); !m[1] {
		t.Fatalf("specials can be switched on: %v", m)
	}
	if again, _ := repo.GetSeries(s.ID); again.EpisodeCount != 3 {
		t.Fatalf("a wanted special counts: %d", again.EpisodeCount)
	}
}
