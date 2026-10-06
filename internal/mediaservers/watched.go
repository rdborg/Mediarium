package mediaservers

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// What has been watched: play counts and last-played dates for movies and
// episodes, read from Plex (for the account its token belongs to) or from
// Jellyfin and Emby (all their users together). Titles are matched to the
// library by TMDB id; episodes by their show's TMDB id, season and number.

// Play is one title's play state.
type Play struct {
	Plays      int
	LastPlayed time.Time
}

// EpisodeKey names an episode by its show's TMDB id.
type EpisodeKey struct {
	ShowTMDB        int
	Season, Episode int
}

// WatchState is everything a server says has been played at least once.
type WatchState struct {
	Movies   map[int]Play // by TMDB id
	Episodes map[EpisodeKey]Play
}

func newWatchState() WatchState {
	return WatchState{Movies: map[int]Play{}, Episodes: map[EpisodeKey]Play{}}
}

// add merges one play into a map: plays add up, the latest date wins.
func merge(cur Play, p Play) Play {
	cur.Plays += p.Plays
	if p.LastPlayed.After(cur.LastPlayed) {
		cur.LastPlayed = p.LastPlayed
	}
	return cur
}

// Watched reads what has been played on a Plex, Jellyfin or Emby server.
// Book servers have nothing to say and answer an empty state.
func (c *Client) Watched(ctx context.Context, s Server) (WatchState, error) {
	switch s.Kind {
	case KindPlex:
		return c.plexWatched(ctx, s)
	case KindJellyfin, KindEmby:
		return c.embyWatched(ctx, s)
	}
	return newWatchState(), nil
}

func tmdbOf(it plexEntry) (int, bool) {
	ids := []string{it.GUID}
	for _, g := range it.Guids {
		ids = append(ids, g.ID)
	}
	for _, g := range ids {
		if id, ok := plexTMDBID(g); ok {
			return id, true
		}
	}
	return 0, false
}

func (c *Client) plexWatched(ctx context.Context, s Server) (WatchState, error) {
	out := newWatchState()
	libs, err := c.plexSections(ctx, s)
	if err != nil {
		return out, err
	}
	for _, l := range libs {
		section := "/library/sections/" + url.PathEscape(l.ID) + "/all"
		switch l.Type {
		case plexSectionType(MediaMovie):
			all, err := c.plexGet(ctx, s, section, url.Values{"includeGuids": {"1"}})
			if err != nil {
				return out, explain(s, err)
			}
			for _, it := range all.items() {
				if it.ViewCount <= 0 {
					continue
				}
				if id, ok := tmdbOf(it); ok {
					out.Movies[id] = merge(out.Movies[id], Play{Plays: it.ViewCount, LastPlayed: time.Unix(it.LastViewedAt, 0).UTC()})
				}
			}
		case plexSectionType(MediaTV):
			shows, err := c.plexGet(ctx, s, section, url.Values{"includeGuids": {"1"}})
			if err != nil {
				return out, explain(s, err)
			}
			showTMDB := map[string]int{}
			for _, sh := range shows.items() {
				if id, ok := tmdbOf(sh); ok {
					showTMDB[sh.RatingKey] = id
				}
			}
			eps, err := c.plexGet(ctx, s, section, url.Values{"type": {"4"}})
			if err != nil {
				return out, explain(s, err)
			}
			for _, e := range eps.items() {
				show, ok := showTMDB[e.GrandparentRatingKey]
				if !ok || e.ViewCount <= 0 {
					continue
				}
				k := EpisodeKey{ShowTMDB: show, Season: e.ParentIndex, Episode: e.Index}
				out.Episodes[k] = merge(out.Episodes[k], Play{Plays: e.ViewCount, LastPlayed: time.Unix(e.LastViewedAt, 0).UTC()})
			}
		}
	}
	return out, nil
}

type embyUser struct {
	ID     string `json:"Id"`
	Policy struct {
		IsDisabled bool `json:"IsDisabled"`
	} `json:"Policy"`
}

type embyPlayed struct {
	Items []struct {
		ID          string            `json:"Id"`
		Type        string            `json:"Type"`
		SeriesID    string            `json:"SeriesId"`
		Season      int               `json:"ParentIndexNumber"`
		Episode     int               `json:"IndexNumber"`
		ProviderIDs map[string]string `json:"ProviderIds"`
		UserData    struct {
			PlayCount      int    `json:"PlayCount"`
			Played         bool   `json:"Played"`
			LastPlayedDate string `json:"LastPlayedDate"`
		} `json:"UserData"`
	} `json:"Items"`
}

// maxWatchUsers caps how many Jellyfin or Emby users are read.
const maxWatchUsers = 25

func (c *Client) embyWatched(ctx context.Context, s Server) (WatchState, error) {
	out := newWatchState()
	var users []embyUser
	if err := c.do(ctx, s, http.MethodGet, "/Users", nil, nil, &users); err != nil {
		return out, explain(s, err)
	}
	showTMDB := map[string]int{}
	for i, u := range users {
		if i >= maxWatchUsers || u.ID == "" || u.Policy.IsDisabled {
			continue
		}
		base := "/Users/" + url.PathEscape(u.ID) + "/Items"
		for _, kind := range []string{"Movie", "Episode"} {
			var res embyPlayed
			q := url.Values{
				"Recursive":        {"true"},
				"IncludeItemTypes": {kind},
				"Filters":          {"IsPlayed"},
				"Fields":           {"ProviderIds"},
				"EnableImages":     {"false"},
				"EnableUserData":   {"true"},
			}
			if err := c.do(ctx, s, http.MethodGet, base, q, nil, &res); err != nil {
				return out, explain(s, err)
			}
			for _, it := range res.Items {
				if !it.UserData.Played && it.UserData.PlayCount == 0 {
					continue
				}
				p := Play{Plays: max(it.UserData.PlayCount, 1)}
				if t, err := time.Parse(time.RFC3339Nano, it.UserData.LastPlayedDate); err == nil {
					p.LastPlayed = t.UTC()
				}
				if kind == "Movie" {
					if id, err := strconv.Atoi(providerID(it.ProviderIDs, "Tmdb")); err == nil && id > 0 {
						out.Movies[id] = merge(out.Movies[id], p)
					}
					continue
				}
				if it.SeriesID == "" {
					continue
				}
				show, known := showTMDB[it.SeriesID]
				if !known {
					show = c.embySeriesTMDB(ctx, s, u.ID, it.SeriesID)
					showTMDB[it.SeriesID] = show
				}
				if show > 0 {
					k := EpisodeKey{ShowTMDB: show, Season: it.Season, Episode: it.Episode}
					out.Episodes[k] = merge(out.Episodes[k], p)
				}
			}
		}
	}
	return out, nil
}

// embySeriesTMDB looks up a show's TMDB id (0 when it has none).
func (c *Client) embySeriesTMDB(ctx context.Context, s Server, userID, seriesID string) int {
	var it struct {
		ProviderIDs map[string]string `json:"ProviderIds"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/Users/"+url.PathEscape(userID)+"/Items/"+url.PathEscape(seriesID), url.Values{"Fields": {"ProviderIds"}}, nil, &it); err != nil {
		return 0
	}
	id, _ := strconv.Atoi(providerID(it.ProviderIDs, "Tmdb"))
	return id
}
