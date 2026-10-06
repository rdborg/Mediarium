package library

import (
	"fmt"
	"sort"
)

// How a show's episodes are numbered in release names.
const (
	SeriesStandard = "standard" // Show.S01E05
	SeriesAnime    = "anime"    // [Group] Show - 105, counted from the first episode
	SeriesDaily    = "daily"    // Show.2024.03.15
)

// ValidSeriesType is t when it is one of the three, else SeriesStandard.
func ValidSeriesType(t string) string {
	switch t {
	case SeriesAnime, SeriesDaily:
		return t
	}
	return SeriesStandard
}

// SetSeriesType changes how a show's releases are numbered.
func (r *Repo) SetSeriesType(id int64, t string) error {
	if _, err := r.db.Exec(`UPDATE series SET series_type = ? WHERE id = ?`, ValidSeriesType(t), id); err != nil {
		return fmt.Errorf("set series type: %w", err)
	}
	return nil
}

// AbsoluteOrder lists a show's regular episodes (specials left out) in
// airing order: season by season, episode by episode. Episode n of an anime
// release ("Show - 105") is AbsoluteOrder(...)[n-1].
func AbsoluteOrder(episodes []Episode) []Episode {
	out := make([]Episode, 0, len(episodes))
	for _, e := range episodes {
		if e.Season > 0 && e.Episode > 0 {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Episode < out[j].Episode
	})
	return out
}

// AbsoluteNumber is an episode's number counted from the first one (0 when
// it isn't a regular episode of the list).
func AbsoluteNumber(episodes []Episode, season, episode int) int {
	for i, e := range AbsoluteOrder(episodes) {
		if e.Season == season && e.Episode == episode {
			return i + 1
		}
	}
	return 0
}
