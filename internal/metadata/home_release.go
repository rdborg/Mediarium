package metadata

import (
	"sort"
	"time"
)

// TMDB's release types: 2 and 3 are in cinemas, 4 is a digital release (the
// first WEB-DL can exist) and 5 a disc.
const (
	releaseTheatricalLimited = 2
	releaseTheatrical        = 3
	releaseDigital           = 4
	releasePhysical          = 5
)

func parseTMDBDate(s string) (time.Time, bool) {
	if len(s) < 10 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s[:10])
	return t, err == nil
}

// ReleaseWindow is when a film reached cinemas and when it first came out
// digitally or on disc, from TMDB's list of release dates (every country: the
// earliest date counts). A zero time means TMDB lists none.
func (d *MovieDetail) ReleaseWindow() (theatrical, home time.Time) {
	var theatricals, homes []time.Time
	for _, country := range d.ReleaseDates.Results {
		for _, rd := range country.ReleaseDates {
			t, ok := parseTMDBDate(rd.Date)
			if !ok {
				continue
			}
			switch rd.Type {
			case releaseTheatricalLimited, releaseTheatrical:
				theatricals = append(theatricals, t)
			case releaseDigital, releasePhysical:
				homes = append(homes, t)
			}
		}
	}
	earliest := func(ts []time.Time) time.Time {
		if len(ts) == 0 {
			return time.Time{}
		}
		sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
		return ts[0]
	}
	return earliest(theatricals), earliest(homes)
}
