package migrate

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Ombi is read with GET /api/v1/Request/movie and /api/v1/Request/tvlite,
// the key in the ApiKey header. Movies carry their title and TMDB id; shows
// their TVDB id (looked up on TMDB).

type ombiUser struct {
	UserName  string `json:"userName"`
	UserAlias string `json:"userAlias"`
	Alias     string `json:"alias"`
}

func (u ombiUser) name() string {
	for _, n := range []string{u.Alias, u.UserAlias, u.UserName} {
		if n = strings.TrimSpace(n); n != "" {
			return n
		}
	}
	return ""
}

type ombiMovie struct {
	Title         string   `json:"title"`
	TheMovieDBID  flexInt  `json:"theMovieDbId"`
	ReleaseDate   string   `json:"releaseDate"`
	RequestedUser ombiUser `json:"requestedUser"`
	Approved      bool     `json:"approved"`
	Denied        bool     `json:"denied"`
	Available     bool     `json:"available"`
}

type ombiChild struct {
	RequestedUser  ombiUser `json:"requestedUser"`
	Approved       bool     `json:"approved"`
	Denied         bool     `json:"denied"`
	Available      bool     `json:"available"`
	SeasonRequests []struct {
		SeasonNumber int `json:"seasonNumber"`
	} `json:"seasonRequests"`
}

type ombiShow struct {
	Title           string      `json:"title"`
	TVDBID          flexInt     `json:"tvDbId"`
	TheMovieDBID    flexInt     `json:"theMovieDbId"`
	ReleaseYear     string      `json:"releaseYear"`
	RequestedUser   ombiUser    `json:"requestedUser"`
	Approved        bool        `json:"approved"`
	Denied          bool        `json:"denied"`
	Available       bool        `json:"available"`
	PartlyAvailable bool        `json:"partlyAvailable"`
	ChildRequests   []ombiChild `json:"childRequests"`
}

type ombiData struct {
	Movies []ombiMovie
	Shows  []ombiShow
}

func fetchOmbi(ctx context.Context, hc *http.Client, c Conn) (ombiData, error) {
	var d ombiData
	if err := read(ctx, hc, c, call{path: "/api/v1/Request/movie", keyHeader: "ApiKey"}, &d.Movies); err != nil {
		return d, err
	}
	if err := read(ctx, hc, c, call{path: "/api/v1/Request/tvlite", keyHeader: "ApiKey"}, &d.Shows); err != nil {
		return d, fmt.Errorf("read show requests: %w", err)
	}
	return d, nil
}

// yearOf reads the leading year of "2010-07-16T00:00:00" or "2010".
func yearOf(s string) int {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil || y < 1800 {
		return 0
	}
	return y
}

func ombiStatus(approved, denied, available, partly bool) string {
	switch {
	case available:
		return RequestAvailable
	case partly:
		return RequestPartiallyAvailable
	case denied && !approved:
		return RequestDeclined
	case approved:
		return RequestApproved
	}
	return RequestPending
}

func nonEmpty(names ...string) []string {
	var out []string
	for _, n := range names {
		if n != "" && !contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func (d ombiData) entries() []requestEntry {
	var out []requestEntry
	for _, m := range d.Movies {
		out = append(out, requestEntry{
			Title: strings.TrimSpace(m.Title), Year: yearOf(m.ReleaseDate), MediaType: "movie", TMDBID: int(m.TheMovieDBID),
			Status: ombiStatus(m.Approved, m.Denied, m.Available, false), By: nonEmpty(m.RequestedUser.name()),
		})
	}
	for _, s := range d.Shows {
		approved, denied, available := s.Approved, s.Denied, s.Available
		partly := s.PartlyAvailable
		names := nonEmpty(s.RequestedUser.name())
		var seasons []int
		if n := len(s.ChildRequests); n > 0 {
			var av, dn int
			for _, c := range s.ChildRequests {
				approved = approved || c.Approved
				if c.Available {
					av++
				}
				if c.Denied {
					dn++
				}
				names = nonEmpty(append(names, c.RequestedUser.name())...)
				for _, sr := range c.SeasonRequests {
					if sr.SeasonNumber > 0 && !containsInt(seasons, sr.SeasonNumber) {
						seasons = append(seasons, sr.SeasonNumber)
					}
				}
			}
			available = available || av == n
			partly = partly || (av > 0 && av < n)
			denied = denied || dn == n
		}
		out = append(out, requestEntry{
			Title: strings.TrimSpace(s.Title), Year: yearOf(s.ReleaseYear), MediaType: "tv", TMDBID: int(s.TheMovieDBID), TVDBID: int(s.TVDBID),
			Status: ombiStatus(approved, denied, available, partly), By: names, Seasons: seasons,
		})
	}
	return out
}
