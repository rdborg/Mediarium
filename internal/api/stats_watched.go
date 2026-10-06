package api

import (
	"sort"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// Watch statistics on the Statistics page, while "Read what's been watched"
// is on: how much of what was downloaded gets watched, the space held by
// things nobody has watched, and the most watched and latest watched titles.

const watchStatsListLen = 8

type statWatchedTitle struct {
	Kind       string `json:"kind"` // "movie" or "series"
	ID         int64  `json:"id"`
	TMDBID     int    `json:"tmdbId,omitempty"` // movies: for the link to the movie's page
	Title      string `json:"title"`
	Plays      int    `json:"plays"`
	Episodes   int    `json:"episodes,omitempty"` // shows: episodes watched
	LastPlayed string `json:"lastPlayed,omitempty"`
}

type watchStatsPayload struct {
	LastSync          string             `json:"lastSync,omitempty"`
	MoviesWatched     int                `json:"moviesWatched"`     // downloaded movies played at least once
	MoviesUnwatched   int                `json:"moviesUnwatched"`   // downloaded movies never played
	EpisodesWatched   int                `json:"episodesWatched"`   // downloaded episodes played at least once
	EpisodesUnwatched int                `json:"episodesUnwatched"` // downloaded episodes never played
	UnwatchedBytes    int64              `json:"unwatchedBytes"`    // space held by files nobody has played
	Plays             int                `json:"plays"`
	Top               []statWatchedTitle `json:"top"`    // most plays first
	Recent            []statWatchedTitle `json:"recent"` // latest played first
}

// statFile is a downloaded movie or episode, for the watch statistics.
type statFile struct {
	TitleID int64 // movie id, or series id for an episode
	Season  int
	Episode int
	Bytes   int64
}

// statName names a movie or show in the watch statistics.
type statName struct {
	Title  string
	TMDBID int
}

type watchKey struct {
	kind            string
	id              int64
	season, episode int
}

// watchStats works out the watch statistics from the downloaded movies and
// episodes, the titles' names and what the media servers say was played.
func watchStats(movies, episodes []statFile, names map[watchKey]statName, played []library.Watched) watchStatsPayload {
	out := watchStatsPayload{Top: []statWatchedTitle{}, Recent: []statWatchedTitle{}}
	seen := map[watchKey]library.Watched{}
	titles := map[watchKey]*statWatchedTitle{}
	for _, w := range played {
		if w.Plays <= 0 && w.LastPlayed.IsZero() {
			continue
		}
		seen[watchKey{w.Kind, w.TitleID, w.Season, w.Episode}] = w
		kind := "movie"
		if w.Kind == "episode" {
			kind = "series"
		}
		k := watchKey{kind: kind, id: w.TitleID}
		n, ok := names[k]
		if !ok {
			continue // no longer in the library
		}
		t := titles[k]
		if t == nil {
			t = &statWatchedTitle{Kind: kind, ID: w.TitleID, TMDBID: n.TMDBID, Title: n.Title}
			titles[k] = t
		}
		plays := max(w.Plays, 1)
		t.Plays += plays
		out.Plays += plays
		if kind == "series" {
			t.Episodes++
		}
		if !w.LastPlayed.IsZero() {
			if last := w.LastPlayed.UTC().Format(time.RFC3339); last > t.LastPlayed {
				t.LastPlayed = last
			}
		}
	}
	for _, m := range movies {
		if _, ok := seen[watchKey{"movie", m.TitleID, 0, 0}]; ok {
			out.MoviesWatched++
		} else {
			out.MoviesUnwatched++
			out.UnwatchedBytes += m.Bytes
		}
	}
	for _, e := range episodes {
		if _, ok := seen[watchKey{"episode", e.TitleID, e.Season, e.Episode}]; ok {
			out.EpisodesWatched++
		} else {
			out.EpisodesUnwatched++
			out.UnwatchedBytes += e.Bytes
		}
	}

	all := make([]statWatchedTitle, 0, len(titles))
	for _, t := range titles {
		all = append(all, *t)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Plays != all[j].Plays {
			return all[i].Plays > all[j].Plays
		}
		return all[i].Title < all[j].Title
	})
	out.Top = append(out.Top, all[:min(len(all), watchStatsListLen)]...)
	recent := make([]statWatchedTitle, 0, len(all))
	for _, t := range all {
		if t.LastPlayed != "" {
			recent = append(recent, t)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].LastPlayed > recent[j].LastPlayed })
	out.Recent = append(out.Recent, recent[:min(len(recent), watchStatsListLen)]...)
	return out
}
