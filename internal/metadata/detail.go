package metadata

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
)

// Genre is one entry of TMDB's genre tables.
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GenreNames resolves genre ids to English names using TMDB's genre lists
// for kind ("movie" or "tv"). Each table is fetched lazily, once per process:
// a failed fetch is not cached, so a later call retries, and in the meantime
// the caller simply gets no names rather than an error (genres are garnish on
// a list, never worth failing the list for). Unknown ids are skipped.
func (c *Client) GenreNames(ctx context.Context, kind string, ids []int) []string {
	out := []string{}
	if len(ids) == 0 {
		return out
	}
	table := c.genreTable(ctx, kind)
	for _, id := range ids {
		if name, ok := table[id]; ok {
			out = append(out, name)
		}
	}
	return out
}

func (c *Client) genreTable(ctx context.Context, kind string) map[int]string {
	c.genreMu.Lock()
	defer c.genreMu.Unlock()
	if t, ok := c.genres[kind]; ok {
		return t
	}
	var resp struct {
		Genres []Genre `json:"genres"`
	}
	if err := c.get(ctx, "/genre/"+kind+"/list", url.Values{"language": {"en-US"}}, &resp); err != nil {
		return nil
	}
	t := make(map[int]string, len(resp.Genres))
	for _, g := range resp.Genres {
		t[g.ID] = g.Name
	}
	if c.genres == nil {
		c.genres = make(map[string]map[int]string)
	}
	c.genres[kind] = t
	return t
}

// itemGenres returns the display names for a movie's or show's genres,
// preferring the full genre objects (single-item endpoints) over ids (lists).
func (c *Client) itemGenres(ctx context.Context, kind string, full []Genre, ids []int) []string {
	if len(full) > 0 {
		names := make([]string, 0, len(full))
		for _, g := range full {
			names = append(names, g.Name)
		}
		return names
	}
	return c.GenreNames(ctx, kind, ids)
}

// MovieGenres is the genre names for m, whether it came from a list (ids) or
// the single-movie endpoint (objects).
func (c *Client) MovieGenres(ctx context.Context, m Movie) []string {
	return c.itemGenres(ctx, "movie", m.Genres, m.GenreIDs)
}

// ShowGenres is the TV equivalent of MovieGenres.
func (c *Client) ShowGenres(ctx context.Context, s Show) []string {
	return c.itemGenres(ctx, "tv", s.Genres, s.GenreIDs)
}

// MultiGenres resolves a combined-search hit's genres from its own media type.
func (c *Client) MultiGenres(ctx context.Context, r MultiResult) []string {
	return c.GenreNames(ctx, r.MediaType, r.GenreIDs)
}

// RoundRating rounds a TMDB vote average to one decimal place.
func RoundRating(v float64) float64 { return math.Round(v*10) / 10 }

// Video is one entry of TMDB's videos list.
type Video struct {
	Name     string `json:"name"`
	Site     string `json:"site"`
	Key      string `json:"key"`
	Type     string `json:"type"`
	Official bool   `json:"official"`
}

// Trailer is a YouTube video with a ready-to-open URL.
type Trailer struct {
	Name     string `json:"name"`
	Site     string `json:"site"`
	Key      string `json:"key"`
	URL      string `json:"url"`
	Type     string `json:"type"`
	Official bool   `json:"official"`
}

// CastMember is one billed actor.
type CastMember struct {
	Name        string `json:"name"`
	Character   string `json:"character"`
	ProfilePath string `json:"profile_path"`
	Order       int    `json:"order"`
}

type videosBlock struct {
	Results []Video `json:"results"`
}

type creditsBlock struct {
	Cast []CastMember `json:"cast"`
}

// ProfileURL builds an actor photo URL (small size, these are thumbnails).
func ProfileURL(profilePath string) string {
	if profilePath == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w185" + profilePath
}

// youtubeTrailers keeps only YouTube videos (the only site with a URL scheme
// we can build), officially-labelled trailers first, then other trailers, then
// official teasers/clips, then the rest; original order is kept within a tier.
func youtubeTrailers(videos []Video) []Trailer {
	tier := func(v Video) int {
		switch {
		case v.Type == "Trailer" && v.Official:
			return 0
		case v.Type == "Trailer":
			return 1
		case v.Official:
			return 2
		default:
			return 3
		}
	}
	var kept []Video
	for _, v := range videos {
		if v.Site == "YouTube" && v.Key != "" {
			kept = append(kept, v)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return tier(kept[i]) < tier(kept[j]) })
	out := make([]Trailer, 0, len(kept))
	for _, v := range kept {
		out = append(out, Trailer{
			Name: v.Name, Site: v.Site, Key: v.Key, Type: v.Type, Official: v.Official,
			URL: "https://www.youtube.com/watch?v=" + v.Key,
		})
	}
	return out
}

// topCast returns up to n cast members in billing order.
func topCast(cast []CastMember, n int) []CastMember {
	sorted := append([]CastMember(nil), cast...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Order < sorted[j].Order })
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

// MovieDetail is GET /movie/{id} with videos, release dates and credits
// appended in the same request.
type MovieDetail struct {
	Movie
	Runtime          int          `json:"runtime"`
	Tagline          string       `json:"tagline"`
	Status           string       `json:"status"`
	OriginalLanguage string       `json:"original_language"`
	Homepage         string       `json:"homepage"`
	IMDBID           string       `json:"imdb_id"`
	Videos           videosBlock  `json:"videos"`
	Credits          creditsBlock `json:"credits"`
	ReleaseDates     struct {
		Results []struct {
			Country      string `json:"iso_3166_1"`
			ReleaseDates []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

// GetMovieDetail fetches everything the movie detail page shows in one call.
func (c *Client) GetMovieDetail(ctx context.Context, tmdbID int) (*MovieDetail, error) {
	var d MovieDetail
	q := url.Values{"append_to_response": {"videos,release_dates,credits"}}
	if err := c.get(ctx, fmt.Sprintf("/movie/%d", tmdbID), q, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Certification is the movie's age rating: the US one when there is one,
// otherwise the first non-empty certification any country lists.
func (d *MovieDetail) Certification() string {
	first := ""
	for _, country := range d.ReleaseDates.Results {
		for _, rd := range country.ReleaseDates {
			if rd.Certification == "" {
				continue
			}
			if country.Country == "US" {
				return rd.Certification
			}
			if first == "" {
				first = rd.Certification
			}
		}
	}
	return first
}

// Trailers are the movie's YouTube videos, best trailer first.
func (d *MovieDetail) Trailers() []Trailer { return youtubeTrailers(d.Videos.Results) }

// Cast is the top n billed actors.
func (d *MovieDetail) Cast(n int) []CastMember { return topCast(d.Credits.Cast, n) }

// ShowFull is GET /tv/{id} with videos, content ratings and credits appended.
type ShowFull struct {
	ShowDetail
	Tagline          string `json:"tagline"`
	Status           string `json:"status"`
	OriginalLanguage string `json:"original_language"`
	Homepage         string `json:"homepage"`
	NumberOfSeasons  int    `json:"number_of_seasons"`
	NumberOfEpisodes int    `json:"number_of_episodes"`
	Networks         []struct {
		Name string `json:"name"`
	} `json:"networks"`
	Videos         videosBlock  `json:"videos"`
	Credits        creditsBlock `json:"credits"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
}

// GetShowFull fetches everything the TV detail page shows in one call.
func (c *Client) GetShowFull(ctx context.Context, tmdbID int) (*ShowFull, error) {
	var d ShowFull
	q := url.Values{"append_to_response": {"videos,content_ratings,credits"}}
	if err := c.get(ctx, fmt.Sprintf("/tv/%d", tmdbID), q, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ContentRating is the show's age rating, preferring the US one.
func (d *ShowFull) ContentRating() string {
	first := ""
	for _, r := range d.ContentRatings.Results {
		if r.Rating == "" {
			continue
		}
		if r.Country == "US" {
			return r.Rating
		}
		if first == "" {
			first = r.Rating
		}
	}
	return first
}

// SeasonCount is the number of real seasons (specials excluded), falling back
// to counting the season list when TMDB omits the total.
func (d *ShowFull) SeasonCount() int {
	if d.NumberOfSeasons > 0 {
		return d.NumberOfSeasons
	}
	n := 0
	for _, s := range d.Seasons {
		if s.SeasonNumber > 0 {
			n++
		}
	}
	return n
}

// NetworkNames lists the broadcasting networks.
func (d *ShowFull) NetworkNames() []string {
	out := []string{}
	for _, n := range d.Networks {
		if n.Name != "" {
			out = append(out, n.Name)
		}
	}
	return out
}

// Trailers are the show's YouTube videos, best trailer first.
func (d *ShowFull) Trailers() []Trailer { return youtubeTrailers(d.Videos.Results) }

// Cast is the top n billed actors.
func (d *ShowFull) Cast(n int) []CastMember { return topCast(d.Credits.Cast, n) }
