package api

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

func TestWatchStats(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 9, d, 20, 0, 0, 0, time.UTC) }
	names := map[watchKey]statName{
		{kind: "movie", id: 1}:  {Title: "Heat"},
		{kind: "movie", id: 2}:  {Title: "Ronin"},
		{kind: "series", id: 7}: {Title: "Severance"},
	}
	movies := []statFile{{TitleID: 1, Bytes: 100}, {TitleID: 2, Bytes: 40}}
	episodes := []statFile{{TitleID: 7, Season: 1, Episode: 1, Bytes: 10}, {TitleID: 7, Season: 1, Episode: 2, Bytes: 11}, {TitleID: 7, Season: 1, Episode: 3, Bytes: 12}}

	tests := []struct {
		name                     string
		played                   []library.Watched
		moviesWatched, unwatched int
		episodesWatched          int
		unwatchedBytes           int64
		plays                    int
		top, recent              []string
	}{
		{
			name:      "nothing watched",
			unwatched: 2, unwatchedBytes: 100 + 40 + 10 + 11 + 12,
			top: []string{}, recent: []string{},
		},
		{
			name: "a movie and two episodes",
			played: []library.Watched{
				{Kind: "movie", TitleID: 1, Plays: 2, LastPlayed: day(3)},
				{Kind: "episode", TitleID: 7, Season: 1, Episode: 1, Plays: 1, LastPlayed: day(10)},
				{Kind: "episode", TitleID: 7, Season: 1, Episode: 2, Plays: 2, LastPlayed: day(12)},
			},
			moviesWatched: 1, unwatched: 1, episodesWatched: 2, unwatchedBytes: 40 + 12, plays: 5,
			top: []string{"Severance", "Heat"}, recent: []string{"Severance", "Heat"},
		},
		{
			name: "a title no longer in the library is left out of the lists",
			played: []library.Watched{
				{Kind: "movie", TitleID: 99, Plays: 9, LastPlayed: day(20)},
				{Kind: "movie", TitleID: 2, Plays: 1},
			},
			moviesWatched: 1, unwatched: 1, unwatchedBytes: 100 + 10 + 11 + 12, plays: 1,
			top: []string{"Ronin"}, recent: []string{},
		},
		{
			name: "watched with no play count counts once",
			played: []library.Watched{
				{Kind: "movie", TitleID: 2, LastPlayed: day(1)},
			},
			moviesWatched: 1, unwatched: 1, unwatchedBytes: 100 + 10 + 11 + 12, plays: 1,
			top: []string{"Ronin"}, recent: []string{"Ronin"},
		},
	}
	titlesOf := func(list []statWatchedTitle) []string {
		out := []string{}
		for _, x := range list {
			out = append(out, x.Title)
		}
		return out
	}
	same := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := watchStats(movies, episodes, names, tt.played)
			if got.MoviesWatched != tt.moviesWatched || got.MoviesUnwatched != tt.unwatched || got.EpisodesWatched != tt.episodesWatched ||
				got.EpisodesUnwatched != len(episodes)-tt.episodesWatched || got.UnwatchedBytes != tt.unwatchedBytes || got.Plays != tt.plays {
				t.Fatalf("counts: %+v", got)
			}
			if !same(titlesOf(got.Top), tt.top) || !same(titlesOf(got.Recent), tt.recent) {
				t.Fatalf("top %v recent %v, want %v and %v", titlesOf(got.Top), titlesOf(got.Recent), tt.top, tt.recent)
			}
		})
	}
}
