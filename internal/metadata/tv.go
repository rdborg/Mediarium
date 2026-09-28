package metadata

import (
	"context"
	"fmt"
	"net/url"
	"sync"
)

// Show is the subset of TMDB's TV-show fields the rest of the app needs
// (search results and trending/popular lists share this shape).
type Show struct {
	TMDBID       int     `json:"id"`
	Name         string  `json:"name"`
	Overview     string  `json:"overview"`
	FirstAirDate string  `json:"first_air_date"`
	PosterPath   string  `json:"poster_path"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	GenreIDs     []int   `json:"genre_ids"`
	Genres       []Genre `json:"genres"` // only on the single-show endpoint
}

// Year extracts the 4-digit first-air year, or 0 if unknown.
func (s Show) Year() int {
	if len(s.FirstAirDate) < 4 {
		return 0
	}
	var y int
	if _, err := fmt.Sscanf(s.FirstAirDate[:4], "%d", &y); err != nil {
		return 0
	}
	return y
}

// SeasonSummary is one entry of a show's season list (GET /tv/{id}).
type SeasonSummary struct {
	SeasonNumber int `json:"season_number"`
	EpisodeCount int `json:"episode_count"`
}

// ShowDetail is a Show plus its season list, from GET /tv/{id}.
type ShowDetail struct {
	Show
	Seasons []SeasonSummary `json:"seasons"`
}

// EpisodeInfo is one episode from GET /tv/{id}/season/{n}.
type EpisodeInfo struct {
	Season   int    `json:"season_number"`
	Episode  int    `json:"episode_number"`
	Name     string `json:"name"`
	Overview string `json:"overview"`
	AirDate  string `json:"air_date"`
}

type pagedShows struct {
	Results []Show `json:"results"`
}

// SearchTV queries TMDB's TV search endpoint.
func (c *Client) SearchTV(ctx context.Context, query string) ([]Show, error) {
	return c.getShowList(ctx, "/search/tv", url.Values{"query": {query}})
}

// TrendingTV / PopularTV mirror the movie equivalents for Discover.
func (c *Client) TrendingTV(ctx context.Context) ([]Show, error) {
	return c.getShowList(ctx, "/trending/tv/week", nil)
}

func (c *Client) PopularTV(ctx context.Context) ([]Show, error) {
	return c.getShowList(ctx, "/tv/popular", nil)
}

func (c *Client) getShowList(ctx context.Context, path string, extra url.Values) ([]Show, error) {
	var page pagedShows
	if err := c.get(ctx, path, extra, &page); err != nil {
		return nil, err
	}
	return page.Results, nil
}

// GetShow fetches a single show's details and season list.
func (c *Client) GetShow(ctx context.Context, tmdbID int) (*ShowDetail, error) {
	var d ShowDetail
	if err := c.get(ctx, fmt.Sprintf("/tv/%d", tmdbID), nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// GetSeason fetches every episode of one season.
func (c *Client) GetSeason(ctx context.Context, tmdbID, season int) ([]EpisodeInfo, error) {
	var out struct {
		Episodes []EpisodeInfo `json:"episodes"`
	}
	if err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", tmdbID, season), nil, &out); err != nil {
		return nil, err
	}
	// The season endpoint's per-episode season_number is reliable, but fill
	// it from the request when a fixture/edge case omits it so callers can
	// always trust EpisodeInfo.Season.
	for i := range out.Episodes {
		if out.Episodes[i].Season == 0 {
			out.Episodes[i].Season = season
		}
	}
	return out.Episodes, nil
}

// maxSeasonFetchConcurrency bounds parallel season requests — a long-running
// show has dozens of seasons, but that's no reason to open dozens of
// simultaneous connections to TMDB.
const maxSeasonFetchConcurrency = 5

// GetShowEpisodes fetches a show's details plus every episode of every real
// season. Season 0 ("Specials") is skipped: specials are rarely posted in a
// searchable form and would show up as permanently "missing" noise in every
// library — same default Sonarr uses (specials unmonitored).
func (c *Client) GetShowEpisodes(ctx context.Context, tmdbID int) (*ShowDetail, []EpisodeInfo, error) {
	detail, err := c.GetShow(ctx, tmdbID)
	if err != nil {
		return nil, nil, err
	}

	var seasons []int
	for _, s := range detail.Seasons {
		if s.SeasonNumber > 0 {
			seasons = append(seasons, s.SeasonNumber)
		}
	}

	results := make([][]EpisodeInfo, len(seasons))
	errs := make([]error, len(seasons))
	sem := make(chan struct{}, maxSeasonFetchConcurrency)
	var wg sync.WaitGroup
	for i, n := range seasons {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], errs[i] = c.GetSeason(ctx, tmdbID, n)
		}()
	}
	wg.Wait()

	var all []EpisodeInfo
	for i := range seasons {
		if errs[i] != nil {
			return nil, nil, fmt.Errorf("fetch season %d: %w", seasons[i], errs[i])
		}
		all = append(all, results[i]...)
	}
	return detail, all, nil
}
