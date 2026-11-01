package api

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// A show with more missing episodes than one run can search goes on with the
// episodes not tried yet, instead of starting at episode 1 every run.
func TestHuntGoesOnWithEpisodesNotTriedYet(t *testing.T) {
	var h huntPosition
	eps := func(ids ...int64) []library.Episode {
		var out []library.Episode
		for _, id := range ids {
			out = append(out, library.Episode{ID: id, Episode: int(id)})
		}
		return out
	}
	wants := []tvWant{
		{series: library.Series{ID: 1}, season: 1, episodes: eps(1, 2, 3)},
		{series: library.Series{ID: 1}, season: 2, episodes: eps(4, 5)},
		{series: library.Series{ID: 2}, season: 1, episodes: eps(6)},
	}
	at := time.Now()
	for _, id := range []int64{1, 2, 3} {
		h.noteEpisode(id, at)
	}
	h.noteEpisode(6, at)
	h.orderEpisodes(wants)

	if wants[0].season != 2 || wants[0].series.ID != 1 {
		t.Fatalf("show 1's untried season 2 should come first, got series %d season %d", wants[0].series.ID, wants[0].season)
	}
	if wants[2].series.ID != 2 {
		t.Fatalf("the order of the shows must be kept, got series %d last", wants[2].series.ID)
	}

	h.noteEpisode(4, at.Add(time.Second))
	h.noteEpisode(1, at.Add(2*time.Second))
	h.orderEpisodes(wants)
	var got []int64
	for _, w := range wants {
		if w.series.ID == 1 && w.season == 1 {
			for _, e := range w.episodes {
				got = append(got, e.ID)
			}
		}
	}
	if len(got) != 3 || got[2] != 1 {
		t.Fatalf("episode 1 was tried last and should go last, got %v", got)
	}
	if wants[0].season != 2 || wants[0].episodes[0].ID != 5 {
		t.Fatalf("episode 5 (never tried) should lead, got season %d episode %d", wants[0].season, wants[0].episodes[0].ID)
	}
}
